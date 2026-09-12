package pteroapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// frame is one websocket message in the panel's console protocol.
type frame struct {
	Event string            `json:"event"`
	Args  []json.RawMessage `json:"args"`
}

// fakeConsole stands in for a panel's console: it serves the credentials
// route and a websocket that speaks the same events the real one does.
type fakeConsole struct {
	server *httptest.Server

	mu       sync.Mutex
	received []frame // what the client sent us
	tokens   int     // how many credential fetches happened

	// script runs once the client has authenticated, and is where a test
	// decides what the server "says".
	script func(send func(event string, args ...interface{}))
}

func newFakeConsole(t *testing.T, script func(send func(event string, args ...interface{}))) *fakeConsole {
	t.Helper()
	console := &fakeConsole{script: script}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/client/servers/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/websocket") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		console.mu.Lock()
		console.tokens++
		token := fmt.Sprintf("tok-%d", console.tokens)
		console.mu.Unlock()

		socket := "ws" + strings.TrimPrefix(console.server.URL, "http") + "/ws"
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]string{"token": token, "socket": socket},
		})
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		send := func(event string, args ...interface{}) {
			if args == nil {
				args = []interface{}{}
			}
			_ = conn.WriteJSON(map[string]interface{}{"event": event, "args": args})
		}

		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var incoming frame
			if json.Unmarshal(raw, &incoming) != nil {
				continue
			}

			console.mu.Lock()
			console.received = append(console.received, incoming)
			console.mu.Unlock()

			if incoming.Event == "auth" {
				send("auth success")
				if console.script != nil {
					console.script(send)
				}
			}
		}
	})

	console.server = httptest.NewServer(mux)
	t.Cleanup(console.server.Close)
	return console
}

// sent returns the events the client sent, in order.
func (f *fakeConsole) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	events := make([]string, 0, len(f.received))
	for _, received := range f.received {
		events = append(events, received.Event)
	}
	return events
}

// waitForSent waits for an event to arrive at the fake panel.
//
// A read that ends on an "until" match returns as soon as the line arrives,
// which can be before the fake server's own read loop has picked up the frame
// the client wrote just after authenticating. Asserting on sent() directly
// therefore races the test against itself.
func (f *fakeConsole) waitForSent(t *testing.T, event string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if contains(f.sent(), event) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("%q was never sent; the client sent %v", event, f.sent())
}

func (f *fakeConsole) client() *Client { return New(f.server.URL, "ptlc_test") }

