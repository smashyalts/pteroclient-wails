// Package safestore keeps local copies of remote panel files.
//
// Two stores live side by side under the app's config directory:
//
//	versions/ — the history. Every write to a panel file first files the bytes
//	            it is about to replace here, so a file can be rolled back to any
//	            earlier state, not just the last one.
//	bin/      — the recycle bin. Content the app displaced wholesale rather than
//	            edited: an upload landing on an existing name, or the file a
//	            version restore writes over.
//
// Both are byte-capped and evict oldest-first. The bin's cap is 100 MB, which
// is the figure the UI reports. The version store has a larger cap plus a
// per-file retention count, so one file churning in a loop cannot push every
// other file's history out.
//
// Nothing here talks to the panel. The caller fetches the bytes and hands them
// over; that keeps the eviction and index logic testable on its own.
package safestore

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Kind selects which of the two stores an operation applies to.
type Kind string

const (
	KindVersion Kind = "version"
	KindBin     Kind = "bin"
)

// Defaults. The bin limit is the one the user asked for; the rest are sized so
// the whole store stays well under a gigabyte in the worst case.
const (
	DefaultBinLimit     int64 = 100 << 20 // 100 MB, the figure the Vault tab shows
	DefaultVersionLimit int64 = 512 << 20 // 512 MB of file history
	// MaxSingleCapture is the largest single file either store will hold. A
	// file over this is reported as uncaptured rather than silently skipped.
	MaxSingleCapture int64 = 64 << 20 // 64 MB
	// DefaultVersionsPerFile is how many past states of one file are kept
	// before the oldest of that file is dropped.
	DefaultVersionsPerFile = 50
)

// ErrTooLarge is returned when one file exceeds the store's limit on its own,
// so no amount of eviction would make room for it.
var ErrTooLarge = errors.New("file is larger than the store's limit")

// ErrNotFound is returned for an unknown entry id.
var ErrNotFound = errors.New("entry not found")

