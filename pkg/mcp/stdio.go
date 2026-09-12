package mcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
)

// ServeStdio runs the server over newline-delimited JSON on a pipe, which is
// how a locally launched MCP server is spoken to.
//
// Nothing but JSON-RPC may reach out: the client parses every line, so a
// stray log line is a protocol error. Diagnostics belong on stderr, which
// this function never touches.
func ServeStdio(ctx context.Context, s *Server, in io.Reader, out io.Writer) error {
	// ReadBytes rather than a Scanner: a file write carries its content in
	// the request, and Scanner's line cap would truncate the message into
	// malformed JSON instead of simply reading a long line.
	reader := bufio.NewReaderSize(in, 64*1024)
	writer := bufio.NewWriter(out)

	// Handlers run in order here, but the writer is guarded anyway so that
	// this stays safe if concurrency is added later.
	var writeMu sync.Mutex

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		line, err := readMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil // the client closed the pipe: a normal exit
			}
			return err
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		reply := s.Handle(ctx, line)
		if reply == nil {
			continue // a notification takes no answer
		}

		writeMu.Lock()
		_, writeErr := writer.Write(append(reply, '\n'))
		if writeErr == nil {
			writeErr = writer.Flush()
		}
		writeMu.Unlock()

		if writeErr != nil {
			return writeErr
		}
	}
}

// readMessage reads one newline-terminated message of any length, returning
// the final unterminated fragment too so a client that closes without a
// trailing newline still gets its last request answered.
func readMessage(reader *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := reader.ReadBytes('\n')
		buf = append(buf, chunk...)
		if err == nil {
			return buf, nil
		}
		if errors.Is(err, io.EOF) && len(bytes.TrimSpace(buf)) > 0 {
			return buf, nil
		}
		if err != nil {
			return nil, err
		}
	}
}