func TestTailConsoleAuthenticatesAndCollectsTheBacklog(t *testing.T) {
	console := newFakeConsole(t, func(send func(string, ...interface{})) {
		send("status", "running")
		send("logs", "[12:00:00 INFO]: Starting server\n[12:00:01 INFO]: Done (1.2s)!")
		send("console output", "\x1b[32m[12:00:02 INFO]\x1b[0m: Player joined")
	})

	tail, err := console.client().TailConsole(context.Background(), ConsoleOptions{
		Server: "abc",
		Until:  "Player joined",
		Wait:   3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	if tail.State != "running" {
		t.Errorf("state was %q", tail.State)
	}
	if len(tail.Lines) != 3 {
		t.Fatalf("expected three lines, got %#v", tail.Lines)
	}
	// Colour escapes are a third of the bytes of a log line and mean
	// nothing to a reader that is not a terminal.
	if tail.Lines[2] != "[12:00:02 INFO]: Player joined" {
		t.Errorf("ANSI was not stripped: %q", tail.Lines[2])
	}
	if !tail.Matched {
		t.Error("the until match was not reported")
	}

	console.waitForSent(t, "send logs")
	events := console.sent()
	if len(events) == 0 || events[0] != "auth" {
		t.Fatalf("the socket has to authenticate first, sent %v", events)
	}
}

func TestTailConsoleKeepsANSIWhenAsked(t *testing.T) {
	console := newFakeConsole(t, func(send func(string, ...interface{})) {
		send("console output", "\x1b[31mred\x1b[0m")
	})

	tail, err := console.client().TailConsole(context.Background(), ConsoleOptions{
		Server:   "abc",
		Until:    "red",
		Wait:     3 * time.Second,
		KeepANSI: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tail.Lines) != 1 || !strings.Contains(tail.Lines[0], "\x1b[31m") {
		t.Errorf("escapes were stripped anyway: %q", tail.Lines)
	}
}

func TestTailConsoleSendsTheCommandAfterAuth(t *testing.T) {
	console := newFakeConsole(t, func(send func(string, ...interface{})) {
		send("console output", "There are 3 of a max of 20 players online")
	})

	tail, err := console.client().TailConsole(context.Background(), ConsoleOptions{
		Server:      "abc",
		Command:     "list",
		Until:       "players online",
		Wait:        3 * time.Second,
		SkipBacklog: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tail.Lines) != 1 {
		t.Fatalf("expected the command's output, got %#v", tail.Lines)
	}

	console.waitForSent(t, "send command")
	events := console.sent()
	// The panel drops frames that arrive before the socket is
	// authenticated, so order matters here.
	if events[0] != "auth" {
		t.Fatalf("sent %v", events)
	}
	if contains(events, "send logs") {
		t.Errorf("skip_backlog was ignored: %v", events)
	}
}

func TestTailConsoleSendsThePowerSignal(t *testing.T) {
	console := newFakeConsole(t, func(send func(string, ...interface{})) {
		send("status", "starting")
		send("console output", "Preparing level")
	})

	if _, err := console.client().TailConsole(context.Background(), ConsoleOptions{
		Server: "abc",
		Power:  "start",
		Until:  "Preparing level",
		Wait:   3 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}

	console.waitForSent(t, "set state")
}

func TestTailConsoleRenewsAnExpiringToken(t *testing.T) {
	console := newFakeConsole(t, func(send func(string, ...interface{})) {
		send("token expiring")
		send("console output", "still here")
	})

	tail, err := console.client().TailConsole(context.Background(), ConsoleOptions{
		Server: "abc",
		Until:  "still here",
		Wait:   3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tail.Note != "" {
		t.Errorf("renewal should be silent, got note %q", tail.Note)
	}

	// Renewing in place is what the panel's own console does: a fresh token
	// and another auth frame on the same socket. The second frame may still
	// be in flight when the read returns, so wait for it.
	authFrames := 0
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		authFrames = 0
		for _, event := range console.sent() {
			if event == "auth" {
				authFrames++
			}
		}
		if authFrames >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if authFrames < 2 {
		t.Errorf("expected a second auth frame, saw %d", authFrames)
	}

	console.mu.Lock()
	tokens := console.tokens
	console.mu.Unlock()
	if tokens < 2 {
		t.Errorf("a fresh token was never fetched, saw %d fetches", tokens)
	}
}

func TestTailConsoleCapsTheLineCountKeepingTheNewest(t *testing.T) {
	console := newFakeConsole(t, func(send func(string, ...interface{})) {
		for i := 1; i <= 10; i++ {
			send("console output", fmt.Sprintf("line %d", i))
		}
		send("console output", "END")
	})

	tail, err := console.client().TailConsole(context.Background(), ConsoleOptions{
		Server:   "abc",
		Until:    "END",
		Wait:     3 * time.Second,
		MaxLines: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tail.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %#v", tail.Lines)
	}
	// A server running for a week has a backlog no caller wants whole, and
	// the newest lines are the ones that explain what just happened.
	if tail.Lines[2] != "END" {
		t.Errorf("the newest line was dropped: %#v", tail.Lines)
	}
	if !tail.Truncated {
		t.Error("truncation was not reported")
	}
}

func TestTailConsoleStopsAtTheWaitWindow(t *testing.T) {
	console := newFakeConsole(t, nil) // says nothing at all

	started := time.Now()
	tail, err := console.client().TailConsole(context.Background(), ConsoleOptions{
		Server: "abc",
		Wait:   300 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Errorf("the read ran for %s; the window should have ended it", elapsed)
	}
	if len(tail.Lines) != 0 {
		t.Errorf("got lines from a silent server: %#v", tail.Lines)
	}
}

func TestTailConsoleReportsAMissingCredentialsRoute(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"errors":[{"code":"Forbidden","detail":"You do not have permission to connect."}]}`))
	}))
	defer panel.Close()

	_, err := New(panel.URL, "k").TailConsole(context.Background(), ConsoleOptions{Server: "abc", Wait: time.Second})
	if err == nil {
		t.Fatal("expected the refusal to surface")
	}
	if !strings.Contains(err.Error(), "permission") {
		t.Errorf("the panel's reason was lost: %v", err)
	}
}

func TestTailConsoleNeedsAServer(t *testing.T) {
	if _, err := New("https://panel.example.com", "k").TailConsole(context.Background(),
		ConsoleOptions{}); err == nil {
		t.Error("a console read with no server should be refused")
	}
}

func TestTailConsoleHonoursContextCancellation(t *testing.T) {
	console := newFakeConsole(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	started := time.Now()
	// A generous window must not outlive the caller's own deadline.
	if _, err := console.client().TailConsole(ctx, ConsoleOptions{
		Server: "abc",
		Wait:   30 * time.Second,
	}); err != nil && !strings.Contains(err.Error(), "context") {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("the read ignored the context deadline, ran for %s", elapsed)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