// Entry describes one captured copy. It is JSON-marshalled into the index and
// handed straight to the frontend, so the tags are part of the UI contract.
type Entry struct {
	ID         string `json:"id"`
	Kind       Kind   `json:"kind"`
	Panel      string `json:"panel"`
	Server     string `json:"server"`
	ServerName string `json:"server_name"`
	Path       string `json:"path"`
	Rel        string `json:"rel"`
	Size       int64  `json:"size"`
	Reason     string `json:"reason"`
	// Batch groups the entries one operation produced, so a folder that was
	// deleted into two hundred entries can be restored as one action.
	Batch     string    `json:"batch,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// Captured is false when only the metadata could be stored — the file was
	// too big, or the panel would not hand over its content. A delete with an
	// uncaptured entry is NOT recoverable, and the UI says so before running.
	Captured bool `json:"captured"`
	// Note carries the reason content is missing, when Captured is false.
	Note string `json:"note,omitempty"`
}

// Key groups entries that describe the same remote file.
func (e Entry) Key() string {
	return e.Panel + "\x00" + e.Server + "\x00" + e.Path
}

type index struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Store is the on-disk store. It is safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	root string

	limits  map[Kind]int64
	perFile map[Kind]int
	dirs    map[Kind]string
	files   map[Kind]string
	data    map[Kind]*index

	settingsPath string

	seq uint64
}

// settings is the small amount of store configuration that has to outlive the
// process. A limit that silently reverted to its default on restart would
// evict — and lose — whatever the user had raised it to hold.
type settings struct {
	BinLimit     int64 `json:"bin_limit,omitempty"`
	VersionLimit int64 `json:"version_limit,omitempty"`
}

// New opens (creating if needed) a store rooted at root. root is normally
// ~/.pteroclient.
func New(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("safestore: empty root")
	}

	s := &Store{
		root:         root,
		limits:       map[Kind]int64{KindVersion: DefaultVersionLimit, KindBin: DefaultBinLimit},
		perFile:      map[Kind]int{KindVersion: DefaultVersionsPerFile, KindBin: 0},
		dirs:         map[Kind]string{KindVersion: filepath.Join(root, "versions"), KindBin: filepath.Join(root, "bin")},
		files:        map[Kind]string{KindVersion: filepath.Join(root, "versions", "index.json"), KindBin: filepath.Join(root, "bin", "index.json")},
		data:         map[Kind]*index{},
		settingsPath: filepath.Join(root, "store-settings.json"),
	}

	for _, kind := range []Kind{KindVersion, KindBin} {
		if err := os.MkdirAll(s.dirs[kind], 0o700); err != nil {
			return nil, fmt.Errorf("safestore: create %s: %w", s.dirs[kind], err)
		}
		s.data[kind] = s.loadIndex(kind)
	}

	s.loadSettings()
	return s, nil
}

func (s *Store) loadSettings() {
	raw, err := os.ReadFile(s.settingsPath)
	if err != nil {
		return
	}
	var got settings
	if err := json.Unmarshal(raw, &got); err != nil {
		return
	}
	if got.BinLimit > 0 {
		s.limits[KindBin] = got.BinLimit
	}
	if got.VersionLimit > 0 {
		s.limits[KindVersion] = got.VersionLimit
	}
}

func (s *Store) saveSettingsLocked() {
	raw, err := json.MarshalIndent(settings{
		BinLimit:     s.limits[KindBin],
		VersionLimit: s.limits[KindVersion],
	}, "", "  ")
	if err != nil {
		return
	}
	_ = writeFileAtomic(s.settingsPath, raw, 0o600)
}

// Root returns the directory the store lives in, for display.
func (s *Store) Root() string { return s.root }

// SetLimit overrides a store's byte cap. A value <= 0 restores the default.
func (s *Store) SetLimit(kind Kind, limit int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		if kind == KindBin {
			limit = DefaultBinLimit
		} else {
			limit = DefaultVersionLimit
		}
	}
	s.limits[kind] = limit
	s.saveSettingsLocked()
}

// Limit returns a store's byte cap.
func (s *Store) Limit(kind Kind) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limits[kind]
}

// Usage returns the bytes currently held by a store.
func (s *Store) Usage(kind Kind) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usageLocked(kind)
}

// Free returns how many bytes a store can still take without evicting.
func (s *Store) Free(kind Kind) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	free := s.limits[kind] - s.usageLocked(kind)
	if free < 0 {
		return 0
	}
	return free
}

func (s *Store) usageLocked(kind Kind) int64 {
	var total int64
	for _, e := range s.data[kind].Entries {
		total += e.Size
	}
	return total
}

// Put writes data into a store and records meta. The caller fills Panel,
// Server, Path, Reason and (optionally) ServerName; every other field is set
// here. Passing nil data records an uncaptured entry, which is how a file too
// large to hold is still accounted for.
func (s *Store) Put(kind Kind, meta Entry, data []byte) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	meta.Kind = kind
	meta.CreatedAt = time.Now().UTC()
	meta.ID = s.nextID(meta)

	if data == nil {
		meta.Size = 0
		meta.Rel = ""
		meta.Captured = false
		if meta.Note == "" {
			meta.Note = "content was not captured"
		}
		s.data[kind].Entries = append(s.data[kind].Entries, meta)
		if err := s.saveIndexLocked(kind); err != nil {
			return meta, err
		}
		return meta, nil
	}

	size := int64(len(data))
	limit := s.limits[kind]
	if size > limit || size > MaxSingleCapture {
		return Entry{}, fmt.Errorf("%w: %d bytes", ErrTooLarge, size)
	}

	// Make room before writing, never after: an interrupted Put must not leave
	// the store over its cap.
	if err := s.pruneLocked(kind, size); err != nil {
		return Entry{}, err
	}

	rel := filepath.Join(bucketFor(meta), meta.ID+"-"+sanitize(path.Base(meta.Path)))
	full := filepath.Join(s.dirs[kind], rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return Entry{}, fmt.Errorf("safestore: create bucket: %w", err)
	}
	if err := writeFileAtomic(full, data, 0o600); err != nil {
		return Entry{}, fmt.Errorf("safestore: write copy: %w", err)
	}

	meta.Rel = filepath.ToSlash(rel)
	meta.Size = size
	meta.Captured = true
	meta.Note = ""

	s.data[kind].Entries = append(s.data[kind].Entries, meta)
	s.trimPerFileLocked(kind, meta.Key())
	if err := s.saveIndexLocked(kind); err != nil {
		// The index is the source of truth; an orphaned blob is harmless but
		// an entry with no index line would leak.
		_ = os.Remove(full)
		return Entry{}, err
	}
	return meta, nil
}

// trimPerFileLocked drops the oldest states of one file once it has more than
// the store's retention count. Without it a file saved in a tight loop would
// evict every other file's history through the global cap.
func (s *Store) trimPerFileLocked(kind Kind, key string) {
	keep := s.perFile[kind]
	if keep <= 0 {
		return
	}

	entries := s.data[kind].Entries
	mine := make([]int, 0, 8)
	for i, e := range entries {
		if e.Key() == key {
			mine = append(mine, i)
		}
	}
	if len(mine) <= keep {
		return
	}

	sort.SliceStable(mine, func(a, b int) bool {
		return entries[mine[a]].CreatedAt.Before(entries[mine[b]].CreatedAt)
	})

	drop := map[int]bool{}
	for _, i := range mine[:len(mine)-keep] {
		drop[i] = true
		if entries[i].Rel != "" {
			_ = os.Remove(filepath.Join(s.dirs[kind], filepath.FromSlash(entries[i].Rel)))
		}
	}

	survivors := make([]Entry, 0, len(entries)-len(drop))
	for i, e := range entries {
		if !drop[i] {
			survivors = append(survivors, e)
		}
	}
	s.data[kind].Entries = survivors
}

// ListBatch returns the entries one operation produced, oldest first so a
// restore recreates directories before the files inside them.
func (s *Store) ListBatch(kind Kind, batch string) []Entry {
	if batch == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Entry, 0, 8)
	for _, e := range s.data[kind].Entries {
		if e.Batch == batch {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// List returns a store's entries, newest first.
func (s *Store) List(kind Kind) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Entry, len(s.data[kind].Entries))
	copy(out, s.data[kind].Entries)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// ListFor returns the entries for one remote file, newest first.
func (s *Store) ListFor(kind Kind, server, remotePath string) []Entry {
	all := s.List(kind)
	out := make([]Entry, 0, 8)
	for _, e := range all {
		if e.Server == server && e.Path == remotePath {
			out = append(out, e)
		}
	}
	return out
}

// Get returns one entry by id.
func (s *Store) Get(kind Kind, id string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.data[kind].Entries {
		if e.ID == id {
			return e, nil
		}
	}
	return Entry{}, ErrNotFound
}

// Read returns the stored bytes for an entry.
func (s *Store) Read(kind Kind, id string) ([]byte, error) {
	entry, err := s.Get(kind, id)
	if err != nil {
		return nil, err
	}
	if !entry.Captured || entry.Rel == "" {
		return nil, fmt.Errorf("entry %s holds no content: %s", id, entry.Note)
	}
	full := filepath.Join(s.dirs[kind], filepath.FromSlash(entry.Rel))
	return os.ReadFile(full)
}

// Remove deletes one entry and its blob.
func (s *Store) Remove(kind Kind, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries := s.data[kind].Entries
	for i, e := range entries {
		if e.ID != id {
			continue
		}
		if e.Rel != "" {
			_ = os.Remove(filepath.Join(s.dirs[kind], filepath.FromSlash(e.Rel)))
		}
		s.data[kind].Entries = append(entries[:i], entries[i+1:]...)
		return s.saveIndexLocked(kind)
	}
	return ErrNotFound
}

// Empty discards a whole store.
func (s *Store) Empty(kind Kind) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, e := range s.data[kind].Entries {
		if e.Rel != "" {
			_ = os.Remove(filepath.Join(s.dirs[kind], filepath.FromSlash(e.Rel)))
		}
	}
	s.data[kind].Entries = nil
	return s.saveIndexLocked(kind)
}

// pruneLocked evicts oldest-first until incoming bytes fit under the cap.
//
// One entry per remote file is held back on the first pass: losing the newest
// copy of a file that still has an older one is worse than losing an old copy
// of a file that has several. Only if that is not enough does the second pass
// take those too.
func (s *Store) pruneLocked(kind Kind, incoming int64) error {
	limit := s.limits[kind]
	if s.usageLocked(kind)+incoming <= limit {
		return nil
	}

	entries := s.data[kind].Entries
	order := make([]int, len(entries))
	for i := range entries {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return entries[order[a]].CreatedAt.Before(entries[order[b]].CreatedAt)
	})

	// Newest index per remote file, so the first pass can skip it.
	newest := map[string]int{}
	for i, e := range entries {
		if cur, ok := newest[e.Key()]; !ok || e.CreatedAt.After(entries[cur].CreatedAt) {
			newest[e.Key()] = i
		}
	}

	drop := map[int]bool{}
	used := s.usageLocked(kind)

	for pass := 0; pass < 2; pass++ {
		for _, i := range order {
			if used+incoming <= limit {
				break
			}
			if drop[i] {
				continue
			}
			if pass == 0 && newest[entries[i].Key()] == i {
				continue
			}
			drop[i] = true
			used -= entries[i].Size
		}
		if used+incoming <= limit {
			break
		}
	}

	if len(drop) == 0 {
		return nil
	}

	kept := make([]Entry, 0, len(entries)-len(drop))
	for i, e := range entries {
		if drop[i] {
			if e.Rel != "" {
				_ = os.Remove(filepath.Join(s.dirs[kind], filepath.FromSlash(e.Rel)))
			}
			continue
		}
		kept = append(kept, e)
	}
	s.data[kind].Entries = kept
	return s.saveIndexLocked(kind)
}

/* ---------------------------------------------------------------- index io */

func (s *Store) loadIndex(kind Kind) *index {
	idx := &index{Version: 1}

	raw, err := os.ReadFile(s.files[kind])
	if err != nil {
		return idx
	}
	if err := json.Unmarshal(raw, idx); err != nil {
		// A corrupt index must not take the app down, and must not make the
		// blobs look deletable either — they are left on disk untracked.
		return &index{Version: 1}
	}

	// Drop entries whose blob has gone missing, so usage stays honest.
	kept := idx.Entries[:0]
	for _, e := range idx.Entries {
		if e.Rel == "" {
			kept = append(kept, e)
			continue
		}
		if _, err := os.Stat(filepath.Join(s.dirs[kind], filepath.FromSlash(e.Rel))); err == nil {
			kept = append(kept, e)
		}
	}
	idx.Entries = kept
	return idx
}

func (s *Store) saveIndexLocked(kind Kind) error {
	raw, err := json.MarshalIndent(s.data[kind], "", "  ")
	if err != nil {
		return fmt.Errorf("safestore: marshal index: %w", err)
	}
	return writeFileAtomic(s.files[kind], raw, 0o600)
}

/* ------------------------------------------------------------------ helpers */

func (s *Store) nextID(meta Entry) string {
	n := atomic.AddUint64(&s.seq, 1)
	sum := sha1.Sum([]byte(meta.Key()))
	return strconv.FormatInt(time.Now().UTC().UnixNano(), 36) +
		strconv.FormatUint(n, 36) + "-" + hex.EncodeToString(sum[:4])
}

func bucketFor(meta Entry) string {
	sum := sha1.Sum([]byte(meta.Key()))
	return hex.EncodeToString(sum[:6])
}

// sanitize makes a remote basename safe to use as a Windows filename.
func sanitize(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r == 0x7f:
			return '_'
		case strings.ContainsRune(`<>:"/\|?*`, r):
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		name = "file"
	}
	if len(name) > 96 {
		name = name[:96]
	}
	return name
}

// writeFileAtomic writes via a temp file in the same directory and renames, so
// a crash mid-write cannot leave a half-written index or blob behind.
func writeFileAtomic(dest string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return err
	}

	// Windows will not rename onto an existing file.
	if err := os.Rename(tmpName, dest); err != nil {
		if removeErr := os.Remove(dest); removeErr != nil && !os.IsNotExist(removeErr) {
			os.Remove(tmpName)
			return err
		}
		if err2 := os.Rename(tmpName, dest); err2 != nil {
			os.Remove(tmpName)
			return err2
		}
	}
	return nil
}
