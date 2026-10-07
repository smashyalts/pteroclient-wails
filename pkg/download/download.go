// Package download fetches large files from a Pterodactyl node as quickly as
// the node will allow, and keeps trying.
//
// Three facts about a stock panel shape everything here.
//
// The panel hands out a signed URL that points at the node, and wings treats
// that token as single use: the second request with the same token is a 404.
// So a URL cannot be shared between connections and cannot be reused for a
// retry. Every attempt mints a fresh one, which is why a Mint function is
// required rather than a URL.
//
// Stock wings serves a backup by writing the whole file to the response, with
// no Accept-Ranges and no handling of a Range header. Ask it for the first
// megabyte and it sends the entire archive with a 200. Splitting a download
// into parts against such a host would fetch the whole file once per part, so
// range support is probed rather than assumed, and the probe is built so that
// a host without it loses nothing: the first request asks for exactly the
// first part, and a 200 means the body already arriving is the whole download.
//
// Nothing can resume a non-ranged transfer. A stream that dies at 4 GB of 5
// starts again from zero, because there is no way to ask the node for the
// rest. Where ranges do work — modified wings, or a reverse proxy in front of
// it — the parts that finished are recorded and a later run skips them.
package download

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"hash"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Mint returns a fresh download URL.
//
// It is called once per attempt and once per part, because the panel's signed
// URLs are single use. Implementations are expected to be the panel API call
// that issues one, and to respect the context.
type Mint func(ctx context.Context) (string, error)

// Job is one file to fetch.
type Job struct {
	// Name is for display and for the manifest. A backup's UUID or filename.
	Name string

	// Dest is the final path. The transfer runs in Dest+".ptpart" and is
	// renamed into place only after the size and checksum check out, so a
	// half-written archive never appears under the real name.
	Dest string

	Mint Mint

	// Size is what the panel said the file is, or 0 when unknown. A mismatch
	// fails the job: a truncated archive that looks complete is worse than an
	// error, because it is discovered when it is needed.
	Size int64

	// Checksum and ChecksumType are the panel's own, from the backup record.
	// Accepted forms are a bare hex digest with the type alongside, or
	// "sha1:<hex>" in Checksum with ChecksumType empty. Empty means no check.
	Checksum     string
	ChecksumType string

	// Reusable says one minted URL may serve more than one request.
	//
	// The two backup drivers differ here and it decides how much of the panel
	// API a transfer costs. An S3 backup is a presigned GetObject URL: any
	// number of connections may use it until it expires, so eight parallel
	// parts cost one mint. A wings backup is a node JWT that wings rejects on
	// its second use, so every request needs its own.
	Reusable bool

	// URLLifetime is how long a minted URL stays good. The panel signs an S3
	// link for five minutes and a wings token for fifteen. A long transfer
	// outlives both, so a reusable URL is re-minted before it lapses rather
	// than after a request has already failed.
	URLLifetime time.Duration

	// Classify inspects the first minted URL and corrects Reusable and
	// URLLifetime from what it finds.
	//
	// It exists because the client API does not say where a backup is kept.
	// The backup record a client key can read has no disk field, so whether
	// the archive is on the node or in a bucket is not knowable in advance —
	// but the signed URL gives it away the moment one is minted, and that
	// costs nothing extra because the URL was needed anyway.
	Classify func(url string) (reusable bool, lifetime time.Duration)
}

// Options tunes a transfer.
type Options struct {
	// Parts is how many ranged connections to use when the host supports
	// ranges. It has no effect on a stock node, which gets one stream however
	// high this is set.
	Parts int

	// MinPartSize keeps a small file on one connection. Splitting a 4 MB
	// archive eight ways costs more in requests and minted tokens than it
	// saves.
	MinPartSize int64

	// MaxAttempts bounds the retries per part, or for the whole stream when
	// the host has no ranges. Zero means the default; negative means keep
	// trying until the context is done, which is what an unattended queue
	// wants.
	MaxAttempts int

	BaseBackoff time.Duration
	MaxBackoff  time.Duration

	// BufferSize is the copy buffer. Large enough that a fast node is not
	// held up by syscall overhead.
	BufferSize int

	// Client is used for the node transfers. It wants no overall timeout:
	// a multi-gigabyte archive legitimately takes an hour, and a deadline on
	// the whole request would kill it mid-transfer. Stalls are caught by the
	// per-read idle timeout instead.
	Client *http.Client

	// IdleTimeout fails an attempt that has gone quiet, so a wedged
	// connection is retried rather than held open forever.
	IdleTimeout time.Duration

	Progress      func(Progress)
	ProgressEvery time.Duration
}

