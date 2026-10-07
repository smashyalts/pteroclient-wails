package download

import (
	"context"
	"crypto/sha1"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// payload makes deterministic bytes that compress badly, so a test cannot
// accidentally pass because everything was zeroes.
func payload(n int) []byte {
	out := make([]byte, n)
	r := rand.New(rand.NewSource(int64(n)))
	r.Read(out)
	return out
}

func sha1hex(b []byte) string {
	sum := sha1.Sum(b)
	return fmt.Sprintf("%x", sum[:])
}

// node is a fake backup host. mode picks how it behaves.
type node struct {
	body []byte

	// ranges off is stock wings: the Range header is ignored and the whole
	// file comes back with a 200.
	ranges bool

	// singleUse is the wings token rule: a token works once.
	singleUse bool

	// failFirst drops the connection partway through the first n requests.
	failFirst int32

	mints    int32
	requests int32
	ranged   int32

	mu   sync.Mutex
	used map[string]bool
}

func newNode(t *testing.T, body []byte, ranges, singleUse bool) (*node, *httptest.Server) {
	t.Helper()
	n := &node{body: body, ranges: ranges, singleUse: singleUse, used: map[string]bool{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n.requests, 1)

		token := r.URL.Query().Get("token")
		if n.singleUse {
			n.mu.Lock()
			spent := n.used[token]
			n.used[token] = true
			n.mu.Unlock()
			if spent {
				// This is what wings does to a reused token.
				w.WriteHeader(http.StatusNotFound)
				return
			}
		}

		start, end := int64(0), int64(len(n.body)-1)
		partial := false
		if rangeHeader := r.Header.Get("Range"); rangeHeader != "" && n.ranges {
			if s, e, ok := parseRange(rangeHeader, int64(len(n.body))); ok {
				start, end, partial = s, e, true
				atomic.AddInt32(&n.ranged, 1)
			}
		}

		chunk := n.body[start : end+1]

		if partial {
			w.Header().Set("Content-Range",
				fmt.Sprintf("bytes %d-%d/%d", start, end, len(n.body)))
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
			w.WriteHeader(http.StatusOK)
		}

		// Cut the body short on the first few requests, to exercise retry.
		if atomic.LoadInt32(&n.failFirst) > 0 {
			atomic.AddInt32(&n.failFirst, -1)
			half := len(chunk) / 2
			w.Write(chunk[:half])
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			// Returning without the rest, having promised a Content-Length,
			// makes the client see an unexpected EOF.
			return
		}
		w.Write(chunk)
	}))
	t.Cleanup(srv.Close)
	return n, srv
}

func parseRange(header string, size int64) (int64, int64, bool) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(header, "bytes=")
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(spec[:dash], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	end := size - 1
	if tail := spec[dash+1:]; tail != "" {
		if parsed, err := strconv.ParseInt(tail, 10, 64); err == nil && parsed < end {
			end = parsed
		}
	}
	if start > end || start >= size {
		return 0, 0, false
	}
	return start, end, true
}

// mint returns a Mint that hands out a fresh token each call.
func (n *node) mint(base string) Mint {
	return func(ctx context.Context) (string, error) {
		id := atomic.AddInt32(&n.mints, 1)
		return fmt.Sprintf("%s/download/backup?token=t%d", base, id), nil
	}
}

func testOpts(t *testing.T) Options {
	t.Helper()
	return Options{
		Parts:       4,
		MinPartSize: 4096,
		MaxAttempts: 6,
		BaseBackoff: time.Millisecond,
		MaxBackoff:  5 * time.Millisecond,
		BufferSize:  8192,
	}
}

func TestStockNodeFallsBackToOneStream(t *testing.T) {
	body := payload(300 * 1024)
	// Stock wings: no ranges, single-use tokens.
	n, srv := newNode(t, body, false, true)

	dest := filepath.Join(t.TempDir(), "backup.tar.gz")
	result, err := Fetch(context.Background(), Job{
		Name: "b1", Dest: dest, Mint: n.mint(srv.URL),
		Size: int64(len(body)), Checksum: sha1hex(body), ChecksumType: "sha1",
	}, testOpts(t))
	if err != nil {
		t.Fatal(err)
	}

	if result.Ranged {
		t.Error("a node that ignores Range must not be treated as ranged")
	}
	if result.Parts != 1 {
		t.Errorf("parts = %d, want 1; splitting a non-ranged host refetches the whole file per part", result.Parts)
	}
	if !result.Verified {
		t.Error("the checksum should have been verified")
	}

	// The probe doubles as the download, so one request is all it should take.
	if got := atomic.LoadInt32(&n.requests); got != 1 {
		t.Errorf("made %d requests for a single-stream download; the probe should have been the download", got)
	}
	if got := atomic.LoadInt32(&n.mints); got != 1 {
		t.Errorf("minted %d URLs, want 1", got)
	}

	onDisk, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if sha1hex(onDisk) != sha1hex(body) {
		t.Error("the file on disk does not match what the node served")
	}
}

