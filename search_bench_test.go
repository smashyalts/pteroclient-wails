package main

// How long does a recursive search take, and where does the time go?
//
// The panel has no search endpoint, so a search is one listing request per
// folder. That makes it network-bound, and the only things the code can change
// are how many requests are in flight, how quickly a worker picks up the next
// folder, and how much work each listing costs once it arrives. This stands up
// a fake panel with a synthetic tree and a settable latency so those three can
// be told apart.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pteroclient-wails/pkg/pterodactyl"
)

// fakeTree serves a tree that is `breadth` folders wide and `depth` deep, with
// `files` files in every folder.
type fakeTree struct {
	srv      *httptest.Server
	breadth  int
	depth    int
	files    int
	latency  time.Duration
	requests int64
}

func newTree(t testing.TB, breadth, depth, files int, latency time.Duration) *fakeTree {
	tree := &fakeTree{breadth: breadth, depth: depth, files: files, latency: latency}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/client/servers/srv/files/list", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&tree.requests, 1)
		if tree.latency > 0 {
			time.Sleep(tree.latency)
		}

		dir := r.URL.Query().Get("directory")
		level := 0
		if dir != "/" {
			level = strings.Count(strings.Trim(dir, "/"), "/") + 1
		}

		type attrs struct {
			Name      string `json:"name"`
			Size      int64  `json:"size"`
			IsFile    bool   `json:"is_file"`
			IsSymlink bool   `json:"is_symlink"`
		}
		type object struct {
			Object     string `json:"object"`
			Attributes attrs  `json:"attributes"`
		}

		var data []object
		if level < tree.depth {
			for i := 0; i < tree.breadth; i++ {
				data = append(data, object{Object: "file_object",
					Attributes: attrs{Name: "folder" + strconv.Itoa(i)}})
			}
		}
		for i := 0; i < tree.files; i++ {
			// One name in every folder carries the needle, so a search has
			// something to find at every level.
			name := "Entity" + strconv.Itoa(i) + ".yml"
			if i == 0 {
				name = "config.yml"
			}
			data = append(data, object{Object: "file_object",
				Attributes: attrs{Name: name, Size: 1024, IsFile: true}})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"object": "list", "data": data})
	})

	// Whatever else the client asks for on the way up.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	})

	tree.srv = httptest.NewServer(mux)
	t.Cleanup(tree.srv.Close)
	return tree
}

// runSearch is SearchFiles' engine without the config plumbing around it: the
// same walk, the same workers, the same client.
func runSearch(tb testing.TB, tree *fakeTree, query string, workers int) *SearchOutcome {
	tb.Helper()

	client := pterodactyl.NewClient(tree.srv.URL, "key", "srv")
	defer client.Close()

	app := &App{}
	out := &SearchOutcome{Query: query, Root: "/", Hits: []SearchHit{}}
	matcher, err := newSearchMatcher(query)
	if err != nil {
		tb.Fatalf("matcher: %v", err)
	}
	walk := newSearchWalk(app, 1, matcher, out)
	walk.push("/", 0)

	ctx, cancel := context.WithTimeout(context.Background(), searchDeadline)
	defer cancel()

	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			walk.wake()
		case <-stopped:
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			walk.run(ctx, client)
		}()
	}
	wg.Wait()
	close(stopped)
	return out
}

// A search over a tree of a realistic shape, with a latency a real panel has.
func TestSearchSpeed(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}

	for _, latency := range []time.Duration{0, 15 * time.Millisecond} {
		tree := newTree(t, 4, 4, 25, latency)

		for _, workers := range []int{1, 8, 16, 24, 32} {
			atomic.StoreInt64(&tree.requests, 0)
			started := time.Now()
			out := runSearch(t, tree, "config", workers)
			took := time.Since(started)

			t.Logf("latency %-5v  workers %2d  %4d folders  %5d entries  %4d hits  %v",
				latency, workers, out.Folders, out.Scanned, len(out.Hits), took.Round(time.Millisecond))

			if len(out.Hits) == 0 {
				t.Fatal("found nothing; the tree or the matcher is wrong")
			}
		}
	}

	// A narrow, deep tree: the shape where the queue keeps running dry and a
	// worker's wait matters more than how many of them there are.
	deep := newTree(t, 2, 12, 6, 15*time.Millisecond)
	for _, workers := range []int{8, 16} {
		started := time.Now()
		out := runSearch(t, deep, "config", workers)
		t.Logf("narrow+deep  workers %2d  %4d folders  %5d entries  %v",
			workers, out.Folders, out.Scanned, time.Since(started).Round(time.Millisecond))
	}
}

