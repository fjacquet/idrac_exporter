# Phase 3a — resty/v2 Transport Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reimplement the Redfish HTTP transport in `internal/collector/redfish.go` on `github.com/go-resty/resty/v2`, preserving every existing behavior and adding bounded retry for idempotent GET/HEAD only — without changing metric output.

**Architecture:** A per-host `*resty.Client` (built in `NewRedfish`) replaces the hand-built `*http.Client`. The client keeps the same custom `*http.Transport` (proxy, TLS `InsecureSkipVerify`/min-1.2, the 2c concurrency-sized conn pool, the timeouts) via `SetTransport`, plus `SetRetryCount(2)` and one `AddRetryCondition`. The condition retries only `GET`/`HEAD` on a transport error or a `>= 500` status, and returns false for everything else — so a `4xx` is never retried and the session-create `POST` is never retried (avoiding duplicate BMC sessions). All session quirks (the `405` → `/Sessions` fallback, the iLO 4 `Location` session id, `\r` stripping, token-safe `--trace` logging, basic-auth fallback) are preserved verbatim.

**Tech Stack:** Go 1.26.4, `github.com/go-resty/resty/v2`, existing `internal/collector` httptest harness.

**Branch:** `phase3a-resty` (off `main`, already created). Design: [Phase 3 spec §3a](../specs/2026-06-15-phase3-payload-resty-design.md).

**resty v2 API used (verified):** `resty.New()`; `client.SetTransport(*http.Transport)`, `SetTimeout(d)`, `SetRetryCount(n)`, `SetRetryWaitTime(d)`, `SetRetryMaxWaitTime(d)`, `AddRetryCondition(func(*resty.Response, error) bool)`; `client.R()`, `req.SetHeader(k,v)`, `req.SetBasicAuth(u,p)`, `req.SetBody(v)`, `req.Get/Post/Head/Delete(url)`; `resp.StatusCode() int`, `resp.Status() string`, `resp.Body() []byte`, `resp.Header() http.Header`, `resp.Request.Method`. In v2 an added retry condition **overrides** the default error-retry, so the condition must itself return `true` on `err != nil` for the methods we want retried.

---

## File structure
- `internal/collector/redfish.go` — **rewrite** the transport onto resty (struct field, `NewRedfish`, `Get`, `Exists`, `CreateSession`, `RefreshSession`, `DeleteSession`, add `retryIdempotent`).
- `internal/collector/redfish_resty_test.go` — **create** new behavior tests (retry-on-5xx, no-retry-on-4xx, session-POST-issued-once).
- `go.mod` / `go.sum` — **modify** add `github.com/go-resty/resty/v2`.
- Existing `redfish_test.go` (conn-pool), `refresh_test.go`, `trace_test.go` — **must stay green** (behavior-neutrality guard).

---

## Task 1: migrate redfish.go onto resty/v2

**Files:**
- Test: `internal/collector/redfish_resty_test.go` (create)
- Modify: `internal/collector/redfish.go` (rewrite)
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Write the failing behavior tests** — create `internal/collector/redfish_resty_test.go`:

```go
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
```

- [ ] **Step 2: Run the new tests against the current net/http code to verify they fail**

Run: `go test ./internal/collector/ -run 'TestGetRetriesTransient|TestGetDoesNotRetry4xx|TestCreateSessionPostNotRetried' -v`
Expected: `TestGetRetriesTransient` FAILS (current code does not retry — `attempts = 1`, Get returns false). The other two may already pass (no retry today); that's fine — they lock in the behavior post-migration.

- [ ] **Step 3: Add the resty dependency**

