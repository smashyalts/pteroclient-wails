package safestore

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func put(t *testing.T, s *Store, kind Kind, path string, size int, reason string) Entry {
	t.Helper()
	e, err := s.Put(kind, Entry{Panel: "p", Server: "srv", Path: path, Reason: reason}, bytes.Repeat([]byte("x"), size))
	if err != nil {
		t.Fatalf("Put(%s): %v", path, err)
	}
	return e
}

func TestPutAndReadRoundTrip(t *testing.T) {
	s := newStore(t)

	body := []byte("motd=hello\nmax-players=20\n")
	entry, err := s.Put(KindVersion, Entry{Panel: "p", Server: "srv", Path: "/server.properties"}, body)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !entry.Captured || entry.Size != int64(len(body)) {
		t.Fatalf("entry not captured properly: %+v", entry)
	}

	got, err := s.Read(KindVersion, entry.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("round trip mismatch: %q", got)
	}
}

// Binary content must survive the store byte for byte: the recycle bin holds
// whatever was deleted, and a mangled .jar is not a recovered .jar.
func TestPutPreservesArbitraryBytes(t *testing.T) {
	s := newStore(t)

	body := []byte{0x00, 0xff, 0xfe, 0x50, 0x4b, 0x03, 0x04, 0x80, 0x00}
	entry, err := s.Put(KindBin, Entry{Panel: "p", Server: "srv", Path: "/plugins/thing.jar"}, body)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Read(KindBin, entry.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("binary round trip mismatch: % x", got)
	}
}

func TestUsageNeverExceedsLimit(t *testing.T) {
	s := newStore(t)
	s.SetLimit(KindBin, 10_000)

	// Twenty 1 kB files into a 10 kB bin.
	for i := 0; i < 20; i++ {
		put(t, s, KindBin, "/logs/latest-"+string(rune('a'+i))+".log", 1000, "deleted")
	}

	if used := s.Usage(KindBin); used > 10_000 {
		t.Fatalf("usage %d exceeds the 10000 limit", used)
	}
	if n := len(s.List(KindBin)); n == 0 || n > 10 {
		t.Fatalf("expected eviction down to at most 10 entries, got %d", n)
	}
}

// Eviction must take the oldest first, and must not leave blobs behind.
func TestEvictionDropsOldestAndItsBlob(t *testing.T) {
	s := newStore(t)
	s.SetLimit(KindBin, 3000)

	first := put(t, s, KindBin, "/a.txt", 1000, "deleted")
	put(t, s, KindBin, "/b.txt", 1000, "deleted")
	put(t, s, KindBin, "/c.txt", 1000, "deleted")
	put(t, s, KindBin, "/d.txt", 1000, "deleted")

	if _, err := s.Get(KindBin, first.ID); err == nil {
		t.Fatal("the oldest entry survived eviction")
	}

	blob := filepath.Join(s.dirs[KindBin], filepath.FromSlash(first.Rel))
	if _, err := os.Stat(blob); !os.IsNotExist(err) {
		t.Fatalf("evicted blob is still on disk: %v", err)
	}
}

// A file with many versions must not be able to push every other file's
// history out of the store.
func TestPruneKeepsTheNewestVersionOfEachFile(t *testing.T) {
	s := newStore(t)
	s.SetLimit(KindVersion, 5000)

	rare := put(t, s, KindVersion, "/rarely-touched.yml", 500, "edited")

	// A file saved in a loop, enough to blow the cap several times over.
	for i := 0; i < 30; i++ {
		put(t, s, KindVersion, "/churning.yml", 500, "edited")
	}

	if _, err := s.Get(KindVersion, rare.ID); err != nil {
		t.Fatal("the only version of an untouched file was evicted by another file's churn")
	}
}

func TestPerFileRetentionCap(t *testing.T) {
	s := newStore(t)
	s.SetLimit(KindVersion, 1<<30) // out of the way; retention is what is under test

	for i := 0; i < DefaultVersionsPerFile+15; i++ {
		put(t, s, KindVersion, "/config.yml", 10, "edited")
	}

	kept := s.ListFor(KindVersion, "srv", "/config.yml")
	if len(kept) != DefaultVersionsPerFile {
		t.Fatalf("kept %d versions, want %d", len(kept), DefaultVersionsPerFile)
	}
	// Newest first.
	for i := 1; i < len(kept); i++ {
		if kept[i].CreatedAt.After(kept[i-1].CreatedAt) {
			t.Fatal("ListFor is not newest-first")
		}
	}
}

func TestOversizedFileIsRefusedNotTruncated(t *testing.T) {
	s := newStore(t)
	s.SetLimit(KindBin, 1000)

	_, err := s.Put(KindBin, Entry{Panel: "p", Server: "srv", Path: "/big.dat"}, bytes.Repeat([]byte("x"), 5000))
	if err == nil {
		t.Fatal("expected a file larger than the whole store to be refused")
	}
	if s.Usage(KindBin) != 0 {
		t.Fatal("a refused Put left bytes behind")
	}
}

// An entry with no content still has to be recorded, so the UI can say a
// delete was not fully recoverable instead of implying it was.
func TestUncapturedEntryIsRecorded(t *testing.T) {
	s := newStore(t)

	entry, err := s.Put(KindBin, Entry{Panel: "p", Server: "srv", Path: "/huge.dat", Note: "too big"}, nil)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if entry.Captured {
		t.Fatal("a nil-content entry claims to be captured")
	}
	if _, err := s.Read(KindBin, entry.ID); err == nil {
		t.Fatal("reading an uncaptured entry should fail rather than return nothing")
	}
	if len(s.List(KindBin)) != 1 {
		t.Fatal("the uncaptured entry was not listed")
	}
}