func (o *Options) applyDefaults() {
	if o.Parts <= 0 {
		o.Parts = 4
	}
	if o.Parts > 32 {
		o.Parts = 32
	}
	if o.MinPartSize <= 0 {
		o.MinPartSize = 8 << 20 // 8 MiB
	}
	if o.MaxAttempts == 0 {
		o.MaxAttempts = 10
	}
	if o.BaseBackoff <= 0 {
		o.BaseBackoff = time.Second
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = 30 * time.Second
	}
	if o.BufferSize <= 0 {
		o.BufferSize = 1 << 20 // 1 MiB
	}
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = 2 * time.Minute
	}
	if o.ProgressEvery <= 0 {
		o.ProgressEvery = 500 * time.Millisecond
	}
	if o.Client == nil {
		o.Client = DefaultClient()
	}
}

// SocketBuffer is the TCP receive buffer asked for on each connection.
//
// A single stream can only have as many bytes in flight as the receive window
// allows, so on a long fat link the default buffer, not the bandwidth, is what
// caps throughput. Pulling a backup from a node several thousand miles away is
// exactly that case: at 100 ms round trip a 256 KiB window tops out near
// 20 Mbit/s no matter how much capacity either end has. The kernel may clamp
// this to its own maximum, which is fine; asking costs nothing.
const SocketBuffer = 8 << 20 // 8 MiB

// DefaultClient is an HTTP client shaped for one large transfer.
//
// No overall timeout, because a multi-gigabyte archive legitimately takes an
// hour and a deadline on the whole request would kill it mid-transfer; stalls
// are caught by the idle timeout instead.
//
// HTTP/2 is deliberately off. It multiplexes streams over one connection and
// polices them with its own flow-control window on top of TCP's, and for a
// single bulk download that window is a second ceiling to run into for no
// benefit — there is nothing to multiplex with. HTTP/1.1 leaves the pacing to
// the kernel, which is what the socket buffer above is for. Pass your own
// Client if a particular host disagrees.
func DefaultClient() *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 64
	transport.MaxIdleConnsPerHost = 32
	transport.MaxConnsPerHost = 0
	transport.IdleConnTimeout = 90 * time.Second
	// Compression is pointless on an already-compressed archive and costs a
	// decompression pass on the way in.
	transport.DisableCompression = true

	// A non-nil empty map is how a Transport is told not to negotiate h2.
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}

	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			// Best effort. A kernel that refuses the size still gives a
			// working connection, which is the only thing that matters.
			_ = tcp.SetReadBuffer(SocketBuffer)
		}
		return conn, nil
	}
	return &http.Client{Transport: transport}
}

// Phase says what a transfer is doing, for a progress display.
type Phase string

const (
	PhaseProbing     Phase = "probing"
	PhaseDownloading Phase = "downloading"
	PhaseVerifying   Phase = "verifying"
	PhaseDone        Phase = "done"
)

// Progress is a snapshot of a running transfer.
type Progress struct {
	Name  string `json:"name"`
	Dest  string `json:"dest"`
	Phase Phase  `json:"phase"`

	Total int64 `json:"total"`
	Done  int64 `json:"done"`

	BytesPerSecond float64 `json:"bytes_per_second"`
	ETASeconds     float64 `json:"eta_seconds"`

	// Ranged says whether the host let us split the file. False is the normal
	// case against a stock node, and means Parts is 1 however it was set.
	Ranged      bool `json:"ranged"`
	Parts       int  `json:"parts"`
	ActiveParts int  `json:"active_parts"`

	// Retries counts attempts beyond the first, across all parts. A climbing
	// number on a slow link is the useful signal that the node is dropping us.
	Retries int `json:"retries"`

	Resumed bool `json:"resumed"`
}

