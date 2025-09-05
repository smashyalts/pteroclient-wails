package pterodactyl

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

// ConsoleWebSocket manages the WebSocket connection for console output
type ConsoleWebSocket struct {
	conn       *websocket.Conn
	url        string
	token      string
	serverID   string
	panelOrigin string
	OnOutput   func(string)
	OnError    func(error)
}

// NewConsoleWebSocket creates a new console WebSocket connection
func NewConsoleWebSocket(socket, token, serverID string) *ConsoleWebSocket {
	// The socket URL from Pterodactyl is already a WebSocket URL
	// It typically looks like: wss://panel.example.com/api/servers/xxx/ws
	return &ConsoleWebSocket{
		url:      socket,
		token:    token,
		serverID: serverID,
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
	
	ws.conn = conn
	
	// Start reading messages
	go ws.readLoop()
	
	// According to Pterodactyl docs, we need to send an auth event after connecting
	// even though the token is in the URL
	authMsg := map[string]interface{}{
		"event": "auth",
		"args":  []string{ws.token},
	}
	
	if err := ws.conn.WriteJSON(authMsg); err != nil {
		ws.conn.Close()
		return fmt.Errorf("failed to send auth message: %w", err)
	}
	
	// Connected, authentication sent
	
	return nil
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
	
	return ws.conn.WriteJSON(msg)
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
	
	return ws.conn.WriteJSON(msg)
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
	
	return ws.conn.WriteJSON(msg)
}

// readLoop reads messages from the WebSocket
func (ws *ConsoleWebSocket) readLoop() {
	defer ws.Close()
	
	for {
		_, message, err := ws.conn.ReadMessage()
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
			// Token is expiring, need to refresh
			if ws.OnError != nil {
				ws.OnError(fmt.Errorf("WebSocket token expiring, please reconnect"))
			}
			
		case "token expired":
			// Token expired, close connection
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

// Close closes the WebSocket connection
func (ws *ConsoleWebSocket) Close() error {
	if ws.conn != nil {
		// Send close message
		ws.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		time.Sleep(100 * time.Millisecond)
		return ws.conn.Close()
	}
	return nil
}

// IsConnected returns true if the WebSocket is connected
func (ws *ConsoleWebSocket) IsConnected() bool {
	return ws.conn != nil
}
