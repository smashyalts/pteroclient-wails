package sftpx

// Does any of it actually help?
//
// The other tests run over loopback, where a round trip is microseconds — and
// round-trip time is the entire thing these changes are about. On loopback a
// sequential transfer and a pipelined one look identical, so those tests prove
// correctness and say nothing at all about speed.
//
// So this puts a delay in front of the server and measures. Every number below
// is produced by the run, not typed in.

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// slowProxy forwards TCP with a delay in each direction, so a request and its
// response cost one full round trip.
type slowProxy struct {
	addr     string
	listener net.Listener
	halfRTT  time.Duration
	wg       sync.WaitGroup
	closed   chan struct{}

	// perFlowBytesPerSec caps each connection independently, the way traffic
	// shaping on shared hosting does. Zero means no cap.
	perFlowBytesPerSec int
}

func startProxy(t *testing.T, target string, rtt time.Duration) *slowProxy {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}

	p := &slowProxy{
		addr:     listener.Addr().String(),
		listener: listener,
		halfRTT:  rtt / 2,
		closed:   make(chan struct{}),
	}

	go func() {
		for {
			client, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			server, dialErr := net.Dial("tcp", target)
			if dialErr != nil {
				client.Close()
				continue
			}
			p.wg.Add(2)
			go p.pump(client, server)
			go p.pump(server, client)
		}
	}()

	t.Cleanup(func() {
		close(p.closed)
		listener.Close()
	})
	return p
}