// Result describes a finished transfer.
type Result struct {
	Name     string        `json:"name"`
	Dest     string        `json:"dest"`
	Bytes    int64         `json:"bytes"`
	Duration time.Duration `json:"duration"`
	Ranged   bool          `json:"ranged"`
	Parts    int           `json:"parts"`
	Retries  int           `json:"retries"`
	Resumed  bool          `json:"resumed"`
	Checksum string        `json:"checksum,omitempty"`
	Verified bool          `json:"verified"`
}

// Fetch downloads one job, returning when the file is in place and verified.
func Fetch(ctx context.Context, job Job, opts Options) (*Result, error) {
	if job.Dest == "" {
		return nil, errors.New("no destination given")
	}
	if job.Mint == nil {
		return nil, errors.New("no way to mint a download URL")
	}
	opts.applyDefaults()

	if err := os.MkdirAll(filepath.Dir(job.Dest), 0o755); err != nil {
		return nil, fmt.Errorf("cannot create the destination directory: %w", err)
	}

	f := &fetcher{job: job, opts: opts, started: time.Now()}
	return f.run(ctx)
}

type fetcher struct {
	job  Job
	opts Options

	file    *os.File
	man     *manifest
	started time.Time

	mu      sync.Mutex
	done    int64
	total   int64
	ranged  bool
	parts   int
	resumed bool

	active  int32
	retries int32

	// digest hashes the bytes as they arrive on the single-stream path, where
	// they arrive in order. It saves reading the whole file back off disk
	// afterwards just to check it, which on a 5 GB archive is an entire
	// extra pass. Ranged transfers cannot do this — their parts land out of
	// order — so they fall back to the re-read.
	digest     hash.Hash
	digestFrom int64

	lastReport time.Time
	rateAt     time.Time
	rateBytes  int64
	rate       float64

	urlMu     sync.Mutex
	cachedURL string
	cachedAt  time.Time
}

// url returns a usable download URL, minting one when there is nothing good
// cached.
//
// Reuse matters because minting is a call against the panel's client API,
// which is rate limited per key. Eight parts retrying a few times each would
// be dozens of calls, and the 429s would cost more than the parallelism won.
func (f *fetcher) url(ctx context.Context) (string, error) {
	if !f.job.Reusable {
		return f.job.Mint(ctx)
	}

	lifetime := f.job.URLLifetime
	if lifetime <= 0 {
		lifetime = 5 * time.Minute
	}
	// Re-mint at two thirds of the stated lifetime. A part that starts just
	// inside the window still has to finish inside it.
	stale := time.Duration(float64(lifetime) * 0.66)

	f.urlMu.Lock()
	defer f.urlMu.Unlock()

	if f.cachedURL != "" && time.Since(f.cachedAt) < stale {
		return f.cachedURL, nil
	}

	fresh, err := f.job.Mint(ctx)
	if err != nil {
		// An expiring URL still beats no URL: the request may well succeed,
		// and failing here would abandon a transfer over a rate limit.
		if f.cachedURL != "" {
			return f.cachedURL, nil
		}
		return "", err
	}
	f.cachedURL, f.cachedAt = fresh, time.Now()
	return fresh, nil
}

// expireURL forces the next request to mint a new one, for when a cached URL
// has evidently lapsed.
func (f *fetcher) expireURL() {
	f.urlMu.Lock()
	f.cachedURL = ""
	f.urlMu.Unlock()
}

func (f *fetcher) partPath() string { return f.job.Dest + ".ptpart" }

