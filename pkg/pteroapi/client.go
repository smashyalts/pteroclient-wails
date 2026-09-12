// Package pteroapi is a complete, dependency-free client for the Pterodactyl
// panel's client API (/api/client).
//
// It is deliberately thin. The panel adds and renames fields between
// releases, and a struct written against one release quietly drops whatever
// the next one adds, so responses come back as decoded JSON with the panel's
// {object, attributes} envelopes removed rather than as typed values. The one
// place types are used is where a caller has to act on a field — the server
// state, the websocket credentials.
//
// This sits alongside pkg/pterodactyl rather than replacing it. That client
// is shaped for the desktop app: one active server, no backup or reinstall
// routes, retry tuned for recursive search. This one has to reach every
// route for any server on any configured panel, which is a different shape.
package pteroapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to one panel with one API key.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client

	// UserAgent is sent on every request so a panel operator reading their
	// access log can tell these calls apart from a browser's.
	UserAgent string

	// MaxRetries bounds how many times a rate-limited request is repeated.
	MaxRetries int
}

// New creates a client for a panel base URL such as https://panel.example.com
// and a client API key (ptlc_...). An application key (ptla_...) also works
// for the client routes it has been granted.
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		http: &http.Client{
			Timeout: 60 * time.Second,
		},
		UserAgent:  "ptero-mcp",
		MaxRetries: 3,
	}
}

// BaseURL returns the panel URL this client was built for.
func (c *Client) BaseURL() string { return c.baseURL }

// Request is one call to the panel.
type Request struct {
	Method string
	// Path is relative to /api/client, with a leading slash — for example
	// "/servers/1a7ce997/files/list". A path already starting with "/api/"
	// is used as given, which is what the raw passthrough tool needs.
	Path  string
	Query url.Values

	// JSON is marshalled as the application/json body. Ignored when Text is
	// used.
	JSON interface{}

	// Text is sent verbatim as text/plain. The file write route takes the
	// file's bytes as the body rather than as a JSON field, so it needs
	// this; nothing else does.
	Text    string
	UseText bool
}

// Result is one panel response, undecoded.
type Result struct {
	Status      int
	Body        []byte
	ContentType string
}

// Send performs a request, retrying only when the panel refused it for rate
// limiting.
//
// 429 is the one status worth repeating: the request was turned away rather
// than carried out, so repeating it cannot apply a write twice. Every other
// failure is returned as it came.
func (c *Client) Send(ctx context.Context, req Request) (*Result, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("no panel URL configured")
	}
	if c.apiKey == "" {
		return nil, fmt.Errorf("no API key configured")
	}

	endpoint, err := c.resolve(req)
	if err != nil {
		return nil, err
	}

	var payload []byte
	contentType := ""
	switch {
	case req.UseText:
		payload = []byte(req.Text)
		contentType = "text/plain"
	case req.JSON != nil:
		payload, err = json.Marshal(req.JSON)
		if err != nil {
			return nil, fmt.Errorf("could not encode request body: %w", err)
		}
		contentType = "application/json"
	}

	attempts := c.MaxRetries + 1
	if attempts < 1 {
		attempts = 1
	}

	var last *Result
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, req.Method, endpoint, bodyReader(payload))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		httpReq.Header.Set("Accept", "application/json")
		httpReq.Header.Set("User-Agent", c.UserAgent)
		if contentType != "" {
			httpReq.Header.Set("Content-Type", contentType)
		}

		resp, err := c.http.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("request to %s failed: %w", req.Path, err)
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("could not read the panel's reply: %w", readErr)
		}

		last = &Result{
			Status:      resp.StatusCode,
			Body:        body,
			ContentType: resp.Header.Get("Content-Type"),
		}

		if resp.StatusCode != http.StatusTooManyRequests || attempt == attempts-1 {
			return last, nil
		}

		// The panel says when it will accept another request. Guessing
		// longer wastes the wait; guessing shorter spends the retry on
		// another refusal.
		wait := retryAfter(resp.Header.Get("Retry-After"), attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return last, nil
}

func (c *Client) resolve(req Request) (string, error) {
	path := req.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasPrefix(path, "/api/") {
		path = "/api/client" + path
	}
	// The server list is reached with a path of "/", which would otherwise
	// arrive as /api/client/. Laravel matches a group's root either way, but
	// panels sit behind proxies with their own opinions about trailing
	// slashes, so send what the panel's own frontend sends.
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}

	parsed, err := url.Parse(c.baseURL + path)
	if err != nil {
		return "", fmt.Errorf("could not build a URL for %s: %w", req.Path, err)
	}
	if len(req.Query) > 0 {
		existing := parsed.Query()
		for key, values := range req.Query {
			for _, value := range values {
				existing.Add(key, value)
			}
		}
		parsed.RawQuery = existing.Encode()
	}
	return parsed.String(), nil
}

func bodyReader(payload []byte) io.Reader {
	if len(payload) == 0 {
		return nil
	}
	return bytes.NewReader(payload)
}

