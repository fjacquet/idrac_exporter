package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
)

// TestStaticOKHandler asserts the probe handler is unconditionally 200 with a
// body, reading no configuration or collection state. /livez and /readyz both
// use it, so this is the whole contract.
func TestStaticOKHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)

	staticOKHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "ok" {
		t.Fatalf("body = %q, want %q", got, "ok")
	}
}

// setTestConfig installs a config for the duration of the test and restores
// the previous global afterwards. config.Config is a package-level global that
// other tests in this package also set.
func setTestConfig(t *testing.T, c *config.RootConfig) {
	t.Helper()
	old := config.Config
	config.Config = c
	t.Cleanup(func() { config.Config = old })
}

// TestHealthHandlerBody asserts /health is 200 with a JSON body naming each
// configured BMC host. Status never depends on host state.
func TestHealthHandlerBody(t *testing.T) {
	c := config.NewConfig()
	c.DefaultTarget = "10.0.0.11"
	c.Hosts["10.0.0.11"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "https"}
	c.Hosts["10.0.0.10"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "https"}
	c.Hosts["default"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "https"}
	setTestConfig(t, c)

	rec := httptest.NewRecorder()
	healthHandler(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", ct, "application/json")
	}

	var got healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if got.Status != "ok" {
		t.Fatalf("status field = %q, want %q", got.Status, "ok")
	}
	if len(got.Hosts) != 2 {
		t.Fatalf("hosts = %+v, want 2 entries (the 'default' credential is not a host)", got.Hosts)
	}
	if got.Hosts[0].Host != "10.0.0.10" || got.Hosts[0].Default {
		t.Fatalf("hosts[0] = %+v, want 10.0.0.10 not default", got.Hosts[0])
	}
	if got.Hosts[1].Host != "10.0.0.11" || !got.Hosts[1].Default {
		t.Fatalf("hosts[1] = %+v, want 10.0.0.11 as default_target", got.Hosts[1])
	}
	if got.Hosts[0].Scheme != "https" {
		t.Fatalf("hosts[0].Scheme = %q, want %q", got.Hosts[0].Scheme, "https")
	}
}

// TestHealthHandlerNoHosts asserts /health is still 200 with an empty (not
// null) host list when only the 'default' credential fallback is configured —
// which is exactly the shipped default-config.yml, i.e. the container's own
// startup state.
func TestHealthHandlerNoHosts(t *testing.T) {
	c := config.NewConfig()
	c.Hosts["default"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "https"}
	setTestConfig(t, c)

	rec := httptest.NewRecorder()
	healthHandler(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"hosts":[]`) {
		t.Fatalf("body = %q, want an empty hosts array, never null", rec.Body.String())
	}
}

func TestResolveMetricsMode(t *testing.T) {
	tests := []struct {
		name          string
		target        string
		defaultTarget string
		hasHosts      bool
		wantMode      metricsMode
		wantTarget    string
	}{
		{"explicit target", "10.0.0.5", "", true, modeSingleTarget, "10.0.0.5"},
		{"explicit target beats default", "10.0.0.5", "1.2.3.4", true, modeSingleTarget, "10.0.0.5"},
		{"default target fallback", "", "1.2.3.4", true, modeSingleTarget, "1.2.3.4"},
		{"scrape all when hosts but no target/default", "", "", true, modeScrapeAll, ""},
		{"error when nothing resolvable", "", "", false, modeError, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, target := resolveMetricsMode(tt.target, tt.defaultTarget, tt.hasHosts)
			if mode != tt.wantMode || target != tt.wantTarget {
				t.Fatalf("resolveMetricsMode(%q,%q,%v) = (%v,%q), want (%v,%q)",
					tt.target, tt.defaultTarget, tt.hasHosts, mode, target, tt.wantMode, tt.wantTarget)
			}
		})
	}
}
