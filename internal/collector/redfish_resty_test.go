package collector

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
)

func newTestRedfish(t *testing.T, srv *httptest.Server, basicAuth bool) *Redfish {
	t.Helper()
	host := strings.TrimPrefix(srv.URL, "http://")
	return NewRedfish(host, &config.AuthConfig{
		Scheme: "http", Username: "u", Password: "p", BasicAuth: basicAuth,
	})
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
		_, _ = w.Write([]byte(`{"ok":true}`))
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