// retryAfter reads the panel's Retry-After header, falling back to a short
// backoff. A search is not worth a minute of waiting, so the header is
// capped rather than obeyed without limit.
func retryAfter(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && seconds >= 0 {
		if seconds > 10 {
			seconds = 10
		}
		if seconds == 0 {
			return 400 * time.Millisecond
		}
		return time.Duration(seconds) * time.Second
	}
	wait := time.Duration(400*(attempt+1)) * time.Millisecond
	if wait > 4*time.Second {
		wait = 4 * time.Second
	}
	return wait
}

// JSON performs a request and returns the panel's response with its
// {object, attributes} envelopes removed.
//
// A non-2xx status becomes an error carrying whatever the panel said about
// it, because "422" on its own tells the caller nothing about which field it
// disliked.
func (c *Client) JSON(ctx context.Context, req Request) (interface{}, error) {
	result, err := c.Send(ctx, req)
	if err != nil {
		return nil, err
	}
	if result.Status < 200 || result.Status > 299 {
		return nil, describeFailure(result)
	}
	if len(bytes.TrimSpace(result.Body)) == 0 {
		// 204 is the panel's answer to most writes.
		return map[string]interface{}{"ok": true, "status": result.Status}, nil
	}

	var decoded interface{}
	if err := json.Unmarshal(result.Body, &decoded); err != nil {
		// A 2xx body that is not JSON is still a success; hand back the
		// text rather than turning it into a failure.
		return map[string]interface{}{"ok": true, "status": result.Status, "body": string(result.Body)}, nil
	}
	return Flatten(decoded), nil
}

// Text performs a request and returns the body as-is, for the routes that
// answer with a file's contents rather than JSON.
func (c *Client) Text(ctx context.Context, req Request) (string, error) {
	result, err := c.Send(ctx, req)
	if err != nil {
		return "", err
	}
	if result.Status < 200 || result.Status > 299 {
		return "", describeFailure(result)
	}
	return string(result.Body), nil
}

// describeFailure turns a panel error response into a sentence.
//
// Pterodactyl answers failures with an errors array: a code, a status, a
// human detail, and for validation failures the field at fault. All of that
// is worth surfacing — a caller told only "422" has to guess.
func describeFailure(result *Result) error {
	var envelope struct {
		Errors []struct {
			Code   string `json:"code"`
			Status string `json:"status"`
			Detail string `json:"detail"`
			Meta   struct {
				SourceField string `json:"source_field"`
				Rule        string `json:"rule"`
			} `json:"meta"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(result.Body, &envelope); err == nil && len(envelope.Errors) > 0 {
		parts := make([]string, 0, len(envelope.Errors))
		for _, e := range envelope.Errors {
			detail := e.Detail
			if detail == "" {
				detail = e.Code
			}
			if e.Meta.SourceField != "" {
				detail = fmt.Sprintf("%s (field %q)", detail, e.Meta.SourceField)
			}
			parts = append(parts, detail)
		}
		return fmt.Errorf("panel returned %d: %s", result.Status, strings.Join(parts, "; "))
	}

	snippet := strings.TrimSpace(string(result.Body))
	if len(snippet) > 400 {
		snippet = snippet[:400] + "…"
	}
	if snippet == "" {
		snippet = http.StatusText(result.Status)
	}
	return fmt.Errorf("panel returned %d: %s", result.Status, snippet)
}

// Flatten strips Pterodactyl's envelopes from a decoded response.
//
// The panel wraps every object as {"object": "...", "attributes": {...}} and
// every list as {"object": "list", "data": [ ... ]}, nesting the same two
// shapes again inside "relationships". Left alone, a directory listing spends
// a third of its size on the word "attributes". Removing them is lossless for
// every caller here: the type name is already implied by the route asked for.
func Flatten(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		if _, hasObject := typed["object"]; hasObject {
			// A list envelope: keep meta, since pagination lives there.
			if data, ok := typed["data"].([]interface{}); ok {
				out := map[string]interface{}{"items": Flatten(data)}
				if meta, ok := typed["meta"]; ok {
					out["meta"] = Flatten(meta)
				}
				return out
			}
			// A single object envelope.
			if attributes, ok := typed["attributes"].(map[string]interface{}); ok {
				flattened, _ := Flatten(attributes).(map[string]interface{})
				if flattened == nil {
					return Flatten(attributes)
				}
				if meta, ok := typed["meta"]; ok {
					flattened["meta"] = Flatten(meta)
				}
				return flattened
			}
		}

		out := make(map[string]interface{}, len(typed))
		for key, child := range typed {
			out[key] = Flatten(child)
		}
		return out

	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, child := range typed {
			out[i] = Flatten(child)
		}
		return out

	default:
		return value
	}
}

// Pretty renders a value as indented JSON, which is what a tool result
// carries back to the model.
func Pretty(value interface{}) string {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(out)
}
