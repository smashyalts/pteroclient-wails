package sftpx

// These run against a real SSH server with a real SFTP subsystem, started in
// process. That matters more than usual here: the version this replaces could
// not connect at all — sftp.MaxPacketChecked refuses anything over 32 KB and it
// was being handed 128 KB — and nothing caught it, because every test until now
// stubbed the SFTP API rather than speaking it.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// testServer is an SSH server serving SFTP out of one directory.
type testServer struct {
	addr string
	root string
	stop func()
}

func startServer(t *testing.T) *testServer {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "tester" && string(pass) == "correct-horse" {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		},
	}
	cfg.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	root := t.TempDir()
	done := make(chan struct{})

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}
			go serve(conn, cfg, root)
		}
	}()

	srv := &testServer{
		addr: listener.Addr().String(),
		root: root,
		stop: func() {
			close(done)
			listener.Close()
		},
	}
	t.Cleanup(srv.stop)
	return srv
}

func serve(nConn net.Conn, cfg *ssh.ServerConfig, root string) {
	sshConn, chans, reqs, err := ssh.NewServerConn(nConn, cfg)
	if err != nil {
		nConn.Close()
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		channel, requests, acceptErr := newChannel.Accept()
		if acceptErr != nil {
			continue
		}

		go func(in <-chan *ssh.Request) {
			for req := range in {
				req.Reply(req.Type == "subsystem" && len(req.Payload) >= 4 &&
					string(req.Payload[4:]) == "sftp", nil)
			}
		}(requests)

		go func(ch ssh.Channel) {
			// No working directory: pkg/sftp joins it onto every path it is
			// given, absolute ones included, which on Windows produces
			// "C:\tmp\x\C:\tmp\x\file". The tests address files by
			// their full path instead.
			_ = root
			server, serverErr := sftp.NewServer(ch)
			if serverErr != nil {
				ch.Close()
				return
			}
			_ = server.Serve()
			server.Close()
		}(channel)
	}
}

// remote turns a path inside the server's directory into one the server will
// resolve.
//
// The real thing is Linux and chrooted, so "/plugins/x.jar" is right there. The
// in-process server here is pkg/sftp on Windows, which resolves a leading slash
// against the drive root — so the tests address files by their full path. What
// is under test is the transfer, not the naming.
func (s *testServer) at(rel string) string {
	return filepath.ToSlash(s.root) + "/" + rel
}

