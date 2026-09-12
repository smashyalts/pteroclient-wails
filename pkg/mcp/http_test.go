package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testServerWithEcho() *Server {
	s := NewServer("test", "0.1")
	s.Register(Tool{
		Name:    "echo",
		Input:   In(),
		Handler: func(ctx context.Context, args Args) (string, error) { return "hello", nil },
	})
	return s
}

func TestHTTPRefusesToListenPubliclyWithoutAToken(t *testing.T) {
	// The tools behind this port can run commands on the user's game
	// servers and read every file on them. An open port doing that is not a
	// configuration choice worth honouring silently.
	if _, err := NewHTTPHandler(testServerWithEcho(), HTTPOptions{Addr: ":8472"}); err == nil {
		t.Fatal("a wildcard listener with no token should be refused")
	}
	if _, err := NewHTTPHandler(testServerWithEcho(), HTTPOptions{Addr: "0.0.0.0:8472"}); err == nil {
		t.Fatal("0.0.0.0 with no token should be refused")
	}
	if _, err := NewHTTPHandler(testServerWithEcho(), HTTPOptions{Addr: "127.0.0.1:8472"}); err != nil {
		t.Fatalf("loopback with no token is fine: %v", err)
	}
	if _, err := NewHTTPHandler(testServerWithEcho(), HTTPOptions{Addr: ":8472", Token: "secret"}); err != nil {
		t.Fatalf("a token makes a public listener acceptable: %v", err)
	}
}

func TestHTTPEnforcesTheBearerToken(t *testing.T) {
	handler, err := NewHTTPHandler(testServerWithEcho(), HTTPOptions{Addr: ":8472", Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	post := func(token string) int {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/mcp",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if code := post(""); code != http.StatusUnauthorized {
		t.Errorf("no token: got %d", code)
	}
	if code := post("Bearer wrong"); code != http.StatusUnauthorized {
		t.Errorf("wrong token: got %d", code)
	}
	if code := post("Bearer secret"); code != http.StatusOK {
		t.Errorf("right token: got %d", code)
	}
	// Case-insensitive scheme, as the HTTP spec requires.
	if code := post("bearer secret"); code != http.StatusOK {
		t.Errorf("lowercase scheme: got %d", code)
	}
}

func TestHTTPHandshakeAndCall(t *testing.T) {
	handler, err := NewHTTPHandler(testServerWithEcho(), HTTPOptions{Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	call := func(body string) (*http.Response, map[string]interface{}) {
		resp, err := http.Post(server.URL+"/mcp", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var decoded map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&decoded)
		return resp, decoded
	}

	resp, reply := call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	if resp.Header.Get("Mcp-Session-Id") == "" {
		t.Error("no session id was issued")
	}
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("content type was %q", resp.Header.Get("Content-Type"))
	}
	if reply["result"] == nil {
		t.Fatalf("initialize failed: %v", reply)
	}

	_, reply = call(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo"}}`)
	content := reply["result"].(map[string]interface{})["content"].([]interface{})[0]
	if content.(map[string]interface{})["text"] != "hello" {
		t.Errorf("call result was %v", reply["result"])
	}
}

func TestHTTPNotificationIsAcknowledgedWithNoBody(t *testing.T) {
	handler, _ := NewHTTPHandler(testServerWithEcho(), HTTPOptions{Addr: "127.0.0.1:0"})
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Post(server.URL+"/mcp", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected 202 for a notification, got %d", resp.StatusCode)
	}
}

func TestHTTPGetIsNotAStream(t *testing.T) {
	handler, _ := NewHTTPHandler(testServerWithEcho(), HTTPOptions{Addr: "127.0.0.1:0"})
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestHTTPRejectsAStrangeBrowserOrigin(t *testing.T) {
	// Without this, a page the user happens to visit could drive a
	// localhost MCP server from the user's own network position.
	handler, _ := NewHTTPHandler(testServerWithEcho(), HTTPOptions{Addr: "127.0.0.1:0"})
	server := httptest.NewServer(handler)
	defer server.Close()

	send := func(origin string) int {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/mcp",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if code := send("https://evil.example.com"); code != http.StatusForbidden {
		t.Errorf("a foreign browser origin should be refused, got %d", code)
	}
	// Non-browser clients send no Origin at all and must keep working.
	if code := send(""); code != http.StatusOK {
		t.Errorf("no origin should be allowed, got %d", code)
	}
	if code := send("http://localhost:3000"); code != http.StatusOK {
		t.Errorf("a loopback origin should be allowed, got %d", code)
	}
}

func TestHTTPAllowsAConfiguredOrigin(t *testing.T) {
	handler, _ := NewHTTPHandler(testServerWithEcho(), HTTPOptions{
		Addr:           "127.0.0.1:0",
		AllowedOrigins: []string{"https://console.example.com"},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Origin", "https://console.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("an allowlisted origin was refused with %d", resp.StatusCode)
	}
}

func TestHTTPCapsTheRequestBody(t *testing.T) {
	handler, _ := NewHTTPHandler(testServerWithEcho(), HTTPOptions{Addr: "127.0.0.1:0", MaxBodyBytes: 64})
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Post(server.URL+"/mcp", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"`+
			strings.Repeat("x", 200)+`"}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", resp.StatusCode)
	}
}

func TestHTTPHealthzNeedsNoToken(t *testing.T) {
	// A container health check cannot carry the token, and the endpoint
	// reveals nothing.
	handler, _ := NewHTTPHandler(testServerWithEcho(), HTTPOptions{Addr: ":8472", Token: "secret"})
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz returned %d", resp.StatusCode)
	}
}

func TestHTTPRejectsAStaleSession(t *testing.T) {
	handler, _ := NewHTTPHandler(testServerWithEcho(), HTTPOptions{Addr: "127.0.0.1:0"})
	server := httptest.NewServer(handler)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Mcp-Session-Id", "from-another-process")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// 404 is what tells a client to start a new session rather than retry.
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown session, got %d", resp.StatusCode)
	}
}