func (f *fetcher) run(ctx context.Context) (*Result, error) {
	// A finished file that already matches is not downloaded again. An
	// unattended queue is restarted far more often than it is drained.
	if ok, err := f.alreadyComplete(); err != nil {
		return nil, err
	} else if ok {
		return &Result{
			Name: f.job.Name, Dest: f.job.Dest, Bytes: f.job.Size,
			Parts: 1, Verified: f.job.Checksum != "", Resumed: true,
		}, nil
	}

	file, err := os.OpenFile(f.partPath(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", f.partPath(), err)
	}
	f.file = file
	defer file.Close()

	f.man = loadManifest(f.manifestPath(), f.job)

	f.report(PhaseProbing)

	probe, err := f.probe(ctx)
	if err != nil {
		return nil, err
	}

	if probe.ranged {
		err = f.runRanged(ctx, probe)
	} else {
		err = f.runSingle(ctx, probe)
	}
	if err != nil {
		// The part file and manifest are deliberately left behind. Where
		// ranges work a later run skips what finished; where they do not,
		// keeping the file costs a little disk and tells the operator the
		// attempt happened.
		f.man.save(f.manifestPath())
		return nil, err
	}

	f.report(PhaseVerifying)
	sum, verified, err := f.verify()
	if err != nil {
		return nil, err
	}

	if err := f.file.Sync(); err != nil {
		return nil, fmt.Errorf("cannot flush %s: %w", f.partPath(), err)
	}
	if err := f.file.Close(); err != nil {
		return nil, fmt.Errorf("cannot close %s: %w", f.partPath(), err)
	}
	if err := os.Rename(f.partPath(), f.job.Dest); err != nil {
		return nil, fmt.Errorf("cannot move the finished file into place: %w", err)
	}
	os.Remove(f.manifestPath())

	f.report(PhaseDone)

	f.mu.Lock()
	defer f.mu.Unlock()
	return &Result{
		Name:     f.job.Name,
		Dest:     f.job.Dest,
		Bytes:    f.done,
		Duration: time.Since(f.started),
		Ranged:   f.ranged,
		Parts:    f.parts,
		Retries:  int(atomic.LoadInt32(&f.retries)),
		Resumed:  f.resumed,
		Checksum: sum,
		Verified: verified,
	}, nil
}

// alreadyComplete reports whether the destination is there and matches.
func (f *fetcher) alreadyComplete() (bool, error) {
	info, err := os.Stat(f.job.Dest)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, fmt.Errorf("%s is a directory", f.job.Dest)
	}
	if f.job.Size > 0 && info.Size() != f.job.Size {
		return false, nil // a short file from an earlier run; fetch again
	}
	if f.job.Checksum == "" {
		// Nothing to check it against, and the size matched or was unknown.
		return f.job.Size > 0, nil
	}
	sum, err := hashFile(f.job.Dest, f.job.ChecksumType, f.job.Checksum)
	if err != nil {
		return false, nil
	}
	return strings.EqualFold(sum, wantedDigest(f.job.Checksum)), nil
}

// probeResult carries what the first request learned, and its body, because
// that body is either the first part or the whole file.
type probeResult struct {
	ranged bool
	total  int64
	body   io.ReadCloser
}

