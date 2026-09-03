package sftpx

// Moving the bytes.
//
// Three things decide how fast SFTP goes, and only the third is obvious:
//
//  1. How many requests are in flight. SFTP is request/response, so a
//     sequential transfer costs one round trip per packet — on a 50 ms link
//     that is about 640 KB/s no matter how much bandwidth there is. Everything
//     here is built to keep many requests outstanding.
//
//  2. How many TCP connections carry them. One SSH connection is one TCP
//     stream with one congestion window; on a long or lossy path a single
//     stream leaves most of the line idle. Large files are split across
//     several connections and written to their own byte ranges.
//
//  3. The cipher. See Dial — on anything with AES-NI, AES-GCM is severalfold
//     faster than ChaCha20, and Go's default order does not prefer it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Multipart bounds.
//
// Variables rather than constants so the throughput tests can lower them: a
// test that has to move 40 MB to cross the threshold takes a minute, and the
// thing being measured does not depend on the size.
var (
	// Below this a file is one stream. Splitting a small file costs an open
	// and a close per part for no gain, and most of a plugins folder is small
	// files where the win is transferring several at once instead.
	multipartMin int64 = 8 << 20 // 8 MB

	// No part smaller than this, however many connections are free: parts that
	// finish in a few round trips are all overhead.
	multipartPartMin int64 = 4 << 20 // 4 MB

	// A ceiling on parts for one file, so a 40 GB world does not open forty
	// handles.
	multipartMaxParts = 8
)

// countingReader adds to a total as it is read.
//
// Size is deliberately exposed. *sftp.File.ReadFrom only takes its concurrent
// path when it can work out how much is left, and it looks for Len, Size,
// *io.LimitedReader or Stat — a wrapper without one of those silently drops the
// transfer back to sequential writes.
type countingReader struct {
	r     io.Reader
	size  int64
	file  *int64
	total *int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		atomic.AddInt64(c.file, int64(n))
		atomic.AddInt64(c.total, int64(n))
	}
	return n, err
}

// Size is what ReadFrom looks for.
func (c *countingReader) Size() int64 { return c.size }

// countingWriter is the same for the download direction.
type countingWriter struct {
	w     io.Writer
	file  *int64
	total *int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 {
		atomic.AddInt64(c.file, int64(n))
		atomic.AddInt64(c.total, int64(n))
	}
	return n, err
}

/* ------------------------------------------------------------ one file up */

// sendFile writes one local file to the far side.
//
// Over multipartMin it is split across connections; under it, one connection
// with many requests in flight.
func (s *Session) sendFile(ctx context.Context, job Job, running *int64) (int64, error) {
	src, err := os.Open(job.Local)
	if err != nil {
		return 0, err
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return 0, err
	}
	size := info.Size()

	// Written beside the target and moved into place, so an interrupted
	// transfer never leaves something that looks complete.
	temp := job.Remote + ".uploading"

	var moved int64
	if size >= multipartMin && len(s.conns) > 1 {
		err = s.sendParts(ctx, src, temp, size, &moved, running)
	} else {
		err = s.sendWhole(ctx, src, temp, size, &moved, running)
	}
	if err != nil {
		s.removeQuietly(temp)
		return moved, err
	}

	if err := s.replace(ctx, temp, job.Remote); err != nil {
		s.removeQuietly(temp)
		return moved, err
	}
	return moved, nil
}

// sendWhole is the single-connection path: one handle, many requests in flight.
func (s *Session) sendWhole(ctx context.Context, src *os.File, remote string, size int64, moved, running *int64) error {
	c, err := s.borrow(ctx)
	if err != nil {
		return err
	}
	defer s.give(c)

	dst, err := c.sftp.Create(remote)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", remote, err)
	}

	// ReadFrom, not io.Copy into a wrapped writer: it is the concurrent write
	// path, and it only runs when it is handed the destination directly.
	_, copyErr := dst.ReadFromWithConcurrency(
		&countingReader{r: src, size: size, file: moved, total: running},
		requestsPerFile,
	)

	// Closed explicitly: the write is finished only when the far side says so,
	// and a close error is a failed transfer.
	closeErr := dst.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return fmt.Errorf("cannot finish %s: %w", remote, closeErr)
	}
	return nil
}