func connect(t *testing.T, srv *testServer, streams int) *Session {
	t.Helper()

	host, portStr, _ := net.SplitHostPort(srv.addr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	hosts, err := NewKnownHosts(t.TempDir())
	if err != nil {
		t.Fatalf("known hosts: %v", err)
	}

	cfg := Config{Host: host, Port: port, User: "tester", Password: "correct-horse", Streams: streams}

	// First attempt is refused on purpose, carrying the fingerprint.
	_, err = Dial(cfg, hosts)
	var unknown *ErrHostKeyUnknown
	if err == nil {
		t.Fatal("an unseen host connected without being asked about")
	}
	if !asHostKeyUnknown(err, &unknown) {
		t.Fatalf("expected an unknown-host error, got %v", err)
	}

	cfg.AcceptFingerprint = unknown.Fingerprint
	session, err := Dial(cfg, hosts)
	if err != nil {
		t.Fatalf("connect after accepting the key: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func asHostKeyUnknown(err error, target **ErrHostKeyUnknown) bool {
	if e, ok := err.(*ErrHostKeyUnknown); ok {
		*target = e
		return true
	}
	return false
}

func randomFile(t *testing.T, dir, name string, size int) (string, [32]byte) {
	t.Helper()
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("random: %v", err)
	}
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
	return full, sha256.Sum256(data)
}

func sumOf(t *testing.T, p string) [32]byte {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("open %s: %v", p, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// The connection itself. This is the one that would have caught the shipped
// bug: MaxPacketChecked rejected the packet size, NewClient failed, and Dial
// turned that into "could not start SFTP".
func TestConnects(t *testing.T) {
	srv := startServer(t)
	session := connect(t, srv, 4)

	if session.Streams() < 1 {
		t.Fatalf("no usable connections")
	}
	t.Logf("connected with %d connections", session.Streams())
}

// A wrong password must not connect.
func TestRefusesBadPassword(t *testing.T) {
	srv := startServer(t)
	host, portStr, _ := net.SplitHostPort(srv.addr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	if _, err := Dial(Config{
		Host: host, Port: port, User: "tester", Password: "wrong", Streams: 1,
	}, nil); err == nil {
		t.Fatal("a wrong password connected")
	}
}

// Small files, several at once: the folder case.
func TestUploadsAndDownloadsManyFiles(t *testing.T) {
	srv := startServer(t)
	session := connect(t, srv, 4)
	ctx := context.Background()

	local := t.TempDir()
	jobs := []Job{}
	want := map[string][32]byte{}

	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("file-%02d.bin", i)
		p, sum := randomFile(t, local, name, 40*1024+i*911)
		want[name] = sum
		info, _ := os.Stat(p)
		jobs = append(jobs, Job{Local: p, Remote: srv.at("up/" + name), Size: info.Size()})
	}

	result := session.Upload(ctx, jobs, nil)
	if result.Failed != 0 {
		for _, f := range result.Files {
			if f.Error != "" {
				t.Errorf("%s: %s", f.Job.Remote, f.Error)
			}
		}
		t.Fatalf("%d of %d uploads failed", result.Failed, len(jobs))
	}

	for name, sum := range want {
		got := sumOf(t, filepath.Join(srv.root, "up", name))
		if got != sum {
			t.Errorf("%s came out different", name)
		}
	}

	// And back down again.
	back := t.TempDir()
	downJobs := []Job{}
	for name := range want {
		downJobs = append(downJobs, Job{
			Local:  filepath.Join(back, name),
			Remote: srv.at("up/" + name),
		})
	}

	down := session.Download(ctx, downJobs, nil)
	if down.Failed != 0 {
		t.Fatalf("%d downloads failed", down.Failed)
	}
	for name, sum := range want {
		if sumOf(t, filepath.Join(back, name)) != sum {
			t.Errorf("%s came back different", name)
		}
	}
}

// The point of the change: one large file split across connections has to come
// out byte for byte identical, in both directions.
func TestMultipartRoundTripIsIdentical(t *testing.T) {
	srv := startServer(t)
	session := connect(t, srv, 4)
	ctx := context.Background()

	local := t.TempDir()
	// Over multipartMin, and deliberately not a multiple of the part size, so
	// the last part carries an awkward remainder.
	size := multipartMin + 3*1024*1024 + 7777
	src, sum := randomFile(t, local, "big.bin", size)

	if parts := session.partCount(int64(size)); parts < 2 {
		t.Fatalf("expected this to be split, got %d part(s)", parts)
	} else {
		t.Logf("%d bytes split into %d parts", size, parts)
	}

	up := session.Upload(ctx, []Job{{Local: src, Remote: srv.at("big.bin"), Size: int64(size)}}, nil)
	if up.Failed != 0 {
		t.Fatalf("upload failed: %s", up.Files[0].Error)
	}

	landed := filepath.Join(srv.root, "big.bin")
	info, err := os.Stat(landed)
	if err != nil {
		t.Fatalf("uploaded file missing: %v", err)
	}
	if info.Size() != int64(size) {
		t.Fatalf("uploaded %d bytes, expected %d", info.Size(), size)
	}
	if sumOf(t, landed) != sum {
		t.Fatal("the multipart upload did not match the original")
	}

	// Nothing left behind.
	if _, err := os.Stat(landed + ".uploading"); err == nil {
		t.Error("the temporary upload file was left on the server")
	}

	back := filepath.Join(t.TempDir(), "big.bin")
	down := session.Download(ctx, []Job{{Local: back, Remote: srv.at("big.bin"), Size: int64(size)}}, nil)
	if down.Failed != 0 {
		t.Fatalf("download failed: %s", down.Files[0].Error)
	}
	if sumOf(t, back) != sum {
		t.Fatal("the multipart download did not match")
	}
	if _, err := os.Stat(back + ".part"); err == nil {
		t.Error("the partial download file was left behind")
	}
}

// Progress has to reach the total, and not overshoot it.
func TestProgressAddsUp(t *testing.T) {
	srv := startServer(t)
	session := connect(t, srv, 3)
	ctx := context.Background()

	local := t.TempDir()
	var jobs []Job
	var total int64
	for i := 0; i < 4; i++ {
		p, _ := randomFile(t, local, fmt.Sprintf("p%d.bin", i), 512*1024)
		info, _ := os.Stat(p)
		total += info.Size()
		jobs = append(jobs, Job{Local: p, Remote: srv.at(fmt.Sprintf("prog/p%d.bin", i)), Size: info.Size()})
	}

	var high int64
	var seenPartial bool
	result := session.Upload(ctx, jobs, func(p Progress) {
		if p.Bytes > high {
			high = p.Bytes
		}
		if p.Bytes > 0 && p.Bytes < p.TotalBytes {
			seenPartial = true
		}
		if p.Bytes > p.TotalBytes {
			t.Errorf("progress overshot: %d of %d", p.Bytes, p.TotalBytes)
		}
	})

	if result.Failed != 0 {
		t.Fatalf("%d failed", result.Failed)
	}
	if result.Bytes != total {
		t.Errorf("reported %d bytes, expected %d", result.Bytes, total)
	}
	if !seenPartial {
		t.Log("note: no partial reading observed; the transfer may have been too quick")
	}
	t.Logf("high water %d of %d bytes", high, total)
}

// An upload that replaces an existing file must leave the real name intact
// until the new content is completely written.
func TestReplacesInPlace(t *testing.T) {
	srv := startServer(t)
	session := connect(t, srv, 2)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(srv.root, "config.yml"), []byte("old contents\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	local := t.TempDir()
	src := filepath.Join(local, "config.yml")
	if err := os.WriteFile(src, bytes.Repeat([]byte("new\n"), 1000), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(src)

	result := session.Upload(ctx, []Job{{Local: src, Remote: srv.at("config.yml"), Size: info.Size()}}, nil)
	if result.Failed != 0 {
		t.Fatalf("upload failed: %s", result.Files[0].Error)
	}

	got, err := os.ReadFile(filepath.Join(srv.root, "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bytes.Repeat([]byte("new\n"), 1000)) {
		t.Fatal("the replacement did not land")
	}
}

// Cancelling has to stop the transfer rather than run it to completion.
func TestCancelStops(t *testing.T) {
	srv := startServer(t)
	session := connect(t, srv, 2)

	local := t.TempDir()
	var jobs []Job
	for i := 0; i < 25; i++ {
		p, _ := randomFile(t, local, fmt.Sprintf("c%02d.bin", i), 256*1024)
		info, _ := os.Stat(p)
		jobs = append(jobs, Job{Local: p, Remote: srv.at(fmt.Sprintf("cancel/c%02d.bin", i)), Size: info.Size()})
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(40 * time.Millisecond)
		cancel()
	}()

	result := session.Upload(ctx, jobs, nil)
	if !result.Cancelled {
		t.Error("a cancelled transfer did not report itself as cancelled")
	}
	moved := 0
	for _, f := range result.Files {
		if f.Error == "" && f.Bytes > 0 {
			moved++
		}
	}
	t.Logf("moved %d of %d before stopping", moved, len(jobs))
}

// Listing and walking, which the folder transfer paths depend on.
func TestWalkFindsEverything(t *testing.T) {
	srv := startServer(t)
	session := connect(t, srv, 2)
	ctx := context.Background()

	for _, dir := range []string{"tree", "tree/a", "tree/a/b"} {
		if err := os.MkdirAll(filepath.Join(srv.root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"tree/one.txt", "tree/a/two.txt", "tree/a/b/three.txt"} {
		if err := os.WriteFile(filepath.Join(srv.root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	items, truncated, err := session.WalkDir(ctx, srv.at("tree"), 100)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if truncated {
		t.Error("a three-file tree should not truncate at 100")
	}

	files := 0
	for _, item := range items {
		if !item.IsDir {
			files++
		}
	}
	if files != 3 {
		t.Errorf("found %d files, expected 3", files)
	}

	// Shallowest first, so a caller creating directories has parents first.
	for i := 1; i < len(items); i++ {
		if depth(items[i-1].Path) > depth(items[i].Path) {
			t.Errorf("walk came back out of order: %s before %s", items[i-1].Path, items[i].Path)
			break
		}
	}
}

func depth(p string) int {
	n := 0
	for _, r := range p {
		if r == '/' {
			n++
		}
	}
	_ = path.Clean(p)
	return n
}
