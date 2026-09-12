package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// exchange sends one JSON-RPC message and decodes the reply.
func exchange(t *testing.T, s *Server, message string) map[string]interface{} {
	t.Helper()
	raw := s.Handle(context.Background(), []byte(message))
	if raw == nil {
		t.Fatalf("no reply to %s", message)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("reply was not JSON: %v (%s)", err, raw)
	}
	return decoded
}

func handshake(t *testing.T, s *Server) {
	t.Helper()
	reply := exchange(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize",
      "params":{"protocolVersion":"2025-06-18","capabilities":{}}}`)
	if reply["error"] != nil {
		t.Fatalf("initialize failed: %v", reply["error"])
	}
}

func echoTool(name string) Tool {
	return Tool{
		Name:        name,
		Description: "echo",
		Input:       In(Str("text", "text to echo").Req()),
		Handler: func(ctx context.Context, args Args) (string, error) {
			return args.String("text", ""), nil
		},
	}
}

func TestInitializeAgreesOnAKnownVersion(t *testing.T) {
	s := NewServer("test", "0.1")
	reply := exchange(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize",
      "params":{"protocolVersion":"2024-11-05"}}`)

	result := reply["result"].(map[string]interface{})
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("a client asking for a supported version should be answered in it, got %v",
			result["protocolVersion"])
	}
	if _, ok := result["capabilities"].(map[string]interface{})["tools"]; !ok {
		t.Error("the tools capability was not advertised")
	}
}

func TestInitializeFallsBackForUnknownVersion(t *testing.T) {
	s := NewServer("test", "0.1")
	reply := exchange(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize",
      "params":{"protocolVersion":"1999-01-01"}}`)

	result := reply["result"].(map[string]interface{})
	if result["protocolVersion"] != supportedVersions[0] {
		t.Errorf("expected the newest known version, got %v", result["protocolVersion"])
	}
}

func TestNotificationsGetNoReply(t *testing.T) {
	s := NewServer("test", "0.1")
	// A reply to a notification is a protocol error at the client: there is
	// no id for it to match.
	if reply := s.Handle(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); reply != nil {
		t.Errorf("a notification was answered with %s", reply)
	}
}

func TestToolsListDescribesSchemaAndHints(t *testing.T) {
	s := NewServer("test", "0.1")
	s.Register(echoTool("echo"))
	s.Register(Tool{
		Name:        "wipe",
		Input:       In(),
		Destructive: true,
		Handler:     func(ctx context.Context, args Args) (string, error) { return "", nil },
	})
	handshake(t, s)

	reply := exchange(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := reply["result"].(map[string]interface{})["tools"].([]interface{})
	if len(tools) != 2 {
		t.Fatalf("expected two tools, got %d", len(tools))
	}

	first := tools[0].(map[string]interface{})
	schema := first["inputSchema"].(map[string]interface{})
	if schema["type"] != "object" {
		t.Errorf("schema type was %v", schema["type"])
	}
	required := schema["required"].([]interface{})
	if len(required) != 1 || required[0] != "text" {
		t.Errorf("required was %v", required)
	}

	second := tools[1].(map[string]interface{})
	if second["annotations"].(map[string]interface{})["destructiveHint"] != true {
		t.Error("the destructive hint did not reach the client")
	}
}

func TestToolCallReturnsTextContent(t *testing.T) {
	s := NewServer("test", "0.1")
	s.Register(echoTool("echo"))
	handshake(t, s)

	reply := exchange(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call",
      "params":{"name":"echo","arguments":{"text":"hello"}}}`)

	result := reply["result"].(map[string]interface{})
	if result["isError"] != false {
		t.Errorf("expected a successful result, got %v", result)
	}
	content := result["content"].([]interface{})[0].(map[string]interface{})
	if content["type"] != "text" || content["text"] != "hello" {
		t.Errorf("content was %v", content)
	}
}