// Just the matching, with no network in the way: how much does one listing
// cost to scan once it has arrived?
func BenchmarkSearchMatch(b *testing.B) {
	names := make([]string, 0, 5000)
	for i := 0; i < 5000; i++ {
		names = append(names, fmt.Sprintf("SomePlugin%d-1.20.4-SNAPSHOT.jar", i))
	}
	needle := "snapshot"

	b.ResetTimer()
	b.ReportAllocs()
	hits := 0
	for i := 0; i < b.N; i++ {
		for _, name := range names {
			if strings.Contains(strings.ToLower(name), needle) {
				hits++
			}
		}
	}
	if hits == 0 {
		b.Fatal("matched nothing")
	}
}

// A queue on a condition variable hangs if a wake-up is ever missed, and the
// deadline would hide it as a slow search. This runs the walk many times over,
// cancelling at every point in its life, and fails if any of them does not come
// back promptly.
func TestSearchAlwaysFinishes(t *testing.T) {
	tree := newTree(t, 3, 3, 8, time.Millisecond)

	for i := 0; i < 40; i++ {
		client := pterodactyl.NewClient(tree.srv.URL, "key", "srv")

		matcher, err := newSearchMatcher("config")
		if err != nil {
			t.Fatal(err)
		}
		out := &SearchOutcome{Hits: []SearchHit{}}
		walk := newSearchWalk(&App{}, 1, matcher, out)
		walk.push("/", 0)

		// Cancelled anywhere from before it starts to well after it ends.
		ctx, cancel := context.WithTimeout(context.Background(),
			time.Duration(i)*time.Millisecond)

		stopped := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				walk.wake()
			case <-stopped:
			}
		}()

		finished := make(chan struct{})
		go func() {
			var wg sync.WaitGroup
			for w := 0; w < searchWorkers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					walk.run(ctx, client)
				}()
			}
			wg.Wait()
			close(finished)
		}()

		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Fatalf("cancelled at %dms: the walk never came back — a wake-up was missed", i)
		}

		close(stopped)
		cancel()
		client.Close()
	}
}

// The panel rate limits per API key, and a recursive search asks it for a
// listing sixteen at a time — so 429 is a normal answer, not a failure. It used
// to be treated as one, and the folder was quietly left out of the results.
func TestRateLimitedListingIsWaitedOutNotDropped(t *testing.T) {
	var attempts int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt64(&attempts, 1) <= 2 && strings.Contains(r.URL.Path, "/files/list") {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"errors":[{"code":"TooManyRequestsHttpException"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"file_object",` +
			`"attributes":{"name":"config.yml","size":12,"is_file":true}}]}`))
	}))
	defer srv.Close()

	client := pterodactyl.NewClient(srv.URL, "key", "srv")
	defer client.Close()

	// NewClient probes the key on the way up; the count starts at the listing.
	atomic.StoreInt64(&attempts, 0)

	files, err := client.ListFiles("/")
	if err != nil {
		t.Fatalf("gave up on a rate limit instead of waiting: %v", err)
	}
	if len(files) != 1 || files[0].Name != "config.yml" {
		t.Fatalf("got %v, want the one file the panel served once it stopped refusing", files)
	}
	if got := atomic.LoadInt64(&attempts); got != 3 {
		t.Errorf("made %d attempts, want 3 (two refused, one served)", got)
	}
}