Run: `go get github.com/go-resty/resty/v2@latest`
(Do **not** run `go mod tidy` yet — resty isn't imported until Step 4, and tidy would drop it. Tidy is run in Step 5 after the import exists.)

- [ ] **Step 4: Rewrite `internal/collector/redfish.go`** — replace the entire file with:

```go
package collector

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"path"
	"strings"
	"time"

	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/fjacquet/idrac_exporter/internal/log"
	"github.com/go-resty/resty/v2"
)

type RedfishSession struct {
	disabled bool
	id       string
	token    string
}

type Redfish struct {
	client   *resty.Client
	baseurl  string
	hostname string
	username string
	password string
	session  RedfishSession
}

const redfishRootPath = "/redfish/v1"

// retryIdempotent retries only idempotent GET/HEAD requests on a transport error
// or a 5xx status. It never retries 4xx (a real answer) and never retries the
// session-create POST (a retried POST could create duplicate BMC sessions). In
// resty v2 an added condition overrides the default error-retry, so this must
// itself return true on err != nil for the methods we want retried.
func retryIdempotent(r *resty.Response, err error) bool {
	if r == nil || r.Request == nil {
		return false
	}
	switch r.Request.Method {
	case http.MethodGet, http.MethodHead:
		return err != nil || r.StatusCode() >= 500
	default:
		return false
	}
}

func NewRedfish(host string, auth *config.AuthConfig) *Redfish {
	baseurl := fmt.Sprintf("%s://%s", auth.Scheme, host)
	if auth.Port > 0 {
		baseurl = fmt.Sprintf("%s:%d", baseurl, auth.Port)
	}

	// Size the connection pool to the configured concurrency when set (Phase 2c).
	// The 10/20 defaults preserve the historical unlimited behavior.
	maxIdle, maxConns := 10, 20
	if n := config.Config.Concurrency; n > 0 {
		maxIdle = int(n)
		maxConns = int(n) + 1
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: !auth.Verify, MinVersion: tls.VersionTLS12},
		MaxIdleConnsPerHost:   maxIdle,
		MaxConnsPerHost:       maxConns,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: time.Duration(config.Config.Timeout) * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := resty.New().
		SetTransport(transport).
		SetTimeout(time.Duration(config.Config.Timeout) * time.Second).
		// BMCs are reached over insecure transport by design, so resty's
		// per-request basic-auth-over-HTTP warning would be log spam.
		SetDisableWarn(true).
		SetRetryCount(2).
		SetRetryWaitTime(200 * time.Millisecond).
		SetRetryMaxWaitTime(1 * time.Second).
		AddRetryCondition(retryIdempotent)

	return &Redfish{
		client:   client,
		baseurl:  baseurl,
		hostname: host,
		username: auth.Username,
		password: auth.Password,
		session: RedfishSession{
			disabled: auth.BasicAuth,
		},
	}
}

func (r *Redfish) DisableSession() {
	r.session.disabled = true
	r.session.token = ""
	r.session.id = ""
	log.Info("Session authentication disabled for %s due to failed creation or refresh", r.hostname)
}

func (r *Redfish) CreateSession() bool {
	if r.session.disabled {
		return false
	}

	url := fmt.Sprintf("%s/redfish/v1/SessionService/Sessions", r.baseurl)
	session := Session{
		Username: r.username,
		Password: r.password,
	}

	resp, err := r.client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(&session).
		Post(url)
	if err != nil {
		log.Error("Failed to query %q: %v", url, err)
		return false
	}

	// iDRAC 8 used /redfish/v1/Sessions; newer firmware uses
	// /redfish/v1/SessionService/Sessions. Fall back on 405.
	if resp.StatusCode() == http.StatusMethodNotAllowed {
		url = fmt.Sprintf("%s/redfish/v1/Sessions", r.baseurl)
		resp, err = r.client.R().
			SetHeader("Content-Type", "application/json").
			SetBody(&session).
			Post(url)
		if err != nil {
			r.DisableSession()
			return false
		}
	}

	if resp.StatusCode() != http.StatusCreated {
		log.Error("Unexpected status code from %q: %s", url, resp.Status())
		return false
	}

	if err := json.Unmarshal(resp.Body(), &session); err != nil {
		log.Error("Error decoding response from %q: %v", url, err)
		return false
	}

	r.session.id = session.OdataId
	r.session.token = resp.Header().Get("X-Auth-Token")

	// iLO 4
	if len(r.session.id) == 0 {
		u, err := neturl.Parse(resp.Header().Get("Location"))
		if err == nil {
			r.session.id = u.Path
		}
	}

	log.Debug("Succesfully created session: %s", path.Base(r.session.id))
	return true
}

func (r *Redfish) DeleteSession() bool {
	if len(r.session.token) == 0 {
		return true
	}

	url := fmt.Sprintf("%s%s", r.baseurl, r.session.id)
	resp, err := r.client.R().
		SetHeader("Accept", "application/json").
		SetHeader("X-Auth-Token", r.session.token).
		Delete(url)
	if err != nil {
		log.Error("Failed to query %q: %v", url, err)
		return false
	}

	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
		log.Error("Unexpected status code from %q: %s", url, resp.Status())
		return false
	}

	log.Debug("Succesfully deleted session: %s", path.Base(r.session.id))
	r.session.id = ""
	r.session.token = ""

	return true
}

func (r *Redfish) RefreshSession() bool {
	if r.session.disabled {
		return false
	}

	if len(r.session.token) == 0 {
		ok := r.CreateSession()
		if !ok {
			r.DisableSession()
		}
		return ok
	}

	url := fmt.Sprintf("%s%s", r.baseurl, r.session.id)
	resp, err := r.client.R().
		SetHeader("Accept", "application/json").
		SetHeader("X-Auth-Token", r.session.token).
		Get(url)
	if err != nil {
		return false
	}

	if resp.StatusCode() == http.StatusUnauthorized || resp.StatusCode() == http.StatusNotFound {
		ok := r.CreateSession()
		if !ok {
			r.DisableSession()
		}
		return ok
	} else if resp.StatusCode() != http.StatusOK {
		log.Error("Unexpected status code %d during session refresh", resp.StatusCode())
		return false
	}

	return true
}

func (r *Redfish) Get(path string, res any) bool {
	if !strings.HasPrefix(path, redfishRootPath) {
		return false
	}

	url := fmt.Sprintf("%s%s", r.baseurl, path)
	req := r.client.R().SetHeader("Accept", "application/json")
	if len(r.session.token) > 0 {
		req.SetHeader("X-Auth-Token", r.session.token)
	} else {
		req.SetBasicAuth(r.username, r.password)
	}

	log.Debug("Querying %q", url)
	resp, err := req.Get(url)
	if err != nil {
		log.Error("Failed to query %q: %v", url, err)
		return false
	}

	if config.Trace {
		log.Info("trace: GET %s -> %d", path, resp.StatusCode())
	}

	if resp.StatusCode() != http.StatusOK {
		log.Error("Unexpected status code from %q: %s", url, resp.Status())
		return false
	}

	if config.Debug {
		log.Debug("Response from %q: %s", url, resp.Body())
	}

	// Issue #192
	body := bytes.ReplaceAll(resp.Body(), []byte("\r"), []byte(""))

	if err := json.Unmarshal(body, res); err != nil {
		log.Error("Error decoding response from %q: %v", url, err)
		return false
	}

	return true
}

func (r *Redfish) Exists(path string) bool {
	if !strings.HasPrefix(path, redfishRootPath) {
		return false
	}

	url := fmt.Sprintf("%s%s", r.baseurl, path)
	req := r.client.R().SetHeader("Accept", "application/json")
	if len(r.session.token) > 0 {
		req.SetHeader("X-Auth-Token", r.session.token)
	} else {
		req.SetBasicAuth(r.username, r.password)
	}

	resp, err := req.Head(url)
	if err != nil {
		return false
	}

	if config.Trace {
		log.Info("trace: HEAD %s -> %d", path, resp.StatusCode())
	}

	if resp.StatusCode() >= 400 && resp.StatusCode() <= 499 {
		return false
	}

	return true
}
```

- [ ] **Step 5: Tidy modules**

Run: `go mod tidy`
Expected: `github.com/go-resty/resty/v2` is now a direct dependency; `golang.org/x/net` etc. resolve. `go.mod`/`go.sum` updated.

- [ ] **Step 6: Run the new tests to verify they pass**

Run: `go test ./internal/collector/ -run 'TestGetRetriesTransient|TestGetDoesNotRetry4xx|TestCreateSessionPostNotRetried' -v`
Expected: PASS (all three).

- [ ] **Step 7: Run the behavior-neutrality guard — existing tests must still pass**

Run: `go test ./internal/collector/ -race -v`
Expected: PASS — `TestRefreshSystem` (exact exposition unchanged), `trace_test` (token never leaked), `redfish_test` (conn-pool sizing), and the concurrency tests all green.

- [ ] **Step 8: Run the full gate**

Run: `make ci`
Expected: PASS (gofmt-check, go vet, golangci-lint 0 issues, `go test -race ./...`, govulncheck).

- [ ] **Step 9: Commit**

```bash
git add go.mod go.sum internal/collector/redfish.go internal/collector/redfish_resty_test.go
git commit -m "feat(3a): migrate Redfish transport to resty/v2 with idempotent retry

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: behavior-neutrality verification

**Files:** none (verification only).

- [ ] **Step 1: Build the binary**

Run: `make cli`
Expected: builds clean.

- [ ] **Step 2: Confirm `--trace` is still token-safe and `\r` stripping survives**

Run: `./bin/idrac_exporter --once --config sample-config.yml --trace 2>&1 | grep -i 'auth-token\|x-auth' || echo "no token leaked in trace (good)"`
Expected: prints "no token leaked in trace (good)". (Example hosts are unreachable; the point is the trace lines carry no token, same as Phase 2a.)

- [ ] **Step 3: Sanity — no `net/http` *client* construction remains in redfish.go**

Run: `grep -n 'http.Client\|http.NewRequest\|r.http' internal/collector/redfish.go || echo "no raw http client usage (resty owns the transport)"`
Expected: prints the "resty owns the transport" message. (Note: `http.Transport`, `http.MethodGet`, `http.StatusOK` etc. legitimately remain — only the hand-built client/request usage is gone.)

- [ ] **Step 4: Final gate**

Run: `make ci`
Expected: PASS.

(No commit — this task only verifies. If Step 2/3 reveal a regression, return to Task 1.)

---

## Self-review notes
- **Spec coverage (§3a):** transport → resty (Task 1 Step 4); all quirks preserved (405 fallback, iLO 4 Location, `\r` strip, token-safe trace, basic-auth fallback — all present verbatim in the rewrite); conn-pool from 2c carried onto resty's transport; retry **GET/HEAD only, excludes 4xx, POST never retried** (`retryIdempotent` + tests in Step 1). Contract-neutral: existing exposition/trace tests guard it (Step 7).
- **Placeholder scan:** none — full file and full test code provided.
- **Type consistency:** `retryIdempotent(*resty.Response, error) bool` defined and passed to `AddRetryCondition`; `Redfish.client` is `*resty.Client` and every method uses `r.client.R()`; `newTestRedfish`/`installConfig` (the latter from `redfish_test.go`, same package) used by the new tests.
- **resty v2 vs v3:** this plan targets **v2** (`github.com/go-resty/resty/v2`) per ADR 0003 — `AddRetryCondition` (singular), `SetTransport`, `http.Client.Timeout` via `SetTimeout`. Do NOT use v3 (`resty.dev/v3`) signatures.
- **Known minor behavior delta:** resty sets a `User-Agent: go-resty/2.x` header where the stdlib client sent `Go-http-client/1.1`. No BMC is known to depend on this; not worth overriding. Noted, not blocking.
- **Untestable-by-unit:** real BMC session lifecycle and live retry timing are manual (`--once`); the retry decision, 4xx-no-retry, POST-once, and behavior-neutrality are all unit-tested.
- **Out of scope:** payload realignment and absent-not-zero are Phase 3b; `docs/metrics.md` is 3c.
