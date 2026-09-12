// Package mcp is a small, dependency-free Model Context Protocol server.
//
// It implements the slice of the protocol a tool server actually needs:
// initialize, tools/list, tools/call, ping, and empty prompt/resource lists
// for clients that ask regardless of the advertised capabilities. Everything
// else is answered with "method not found" rather than guessed at.
//
// The transport is kept separate (see stdio.go and http.go) because the same
// registry has to serve a local stdio client and a self-hosted HTTP one, and
// the protocol does not change between them.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Protocol revisions this server knows how to speak, newest first. A client
// asking for one of these is answered in its own version; anything else is
// answered in the newest, which is what the spec asks for.
var supportedVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// JSON-RPC error codes. These are from the JSON-RPC 2.0 spec; MCP adds no
// codes of its own that a tool server needs.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// request is one incoming JSON-RPC message. A message with no ID is a
// notification and must not be answered.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Handler runs one tool call. args holds the raw JSON of each supplied
// argument, so a handler can tell "absent" from "null" — the difference
// between leaving a panel field alone and clearing it.
type Handler func(ctx context.Context, args Args) (string, error)

// Tool is one callable entry in the registry.
type Tool struct {
	Name        string
	Description string
	Input       Schema

	// ReadOnly and Destructive become the hints a client shows before
	// running a tool. They are advisory to the client; the real gate is
	// whether the tool was registered at all.
	ReadOnly    bool
	Destructive bool

	Handler Handler
}

// Server is a registry of tools plus the protocol plumbing to expose them.
type Server struct {
	name    string
	version string

	mu    sync.RWMutex
	tools map[string]Tool
	order []string

	// initialized turns true after the client's initialize. Tool calls
	// before it are refused: a client that skipped the handshake has not
	// agreed a protocol version, so nothing it sends can be trusted to mean
	// what this server thinks it means.
	initialized bool
	agreed      string // protocol version settled on with the client
}

// NewServer creates an empty server advertising the given implementation name.
func NewServer(name, version string) *Server {
	return &Server{
		name:    name,
		version: version,
		tools:   make(map[string]Tool),
	}
}

// Register adds a tool. A duplicate name replaces the earlier tool rather
// than being silently ignored, so a later registration wins predictably.
func (s *Server) Register(t Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tools[t.Name]; !exists {
		s.order = append(s.order, t.Name)
	}
	s.tools[t.Name] = t
}

// ToolNames returns the registered names in sorted order.
func (s *Server) ToolNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := append([]string(nil), s.order...)
	sort.Strings(names)
	return names
}

// ProtocolVersion reports the revision agreed with the client, empty before
// the handshake.
func (s *Server) ProtocolVersion() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.agreed
}

// Handle processes one raw JSON-RPC message and returns the raw reply, or nil
// when the message was a notification and takes no reply.
func (s *Server) Handle(ctx context.Context, raw []byte) []byte {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil
	}

	// A batch is a JSON array. Each member is handled on its own and the
	// replies are returned as an array, minus any notification.
	if strings.HasPrefix(trimmed, "[") {
		var batch []json.RawMessage
		if err := json.Unmarshal(raw, &batch); err != nil {
			return s.errorReply(nil, codeParseError, "malformed JSON batch")
		}
		replies := make([]json.RawMessage, 0, len(batch))
		for _, member := range batch {
			if reply := s.Handle(ctx, member); reply != nil {
				replies = append(replies, reply)
			}
		}
		if len(replies) == 0 {
			return nil
		}
		out, err := json.Marshal(replies)
		if err != nil {
			return s.errorReply(nil, codeInternalError, err.Error())
		}
		return out
	}

	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return s.errorReply(nil, codeParseError, "malformed JSON")
	}
	if req.Method == "" {
		return s.errorReply(req.ID, codeInvalidRequest, "missing method")
	}

	result, rpcErr := s.dispatch(ctx, req)

	// Notifications are answered with silence, including when they fail:
	// there is no ID to answer to.
	if len(req.ID) == 0 {
		return nil
	}
	if rpcErr != nil {
		return s.errorReply(req.ID, rpcErr.Code, rpcErr.Message)
	}
	out, err := json.Marshal(response{JSONRPC: "2.0", ID: req.ID, Result: result})
	if err != nil {
		return s.errorReply(req.ID, codeInternalError, err.Error())
	}
	return out
}