func TestRangedNodeDownloadsInParallel(t *testing.T) {
	body := payload(256 * 1024)
	n, srv := newNode(t, body, true, false)

	dest := filepath.Join(t.TempDir(), "backup.tar.gz")
	opts := testOpts(t)
	opts.MinPartSize = 16 * 1024

	result, err := Fetch(context.Background(), Job{
		Name: "b1", Dest: dest, Mint: n.mint(srv.URL),
		Size: int64(len(body)), Checksum: sha1hex(body), ChecksumType: "sha1",
		Reusable: true, URLLifetime: time.Minute,
	}, opts)
	if err != nil {
		t.Fatal(err)
	}

	if !result.Ranged {
		t.Fatal("a node serving 206 should have been used in parallel")
	}
	if result.Parts < 2 {
		t.Errorf("parts = %d, want several", result.Parts)
	}
	if !result.Verified {
		t.Error("checksum not verified")
	}

	onDisk, _ := os.ReadFile(dest)
	if sha1hex(onDisk) != sha1hex(body) {
		t.Fatal("the reassembled file does not match")
	}

	// A presigned URL serves every part, so parallelism should not cost a
	// mint per connection.
	if got := atomic.LoadInt32(&n.mints); got != 1 {
		t.Errorf("minted %d URLs for a reusable link, want 1", got)
	}
}

func TestSingleUseTokenGetsAFreshURLPerRequest(t *testing.T) {
	body := payload(128 * 1024)
	// Ranges work but tokens are single use, which is what a wings host
	// behind a range-capable proxy looks like.
	n, srv := newNode(t, body, true, true)

	dest := filepath.Join(t.TempDir(), "backup.tar.gz")
	opts := testOpts(t)
	opts.MinPartSize = 16 * 1024

	if _, err := Fetch(context.Background(), Job{
		Name: "b1", Dest: dest, Mint: n.mint(srv.URL),
		Size: int64(len(body)), Checksum: sha1hex(body), ChecksumType: "sha1",
		Reusable: false, // the point of this test
	}, opts); err != nil {
		t.Fatal(err)
	}

	// Every request needs its own token, or the node 404s the second one.
	if mints, requests := atomic.LoadInt32(&n.mints), atomic.LoadInt32(&n.requests); mints < requests {
		t.Errorf("minted %d URLs for %d requests; a single-use token cannot be shared", mints, requests)
	}
	onDisk, _ := os.ReadFile(dest)
	if sha1hex(onDisk) != sha1hex(body) {
		t.Error("file mismatch")
	}
}

