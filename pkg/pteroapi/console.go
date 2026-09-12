package pteroapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketCredentials is the short-lived token and socket URL the panel
// hands out for a server's console.
type WebSocketCredentials struct {
	Token  string `json:"token"`
	Socket string `json:"socket"`
}

// WebSocketCredentials fetches console credentials for a server.
func (c *Client) WebSocketCredentials(ctx context.Context, server string) (*WebSocketCredentials, error) {
	result, err := c.Send(ctx, Request{Method: http.MethodGet, Path: "/servers/" + server + "/websocket"})
	if err != nil {
		return nil, err
	}
	if result.Status < 200 || result.Status > 299 {
		return nil, describeFailure(result)
	}

	var envelope struct {
		Data WebSocketCredentials `json:"data"`
	}
	if err := json.Unmarshal(result.Body, &envelope); err != nil {
		return nil, fmt.Errorf("could not read the console credentials: %w", err)
	}
	if envelope.Data.Socket == "" || envelope.Data.Token == "" {
		return nil, fmt.Errorf("the panel returned no console credentials for %s", server)
	}
	return &envelope.Data, nil
}

// ConsoleOptions controls one console read.
type ConsoleOptions struct {
	Server string

	// Command, when set, is sent once the socket is authenticated and the
	// backlog has been requested, so its output lands in the same read.
	Command string

	// Power, when set, sends a power signal over the same socket. Same
	// reason: the state change and the lines it produces come back together.
	Power string

	// Wait is how long to keep reading after the backlog. Output from a
	// command does not arrive instantly, so this is where the caller trades
	// latency for completeness.
	Wait time.Duration

	// MaxLines caps what is returned, keeping the newest lines. A server
	// that has been running for a week has a backlog no caller wants whole.
	MaxLines int

	// Until stops the read early at the first line containing this text.
	// Matching is case-insensitive.
	Until string

	// SkipBacklog omits the panel's stored history, returning only what
	// arrives during the read. Use it when the point is a command's output
	// rather than the state of the server.
	SkipBacklog bool

	// KeepANSI leaves the colour escapes in place. They are stripped by
	// default: they are a third of the bytes of a Minecraft log line and
	// mean nothing to a reader that is not a terminal.
	KeepANSI bool
}

// ConsoleTail is the outcome of one console read.
type ConsoleTail struct {
	Server string   `json:"server"`
	State  string   `json:"state,omitempty"`
	Lines  []string `json:"lines"`

	// Stats is the last resource frame seen during the read, if any.
	Stats map[string]interface{} `json:"stats,omitempty"`

	// Truncated says the line cap dropped older lines.
	Truncated bool `json:"truncated,omitempty"`

	// Matched says the read stopped because Until was found.
	Matched bool `json:"matched_until,omitempty"`

	// Note carries anything the caller should know about how the read ended
	// — a token that could not be renewed, a socket the panel closed.
	Note string `json:"note,omitempty"`
}

// ansiEscape matches the CSI and OSC sequences a game server writes.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]|\x1b\\][^\x07]*\x07|\x1b[()][A-B0-9]")

// StripANSI removes terminal escape sequences from a console line.
func StripANSI(line string) string {
	return strings.TrimRight(ansiEscape.ReplaceAllString(line, ""), "\r")
}

