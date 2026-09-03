package pterodactyl

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ConsoleWebSocket manages the WebSocket connection for console output.
//
// The panel's token is short lived. It sends "token expiring" a minute or so
// before the end and "token expired" at it, and a client that does nothing with
// the first one gets the second. Renewing in place — a fresh token and another
// auth frame on the same socket — is what the panel's own console does, and it
// costs nothing visible.
type ConsoleWebSocket struct {
	// mu guards conn. Wails runs every binding call on its own goroutine, so
	// SendCommand and Close can land while the read loop is tearing down.
	mu          sync.Mutex
	conn        *websocket.Conn
	url         string
	token       string
	serverID    string
	panelOrigin string

	OnOutput func(string)
	OnError  func(error)

	// OnClose fires once when the connection is finished, however it ended.
	// Without it the UI had no way to know, and went on saying "Disconnect"
	// over a socket that was gone.
	OnClose func()

	// RefreshToken fetches a new websocket token from the panel. When it is
	// set, an expiring token is renewed in place instead of being reported.
	RefreshToken func() (string, error)

	closed bool
}

// NewConsoleWebSocket creates a new console WebSocket connection
func NewConsoleWebSocket(socket, token, serverID string) *ConsoleWebSocket {
	// The socket URL from Pterodactyl is already a WebSocket URL
	// It typically looks like: wss://panel.example.com/api/servers/xxx/ws
	return &ConsoleWebSocket{
		url:         socket,
		token:       token,
		serverID:    serverID,
		panelOrigin: "https://mc.bloom.host", // Default for Bloom Host
	}
}

// NewConsoleWebSocketWithOrigin creates a new console WebSocket connection with custom origin
func NewConsoleWebSocketWithOrigin(socket, token, serverID, panelOrigin string) *ConsoleWebSocket {
	return &ConsoleWebSocket{
		url:         socket,
		token:       token,
		serverID:    serverID,
		panelOrigin: panelOrigin,
	}
}

// Connect establishes the WebSocket connection
func (ws *ConsoleWebSocket) Connect() error {
	// Append the token to the WebSocket URL as a query parameter
	separator := "?"
	if strings.Contains(ws.url, "?") {
		separator = "&"
	}
	connectURL := fmt.Sprintf("%s%stoken=%s", ws.url, separator, ws.token)

	// Set up headers
	headers := http.Header{}
	// Use the panel origin (e.g., https://mc.bloom.host)
	origin := ws.panelOrigin
	if origin == "" {
		// Fallback: extract from WebSocket URL if no origin specified
		origin = ws.url
		origin = strings.Replace(origin, "wss://", "https://", 1)
		origin = strings.Replace(origin, "ws://", "http://", 1)
		if idx := strings.Index(origin, "/api/"); idx > 0 {
			origin = origin[:idx]
		}
	}
	headers.Add("Origin", origin)
	headers.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	// Connect to WebSocket
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, resp, err := dialer.Dial(connectURL, headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("failed to connect to websocket: %w (status: %d)", err, resp.StatusCode)
		}
		return fmt.Errorf("failed to connect to websocket: %w", err)
	}

	ws.mu.Lock()
	ws.conn = conn
	ws.closed = false
	ws.mu.Unlock()

	// According to Pterodactyl docs, we need to send an auth event after connecting
	// even though the token is in the URL
	if err := ws.write(map[string]interface{}{
		"event": "auth",
		"args":  []string{ws.token},
	}); err != nil {
		ws.mu.Lock()
		ws.conn = nil
		ws.closed = true
		ws.mu.Unlock()
		conn.Close()
		return fmt.Errorf("failed to send auth message: %w", err)
	}

	// Only once the socket is usable, so a failed auth does not fire OnClose
	// for a connection the caller was told never opened.
	go ws.readLoop(conn)

	return nil
}

// write sends one frame. Gorilla allows a single concurrent writer, and every
// send here arrives on its own goroutine.
func (ws *ConsoleWebSocket) write(msg interface{}) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.conn == nil {
		return fmt.Errorf("not connected")
	}
	return ws.conn.WriteJSON(msg)
}

// RequestLogs requests the console logs
func (ws *ConsoleWebSocket) RequestLogs() error {
	if ws.conn == nil {
		return fmt.Errorf("not connected")
	}

	msg := map[string]interface{}{
		"event": "send logs",
		"args":  []interface{}{nil},
	}

	return ws.write(msg)
}

// SendCommand sends a command to the console
func (ws *ConsoleWebSocket) SendCommand(command string) error {
	if ws.conn == nil {
		return fmt.Errorf("not connected")
	}

	msg := map[string]interface{}{
		"event": "send command",
		"args":  []string{command},
	}

	return ws.write(msg)
}

