package pteroapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFlattenRemovesEnvelopes(t *testing.T) {
	var decoded interface{}
	raw := `{
      "object": "list",
      "data": [
        {"object": "file_object", "attributes": {"name": "server.properties", "size": 12}},
        {"object": "file_object", "attributes": {"name": "plugins", "is_file": false}}
      ],
      "meta": {"pagination": {"total": 2}}
    }`
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatal(err)
	}

	flat, ok := Flatten(decoded).(map[string]interface{})
	if !ok {
		t.Fatalf("expected an object, got %T", Flatten(decoded))
	}

	items, ok := flat["items"].([]interface{})
	if !ok || len(items) != 2 {
		t.Fatalf("expected two items, got %v", flat["items"])
	}
	first, _ := items[0].(map[string]interface{})
	if first["name"] != "server.properties" {
		t.Errorf("attributes were not lifted: %v", first)
	}
	if _, still := first["attributes"]; still {
		t.Error("the attributes wrapper survived")
	}
	// Pagination has to survive: without it a caller cannot tell a first
	// page from a whole list.
	if flat["meta"] == nil {
		t.Error("meta was dropped")
	}
}

func TestFlattenHandlesNestedRelationships(t *testing.T) {
	var decoded interface{}
	raw := `{
      "object": "server",
      "attributes": {
        "identifier": "1a7ce997",
        "relationships": {
          "allocations": {
            "object": "list",
            "data": [{"object": "allocation", "attributes": {"port": 25565}}]
          }
        }
      }
    }`
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatal(err)
	}

	flat := Flatten(decoded).(map[string]interface{})
	if flat["identifier"] != "1a7ce997" {
		t.Fatalf("top level not flattened: %v", flat)
	}

	relationships := flat["relationships"].(map[string]interface{})
	allocations := relationships["allocations"].(map[string]interface{})
	items := allocations["items"].([]interface{})
	port := items[0].(map[string]interface{})["port"]
	if port != float64(25565) {
		t.Errorf("nested allocation was not flattened, got %v", items[0])
	}
}

func TestJSONSurfacesPanelErrorDetail(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"errors":[{"code":"ValidationException","status":"422",
          "detail":"The name field is required.","meta":{"source_field":"name"}}]}`))
	}))
	defer panel.Close()

	client := New(panel.URL, "ptlc_test")
	_, err := client.JSON(context.Background(), Request{Method: http.MethodPost, Path: "/servers/x/databases"})
	if err == nil {
		t.Fatal("expected an error for a 422")
	}
	message := err.Error()
	// A caller told only "422" has to guess which field the panel disliked.
	if !strings.Contains(message, "The name field is required.") {
		t.Errorf("panel detail missing from %q", message)
	}
	if !strings.Contains(message, "name") {
		t.Errorf("offending field missing from %q", message)
	}
}

func TestSendRetriesOnlyRateLimits(t *testing.T) {
	var calls int32
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer panel.Close()

	client := New(panel.URL, "ptlc_test")
	result, err := client.Send(context.Background(), Request{Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("expected the retry to succeed, got %d", result.Status)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected exactly one retry, saw %d calls", got)
	}
}

func TestSendDoesNotRetryServerErrors(t *testing.T) {
	var calls int32
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer panel.Close()

	client := New(panel.URL, "ptlc_test")
	// A write that failed with a 500 may still have been applied, so
	// repeating it could apply it twice.
	if _, err := client.Send(context.Background(), Request{Method: http.MethodPost, Path: "/servers/x/power"}); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("a 500 was retried %d times; it must not be", got-1)
	}
}

func TestRequestShapeReachesThePanel(t *testing.T) {
	type seen struct {
		path, query, auth, contentType, body string
	}
	var got seen

	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			r.Body.Read(buf)
		}
		got = seen{
			path:        r.URL.Path,
			query:       r.URL.RawQuery,
			auth:        r.Header.Get("Authorization"),
			contentType: r.Header.Get("Content-Type"),
			body:        string(buf),
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer panel.Close()

	client := New(panel.URL+"/", "ptlc_secret")
	_, err := client.Send(context.Background(), Request{
		Method:  http.MethodPost,
		Path:    "/servers/abc/files/write",
		Query:   map[string][]string{"file": {"/plugins/config.yml"}},
		Text:    "port: 25565\n",
		UseText: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.path != "/api/client/servers/abc/files/write" {
		t.Errorf("path was %q", got.path)
	}
	if got.query != "file=%2Fplugins%2Fconfig.yml" {
		t.Errorf("query was %q", got.query)
	}
	if got.auth != "Bearer ptlc_secret" {
		t.Errorf("auth header was %q", got.auth)
	}
	// The write route takes the bytes as the body, not as a JSON field.
	if got.contentType != "text/plain" {
		t.Errorf("content type was %q", got.contentType)
	}
	if got.body != "port: 25565\n" {
		t.Errorf("body was %q", got.body)
	}
}

func TestPathResolution(t *testing.T) {
	var seen string
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Path
		w.Write([]byte(`{}`))
	}))
	defer panel.Close()

	client := New(panel.URL, "k")
	cases := map[string]string{
		"/":                      "/api/client", // the server list
		"/servers/abc":           "/api/client/servers/abc",
		"servers/abc/resources":  "/api/client/servers/abc/resources", // no leading slash
		"/api/application/nodes": "/api/application/nodes",            // already absolute
	}
	for given, want := range cases {
		if _, err := client.Send(context.Background(),
			Request{Method: http.MethodGet, Path: given}); err != nil {
			t.Fatal(err)
		}
		if seen != want {
			t.Errorf("Path %q reached %q, want %q", given, seen, want)
		}
	}
}

func TestNoContentBecomesASuccess(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer panel.Close()

	value, err := New(panel.URL, "k").JSON(context.Background(),
		Request{Method: http.MethodPost, Path: "/servers/x/power", JSON: map[string]string{"signal": "start"}})
	if err != nil {
		t.Fatal(err)
	}
	if asMap, ok := value.(map[string]interface{}); !ok || asMap["ok"] != true {
		t.Fatalf("204 should read as a success, got %v", value)
	}
}

func TestMissingConfigurationIsRefusedBeforeAnyRequest(t *testing.T) {
	if _, err := New("", "key").Send(context.Background(), Request{Method: http.MethodGet, Path: "/"}); err == nil {
		t.Error("a client with no URL should refuse to send")
	}
	if _, err := New("https://panel.example.com", "").Send(context.Background(),
		Request{Method: http.MethodGet, Path: "/"}); err == nil {
		t.Error("a client with no API key should refuse to send")
	}
}

func TestStripANSI(t *testing.T) {
	line := "\x1b[32m[12:00:00 INFO]\x1b[0m: Done (3.1s)!\r"
	if got := StripANSI(line); got != "[12:00:00 INFO]: Done (3.1s)!" {
		t.Errorf("got %q", got)
	}
}
