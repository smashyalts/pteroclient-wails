package safestore

import (
	"errors"
	"testing"
)

// A single delete larger than the bin used to evict its own earlier captures:
// each Put prunes, and within one batch the oldest entries are that batch's
// own files — the copies of things about to be deleted for real.
func TestBatchDoesNotEvictItsOwnCaptures(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	s.SetLimit(KindBin, 300)

	ids := []string{}
	refused := 0
	for _, name := range []string{"/a.txt", "/b.txt", "/c.txt", "/d.txt", "/e.txt"} {
		e, err := s.Put(KindBin, Entry{Path: name, Server: "s1", Reason: "deleted", Batch: "B1"}, make([]byte, 100))
		if errors.Is(err, ErrWouldEvictBatch) {
			refused++
			continue
		}
		if err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
		ids = append(ids, e.ID)
	}

	// Three fit. The fourth and fifth cannot be stored without throwing away
	// one of the first three, so the store says so instead of doing it
	// quietly — the caller reports those two files as unrecoverable.
	if len(ids) != 3 {
		t.Fatalf("expected 3 copies to be stored, got %d", len(ids))
	}
	if refused != 2 {
		t.Fatalf("expected 2 refusals, got %d", refused)
	}
	for _, id := range ids {
		if _, err := s.Get(KindBin, id); err != nil {
			t.Errorf("a copy from this delete was evicted by the same delete: %v", err)
		}
	}
}