// TailConsole opens the console websocket, optionally sends a command or a
// power signal, and collects output for the requested window.
//
// The panel's console is the only place some information exists — a crash
// stack, a plugin's startup complaint, the reason a start attempt gave up —
// and none of it is reachable from the REST routes. A bounded read is how a
// request/response tool can get at a stream.
func (c *Client) TailConsole(ctx context.Context, opts ConsoleOptions) (*ConsoleTail, error) {
	if opts.Server == "" {
		return nil, fmt.Errorf("no server given")
	}
	if opts.Wait <= 0 {
		opts.Wait = 5 * time.Second
	}
	if opts.Wait > 5*time.Minute {
		opts.Wait = 5 * time.Minute
	}
	if opts.MaxLines <= 0 {
		opts.MaxLines = 200
	}

	creds, err := c.WebSocketCredentials(ctx, opts.Server)
	if err != nil {
		return nil, err
	}

	// The panel checks Origin on the websocket upgrade and refuses a
	// mismatch, so it has to be the panel's own URL rather than absent.
	headers := http.Header{}
	headers.Set("Origin", c.baseURL)
	headers.Set("User-Agent", c.UserAgent)

	dialer := &websocket.Dialer{HandshakeTimeout: 15 * time.Second}

	separator := "?"
	if strings.Contains(creds.Socket, "?") {
		separator = "&"
	}
	conn, resp, err := dialer.DialContext(ctx, creds.Socket+separator+"token="+creds.Token, headers)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("could not open the console socket (status %d): %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("could not open the console socket: %w", err)
	}
	defer conn.Close()

	// The token travels in the URL and again in an auth frame; the panel's
	// own console sends both and rejects a socket that sends neither.
	send := func(event string, args ...interface{}) error {
		if args == nil {
			args = []interface{}{}
		}
		return conn.WriteJSON(map[string]interface{}{"event": event, "args": args})
	}
	if err := send("auth", creds.Token); err != nil {
		return nil, fmt.Errorf("could not authenticate the console socket: %w", err)
	}

	tail := &ConsoleTail{Server: opts.Server, Lines: []string{}}
	deadline := time.Now().Add(opts.Wait)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	// Requests are sent after "auth success" rather than immediately: the
	// panel drops frames that arrive before the socket is authenticated.
	pending := func() error {
		if !opts.SkipBacklog {
			if err := send("send logs", nil); err != nil {
				return err
			}
		}
		if opts.Power != "" {
			if err := send("set state", opts.Power); err != nil {
				return err
			}
		}
		if opts.Command != "" {
			if err := send("send command", opts.Command); err != nil {
				return err
			}
		}
		return nil
	}

	appendLine := func(text string) {
		for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
			if !opts.KeepANSI {
				line = StripANSI(line)
			}
			if strings.TrimSpace(line) == "" {
				continue
			}
			tail.Lines = append(tail.Lines, line)
			if opts.Until != "" && strings.Contains(strings.ToLower(line), strings.ToLower(opts.Until)) {
				tail.Matched = true
			}
		}
	}

	authenticated := false
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 || tail.Matched {
			break
		}
		if err := conn.SetReadDeadline(time.Now().Add(remaining)); err != nil {
			break
		}

		_, raw, err := conn.ReadMessage()
		if err != nil {
			// A read deadline is how a completed window ends, not a fault.
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				tail.Note = "the panel closed the console socket"
			} else if netErr, ok := err.(interface{ Timeout() bool }); !ok || !netErr.Timeout() {
				tail.Note = "console socket ended: " + err.Error()
			}
			break
		}

		var frame struct {
			Event string            `json:"event"`
			Args  []json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal(raw, &frame); err != nil {
			continue
		}

		switch frame.Event {
		case "auth success":
			authenticated = true
			if err := pending(); err != nil {
				// Nothing was asked for, so waiting out the window would
				// collect nothing. End the read instead.
				tail.Note = "could not send the console request: " + err.Error()
				deadline = time.Now()
			}

		case "console output", "install output", "daemon message":
			if len(frame.Args) > 0 {
				var text string
				if json.Unmarshal(frame.Args[0], &text) == nil {
					appendLine(text)
				}
			}

		case "logs":
			// The backlog arrives either as one blob or as an array of
			// lines, depending on panel version.
			if len(frame.Args) > 0 {
				var text string
				if json.Unmarshal(frame.Args[0], &text) == nil {
					appendLine(text)
					break
				}
				var lines []string
				if json.Unmarshal(frame.Args[0], &lines) == nil {
					for _, line := range lines {
						appendLine(line)
					}
				}
			}

		case "status":
			if len(frame.Args) > 0 {
				var state string
				if json.Unmarshal(frame.Args[0], &state) == nil {
					tail.State = state
				}
			}

		case "stats":
			if len(frame.Args) > 0 {
				var encoded string
				if json.Unmarshal(frame.Args[0], &encoded) == nil {
					var stats map[string]interface{}
					if json.Unmarshal([]byte(encoded), &stats) == nil {
						tail.Stats = stats
						if state, ok := stats["state"].(string); ok && tail.State == "" {
							tail.State = state
						}
					}
				}
			}

		case "token expiring":
			// Renewing in place is what the panel's own console does: a
			// fresh token and another auth frame on the same socket, with
			// nothing lost and nothing to reconnect.
			fresh, refreshErr := c.WebSocketCredentials(ctx, opts.Server)
			if refreshErr != nil {
				tail.Note = "could not renew the console token: " + refreshErr.Error()
			} else if err := send("auth", fresh.Token); err != nil {
				tail.Note = "could not re-authenticate the console socket: " + err.Error()
			}

		case "token expired":
			tail.Note = "the console token expired"
			deadline = time.Now() // end the read
		}
	}

	if !authenticated && tail.Note == "" && len(tail.Lines) == 0 {
		tail.Note = "the console socket never authenticated; check that the API key has websocket permission"
	}

	if len(tail.Lines) > opts.MaxLines {
		tail.Truncated = true
		tail.Lines = tail.Lines[len(tail.Lines)-opts.MaxLines:]
	}

	_ = conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))

	return tail, nil
}