func (s *Server) dispatch(ctx context.Context, req request) (interface{}, *rpcError) {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req.Params), nil

	case "notifications/initialized", "notifications/cancelled":
		return nil, nil

	case "ping":
		// The spec's ping result is an empty object, not a pong.
		return map[string]interface{}{}, nil

	case "tools/list":
		return map[string]interface{}{"tools": s.toolDescriptors()}, nil

	case "tools/call":
		return s.handleCall(ctx, req.Params)

	// Declared capabilities do not include these, but clients ask anyway
	// and an empty list is friendlier than an error they log as a fault.
	case "prompts/list":
		return map[string]interface{}{"prompts": []interface{}{}}, nil
	case "resources/list":
		return map[string]interface{}{"resources": []interface{}{}}, nil
	case "resources/templates/list":
		return map[string]interface{}{"resourceTemplates": []interface{}{}}, nil

	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: "unknown method " + req.Method}
	}
}

func (s *Server) handleInitialize(params json.RawMessage) interface{} {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &p)

	agreed := supportedVersions[0]
	for _, known := range supportedVersions {
		if p.ProtocolVersion == known {
			agreed = known
			break
		}
	}

	s.mu.Lock()
	s.initialized = true
	s.agreed = agreed
	s.mu.Unlock()

	return map[string]interface{}{
		"protocolVersion": agreed,
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    s.name,
			"version": s.version,
		},
	}
}

func (s *Server) toolDescriptors() []interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]interface{}, 0, len(s.order))
	for _, name := range s.order {
		t := s.tools[name]
		out = append(out, map[string]interface{}{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.Input.JSON(),
			"annotations": map[string]interface{}{
				"readOnlyHint":    t.ReadOnly,
				"destructiveHint": t.Destructive,
				"idempotentHint":  t.ReadOnly,
				"openWorldHint":   true,
			},
		})
	}
	return out
}

func (s *Server) handleCall(ctx context.Context, params json.RawMessage) (interface{}, *rpcError) {
	var p struct {
		Name      string                     `json:"name"`
		Arguments map[string]json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "could not read call parameters"}
	}

	s.mu.RLock()
	tool, found := s.tools[p.Name]
	ready := s.initialized
	s.mu.RUnlock()

	if !ready {
		return nil, &rpcError{Code: codeInvalidRequest, Message: "initialize has not been called"}
	}
	if !found {
		return nil, &rpcError{Code: codeMethodNotFound, Message: "no tool named " + p.Name}
	}

	args := Args(p.Arguments)
	if missing := tool.Input.Missing(args); len(missing) > 0 {
		return toolError(fmt.Sprintf("missing required argument(s): %s", strings.Join(missing, ", "))), nil
	}

	text, err := tool.Handler(ctx, args)
	if err != nil {
		// A tool that failed is a result, not a protocol fault: the model
		// should see the panel's complaint and be able to act on it.
		return toolError(err.Error()), nil
	}
	if text == "" {
		text = "ok"
	}
	return map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": text},
		},
		"isError": false,
	}, nil
}

func toolError(message string) map[string]interface{} {
	return map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "Error: " + message},
		},
		"isError": true,
	}
}

func (s *Server) errorReply(id json.RawMessage, code int, message string) []byte {
	out, err := json.Marshal(response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	})
	if err != nil {
		// Cannot happen with these types, and there is nothing better to
		// send than a hand-written frame if it ever does.
		return []byte("{\"jsonrpc\":\"2.0\",\"error\":{\"code\":-32603,\"message\":\"internal error\"}}")
	}
	return out
}