// SendPowerState sends a power state change
func (ws *ConsoleWebSocket) SendPowerState(state string) error {
	if ws.conn == nil {
		return fmt.Errorf("not connected")
	}

	msg := map[string]interface{}{
		"event": "set state",
		"args":  []string{state},
	}

	return ws.write(msg)
}

// readLoop reads messages from the WebSocket.
//
// It takes the connection it was started for rather than reading ws.conn, so a
// loop belonging to a socket that has since been replaced cannot tear down the
// new one.
func (ws *ConsoleWebSocket) readLoop(conn *websocket.Conn) {
	defer ws.finish(conn)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if ws.OnError != nil && !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				ws.OnError(err)
			}
			return
		}

		// Debug: log event types (not raw messages)
		// We'll log the event type after parsing instead

		// Parse the message
		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			continue // Skip invalid messages
		}

		// Handle different event types
		event, ok := msg["event"].(string)
		if !ok {
			continue
		}

		switch event {
		case "console output":
			// Extract console output
			if args, ok := msg["args"].([]interface{}); ok && len(args) > 0 {
				if output, ok := args[0].(string); ok && ws.OnOutput != nil {
					// Don't trim the output as it may contain important formatting
					ws.OnOutput(output)
				}
			}

		case "stats":
			// Server statistics update - silently handled
			// Stats can be processed by a separate handler if needed

		case "logs":
			// Initial console logs
			if args, ok := msg["args"].([]interface{}); ok && len(args) > 0 {
				if logs, ok := args[0].([]interface{}); ok {
					for _, log := range logs {
						if logStr, ok := log.(string); ok && ws.OnOutput != nil {
							ws.OnOutput(logStr)
						}
					}
				} else if logStr, ok := args[0].(string); ok && ws.OnOutput != nil {
					ws.OnOutput(logStr)
				}
			}

		case "token expiring":
			// The panel gives about a minute of warning. Renewing in place is
			// what its own console does: a fresh token and another auth frame
			// on the same socket, with nothing lost and nothing to reconnect.
			if err := ws.renew(); err != nil && ws.OnError != nil {
				ws.OnError(fmt.Errorf("could not renew the console token: %w", err))
			}

		case "token expired":
			// Only reached if the renewal above did not happen or did not
			// work. The socket is finished either way.
			if ws.OnError != nil {
				ws.OnError(fmt.Errorf("WebSocket token expired"))
			}
			return

		case "auth success":
			// Successfully authenticated - no message needed

		case "status":
			// Server status update
			if args, ok := msg["args"].([]interface{}); ok && len(args) > 0 {
				if status, ok := args[0].(string); ok && ws.OnOutput != nil {
					ws.OnOutput(fmt.Sprintf("[Server status: %s]", status))
				}
			}
		}
	}
}

// renew swaps in a fresh token without dropping the socket.
func (ws *ConsoleWebSocket) renew() error {
	if ws.RefreshToken == nil {
		return fmt.Errorf("no way to fetch a new token")
	}
	token, err := ws.RefreshToken()
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("the panel returned an empty token")
	}

	ws.mu.Lock()
	ws.token = token
	ws.mu.Unlock()

	return ws.write(map[string]interface{}{
		"event": "auth",
		"args":  []string{token},
	})
}

// finish marks the socket closed and tells the caller once.
//
// The old code left ws.conn set after the read loop ended, so IsConnected kept
// answering yes for a connection that was gone — which is why the button went
// on offering to disconnect from a console that had already expired.
func (ws *ConsoleWebSocket) finish(conn *websocket.Conn) {
	ws.mu.Lock()
	// A loop for a socket that has already been replaced must not clear the
	// live one.
	if ws.conn != nil && ws.conn != conn {
		ws.mu.Unlock()
		conn.Close()
		return
	}
	already := ws.closed
	ws.conn = nil
	ws.closed = true
	ws.mu.Unlock()

	conn.Close()

	if !already && ws.OnClose != nil {
		ws.OnClose()
	}
}

// Close closes the WebSocket connection.
func (ws *ConsoleWebSocket) Close() error {
	ws.mu.Lock()
	conn := ws.conn
	already := ws.closed
	ws.conn = nil
	ws.closed = true
	ws.mu.Unlock()

	if conn == nil {
		return nil
	}

	// Best effort: the peer may already be gone, and the close frame is a
	// courtesy rather than something to fail over.
	_ = conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	err := conn.Close()

	if !already && ws.OnClose != nil {
		ws.OnClose()
	}
	return err
}

// IsConnected returns true if the WebSocket is connected.
func (ws *ConsoleWebSocket) IsConnected() bool {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.conn != nil
}
