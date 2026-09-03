package main

// How many times does one action talk to the panel?
//
// Every user action here is network-bound, so what makes the app feel quick is
// not how fast the code runs but how many round trips it takes. This stands up
// a fake panel that counts requests and exercises the operations behind the
// common actions.
//
// The numbers are printed rather than asserted, except where a regression would
// be a real cost — a test that fails when someone shaves a request off would be
// worse than no test.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"pteroclient-wails/pkg/pterodactyl"
)

// countingPanel is a Pterodactyl-shaped server that records what was asked of
// it.
type countingPanel struct {
	mu       sync.Mutex
	requests []string
	server   *httptest.Server
	files    map[string]string
}

func newPanel(t *testing.T) *countingPanel {
	t.Helper()

	p := &countingPanel{files: map[string]string{}}
	mux := http.NewServeMux()

	record := func(kind string, h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			p.mu.Lock()
			p.requests = append(p.requests, kind)
			p.mu.Unlock()
			h(w, r)
		}
	}

	mux.HandleFunc("/api/client/servers/srv/files/list", record("list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"attributes": map[string]interface{}{
					"name": "config.yml", "size": 1234, "is_file": true,
					"mode": "-rw-r--r--", "modified_at": "2026-08-01T10:00:00Z",
				}},
			},
		})
	}))

	mux.HandleFunc("/api/client/servers/srv/files/contents", record("contents", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "name: value\n")
	}))

	mux.HandleFunc("/api/client/servers/srv/files/download", record("signed-url", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"attributes": map[string]interface{}{
				"url": p.server.URL + "/dl/blob",
			},
		})
	}))

	mux.HandleFunc("/dl/blob", record("download", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "name: value\n")
	}))

	mux.HandleFunc("/api/client/servers/srv/files/write", record("write", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.HandleFunc("/api/client/servers/srv/files/delete", record("delete", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	p.server = httptest.NewServer(mux)
	t.Cleanup(p.server.Close)
	return p
}

func (p *countingPanel) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = nil
}

func (p *countingPanel) took() (int, string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	counts := map[string]int{}
	for _, r := range p.requests {
		counts[r]++
	}
	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, fmt.Sprintf("%s×%d", k, counts[k]))
	}
	sort.Strings(kinds)
	return len(p.requests), strings.Join(kinds, ", ")
}

// TestRoundTripsPerAction reports what each action costs in requests.
func TestRoundTripsPerAction(t *testing.T) {
	panel := newPanel(t)
	client := pterodactyl.NewClient(panel.server.URL, "key", "srv")
	defer client.Close()

	t.Log("requests to the panel, per action:")

	// Browsing a folder.
	panel.reset()
	if _, err := client.ListFiles("/"); err != nil {
		t.Fatalf("list: %v", err)
	}
	n, detail := panel.took()
	t.Logf("  browse a folder ........ %d  (%s)", n, detail)

	// Opening a file for editing, as ReadFileForEdit does it: the download
	// endpoint, which is a signed URL and then the blob. No listing — the
	// download enforces the size cap itself.
	panel.reset()
	if _, err := client.DownloadFileBytes("/config.yml", 1<<20); err != nil {
		t.Fatalf("download: %v", err)
	}
	n, detail = panel.took()
	t.Logf("  open a file ............ %d  (%s)", n, detail)
	if n != 2 {
		t.Errorf("opening a file took %d requests, expected 2", n)
	}

	// Saving: read what is there to compare, then write.
	panel.reset()
	if _, err := client.GetFileContent("/config.yml"); err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := client.SaveFileContent("/config.yml", "name: other\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	n, detail = panel.took()
	t.Logf("  save a file ............ %d  (%s)", n, detail)

	// Deleting one file: the plan lists, the delete re-lists and removes.
	panel.reset()
	if _, err := client.ListFiles("/"); err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := client.ListFiles("/"); err != nil {
		t.Fatalf("relist: %v", err)
	}
	if _, err := client.DownloadFileBytes("/config.yml", 1<<20); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if err := client.DeleteFiles("/", []string{"config.yml"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	n, detail = panel.took()
	t.Logf("  delete one file ........ %d  (%s)", n, detail)
}

// TestDownloadCostsTwoRequests pins the shape of the download path.
//
// Every read of a file goes through it — opening one in the editor, capturing
// one before a delete, copying between servers — so if it ever becomes three,
// everything gets slower at once.
func TestDownloadCostsTwoRequests(t *testing.T) {
	panel := newPanel(t)
	client := pterodactyl.NewClient(panel.server.URL, "key", "srv")
	defer client.Close()

	panel.reset()
	if _, err := client.DownloadFileBytes("/config.yml", 1<<20); err != nil {
		t.Fatalf("download: %v", err)
	}

	n, detail := panel.took()
	if n != 2 {
		t.Errorf("a download took %d requests (%s); it is a signed URL and then the blob", n, detail)
	}
}