// sendParts splits one file across connections, each writing its own range.
//
// This is what a single large file needs: separate connections mean separate
// TCP streams, and one stream cannot fill a long path on its own however many
// requests it pipelines.
func (s *Session) sendParts(ctx context.Context, src *os.File, remote string, size int64, moved, running *int64) error {
	parts := s.partCount(size)

	// The file is created and sized once, before any worker writes into it.
	first, err := s.borrow(ctx)
	if err != nil {
		return err
	}
	handle, err := first.sftp.Create(remote)
	if err != nil {
		s.give(first)
		return fmt.Errorf("cannot write %s: %w", remote, err)
	}
	_ = handle.Truncate(size)
	closeErr := handle.Close()
	s.give(first)
	if closeErr != nil {
		return fmt.Errorf("cannot create %s: %w", remote, closeErr)
	}

	each := size / int64(parts)
	var wg sync.WaitGroup
	errs := make([]error, parts)

	for i := 0; i < parts; i++ {
		start := int64(i) * each
		end := start + each
		if i == parts-1 {
			end = size // the last part carries the remainder
		}

		wg.Add(1)
		go func(index int, from, to int64) {
			defer wg.Done()
			errs[index] = s.sendRange(ctx, src.Name(), remote, from, to, moved, running)
		}(i, start, end)
	}
	wg.Wait()

	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// sendRange writes [from, to) of the local file into the remote one.
//
// Its own file handles on both sides: a shared *os.File has one offset, and
// sharing a remote handle across goroutines would serialise them again.
func (s *Session) sendRange(ctx context.Context, local, remote string, from, to int64, moved, running *int64) error {
	c, err := s.borrow(ctx)
	if err != nil {
		return err
	}
	defer s.give(c)

	src, err := os.Open(local)
	if err != nil {
		return err
	}
	defer src.Close()

	if _, err := src.Seek(from, io.SeekStart); err != nil {
		return err
	}

	dst, err := c.sftp.OpenFile(remote, os.O_WRONLY)
	if err != nil {
		return fmt.Errorf("cannot open %s for part %d: %w", remote, from, err)
	}
	defer dst.Close()

	if _, err := dst.Seek(from, io.SeekStart); err != nil {
		return err
	}

	// Bounded to this part, and *io.LimitedReader is one of the shapes
	// ReadFrom recognises for its concurrent path.
	limited := io.LimitReader(src, to-from)
	_, copyErr := dst.ReadFromWithConcurrency(
		&countingReader{r: limited, size: to - from, file: moved, total: running},
		requestsPerFile,
	)
	if copyErr != nil {
		return copyErr
	}
	return dst.Close()
}

/* ---------------------------------------------------------- one file down */

// fetchFile pulls one remote file to disk.
func (s *Session) fetchFile(ctx context.Context, job Job, running *int64) (int64, error) {
	size := job.Size
	if size <= 0 {
		if entry, ok, err := s.Stat(ctx, job.Remote); err == nil && ok {
			size = entry.Size
		}
	}

	temp := job.Local + ".part"
	var moved int64
	var err error

	if size >= multipartMin && len(s.conns) > 1 {
		err = s.fetchParts(ctx, job.Remote, temp, size, &moved, running)
	} else {
		err = s.fetchWhole(ctx, job.Remote, temp, &moved, running)
	}
	if err != nil {
		os.Remove(temp)
		return moved, err
	}

	if err := os.Rename(temp, job.Local); err != nil {
		// Windows will not always rename onto an existing file.
		_ = os.Remove(job.Local)
		if err2 := os.Rename(temp, job.Local); err2 != nil {
			os.Remove(temp)
			return moved, err2
		}
	}
	return moved, nil
}

func (s *Session) fetchWhole(ctx context.Context, remote, temp string, moved, running *int64) error {
	c, err := s.borrow(ctx)
	if err != nil {
		return err
	}
	defer s.give(c)

	src, err := c.sftp.Open(remote)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", remote, err)
	}
	defer src.Close()

	dst, err := os.Create(temp)
	if err != nil {
		return err
	}

	// WriteTo is the concurrent read path, and it takes the destination
	// directly rather than through io.Copy.
	_, copyErr := src.WriteTo(&countingWriter{w: dst, file: moved, total: running})
	closeErr := dst.Close()

	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// fetchParts pulls one file down several connections at once.
func (s *Session) fetchParts(ctx context.Context, remote, temp string, size int64, moved, running *int64) error {
	parts := s.partCount(size)

	// Sized up front so every worker can write straight to its own offset.
	prep, err := os.Create(temp)
	if err != nil {
		return err
	}
	if err := prep.Truncate(size); err != nil {
		prep.Close()
		return err
	}
	if err := prep.Close(); err != nil {
		return err
	}

	each := size / int64(parts)
	var wg sync.WaitGroup
	errs := make([]error, parts)

	for i := 0; i < parts; i++ {
		start := int64(i) * each
		end := start + each
		if i == parts-1 {
			end = size
		}

		wg.Add(1)
		go func(index int, from, to int64) {
			defer wg.Done()
			errs[index] = s.fetchRange(ctx, remote, temp, from, to, moved, running)
		}(i, start, end)
	}
	wg.Wait()

	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// fetchRange reads [from, to) of the remote file into the local one.
func (s *Session) fetchRange(ctx context.Context, remote, temp string, from, to int64, moved, running *int64) error {
	c, err := s.borrow(ctx)
	if err != nil {
		return err
	}
	defer s.give(c)

	src, err := c.sftp.Open(remote)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", remote, err)
	}
	defer src.Close()

	if _, err := src.Seek(from, io.SeekStart); err != nil {
		return err
	}

	dst, err := os.OpenFile(temp, os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := dst.Seek(from, io.SeekStart); err != nil {
		return err
	}

	// Bounded to this part; WriteTo would otherwise read to the end of file.
	limited := io.LimitReader(src, to-from)
	buf := make([]byte, 1<<20)
	_, copyErr := io.CopyBuffer(&countingWriter{w: dst, file: moved, total: running}, limited, buf)
	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		return copyErr
	}
	return dst.Close()
}

/* ------------------------------------------------------------- shared bits */

// partCount decides how many ways to split a file.
func (s *Session) partCount(size int64) int {
	parts := len(s.conns)
	if parts > multipartMaxParts {
		parts = multipartMaxParts
	}
	// Never so many that the parts stop being worth the handles.
	for parts > 1 && size/int64(parts) < multipartPartMin {
		parts--
	}
	if parts < 1 {
		parts = 1
	}
	return parts
}

// replace moves the temporary file over the real one.
func (s *Session) replace(ctx context.Context, from, to string) error {
	c, err := s.borrow(ctx)
	if err != nil {
		return err
	}
	defer s.give(c)

	// Most servers will not rename onto an existing name, so the old one goes
	// first. It has already been captured by the caller when that was asked
	// for, and the new content is completely written by this point.
	if _, statErr := c.sftp.Stat(to); statErr == nil {
		_ = c.sftp.Remove(to)
	}
	if err := c.sftp.Rename(from, to); err != nil {
		return fmt.Errorf("cannot put %s in place: %w", to, err)
	}
	return nil
}

// removeQuietly drops a temporary file after a failure. It is best effort by
// design: the transfer has already failed, and a cleanup error on top of that
// tells nobody anything useful.
func (s *Session) removeQuietly(remote string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	c, err := s.borrow(ctx)
	if err != nil {
		return
	}
	defer s.give(c)
	_ = c.sftp.Remove(remote)
}
