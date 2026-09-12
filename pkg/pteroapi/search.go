package pteroapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
)

// FileEntry is one row of a directory listing, reduced to the fields a caller
// acts on.
type FileEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	IsFile      bool   `json:"is_file"`
	IsSymlink   bool   `json:"is_symlink"`
	Mode        string `json:"mode,omitempty"`
	MimeType    string `json:"mimetype,omitempty"`
	ModifiedAt  string `json:"modified_at,omitempty"`
	IsDirectory bool   `json:"is_directory"`
}

// ListDirectory reads one directory.
func (c *Client) ListDirectory(ctx context.Context, server, dir string) ([]FileEntry, error) {
	if dir == "" {
		dir = "/"
	}
	query := url.Values{}
	query.Set("directory", dir)

	result, err := c.Send(ctx, Request{
		Method: http.MethodGet,
		Path:   "/servers/" + server + "/files/list",
		Query:  query,
	})
	if err != nil {
		return nil, err
	}
	if result.Status < 200 || result.Status > 299 {
		return nil, describeFailure(result)
	}

	var envelope struct {
		Data []struct {
			Attributes struct {
				Name       string `json:"name"`
				Mode       string `json:"mode"`
				Size       int64  `json:"size"`
				IsFile     bool   `json:"is_file"`
				IsSymlink  bool   `json:"is_symlink"`
				MimeType   string `json:"mimetype"`
				ModifiedAt string `json:"modified_at"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(result.Body, &envelope); err != nil {
		return nil, fmt.Errorf("could not read the directory listing: %w", err)
	}

	entries := make([]FileEntry, 0, len(envelope.Data))
	for _, row := range envelope.Data {
		a := row.Attributes
		entries = append(entries, FileEntry{
			Name:        a.Name,
			Path:        joinRemote(dir, a.Name),
			Size:        a.Size,
			IsFile:      a.IsFile,
			IsDirectory: !a.IsFile,
			IsSymlink:   a.IsSymlink,
			Mode:        a.Mode,
			MimeType:    a.MimeType,
			ModifiedAt:  a.ModifiedAt,
		})
	}
	return entries, nil
}

// ReadFile fetches a file's contents through the editor route.
func (c *Client) ReadFile(ctx context.Context, server, file string) (string, error) {
	query := url.Values{}
	query.Set("file", file)
	return c.Text(ctx, Request{
		Method: http.MethodGet,
		Path:   "/servers/" + server + "/files/contents",
		Query:  query,
	})
}

// SearchOptions describes one recursive search.
type SearchOptions struct {
	Server string
	Root   string

	// Name is a glob matched against each entry's base name, case
	// insensitively — "*.yml", "server.properties", "*log*".
	Name string

	// Contains is a substring searched for inside matching files. Set it
	// alone to search every file's content; set it with Name to search only
	// the files whose names match, which is far fewer requests.
	Contains      string
	CaseSensitive bool

	MaxDepth     int
	MaxResults   int
	MaxFileBytes int64
	Concurrency  int

	// SkipDirs are directory names never descended into. Left empty, a
	// search of a Minecraft server walks every world region file and every
	// node_modules tree, which is thousands of listings for nothing.
	SkipDirs []string
}

// SearchHit is one match.
type SearchHit struct {
	Path    string `json:"path"`
	Size    int64  `json:"size,omitempty"`
	IsDir   bool   `json:"is_dir,omitempty"`
	Line    int    `json:"line,omitempty"`
	Preview string `json:"preview,omitempty"`
}

// SearchResult is the outcome of a search, with enough bookkeeping for the
// caller to know whether it saw everything.
type SearchResult struct {
	Hits              []SearchHit `json:"hits"`
	DirectoriesRead   int         `json:"directories_read"`
	FilesScanned      int         `json:"files_scanned"`
	Truncated         bool        `json:"truncated,omitempty"`
	DepthLimitHit     bool        `json:"depth_limit_reached,omitempty"`
	SkippedTooLarge   int         `json:"skipped_too_large,omitempty"`
	SkippedUnreadable int         `json:"skipped_unreadable,omitempty"`
}

var defaultSkipDirs = []string{
	"node_modules", ".git", "cache", "libraries", "versions",
	"region", "DIM-1", "DIM1", "entities", "poi", "playerdata",
}

func (o *SearchOptions) applyDefaults() {
	if o.Root == "" {
		o.Root = "/"
	}
	if o.MaxDepth <= 0 {
		o.MaxDepth = 6
	}
	if o.MaxResults <= 0 {
		o.MaxResults = 100
	}
	if o.MaxFileBytes <= 0 {
		o.MaxFileBytes = 512 * 1024
	}
	if o.Concurrency <= 0 {
		// The panel rate-limits the client API per key. Eight listings in
		// flight is brisk without spending most of the search waiting out
		// 429s that the retry then has to absorb.
		o.Concurrency = 8
	}
	if o.Concurrency > 16 {
		o.Concurrency = 16
	}
	if o.SkipDirs == nil {
		o.SkipDirs = defaultSkipDirs
	}
}

// Search walks a server's files looking for names, contents, or both.
//
// The client API has no search route: the panel's own file manager searches
// one directory at a time in the browser. Walking it here is the only way a
// caller can ask "which config sets this port" without reading the tree by
// hand, one listing per turn.
func (c *Client) Search(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	opts.applyDefaults()

	if opts.Name == "" && opts.Contains == "" {
		return nil, fmt.Errorf("give a name pattern, a contains string, or both")
	}

	namePattern := strings.ToLower(opts.Name)
	needle := opts.Contains
	if !opts.CaseSensitive {
		needle = strings.ToLower(needle)
	}

	skip := make(map[string]bool, len(opts.SkipDirs))
	for _, dir := range opts.SkipDirs {
		skip[strings.ToLower(dir)] = true
	}

	var (
		mu      sync.Mutex
		result  = &SearchResult{Hits: []SearchHit{}}
		stopped bool
	)

	// full reports whether enough has been found to stop the walk.
	full := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return stopped || len(result.Hits) >= opts.MaxResults
	}

	addHit := func(hit SearchHit) {
		mu.Lock()
		defer mu.Unlock()
		if len(result.Hits) >= opts.MaxResults {
			result.Truncated = true
			stopped = true
			return
		}
		result.Hits = append(result.Hits, hit)
		if len(result.Hits) >= opts.MaxResults {
			result.Truncated = true
		}
	}

	// The walk is breadth-first by depth so that a truncated search returns
	// the shallow matches — the ones a caller is nearly always after —
	// rather than whatever a depth-first walk happened to reach first.
	frontier := []string{normalizeDir(opts.Root)}

	for depth := 0; depth < opts.MaxDepth && len(frontier) > 0; depth++ {
		if full() || ctx.Err() != nil {
			break
		}

		var (
			nextMu sync.Mutex
			next   []string
			wg     sync.WaitGroup
			slots  = make(chan struct{}, opts.Concurrency)
			// firstErr keeps the first hard failure. A single unreadable
			// directory should not fail the whole search, but a bad key or
			// an unreachable panel should be reported rather than returned
			// as "no matches".
			firstErr     error
			failures     int
			directories  int
			filesScanned int
			tooLarge     int
			unreadable   int
		)

		for _, dir := range frontier {
			if full() || ctx.Err() != nil {
				break
			}
			wg.Add(1)
			slots <- struct{}{}

			go func(dir string) {
				defer wg.Done()
				defer func() { <-slots }()

				entries, err := c.ListDirectory(ctx, opts.Server, dir)

				nextMu.Lock()
				directories++
				if err != nil {
					failures++
					if firstErr == nil {
						firstErr = err
					}
					nextMu.Unlock()
					return
				}
				nextMu.Unlock()

				for _, entry := range entries {
					if full() {
						return
					}

					nameMatches := namePattern == "" ||
						globMatch(namePattern, strings.ToLower(entry.Name))

					if entry.IsDirectory {
						if nameMatches && opts.Contains == "" {
							addHit(SearchHit{Path: entry.Path, IsDir: true})
						}
						if entry.IsSymlink || skip[strings.ToLower(entry.Name)] {
							continue
						}
						nextMu.Lock()
						next = append(next, entry.Path)
						nextMu.Unlock()
						continue
					}

					if !nameMatches {
						continue
					}
					if opts.Contains == "" {
						addHit(SearchHit{Path: entry.Path, Size: entry.Size})
						continue
					}

					// Content match. A file too big to read is reported as
					// skipped rather than silently passed over: "no
					// matches" and "did not look" are different answers.
					if entry.Size > opts.MaxFileBytes {
						nextMu.Lock()
						tooLarge++
						nextMu.Unlock()
						continue
					}

					content, readErr := c.ReadFile(ctx, opts.Server, entry.Path)
					nextMu.Lock()
					filesScanned++
					if readErr != nil {
						unreadable++
					}
					nextMu.Unlock()
					if readErr != nil {
						continue
					}

					haystack := content
					if !opts.CaseSensitive {
						haystack = strings.ToLower(content)
					}
					if !strings.Contains(haystack, needle) {
						continue
					}

					line, preview := locate(content, haystack, needle)
					addHit(SearchHit{
						Path:    entry.Path,
						Size:    entry.Size,
						Line:    line,
						Preview: preview,
					})
				}
			}(dir)
		}

		wg.Wait()

		mu.Lock()
		result.DirectoriesRead += directories
		result.FilesScanned += filesScanned
		result.SkippedTooLarge += tooLarge
		result.SkippedUnreadable += unreadable
		mu.Unlock()

		// Every directory at this level failed and nothing has matched:
		// that is a broken connection rather than an empty tree.
		if firstErr != nil && failures == directories && len(result.Hits) == 0 {
			return nil, firstErr
		}

		frontier = next
		if depth == opts.MaxDepth-1 && len(frontier) > 0 {
			result.DepthLimitHit = true
		}
	}

	sort.SliceStable(result.Hits, func(i, j int) bool {
		return result.Hits[i].Path < result.Hits[j].Path
	})
	return result, ctx.Err()
}

// locate finds the line number and a trimmed preview of the first match.
//
// The index comes from the case-folded copy, and lowercasing a handful of
// Unicode letters changes their byte length, so the offset is not guaranteed
// to be valid in the original. It is clamped rather than trusted: a slightly
// wrong preview is a nuisance, an out-of-range slice is a crash.
func locate(original, haystack, needle string) (int, string) {
	index := strings.Index(haystack, needle)
	if index < 0 {
		return 0, ""
	}
	if index > len(original) {
		index = len(original)
	}
	line := strings.Count(original[:index], "\n") + 1

	start := strings.LastIndexByte(original[:index], '\n') + 1
	end := index + len(needle)
	if end > len(original) {
		end = len(original)
	}
	if newline := strings.IndexByte(original[end:], '\n'); newline >= 0 {
		end += newline
	} else {
		end = len(original)
	}

	preview := strings.TrimSpace(StripANSI(original[start:end]))
	if len(preview) > 240 {
		preview = preview[:240] + "…"
	}
	return line, preview
}

// globMatch is a case-folded match for the one wildcard the panel's own file
// names call for. path.Match covers * and ? and character classes, which is
// everything a file search needs; a pattern with no wildcard at all is
// treated as a substring so that searching for "properties" finds
// server.properties without the caller having to write the stars.
func globMatch(pattern, name string) bool {
	if pattern == "" {
		return true
	}
	if !strings.ContainsAny(pattern, "*?[") {
		return strings.Contains(name, pattern)
	}
	matched, err := path.Match(pattern, name)
	if err != nil {
		// A malformed pattern falls back to substring rather than matching
		// nothing, which would look like an empty server.
		return strings.Contains(name, strings.Trim(pattern, "*"))
	}
	return matched
}

func normalizeDir(dir string) string {
	if dir == "" {
		return "/"
	}
	if !strings.HasPrefix(dir, "/") {
		dir = "/" + dir
	}
	return path.Clean(dir)
}

func joinRemote(dir, name string) string {
	return path.Join(normalizeDir(dir), name)
}