func TestRetriesATruncatedStream(t *testing.T) {
	body := payload(64 * 1024)
	n, srv := newNode(t, body, false, true)
	// The first two attempts get cut off halfway.
	atomic.StoreInt32(&n.failFirst, 2)

	dest := filepath.Join(t.TempDir(), "backup.tar.gz")
	result, err := Fetch(context.Background(), Job{
		Name: "b1", Dest: dest, Mint: n.mint(srv.URL),
		Size: int64(len(body)), Checksum: sha1hex(body), ChecksumType: "sha1",
	}, testOpts(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.Retries < 2 {
		t.Errorf("retries = %d, want at least 2", result.Retries)
	}

	onDisk, _ := os.ReadFile(dest)
	// The real risk here: a restarted stream appending to the bytes from the
	// failed attempt, leaving a file that is too long and corrupt.
	if len(onDisk) != len(body) {
		t.Fatalf("file is %d bytes, want %d; a retry wrote over the wrong offset", len(onDisk), len(body))
	}
	if sha1hex(onDisk) != sha1hex(body) {
		t.Error("file mismatch after retry")
	}
}

func TestChecksumMismatchIsAnError(t *testing.T) {
	body := payload(32 * 1024)
	n, srv := newNode(t, body, false, true)

	dest := filepath.Join(t.TempDir(), "backup.tar.gz")
	_, err := Fetch(context.Background(), Job{
		Name: "b1", Dest: dest, Mint: n.mint(srv.URL),
		Size: int64(len(body)), Checksum: sha1hex(payload(99)), ChecksumType: "sha1",
	}, testOpts(t))
	if err == nil {
		t.Fatal("a corrupt archive must not be accepted")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("error should name the checksum: %v", err)
	}
	// A file that failed verification must not appear under the real name,
	// or it will be trusted later.
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("the unverified file was moved into place")
	}
}

func TestSizeMismatchIsAnError(t *testing.T) {
	body := payload(16 * 1024)
	n, srv := newNode(t, body, false, true)

	dest := filepath.Join(t.TempDir(), "backup.tar.gz")
	_, err := Fetch(context.Background(), Job{
		Name: "b1", Dest: dest, Mint: n.mint(srv.URL),
		Size: int64(len(body)) * 2, // the panel says it is twice as big
	}, testOpts(t))
	if err == nil {
		t.Fatal("a short archive must not be accepted")
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("the short file was moved into place")
	}
}

func TestAlreadyDownloadedIsNotFetchedAgain(t *testing.T) {
	body := payload(8 * 1024)
	n, srv := newNode(t, body, false, true)

	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.tar.gz")
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Fetch(context.Background(), Job{
		Name: "b1", Dest: dest, Mint: n.mint(srv.URL),
		Size: int64(len(body)), Checksum: sha1hex(body), ChecksumType: "sha1",
	}, testOpts(t))
	if err != nil {
		t.Fatal(err)
	}
	// An unattended queue is restarted far more often than it is drained.
	if got := atomic.LoadInt32(&n.requests); got != 0 {
		t.Errorf("made %d requests for a file already on disk", got)
	}
}

func TestAWrongFileOnDiskIsReplaced(t *testing.T) {
	body := payload(8 * 1024)
	n, srv := newNode(t, body, false, true)

	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.tar.gz")
	// Same length, different contents: only the checksum catches this.
	if err := os.WriteFile(dest, payload(8*1024 + 1)[:8*1024], 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Fetch(context.Background(), Job{
		Name: "b1", Dest: dest, Mint: n.mint(srv.URL),
		Size: int64(len(body)), Checksum: sha1hex(body), ChecksumType: "sha1",
	}, testOpts(t)); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&n.requests) == 0 {
		t.Error("a file whose checksum did not match should have been fetched again")
	}
	onDisk, _ := os.ReadFile(dest)
	if sha1hex(onDisk) != sha1hex(body) {
		t.Error("the wrong file was left in place")
	}
}

func TestRangedTransferResumesFromAManifest(t *testing.T) {
	body := payload(256 * 1024)
	n, srv := newNode(t, body, true, false)

	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.tar.gz")
	opts := testOpts(t)
	opts.MinPartSize = 32 * 1024
	opts.Parts = 4

	job := Job{
		Name: "b1", Dest: dest, Mint: n.mint(srv.URL),
		Size: int64(len(body)), Checksum: sha1hex(body), ChecksumType: "sha1",
		Reusable: true, URLLifetime: time.Minute,
	}

	// Cancel the first run almost immediately, then finish it.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Fetch(ctx, job, opts); err == nil {
		t.Fatal("a cancelled transfer should report an error")
	}
	// The part file has to survive, or there is nothing to resume from.
	if _, err := os.Stat(dest + ".ptpart"); err != nil {
		t.Fatalf("the part file was not kept: %v", err)
	}

	if _, err := Fetch(context.Background(), job, opts); err != nil {
		t.Fatal(err)
	}
	onDisk, _ := os.ReadFile(dest)
	if sha1hex(onDisk) != sha1hex(body) {
		t.Error("the resumed file does not match")
	}
	// The manifest is cleaned up on success, so a later run does not try to
	// resume a file that is already finished.
	if _, err := os.Stat(dest + ".ptdl.json"); err == nil {
		t.Error("the manifest was left behind")
	}
}