func TestMissingRequiredArgumentIsAToolErrorNotAProtocolError(t *testing.T) {
	s := NewServer("test", "0.1")
	s.Register(echoTool("echo"))
	handshake(t, s)

	reply := exchange(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call",
      "params":{"name":"echo","arguments":{}}}`)

	// The model has to see what went wrong so it can retry with the
	// argument, which it cannot do if the client logs a transport fault.
	if reply["error"] != nil {
		t.Fatalf("a missing argument should not be a protocol error: %v", reply["error"])
	}
	result := reply["result"].(map[string]interface{})
	if result["isError"] != true {
		t.Fatalf("expected isError, got %v", result)
	}
	text := result["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "text") {
		t.Errorf("the message did not name the missing argument: %q", text)
	}
}

func TestHandlerFailureBecomesAToolError(t *testing.T) {
	s := NewServer("test", "0.1")
	s.Register(Tool{
		Name:  "boom",
		Input: In(),
		Handler: func(ctx context.Context, args Args) (string, error) {
			return "", fmt.Errorf("panel returned 409: server is suspended")
		},
	})
	handshake(t, s)

	reply := exchange(t, s, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"boom"}}`)
	result := reply["result"].(map[string]interface{})
	if result["isError"] != true {
		t.Fatalf("expected isError, got %v", result)
	}
	text := result["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "suspended") {
		t.Errorf("the panel's complaint was lost: %q", text)
	}
}

func TestCallBeforeInitializeIsRefused(t *testing.T) {
	s := NewServer("test", "0.1")
	s.Register(echoTool("echo"))

	reply := exchange(t, s, `{"jsonrpc":"2.0","id":6,"method":"tools/call",
      "params":{"name":"echo","arguments":{"text":"hi"}}}`)
	if reply["error"] == nil {
		t.Error("a tool call without a handshake should be refused")
	}
}

func TestUnknownToolAndMethod(t *testing.T) {
	s := NewServer("test", "0.1")
	handshake(t, s)

	reply := exchange(t, s, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"nope"}}`)
	if reply["error"] == nil {
		t.Error("an unknown tool should be a method-not-found error")
	}

	reply = exchange(t, s, `{"jsonrpc":"2.0","id":8,"method":"does/not/exist"}`)
	errObj := reply["error"].(map[string]interface{})
	if errObj["code"].(float64) != codeMethodNotFound {
		t.Errorf("code was %v", errObj["code"])
	}
}

func TestPingAndEmptyListsAreAnswered(t *testing.T) {
	s := NewServer("test", "0.1")
	handshake(t, s)

	if reply := exchange(t, s, `{"jsonrpc":"2.0","id":9,"method":"ping"}`); reply["error"] != nil {
		t.Errorf("ping failed: %v", reply["error"])
	}
	// Clients ask for these regardless of the advertised capabilities; an
	// empty list is friendlier than an error they log as a fault.
	for _, method := range []string{"prompts/list", "resources/list", "resources/templates/list"} {
		reply := exchange(t, s, fmt.Sprintf(`{"jsonrpc":"2.0","id":10,"method":%q}`, method))
		if reply["error"] != nil {
			t.Errorf("%s returned %v", method, reply["error"])
		}
	}
}

func TestMalformedJSONBecomesAParseError(t *testing.T) {
	s := NewServer("test", "0.1")
	reply := exchange(t, s, `{"jsonrpc":"2.0","id":1,`)
	errObj := reply["error"].(map[string]interface{})
	if errObj["code"].(float64) != codeParseError {
		t.Errorf("code was %v", errObj["code"])
	}
}

func TestBatchRepliesOnlyToRequests(t *testing.T) {
	s := NewServer("test", "0.1")
	s.Register(echoTool("echo"))
	handshake(t, s)

	raw := s.Handle(context.Background(), []byte(`[
      {"jsonrpc":"2.0","method":"notifications/initialized"},
      {"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"echo","arguments":{"text":"a"}}}
    ]`))

	var replies []map[string]interface{}
	if err := json.Unmarshal(raw, &replies); err != nil {
		t.Fatalf("batch reply was not an array: %s", raw)
	}
	if len(replies) != 1 {
		t.Fatalf("expected one reply for one request, got %d", len(replies))
	}
	if replies[0]["id"].(float64) != 11 {
		t.Errorf("wrong id echoed: %v", replies[0]["id"])
	}
}

func TestRegisteringTheSameNameReplaces(t *testing.T) {
	s := NewServer("test", "0.1")
	s.Register(echoTool("echo"))
	s.Register(Tool{
		Name:    "echo",
		Input:   In(),
		Handler: func(ctx context.Context, args Args) (string, error) { return "replaced", nil },
	})
	if names := s.ToolNames(); len(names) != 1 {
		t.Fatalf("expected one tool, got %v", names)
	}

	handshake(t, s)
	reply := exchange(t, s, `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"echo"}}`)
	text := reply["result"].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})["text"]
	if text != "replaced" {
		t.Errorf("the later registration did not win: %v", text)
	}
}
