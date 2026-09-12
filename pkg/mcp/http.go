package mcp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPOptions configures the self-hosted transport.
type HTTPOptions struct {
	// Addr is the listen address, e.g. ":8472" or "127.0.0.1:8472".
	Addr string

	// Token, when set, must be presented as "Authorization: Bearer <token>".
	// A server reachable from anything but localhost without one is refused
	// at startup rather than left open.
	Token string

	// AllowedOrigins are the browser origins permitted to call this server.
	// Requests carrying no Origin — every non-browser MCP client — are
	// always allowed; a browser origin that is not listed is refused.
	//
	// This is the DNS-rebinding guard the MCP spec asks for: without it, a
	// page the user happens to visit could drive a localhost MCP server
	// using the user's own network position.
	AllowedOrigins []string

	// MaxBodyBytes caps one request. A file write carries its content in the
	// body, so the ceiling is high, but it is a ceiling.
	MaxBodyBytes int64

	// RequestTimeout bounds a single tool call. Console tailing waits on a
	// live server, so this is minutes rather than seconds.
	RequestTimeout time.Duration
}

func (o *HTTPOptions) applyDefaults() {
	if o.MaxBodyBytes <= 0 {
		o.MaxBodyBytes = 32 << 20 // 32 MiB
	}
	if o.RequestTimeout <= 0 {
		o.RequestTimeout = 5 * time.Minute
	}
}

// isLoopback reports whether the listen address is bound to localhost only.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" {
		return false // ":8472" listens on every interface
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// NewHTTPHandler builds the HTTP handler for a server.
//
// A token is mandatory unless the listener is loopback-only, because the
// tools behind it can run commands on the user's game servers and read every
// file on them. An unauthenticated port doing that is not a configuration
// choice worth honouring silently.
func NewHTTPHandler(s *Server, opts HTTPOptions) (http.Handler, error) {
	opts.applyDefaults()

	if opts.Token == "" && !isLoopback(opts.Addr) {
		return nil, fmt.Errorf("a bearer token is required when listening on %s; "+
			"pass -token, set PTERO_MCP_TOKEN, or bind to 127.0.0.1", opts.Addr)
	}

	// One session id per process. Enough to satisfy clients that carry the
	// header back, without pretending to hold per-client state this server
	// does not keep.
	sessionID := newSessionID()

	mux := http.NewServeMux()

	handleRPC := func(w http.ResponseWriter, r *http.Request) {
		if !checkOrigin(r, opts.AllowedOrigins) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		if !checkToken(r, opts.Token) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		switch r.Method {
		case http.MethodPost:
			// A session id from another process is stale rather than
			// hostile — say so plainly so the client starts a new session.
			if given := r.Header.Get("Mcp-Session-Id"); given != "" && given != sessionID {
				http.Error(w, "unknown session", http.StatusNotFound)
				return
			}

			body, err := io.ReadAll(io.LimitReader(r.Body, opts.MaxBodyBytes+1))
			if err != nil {
				http.Error(w, "could not read request body", http.StatusBadRequest)
				return
			}
			if int64(len(body)) > opts.MaxBodyBytes {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}

			ctx, cancel := contextWithTimeout(r, opts.RequestTimeout)
			defer cancel()

			reply := s.Handle(ctx, body)

			w.Header().Set("Mcp-Session-Id", sessionID)
			if reply == nil {
				// Notifications get an acknowledgement with no body.
				w.WriteHeader(http.StatusAccepted)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(reply)

		case http.MethodDelete:
			// The client is ending its session. Nothing is held per
			// session, so there is nothing to tear down.
			w.WriteHeader(http.StatusNoContent)

		case http.MethodGet:
			// No server-initiated messages are sent, so there is no stream
			// to open. The spec's answer for that is 405.
			w.Header().Set("Allow", "POST, DELETE")
			http.Error(w, "this server does not open server-sent streams", http.StatusMethodNotAllowed)

		default:
			w.Header().Set("Allow", "POST, DELETE")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}

	mux.HandleFunc("/mcp", handleRPC)
	// Clients configured with a bare origin and no path are common enough
	// that pointing them at the same handler is worth two lines.
	mux.HandleFunc("/", handleRPC)

	// Unauthenticated on purpose: it reveals nothing and a container health
	// check cannot carry the token.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "{\"status\":\"ok\",\"tools\":%d}\n", len(s.ToolNames()))
	})

	return mux, nil
}

// NewHTTPServer wraps NewHTTPHandler in a configured *http.Server.
func NewHTTPServer(s *Server, opts HTTPOptions) (*http.Server, error) {
	opts.applyDefaults()
	handler, err := NewHTTPHandler(s, opts)
	if err != nil {
		return nil, err
	}
	return &http.Server{
		Addr:    opts.Addr,
		Handler: handler,
		// No WriteTimeout: a console tail legitimately holds the response
		// open for as long as the caller asked it to wait.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}, nil
}

func checkToken(r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return false
	}
	// Constant time: the comparison is against a shared secret an attacker
	// can retry freely.
	return subtle.ConstantTimeCompare([]byte(header[len(prefix):]), []byte(token)) == 1
}

func checkOrigin(r *http.Request, allowed []string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // not a browser
	}
	for _, candidate := range allowed {
		if candidate == "*" || strings.EqualFold(candidate, origin) {
			return true
		}
	}
	// A browser page on the same loopback server is the one origin allowed
	// without configuration, since that is the server's own console.
	if parsed, err := url.Parse(origin); err == nil {
		if host := parsed.Hostname(); host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return true
		}
	}
	return false
}

// contextWithTimeout bounds one request, inheriting the connection's context
// so a client that hangs up still cancels the work it asked for.
func contextWithTimeout(r *http.Request, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), timeout)
}

func newSessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// Only reached if the OS entropy source fails. A fixed id is worse
		// than a random one but better than refusing to start.
		return "ptero-mcp-session"
	}
	return hex.EncodeToString(buf)
}