// probe makes the first request, asking for exactly the first part.
//
// A 206 means the host honours ranges and the body is part zero. A 200 means
// it ignored the header and the body is the whole file, so the request is the
// download rather than a wasted round trip. Either way one minted token buys
// one useful transfer, which matters because the tokens are single use and
// minting one is a rate-limited call against the panel.
func (f *fetcher) probe(ctx context.Context) (*probeResult, error) {
	url, err := f.url(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get a download URL: %w", err)
	}

	// The URL says what kind of host is on the other end, which decides
	// whether it may be shared between parts.
	if f.job.Classify != nil {
		reusable, lifetime := f.job.Classify(url)
		f.job.Reusable = reusable
		if lifetime > 0 {
			f.job.URLLifetime = lifetime
		}
		if reusable {
			// Worth caching now: it was just minted and the parts will want it.
			f.urlMu.Lock()
			f.cachedURL, f.cachedAt = url, time.Now()
			f.urlMu.Unlock()
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// Ask for the first part. If the file is smaller than a part the server
	// clamps the range, and a host that ignores ranges sends everything.
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", f.opts.MinPartSize-1))
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := f.opts.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the node: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusPartialContent:
		total, ok := totalFromContentRange(resp.Header.Get("Content-Range"))
		if !ok {
			// A 206 whose Content-Range cannot be read is not something to
			// build a part plan on. Fall back to one stream.
			resp.Body.Close()
			return f.plainGet(ctx)
		}
		f.mu.Lock()
		f.ranged, f.total = true, total
		f.mu.Unlock()
		return &probeResult{ranged: true, total: total, body: resp.Body}, nil

	case http.StatusOK:
		total := resp.ContentLength
		if total < 0 {
			total = f.job.Size
		}
		f.mu.Lock()
		f.ranged, f.total = false, total
		f.parts = 1
		f.mu.Unlock()
		return &probeResult{ranged: false, total: total, body: resp.Body}, nil

	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return nil, fmt.Errorf("the node refused the download with %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

// plainGet opens an unconditioned GET, for the retry path and for the odd
// server whose 206 cannot be parsed.
func (f *fetcher) plainGet(ctx context.Context) (*probeResult, error) {
	url, err := f.url(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get a download URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := f.opts.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the node: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return nil, fmt.Errorf("the node refused the download with %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	total := resp.ContentLength
	if total < 0 {
		total = f.job.Size
	}
	f.mu.Lock()
	f.ranged, f.total, f.parts = false, total, 1
	f.mu.Unlock()
	return &probeResult{ranged: false, total: total, body: resp.Body}, nil
}

// runSingle is the stock-node path: one stream, restarted from the beginning
// on failure because the node offers no way to ask for the rest.
func (f *fetcher) runSingle(ctx context.Context, probe *probeResult) error {
	body := probe.body

	for attempt := 1; ; attempt++ {
		if body == nil {
			fresh, err := f.plainGet(ctx)
			if err != nil {
				if !f.shouldRetry(ctx, attempt, err) {
					return err
				}
				continue
			}
			body = fresh.body
		}

		// Every attempt starts at zero, so the counter does too, or the
		// progress bar would climb past the total across retries.
		f.setDone(0)

		// Sizing the file up front lets the filesystem pick one extent
		// instead of growing it a megabyte at a time, which on a large
		// archive is the difference between a contiguous file and a badly
		// fragmented one.
		if probe.total > 0 {
			_ = f.file.Truncate(probe.total)
		}

		// Hash as the bytes go past. Restarted from scratch each attempt,
		// since the stream restarts from zero too.
		if h, err := newHash(f.job.ChecksumType, f.job.Checksum); err == nil && f.job.Checksum != "" {
			f.digest, f.digestFrom = h, 0
		}

		f.report(PhaseDownloading)

		written, err := f.copyAt(ctx, body, 0)
		body.Close()
		body = nil

		if err == nil {
			if f.total > 0 && written != f.total {
				err = fmt.Errorf("the node sent %d bytes of an expected %d", written, f.total)
			} else {
				// Truncate in case an earlier, longer attempt left a tail
				// behind this one.
				if trErr := f.file.Truncate(written); trErr != nil {
					return trErr
				}
				f.mu.Lock()
				f.total = written
				f.mu.Unlock()
				return nil
			}
		}

		if !f.shouldRetry(ctx, attempt, err) {
			return err
		}
	}
}

// runRanged splits the file across connections. The probe already delivered
// part zero, so that body is consumed in place and the rest are fetched in
// parallel.
func (f *fetcher) runRanged(ctx context.Context, probe *probeResult) error {
	total := probe.total
	plan := planParts(total, f.opts.Parts, f.opts.MinPartSize)

	f.mu.Lock()
	f.parts = len(plan)
	f.mu.Unlock()

	if err := f.file.Truncate(total); err != nil {
		return fmt.Errorf("cannot size %s: %w", f.partPath(), err)
	}

	f.man.adopt(plan, total)
	if f.man.doneBytes() > 0 {
		f.mu.Lock()
		f.resumed = true
		f.mu.Unlock()
	}
	f.setDone(f.man.doneBytes())
	f.report(PhaseDownloading)

	// Part zero is already arriving on the probe's connection. Only use it if
	// the manifest does not already have that part.
	if f.man.isDone(0) {
		probe.body.Close()
	} else {
		atomic.AddInt32(&f.active, 1)
		written, err := f.copyAt(ctx, probe.body, plan[0].Start)
		probe.body.Close()
		atomic.AddInt32(&f.active, -1)

		if err == nil && written == plan[0].Size() {
			f.man.complete(0)
			f.man.save(f.manifestPath())
		} else {
			// Hand part zero back to the worker pool rather than failing: the
			// retry path there already knows how to mint a fresh URL.
			f.man.reset(0)
			f.addDone(-written)
		}
	}

	pending := make(chan int, len(plan))
	for i := range plan {
		if !f.man.isDone(i) {
			pending <- i
		}
	}
	close(pending)

	workers := f.opts.Parts
	if workers > len(plan) {
		workers = len(plan)
	}
	if workers < 1 {
		workers = 1
	}

	groupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
	)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range pending {
				if groupCtx.Err() != nil {
					return
				}
				if err := f.fetchPart(groupCtx, plan[index], index); err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errMu.Unlock()
					cancel() // one unrecoverable part fails the job
					return
				}
			}
		}()
	}
	wg.Wait()

	f.man.save(f.manifestPath())

	errMu.Lock()
	defer errMu.Unlock()
	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if remaining := f.man.remaining(); remaining > 0 {
		return fmt.Errorf("%d part(s) did not finish", remaining)
	}
	return nil
}

// fetchPart downloads one range, retrying with a fresh URL each time.
func (f *fetcher) fetchPart(ctx context.Context, p Part, index int) error {
	for attempt := 1; ; attempt++ {
		// A part that was partly written on an earlier attempt continues from
		// where it stopped. This is the whole reason ranges are worth
		// probing for.
		offset := f.man.progress(index)
		if offset >= p.Size() {
			f.man.complete(index)
			return nil
		}

		err := func() error {
			url, err := f.url(ctx)
			if err != nil {
				return fmt.Errorf("could not get a download URL: %w", err)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", p.Start+offset, p.End))
			req.Header.Set("Accept-Encoding", "identity")

			resp, err := f.opts.Client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusPartialContent {
				// An expired presigned URL comes back as 403, and a consumed
				// wings token as 404. Both are worth one more mint before
				// giving up on the part.
				if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
					f.expireURL()
				}
				// A host that answered 206 to the probe and 200 now would
				// send the whole file down a part-sized connection. Refuse
				// rather than write the file over itself.
				return fmt.Errorf("expected 206 for a range request, got %d", resp.StatusCode)
			}

			atomic.AddInt32(&f.active, 1)
			defer atomic.AddInt32(&f.active, -1)

			written, err := f.copyAt(ctx, resp.Body, p.Start+offset)
			f.man.advance(index, written)
			if err != nil {
				return err
			}
			if got := offset + written; got != p.Size() {
				return fmt.Errorf("part %d: got %d bytes of %d", index, got, p.Size())
			}
			return nil
		}()

		if err == nil {
			f.man.complete(index)
			f.man.save(f.manifestPath())
			return nil
		}
		if !f.shouldRetry(ctx, attempt, err) {
			return fmt.Errorf("part %d: %w", index, err)
		}
		f.man.save(f.manifestPath())
	}
}

// copyAt streams a body into the part file at an offset, counting bytes and
// enforcing the idle timeout.
func (f *fetcher) copyAt(ctx context.Context, body io.Reader, offset int64) (int64, error) {
	buf := make([]byte, f.opts.BufferSize)
	var written int64
	lastData := time.Now()

	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}

		n, readErr := body.Read(buf)
		if n > 0 {
			if _, writeErr := f.file.WriteAt(buf[:n], offset+written); writeErr != nil {
				return written, fmt.Errorf("cannot write to %s: %w", f.partPath(), writeErr)
			}
			// Only the in-order path has a digest set, and only the stretch
			// it has not already hashed is fed in, so a retry that re-reads
			// the same offsets cannot hash them twice.
			if f.digest != nil && offset+written == f.digestFrom {
				f.digest.Write(buf[:n])
				f.digestFrom += int64(n)
			}
			written += int64(n)
			f.addDone(int64(n))
			lastData = time.Now()
			f.maybeReport(PhaseDownloading)
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, nil
			}
			return written, readErr
		}
		if n == 0 && time.Since(lastData) > f.opts.IdleTimeout {
			return written, fmt.Errorf("no data for %s", f.opts.IdleTimeout)
		}
	}
}