// pump copies one direction, holding each chunk for half the round trip.
//
// Delaying per chunk rather than per byte is what a network does: latency is
// paid once per packet in flight, and throughput comes from having several in
// flight at a time. That is precisely the thing being measured.
func (p *slowProxy) pump(from, to net.Conn) {
	defer p.wg.Done()
	defer to.Close()

	buf := make([]byte, 64*1024)
	for {
		n, err := from.Read(buf)
		if n > 0 {
			wait := p.halfRTT
			if p.perFlowBytesPerSec > 0 {
				// The time this many bytes would take on a connection limited
				// to that rate, on top of the propagation delay.
				wait += time.Duration(float64(n) / float64(p.perFlowBytesPerSec) * float64(time.Second))
			}
			select {
			case <-time.After(wait):
			case <-p.closed:
				return
			}
			if _, writeErr := to.Write(buf[:n]); writeErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// serialUpload is what the code did before: io.Copy into a wrapped writer,
// which hides *sftp.File's ReaderFrom and falls back to sequential writes.
//
// Kept here so the comparison is against the real previous behaviour rather
// than against an estimate of it.
func serialUpload(t *testing.T, s *Session, local, remote string) time.Duration {
	t.Helper()

	src, err := os.Open(local)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	c, err := s.borrow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.give(c)

	dst, err := c.sftp.Create(remote)
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	var sink int64
	// io.MultiWriter is the detail that mattered: it makes the destination not
	// a ReaderFrom, so io.Copy uses its own 32 KB buffer loop.
	if _, err := io.Copy(io.MultiWriter(dst, &countingWriter{
		w: io.Discard, file: &sink, total: &sink,
	}), src); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	return time.Since(started)
}

func rate(bytes int64, d time.Duration) string {
	if d <= 0 {
		return "instant"
	}
	perSec := float64(bytes) / d.Seconds()
	return fmt.Sprintf("%.2f MB/s", perSec/(1024*1024))
}

// TestThroughput is the answer to "was it actually faster".
//
// Run with -v to see the numbers. It is skipped in a short run: the delays are
// real, and it takes a few seconds by design.
func TestThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("measures against a simulated 60 ms link; takes a few seconds")
	}

	const rtt = 60 * time.Millisecond
	const size = 6 << 20 // 6 MB

	// The real thresholds are 8 MB with 4 MB parts, so 6 MB would not have
	// been split at all — the first run of this test compared one stream
	// against itself and reported 1.0x.
	defer restoreSplitLimits(multipartMin, multipartPartMin)
	multipartMin, multipartPartMin = 1<<20, 1<<20

	srv := startServer(t)
	proxy := startProxy(t, srv.addr, rtt)

	host, portStr, _ := net.SplitHostPort(proxy.addr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	hosts, err := NewKnownHosts(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	dial := func(streams int) *Session {
		cfg := Config{Host: host, Port: port, User: "tester", Password: "correct-horse", Streams: streams}
		if _, dialErr := Dial(cfg, hosts); dialErr != nil {
			var unknown *ErrHostKeyUnknown
			if asHostKeyUnknown(dialErr, &unknown) {
				cfg.AcceptFingerprint = unknown.Fingerprint
			}
		}
		session, dialErr := Dial(cfg, hosts)
		if dialErr != nil {
			t.Fatalf("connect: %v", dialErr)
		}
		t.Cleanup(func() { session.Close() })
		return session
	}

	local := t.TempDir()
	src, sum := randomFile(t, local, "payload.bin", size)

	t.Logf("6 MB over a simulated %v link", rtt)

	// 1. The old way: one connection, sequential 32 KB writes.
	single := dial(1)
	serial := serialUpload(t, single, src, srv.at("serial.bin"))
	t.Logf("  sequential (what it did before) ... %8s  %v", rate(int64(size), serial), serial.Round(time.Millisecond))

	// 2. One connection, pipelined.
	var pipelined int64
	startPipe := time.Now()
	if err := single.sendWhole(context.Background(), mustOpen(t, src), srv.at("pipe.bin"),
		int64(size), &pipelined, new(int64)); err != nil {
		t.Fatalf("pipelined upload: %v", err)
	}
	pipeTime := time.Since(startPipe)
	t.Logf("  pipelined, one connection ........ %8s  %v", rate(int64(size), pipeTime), pipeTime.Round(time.Millisecond))

	// 3. Several connections, split into parts.
	many := dial(4)
	if parts := many.partCount(int64(size)); parts < 2 {
		t.Fatalf("this was meant to be split; partCount said %d", parts)
	} else {
		t.Logf("  (split into %d parts)", parts)
	}
	startParts := time.Now()
	result := many.Upload(context.Background(), []Job{
		{Local: src, Remote: srv.at("parts.bin"), Size: int64(size)},
	}, nil)
	partsTime := time.Since(startParts)
	if result.Failed != 0 {
		t.Fatalf("multipart upload: %s", result.Files[0].Error)
	}
	t.Logf("  multipart, %d connections ......... %8s  %v",
		many.Streams(), rate(int64(size), partsTime), partsTime.Round(time.Millisecond))

	// And it has to still be the same file.
	if sumOf(t, filepath.Join(srv.root, "parts.bin")) != sum {
		t.Fatal("the fast path did not produce the same file")
	}

	t.Logf("")
	t.Logf("  pipelining alone:  %.1fx", float64(serial)/float64(pipeTime))
	t.Logf("  with multipart:    %.1fx", float64(serial)/float64(partsTime))

	if pipeTime >= serial {
		t.Errorf("pipelining was not faster: %v vs %v", pipeTime, serial)
	}
}

func mustOpen(t *testing.T, p string) *os.File {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// TestMultipartWhenItHelps checks the premise behind splitting a file.
//
// The first measurement showed multipart no faster than a single pipelined
// connection on a clean link — once 64 requests are in flight, latency is
// already hidden and more connections only add handshakes. Splitting is
// supposed to win when one TCP flow is the limit rather than the round trip:
// per-flow shaping, which is ordinary on shared hosting.
//
// So this caps each connection's throughput and measures again. If multipart
// does not win here, it does not win anywhere, and it should come out.
func TestMultipartWhenItHelps(t *testing.T) {
	if testing.Short() {
		t.Skip("measures against a shaped link; takes a few seconds")
	}

	const rtt = 40 * time.Millisecond
	const perFlow = 700 * 1024 // bytes per second, per connection
	const size = 3 << 20

	defer restoreSplitLimits(multipartMin, multipartPartMin)
	multipartMin, multipartPartMin = 1<<20, 512<<10

	srv := startServer(t)
	proxy := startProxy(t, srv.addr, rtt)
	proxy.perFlowBytesPerSec = perFlow

	host, portStr, _ := net.SplitHostPort(proxy.addr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	hosts, err := NewKnownHosts(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dial := func(streams int) *Session {
		cfg := Config{Host: host, Port: port, User: "tester", Password: "correct-horse", Streams: streams}
		if _, dialErr := Dial(cfg, hosts); dialErr != nil {
			var unknown *ErrHostKeyUnknown
			if asHostKeyUnknown(dialErr, &unknown) {
				cfg.AcceptFingerprint = unknown.Fingerprint
			}
		}
		session, dialErr := Dial(cfg, hosts)
		if dialErr != nil {
			t.Fatalf("connect: %v", dialErr)
		}
		t.Cleanup(func() { session.Close() })
		return session
	}

	local := t.TempDir()
	src, sum := randomFile(t, local, "shaped.bin", size)

	t.Logf("3 MB over %v with each connection capped at %d KB/s", rtt, perFlow/1024)

	one := dial(1)
	startOne := time.Now()
	var moved int64
	if err := one.sendWhole(context.Background(), mustOpen(t, src), srv.at("one.bin"),
		int64(size), &moved, new(int64)); err != nil {
		t.Fatalf("single: %v", err)
	}
	oneTime := time.Since(startOne)
	t.Logf("  one connection .......... %8s  %v", rate(int64(size), oneTime), oneTime.Round(time.Millisecond))

	four := dial(4)
	if parts := four.partCount(int64(size)); parts < 2 {
		t.Fatalf("this was meant to be split; partCount said %d", parts)
	}
	startFour := time.Now()
	result := four.Upload(context.Background(), []Job{
		{Local: src, Remote: srv.at("four.bin"), Size: int64(size)},
	}, nil)
	fourTime := time.Since(startFour)
	if result.Failed != 0 {
		t.Fatalf("multipart: %s", result.Files[0].Error)
	}
	t.Logf("  %d connections, split .... %8s  %v", four.Streams(), rate(int64(size), fourTime), fourTime.Round(time.Millisecond))
	t.Logf("  multipart is %.1fx", float64(oneTime)/float64(fourTime))

	if sumOf(t, filepath.Join(srv.root, "four.bin")) != sum {
		t.Fatal("the split upload did not match")
	}
}

// restoreSplitLimits puts the real thresholds back after a test lowers them.
func restoreSplitLimits(min, part int64) {
	multipartMin, multipartPartMin = min, part
}
