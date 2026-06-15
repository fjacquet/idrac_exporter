package collector

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
)

// testConfig installs a minimal valid global config with only the given metric
// groups enabled. Tests must not run in parallel: config.Config is a singleton.
func testConfig(t *testing.T, enable func(*config.CollectConfig)) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Hosts["default"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "http"}
	enable(&cfg.Collect)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate test config: %v", err)
	}
	config.SetConfig(cfg)
}

// mockRedfish serves canned JSON per path; everything else 404s.
func mockRedfish(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for path, file := range routes {
		raw, err := os.ReadFile("testdata/" + file)
		if err != nil {
			t.Fatalf("read fixture %s: %v", file, err)
		}
		var payload json.RawMessage
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("parse fixture %s: %v", file, err)
		}
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(payload); err != nil {
				http.Error(w, "encode error", http.StatusInternalServerError)
			}
		})
	}
	return httptest.NewServer(mux)
}

// testClient builds a Client whose Redfish transport points at srv, using basic
// auth so no SessionService mock is required (session is disabled).
func testClient(srv *httptest.Server) *Client {
	host := strings.TrimPrefix(srv.URL, "http://")
	auth := &config.AuthConfig{Scheme: "http", Username: "u", Password: "p", BasicAuth: true}
	return &Client{redfish: NewRedfish(host, auth)}
}