// shouldRetry decides whether to go round again, and waits out the backoff.
func (f *fetcher) shouldRetry(ctx context.Context, attempt int, cause error) bool {
	if ctx.Err() != nil {
		return false
	}
	// A context error wrapped by the transport is the caller cancelling, not
	// the node failing.
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return false
	}
	if f.opts.MaxAttempts > 0 && attempt >= f.opts.MaxAttempts {
		return false
	}

	atomic.AddInt32(&f.retries, 1)

	wait := f.opts.BaseBackoff << uint(min(attempt-1, 16))
	if wait > f.opts.MaxBackoff || wait <= 0 {
		wait = f.opts.MaxBackoff
	}
	// Jitter, so several parts that failed together do not all come back at
	// the same instant and fail together again.
	wait += time.Duration(rand.Int63n(int64(f.opts.BaseBackoff)))

	select {
	case <-ctx.Done():
		return false
	case <-time.After(wait):
		return true
	}
}

// verify checks the finished file against what the panel said it should be.
func (f *fetcher) verify() (string, bool, error) {
	f.mu.Lock()
	written := f.done
	total := f.total
	f.mu.Unlock()

	if f.job.Size > 0 && written != f.job.Size {
		return "", false, fmt.Errorf(
			"downloaded %d bytes but the panel said the backup is %d; treating it as incomplete",
			written, f.job.Size)
	}
	if total > 0 && written != total {
		return "", false, fmt.Errorf("downloaded %d bytes of %d", written, total)
	}

	if f.job.Checksum == "" {
		return "", false, nil
	}

	var sum string
	if f.digest != nil && f.digestFrom == written {
		// Hashed on the way in; no need to read the archive back.
		sum = fmt.Sprintf("%x", f.digest.Sum(nil))
	} else {
		var err error
		sum, err = hashOpenFile(f.file, f.job.ChecksumType, f.job.Checksum)
		if err != nil {
			return "", false, err
		}
	}
	want := wantedDigest(f.job.Checksum)
	if !strings.EqualFold(sum, want) {
		return sum, false, fmt.Errorf(
			"checksum mismatch: the node sent %s, the panel expected %s. The archive is "+
				"corrupt or was replaced mid-download", sum, want)
	}
	return sum, true, nil
}