func TestListBatchIsOldestFirst(t *testing.T) {
	s := newStore(t)

	for _, p := range []string{"/w", "/w/a.dat", "/w/b.dat"} {
		if _, err := s.Put(KindBin, Entry{Panel: "p", Server: "srv", Path: p, Batch: "b1"}, []byte("x")); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	if _, err := s.Put(KindBin, Entry{Panel: "p", Server: "srv", Path: "/other", Batch: "b2"}, []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got := s.ListBatch(KindBin, "b1")
	if len(got) != 3 {
		t.Fatalf("batch has %d entries, want 3", len(got))
	}
	if got[0].Path != "/w" {
		t.Fatalf("batch is not oldest-first: %s came first", got[0].Path)
	}
	for i := 1; i < len(got); i++ {
		if got[i].CreatedAt.Before(got[i-1].CreatedAt) {
			t.Fatal("batch ordering is wrong")
		}
	}
}

// The index is the source of truth; a reopened store must agree with itself.
func TestIndexSurvivesReopen(t *testing.T) {
	root := t.TempDir()

	s, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	entry := put(t, s, KindBin, "/keep.txt", 128, "deleted")
	before := s.Usage(KindBin)

	again, err := New(root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := again.Usage(KindBin); got != before {
		t.Fatalf("usage after reopen is %d, was %d", got, before)
	}
	body, err := again.Read(KindBin, entry.ID)
	if err != nil || len(body) != 128 {
		t.Fatalf("entry unreadable after reopen: %v", err)
	}
}

func TestReopenDropsEntriesWhoseBlobVanished(t *testing.T) {
	root := t.TempDir()

	s, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	entry := put(t, s, KindBin, "/gone.txt", 64, "deleted")

	// Something outside the app removed the blob.
	if err := os.Remove(filepath.Join(s.dirs[KindBin], filepath.FromSlash(entry.Rel))); err != nil {
		t.Fatalf("remove blob: %v", err)
	}

	again, err := New(root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if len(again.List(KindBin)) != 0 {
		t.Fatal("an entry with no blob is still listed, so usage is a lie")
	}
	if again.Usage(KindBin) != 0 {
		t.Fatal("usage still counts the missing blob")
	}
}

func TestLimitPersistsAcrossReopen(t *testing.T) {
	root := t.TempDir()

	s, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.SetLimit(KindBin, 250<<20)

	again, err := New(root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := again.Limit(KindBin); got != 250<<20 {
		t.Fatalf("limit after reopen is %d, want %d — a limit that resets would evict what the user kept", got, 250<<20)
	}
}

func TestRemoveAndEmpty(t *testing.T) {
	s := newStore(t)

	a := put(t, s, KindBin, "/a.txt", 32, "deleted")
	put(t, s, KindBin, "/b.txt", 32, "deleted")

	if err := s.Remove(KindBin, a.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(s.List(KindBin)) != 1 {
		t.Fatal("Remove did not drop exactly one entry")
	}
	if err := s.Remove(KindBin, a.ID); err != ErrNotFound {
		t.Fatalf("removing twice returned %v, want ErrNotFound", err)
	}

	if err := s.Empty(KindBin); err != nil {
		t.Fatalf("Empty: %v", err)
	}
	if s.Usage(KindBin) != 0 || len(s.List(KindBin)) != 0 {
		t.Fatal("Empty left something behind")
	}
}

// The two stores must not see each other's entries: emptying the bin cannot
// take the edit history with it.
func TestStoresAreIndependent(t *testing.T) {
	s := newStore(t)

	put(t, s, KindVersion, "/config.yml", 100, "edited")
	put(t, s, KindBin, "/config.yml", 100, "deleted")

	if err := s.Empty(KindBin); err != nil {
		t.Fatalf("Empty: %v", err)
	}
	if len(s.List(KindVersion)) != 1 {
		t.Fatal("emptying the bin also emptied the version history")
	}
}

func TestSanitizeKeepsNamesUsableOnWindows(t *testing.T) {
	cases := map[string]string{
		"config.yml":     "config.yml",
		"a:b*c?.txt":     "a_b_c_.txt",
		`weird\name`:     "weird_name",
		"  ":             "file",
		"...":            "file",
		"con<trol>.json": "con_trol_.json",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

// A corrupt index used to be replaced silently, leaving every blob it named on
// disk with nothing tracking it and no sign anything had gone wrong.
func TestCorruptIndexIsMovedAsideNotDropped(t *testing.T) {
	root := t.TempDir()

	s, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	put(t, s, KindBin, "/a.txt", 64, "deleted")

	indexPath := s.files[KindBin]
	if err := os.WriteFile(indexPath, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("corrupt the index: %v", err)
	}

	again, err := New(root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}

	if n := len(again.List(KindBin)); n != 0 {
		t.Fatalf("expected an empty store after a corrupt index, got %d entries", n)
	}

	aside := again.Orphaned(KindBin)
	if aside == "" {
		t.Fatal("the corrupt index was not reported")
	}
	if _, statErr := os.Stat(aside); statErr != nil {
		t.Fatalf("the corrupt index was not kept at %s: %v", aside, statErr)
	}

	// A fresh index takes over cleanly.
	put(t, again, KindBin, "/b.txt", 32, "deleted")
	if n := len(again.List(KindBin)); n != 1 {
		t.Fatalf("the store did not recover: %d entries", n)
	}
}