func TestManifestForADifferentFileIsDiscarded(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.tar.gz")
	path := dest + ".ptdl.json"

	// A manifest whose validator is for another archive would claim bytes are
	// present that are not the bytes we want.
	if err := os.WriteFile(path, []byte(`{"name":"old","total":999,"validator":"deadbeef",
      "parts":[{"start":0,"end":998,"got":500,"done":false}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	m := loadManifest(path, Job{Name: "new", Checksum: "cafebabe", Size: 999})
	if m.doneBytes() != 0 {
		t.Errorf("a manifest for a different file should be discarded, got %d bytes claimed", m.doneBytes())
	}

	// The matching one survives.
	m = loadManifest(path, Job{Name: "old", Checksum: "deadbeef", Size: 999})
	if m.doneBytes() != 500 {
		t.Errorf("a matching manifest should be kept, got %d", m.doneBytes())
	}
}

func TestManifestDiscardsAChangedPartPlan(t *testing.T) {
	m := &manifest{Parts: []partState{
		{Start: 0, End: 99, Got: 100, Done: true},
		{Start: 100, End: 199, Got: 50},
	}}

	// Same file, different part count: the old offsets no longer describe the
	// new plan, and reusing them would stitch the archive together wrongly.
	m.adopt([]Part{{Start: 0, End: 49}, {Start: 50, End: 99}, {Start: 100, End: 199}}, 200)
	if m.doneBytes() != 0 {
		t.Errorf("a changed plan should reset progress, got %d", m.doneBytes())
	}

	// An unchanged plan keeps it.
	m2 := &manifest{Parts: []partState{{Start: 0, End: 99, Got: 100, Done: true}}}
	m2.adopt([]Part{{Start: 0, End: 99}}, 100)
	if m2.doneBytes() != 100 {
		t.Errorf("an unchanged plan should keep progress, got %d", m2.doneBytes())
	}
}

func TestPlanParts(t *testing.T) {
	cases := []struct {
		total   int64
		parts   int
		minPart int64
		want    int
	}{
		// Too small to split: one part, whatever was asked for.
		{total: 1000, parts: 8, minPart: 8 << 20, want: 1},
		{total: 100, parts: 4, minPart: 50, want: 2},
		{total: 1000, parts: 4, minPart: 100, want: 4},
		{total: 1000, parts: 100, minPart: 100, want: 10},
	}
	for _, tc := range cases {
		plan := planParts(tc.total, tc.parts, tc.minPart)
		if len(plan) != tc.want {
			t.Errorf("planParts(%d, %d, %d) = %d parts, want %d",
				tc.total, tc.parts, tc.minPart, len(plan), tc.want)
		}
		// The plan has to tile the file exactly, with no gap and no overlap,
		// or the assembled archive is corrupt.
		var covered int64
		for i, p := range plan {
			if i > 0 && p.Start != plan[i-1].End+1 {
				t.Errorf("gap or overlap between part %d and %d", i-1, i)
			}
			covered += p.Size()
		}
		if covered != tc.total {
			t.Errorf("plan covers %d bytes of %d", covered, tc.total)
		}
		if plan[len(plan)-1].End != tc.total-1 {
			t.Errorf("last part ends at %d, want %d", plan[len(plan)-1].End, tc.total-1)
		}
	}
}

func TestTotalFromContentRange(t *testing.T) {
	cases := map[string]int64{
		"bytes 0-1048575/5368709120": 5368709120,
		"bytes 0-0/1":                1,
		"bytes 0-99/*":               0,
		"garbage":                    0,
		"":                           0,
	}
	for header, want := range cases {
		got, ok := totalFromContentRange(header)
		if want == 0 && ok {
			t.Errorf("%q should not have parsed", header)
		}
		if want != 0 && got != want {
			t.Errorf("%q gave %d, want %d", header, got, want)
		}
	}
}

func TestChecksumTypeForms(t *testing.T) {
	body := []byte("hello")

	want := sha1hex(body)

	// Bare hex with the type alongside, and the folded "sha1:<hex>" form,
	// both appear in the wild.
	for _, job := range []Job{
		{Checksum: want, ChecksumType: "sha1"},
		{Checksum: "sha1:" + want},
	} {
		h, err := newHash(job.ChecksumType, job.Checksum)
		if err != nil {
			t.Fatal(err)
		}
		h.Write(body)
		if digest := fmt.Sprintf("%x", h.Sum(nil)); digest != wantedDigest(job.Checksum) {
			t.Errorf("checksum %q/%q did not round trip: %s", job.Checksum, job.ChecksumType, digest)
		}
	}

	if _, err := newHash("crc32", ""); err == nil {
		t.Error("an unknown checksum type should be refused")
	}
}

func TestPoolRunsJobsAndReportsThem(t *testing.T) {
	body := payload(32 * 1024)
	n, srv := newNode(t, body, false, true)
	dir := t.TempDir()

	var events int32
	pool := NewPool(context.Background(), 3, testOpts(t), func(Status) {
		atomic.AddInt32(&events, 1)
	})
	defer pool.Close()

	var ids []string
	for i := 0; i < 4; i++ {
		id, err := pool.Add(Job{
			Name: fmt.Sprintf("b%d", i),
			Dest: filepath.Join(dir, fmt.Sprintf("b%d.tar.gz", i)),
			Mint: n.mint(srv.URL), Size: int64(len(body)),
			Checksum: sha1hex(body), ChecksumType: "sha1",
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && pool.Active() > 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if pool.Active() != 0 {
		t.Fatal("the pool did not drain")
	}

	for _, id := range ids {
		status, found := pool.Status(id)
		if !found {
			t.Fatalf("%s is missing from the pool", id)
		}
		if status.State != StateDone {
			t.Errorf("%s ended as %s: %s", id, status.State, status.Error)
		}
	}
	if atomic.LoadInt32(&events) == 0 {
		t.Error("no events were emitted; a UI would have nothing to render")
	}
	if len(pool.List()) != 4 {
		t.Errorf("List returned %d jobs, want 4", len(pool.List()))
	}
}

func TestPoolRefusesTwoJobsForOneDestination(t *testing.T) {
	body := payload(4 * 1024)
	n, srv := newNode(t, body, false, true)
	dest := filepath.Join(t.TempDir(), "same.tar.gz")

	// Two writers on one part file would interleave bytes into something
	// that cannot verify.
	pool := NewPool(context.Background(), 1, testOpts(t), nil)
	defer pool.Close()

	job := Job{Name: "b", Dest: dest, Mint: n.mint(srv.URL), Size: int64(len(body))}
	if _, err := pool.Add(job); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Add(job); err == nil {
		t.Error("queueing the same destination twice should be refused")
	}
}

func TestPoolCancelStopsAJob(t *testing.T) {
	// A body big enough that the transfer is still running when we cancel.
	body := payload(8 << 20)
	n, srv := newNode(t, body, false, false)
	dest := filepath.Join(t.TempDir(), "big.tar.gz")

	pool := NewPool(context.Background(), 1, testOpts(t), nil)
	defer pool.Close()

	id, err := pool.Add(Job{Name: "big", Dest: dest, Mint: n.mint(srv.URL), Size: int64(len(body))})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := pool.Status(id); s.State == StateRunning {
			break
		}
		time.Sleep(time.Millisecond)
	}
	pool.Cancel(id)

	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := pool.Status(id); s.State != StateRunning && s.State != StateQueued {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	status, _ := pool.Status(id)
	if status.State != StateCancelled {
		t.Errorf("state = %s, want cancelled (error: %s)", status.State, status.Error)
	}
	// A cancelled job must not leave a half file under the real name.
	if _, err := os.Stat(dest); err == nil {
		t.Error("a cancelled download left a file in place")
	}
}

func TestPoolForgetKeepsRunningJobs(t *testing.T) {
	pool := NewPool(context.Background(), 1, testOpts(t), nil)
	defer pool.Close()

	body := payload(1024)
	n, srv := newNode(t, body, false, true)

	dir := t.TempDir()
	id, _ := pool.Add(Job{Name: "a", Dest: filepath.Join(dir, "a.gz"),
		Mint: n.mint(srv.URL), Size: int64(len(body))})

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && pool.Active() > 0 {
		time.Sleep(5 * time.Millisecond)
	}

	if dropped := pool.Forget(); dropped != 1 {
		t.Errorf("Forget dropped %d finished jobs, want 1", dropped)
	}
	if _, found := pool.Status(id); found {
		t.Error("a forgotten job is still listed")
	}
}

func TestAddRejectsAJobWithNoMint(t *testing.T) {
	pool := NewPool(context.Background(), 1, testOpts(t), nil)
	defer pool.Close()

	if _, err := pool.Add(Job{Name: "x", Dest: "x.gz"}); err == nil {
		t.Error("a job with no way to get a URL should be refused")
	}
	if _, err := pool.Add(Job{Name: "x", Mint: func(context.Context) (string, error) { return "", nil }}); err == nil {
		t.Error("a job with no destination should be refused")
	}
}

func TestFetchRejectsBadJobs(t *testing.T) {
	if _, err := Fetch(context.Background(), Job{Dest: "x"}, Options{}); err == nil {
		t.Error("no Mint should be refused")
	}
	if _, err := Fetch(context.Background(), Job{Mint: func(context.Context) (string, error) {
		return "", nil
	}}, Options{}); err == nil {
		t.Error("no Dest should be refused")
	}
}

func TestNodeRefusalIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"The requested backup was not found on this server."}`))
	}))
	defer srv.Close()

	_, err := Fetch(context.Background(), Job{
		Name: "gone", Dest: filepath.Join(t.TempDir(), "gone.gz"),
		Mint: func(context.Context) (string, error) { return srv.URL, nil },
	}, testOpts(t))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "404") {
		t.Errorf("the node's reason was lost: %v", err)
	}
}
