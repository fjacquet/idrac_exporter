package collector

import (
	"net/http"
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
)

// installConfig sets a valid global config with the given concurrency.
// Tests must not run in parallel: config.Config is a singleton.
func installConfig(t *testing.T, concurrency uint) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Concurrency = concurrency
	cfg.Hosts["default"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "http"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	config.SetConfig(cfg)
}

func transportOf(t *testing.T, r *Redfish) *http.Transport {
	t.Helper()
	tr, ok := r.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", r.http.Transport)
	}
	return tr
}

func TestNewRedfishConnPoolFromConcurrency(t *testing.T) {
	installConfig(t, 3)
	r := NewRedfish("127.0.0.1", &config.AuthConfig{Scheme: "http", Username: "u", Password: "p"})
	tr := transportOf(t, r)
	if tr.MaxConnsPerHost != 4 {
		t.Fatalf("MaxConnsPerHost = %d, want 4 (n+1)", tr.MaxConnsPerHost)
	}
	if tr.MaxIdleConnsPerHost != 3 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want 3 (n)", tr.MaxIdleConnsPerHost)
	}
}

func TestNewRedfishConnPoolDefault(t *testing.T) {
	installConfig(t, 0)
	r := NewRedfish("127.0.0.1", &config.AuthConfig{Scheme: "http", Username: "u", Password: "p"})
	tr := transportOf(t, r)
	if tr.MaxConnsPerHost != 20 || tr.MaxIdleConnsPerHost != 10 {
		t.Fatalf("default pool = (%d,%d), want (20,10)", tr.MaxConnsPerHost, tr.MaxIdleConnsPerHost)
	}
}
