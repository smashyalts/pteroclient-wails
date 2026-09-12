package pteroapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeTree is a server's files, keyed by directory. Values ending in "/" are
// subdirectories.
type fakeTree map[string][]string

// newFakePanel serves the two file routes a search uses.
func newFakePanel(t *testing.T, tree fakeTree, contents map[string]string) (*httptest.Server, *int32) {
	t.Helper()
	var listings int32

	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/files/list"):
			atomic.AddInt32(&listings, 1)
			dir := r.URL.Query().Get("directory")
			entries, found := tree[dir]
			if !found {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"errors":[{"code":"NotFound","detail":"no such directory"}]}`))
				return
			}

			rows := make([]map[string]interface{}, 0, len(entries))
			for _, name := range entries {
				isDir := strings.HasSuffix(name, "/")
				clean := strings.TrimSuffix(name, "/")
				full := strings.TrimSuffix(dir, "/") + "/" + clean
				rows = append(rows, map[string]interface{}{
					"object": "file_object",
					"attributes": map[string]interface{}{
						"name":        clean,
						"is_file":     !isDir,
						"size":        len(contents[full]),
						"mode":        "rw-r--r--",
						"modified_at": "2026-09-01T12:00:00+00:00",
					},
				})
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"object": "list", "data": rows})

		case strings.HasSuffix(r.URL.Path, "/files/contents"):
			body, found := contents[r.URL.Query().Get("file")]
			if !found {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			fmt.Fprint(w, body)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(panel.Close)
	return panel, &listings
}

func TestSearchByName(t *testing.T) {
	tree := fakeTree{
		"/":                   {"server.properties", "plugins/", "logs/"},
		"/plugins":            {"Essentials/", "worldedit.jar"},
		"/plugins/Essentials": {"config.yml"},
		"/logs":               {"latest.log"},
	}
	panel, _ := newFakePanel(t, tree, map[string]string{})

	result, err := New(panel.URL, "k").Search(context.Background(), SearchOptions{
		Server: "abc",
		Name:   "*.yml",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 1 || result.Hits[0].Path != "/plugins/Essentials/config.yml" {
		t.Fatalf("expected the one yml file, got %+v", result.Hits)
	}
}

func TestSearchTreatsPlainWordAsSubstring(t *testing.T) {
	tree := fakeTree{"/": {"server.properties", "eula.txt"}}
	panel, _ := newFakePanel(t, tree, map[string]string{})

	// Someone searching for "properties" means server.properties, and
	// should not have to write the stars.
	result, err := New(panel.URL, "k").Search(context.Background(), SearchOptions{
		Server: "abc",
		Name:   "properties",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 1 || result.Hits[0].Path != "/server.properties" {
		t.Fatalf("got %+v", result.Hits)
	}
}

func TestSearchByContentReportsLineAndPreview(t *testing.T) {
	tree := fakeTree{"/": {"server.properties", "eula.txt"}}
	contents := map[string]string{
		"/server.properties": "motd=A server\nserver-port=25565\nmax-players=20\n",
		"/eula.txt":          "eula=true\n",
	}
	panel, _ := newFakePanel(t, tree, contents)

	result, err := New(panel.URL, "k").Search(context.Background(), SearchOptions{
		Server:   "abc",
		Contains: "server-port",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("expected one hit, got %+v", result.Hits)
	}
	hit := result.Hits[0]
	if hit.Line != 2 {
		t.Errorf("expected line 2, got %d", hit.Line)
	}
	if hit.Preview != "server-port=25565" {
		t.Errorf("preview was %q", hit.Preview)
	}
}

func TestSearchNarrowsContentScanByName(t *testing.T) {
	tree := fakeTree{"/": {"server.properties", "world.dat", "eula.txt"}}
	contents := map[string]string{
		"/server.properties": "server-port=25565\n",
		"/world.dat":         "server-port binary noise",
		"/eula.txt":          "eula=true",
	}
	panel, _ := newFakePanel(t, tree, contents)

	result, err := New(panel.URL, "k").Search(context.Background(), SearchOptions{
		Server:   "abc",
		Name:     "*.properties",
		Contains: "server-port",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 1 || result.Hits[0].Path != "/server.properties" {
		t.Fatalf("the name filter should have kept world.dat out: %+v", result.Hits)
	}
	// Only the one matching name should have been fetched.
	if result.FilesScanned != 1 {
		t.Errorf("scanned %d files; the name filter is meant to cut that to 1", result.FilesScanned)
	}
}

func TestSearchRespectsDepthLimit(t *testing.T) {
	tree := fakeTree{
		"/":      {"a/"},
		"/a":     {"b/"},
		"/a/b":   {"c/"},
		"/a/b/c": {"deep.yml"},
	}
	panel, _ := newFakePanel(t, tree, map[string]string{})

	result, err := New(panel.URL, "k").Search(context.Background(), SearchOptions{
		Server:   "abc",
		Name:     "*.yml",
		MaxDepth: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 0 {
		t.Fatalf("depth 2 should not have reached /a/b/c: %+v", result.Hits)
	}
	// The caller has to be told the walk stopped early, or an empty result
	// reads as "the file is not there".
	if !result.DepthLimitHit {
		t.Error("depth_limit_reached was not reported")
	}
}

func TestSearchTruncatesAndSaysSo(t *testing.T) {
	tree := fakeTree{"/": {"one.yml", "two.yml", "three.yml", "four.yml"}}
	panel, _ := newFakePanel(t, tree, map[string]string{})

	result, err := New(panel.URL, "k").Search(context.Background(), SearchOptions{
		Server:     "abc",
		Name:       "*.yml",
		MaxResults: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 2 {
		t.Fatalf("expected the cap to hold at 2, got %d", len(result.Hits))
	}
	if !result.Truncated {
		t.Error("truncated was not reported")
	}
}

func TestSearchSkipsNamedDirectories(t *testing.T) {
	tree := fakeTree{
		"/":      {"config.yml", "cache/"},
		"/cache": {"stale.yml"},
	}
	panel, listings := newFakePanel(t, tree, map[string]string{})

	result, err := New(panel.URL, "k").Search(context.Background(), SearchOptions{
		Server: "abc",
		Name:   "*.yml",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 1 || result.Hits[0].Path != "/config.yml" {
		t.Fatalf("cache should have been skipped: %+v", result.Hits)
	}
	if got := atomic.LoadInt32(listings); got != 1 {
		t.Errorf("expected one listing, saw %d; the skip list did not hold", got)
	}
}

func TestSearchNeedsSomethingToLookFor(t *testing.T) {
	panel, _ := newFakePanel(t, fakeTree{"/": {}}, map[string]string{})
	if _, err := New(panel.URL, "k").Search(context.Background(), SearchOptions{Server: "abc"}); err == nil {
		t.Error("a search with no name and no contains should be refused")
	}
}

func TestSearchReportsAnUnreachablePanel(t *testing.T) {
	// Every listing fails: that is a broken connection or a bad key, and
	// returning "no matches" for it would be a lie.
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"errors":[{"code":"Unauthorized","detail":"This action is unauthorized."}]}`))
	}))
	defer panel.Close()

	_, err := New(panel.URL, "bad").Search(context.Background(), SearchOptions{Server: "abc", Name: "*"})
	if err == nil {
		t.Fatal("expected the unauthorized panel to surface as an error")
	}
	if !strings.Contains(err.Error(), "unauthorized") && !strings.Contains(err.Error(), "401") {
		t.Errorf("error did not explain itself: %v", err)
	}
}

func TestListDirectoryBuildsFullPaths(t *testing.T) {
	tree := fakeTree{"/plugins": {"config.yml", "Essentials/"}}
	panel, _ := newFakePanel(t, tree, map[string]string{})

	entries, err := New(panel.URL, "k").ListDirectory(context.Background(), "abc", "/plugins")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries", len(entries))
	}
	if entries[0].Path != "/plugins/config.yml" {
		t.Errorf("path was %q", entries[0].Path)
	}
	if !entries[1].IsDirectory {
		t.Error("the directory was not marked as one")
	}
}
