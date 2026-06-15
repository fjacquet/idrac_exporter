package collector

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
)

// newTestRedfish builds a Redfish pointing at srv. These transport-behavior
// tests deliberately use bespoke httptest servers (dynamic status codes and
// hijacked/closed connections) rather than the shared mockRedfish harness, which
// only serves static 200 JSON per path and cannot simulate retries or transport
// failures.
func newTestRedfish(t *testing.T, srv *httptest.Server, basicAuth bool) *Redfish {
	t.Helper()
	host := strings.TrimPrefix(srv.URL, "http://")
	return NewRedfish(host, &config.AuthConfig{
		Scheme: "http", Username: "u", Password: "p", BasicAuth: basicAuth,
	})
}

// hijackCloseOnce returns a handler that abruptly closes the connection (a
// transport-level error for the client) on the first matching request, then
// serves okBody afterwards. attempts counts only requests where match is true.
func hijackCloseOnce(attempts *atomic.Int32, match func(*http.Request) bool, okBody string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !match(req) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if attempts.Add(1) == 1 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				return
			}
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close() // abrupt close → transport error on the client
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(json.RawMessage(okBody))
	}
}

// TestGetRetriesTransient: a GET that returns 503 then 200 is retried and succeeds.
func TestGetRetriesTransient(t *testing.T) {
	installConfig(t, 0)
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(json.RawMessage(`{"ok":true}`))
	}))
	defer srv.Close()

	r := newTestRedfish(t, srv, true) // basic auth → no session needed
	var out map[string]any
	if !r.Get("/redfish/v1/test", &out) {
		t.Fatal("Get should succeed after one retry")
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2 (one retry)", got)
	}
}

// TestGetDoesNotRetry4xx: a GET that returns 404 is not retried.
func TestGetDoesNotRetry4xx(t *testing.T) {
	installConfig(t, 0)
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	r := newTestRedfish(t, srv, true)
	var out map[string]any
	if r.Get("/redfish/v1/test", &out) {
		t.Fatal("Get should fail on 404")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry on 4xx)", got)
	}
}

// TestCreateSessionPostNotRetried: the session-create POST is issued exactly once,
// even on a 500 (a retried POST could create duplicate BMC sessions).
func TestCreateSessionPostNotRetried(t *testing.T) {
	installConfig(t, 0)
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost {
			posts.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	r := newTestRedfish(t, srv, false) // session enabled
	if r.CreateSession() {
		t.Fatal("CreateSession should fail on 500")
	}
	if got := posts.Load(); got != 1 {
		t.Fatalf("session POSTs = %d, want 1 (POST must not retry)", got)
	}
}

// TestGetRetriesTransportError: a GET whose first attempt hits a transport error
// (connection closed mid-flight) is retried and succeeds. Proves resty v2 invokes
// the retry condition with a non-nil response (method gated to GET) on transport
// errors, so idempotent transport failures ARE retried.
func TestGetRetriesTransportError(t *testing.T) {
	installConfig(t, 0)
	var attempts atomic.Int32
	srv := httptest.NewServer(hijackCloseOnce(&attempts,
		func(req *http.Request) bool { return req.Method == http.MethodGet },
		`{"ok":true}`))
	defer srv.Close()

	r := newTestRedfish(t, srv, true)
	var out map[string]any
	if !r.Get("/redfish/v1/test", &out) {
		t.Fatal("Get should succeed after retrying a transport error")
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2 (GET transport error must be retried)", got)
	}
}

// TestCreateSessionPostNotRetriedOnTransportError: a session POST that hits a
// transport error is issued exactly once. Guards against retrying non-idempotent
// POSTs on transport failures (which could create duplicate BMC sessions) — the
// failure mode a naive `if r == nil { return err != nil }` retry would introduce.
func TestCreateSessionPostNotRetriedOnTransportError(t *testing.T) {
	installConfig(t, 0)
	var posts atomic.Int32
	srv := httptest.NewServer(hijackCloseOnce(&posts,
		func(req *http.Request) bool { return req.Method == http.MethodPost },
		`{}`))
	defer srv.Close()

	r := newTestRedfish(t, srv, false) // session enabled
	if r.CreateSession() {
		t.Fatal("CreateSession should fail on a transport error")
	}
	if got := posts.Load(); got != 1 {
		t.Fatalf("session POSTs = %d, want 1 (POST must not retry on transport error)", got)
	}
}