func (f *fetcher) manifestPath() string { return f.job.Dest + ".ptdl.json" }

func (f *fetcher) setDone(n int64) {
	f.mu.Lock()
	f.done = n
	f.mu.Unlock()
}

func (f *fetcher) addDone(n int64) {
	f.mu.Lock()
	f.done += n
	if f.done < 0 {
		f.done = 0
	}
	f.mu.Unlock()
}

// maybeReport rate-limits progress callbacks. A 1 MiB buffer on a fast link
// fires thousands of times a second, and a UI cannot use that.
func (f *fetcher) maybeReport(phase Phase) {
	f.mu.Lock()
	if time.Since(f.lastReport) < f.opts.ProgressEvery {
		f.mu.Unlock()
		return
	}
	f.mu.Unlock()
	f.report(phase)
}

func (f *fetcher) report(phase Phase) {
	if f.opts.Progress == nil {
		return
	}

	f.mu.Lock()
	now := time.Now()
	if !f.rateAt.IsZero() {
		elapsed := now.Sub(f.rateAt).Seconds()
		if elapsed > 0 {
			instant := float64(f.done-f.rateBytes) / elapsed
			if f.rate == 0 {
				f.rate = instant
			} else {
				// Smoothed, because a chunked transfer's raw rate swings
				// wildly as parts start and finish.
				f.rate = 0.7*f.rate + 0.3*instant
			}
		}
	}
	f.rateAt, f.rateBytes = now, f.done
	f.lastReport = now

	p := Progress{
		Name:           f.job.Name,
		Dest:           f.job.Dest,
		Phase:          phase,
		Total:          f.total,
		Done:           f.done,
		BytesPerSecond: f.rate,
		Ranged:         f.ranged,
		Parts:          f.parts,
		ActiveParts:    int(atomic.LoadInt32(&f.active)),
		Retries:        int(atomic.LoadInt32(&f.retries)),
		Resumed:        f.resumed,
	}
	if p.Parts == 0 {
		p.Parts = 1
	}
	if p.BytesPerSecond > 0 && p.Total > p.Done {
		p.ETASeconds = float64(p.Total-p.Done) / p.BytesPerSecond
	}
	f.mu.Unlock()

	f.opts.Progress(p)
}

// Part is one byte range of the file.
type Part struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"` // inclusive, as in a Range header
}

func (p Part) Size() int64 { return p.End - p.Start + 1 }

// planParts divides a file, keeping a small one on a single connection.
func planParts(total int64, parts int, minPart int64) []Part {
	if total <= 0 {
		return []Part{{Start: 0, End: 0}}
	}
	if parts < 1 {
		parts = 1
	}
	// Never make a part smaller than minPart, so a modest file does not get
	// cut into pieces that cost more in requests than they save.
	if max := total / minPart; max < int64(parts) {
		parts = int(max)
	}
	if parts < 1 {
		parts = 1
	}

	size := total / int64(parts)
	out := make([]Part, 0, parts)
	for i := 0; i < parts; i++ {
		start := int64(i) * size
		end := start + size - 1
		if i == parts-1 {
			end = total - 1 // the last part absorbs the remainder
		}
		out = append(out, Part{Start: start, End: end})
	}
	return out
}

func totalFromContentRange(header string) (int64, bool) {
	// "bytes 0-1048575/5368709120"
	slash := strings.LastIndexByte(header, '/')
	if slash < 0 {
		return 0, false
	}
	size := strings.TrimSpace(header[slash+1:])
	if size == "" || size == "*" {
		return 0, false
	}
	total, err := strconv.ParseInt(size, 10, 64)
	if err != nil || total <= 0 {
		return 0, false
	}
	return total, true
}

// newHash picks a digest. Pterodactyl records a backup's checksum as sha1 by
// default but says so in a separate field, and some panels fold the two into
// one "sha1:<hex>" string, so both spellings are accepted.
func newHash(checksumType, checksum string) (hash.Hash, error) {
	name := strings.ToLower(strings.TrimSpace(checksumType))
	if name == "" {
		if colon := strings.IndexByte(checksum, ':'); colon > 0 {
			name = strings.ToLower(checksum[:colon])
		}
	}
	switch name {
	case "", "sha1":
		return sha1.New(), nil
	case "sha256":
		return sha256.New(), nil
	case "md5":
		return md5.New(), nil
	default:
		return nil, fmt.Errorf("unknown checksum type %q", checksumType)
	}
}

// wantedDigest strips a "sha1:" prefix if there is one.
func wantedDigest(checksum string) string {
	if colon := strings.IndexByte(checksum, ':'); colon > 0 {
		return strings.TrimSpace(checksum[colon+1:])
	}
	return strings.TrimSpace(checksum)
}

func hashOpenFile(f *os.File, checksumType, checksum string) (string, error) {
	h, err := newHash(checksumType, checksum)
	if err != nil {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("cannot read back the download to check it: %w", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func hashFile(path, checksumType, checksum string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return hashOpenFile(f, checksumType, checksum)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
