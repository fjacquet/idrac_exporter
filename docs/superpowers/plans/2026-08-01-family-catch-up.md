# Family Standard Catch-Up (Probes + Container Health) Implementation Plan

> This plan is written for agentic workers. Each task is self-contained: it names
> the exact files, the exact code, and the exact commands to run. Do not read
> other plans for context, and do not treat any step as "similar to" another —
> every step spells out what it needs. Work the tasks in order; each ends at a
> green gate and a commit.

**Goal**

Bring `idrac_exporter` onto the two family standards it was skipped by on
2026-08-01: the always-200 `/livez` + `/readyz` probe pattern, and the Alpine
container-image standard (unpinned base, `HEALTHCHECK` in both Dockerfiles,
`healthcheck:` in both compose files). Additionally, give the currently-empty
`/health` handler the family's informational JSON body — here, one entry per
configured BMC host.

**Architecture**

`idrac_exporter` is a **multi-target, blackbox-style exporter**. `/metrics?target=…`
collects one BMC per request (or every configured host when no target is given);
there is no always-running per-cluster collection loop feeding `/metrics`. The
`SnapshotStore` in `internal/collector/snapshot.go` exists but is **only** created
and fed when `otlp.enabled` is true (`cmd/idrac_exporter/main.go` builds it inside
`if config.Config.OTLP.Enabled`), and `/metrics` never reads it.

Consequence for this work: **there is no collection state that `/health` could
meaningfully report on**, and no snapshot to consult. The `/health` body is
derived purely from configuration — the set of BMC hosts the exporter is set up
to scrape — plus build info. Do not invent a "last scrape" or "ok" field; there
is nothing behind it. Do not wire `/health` to the `SnapshotStore`: it would be
empty for every user who has not enabled OTLP.

Routes are registered with top-level `http.HandleFunc` on `http.DefaultServeMux`
(`main.go` lines 86-91) and the `http.Server` is constructed with **no `Handler`
field**, so it serves `http.DefaultServeMux` implicitly.

**DECISION (spec decision 4, binding): keep this idiom. Do NOT refactor to an
explicit `http.NewServeMux()` and do NOT set `srv.Handler`.** An implementer who
"improves" this is out of scope and will be reverted. The two new routes go in as
two more `http.HandleFunc` lines next to the existing six.

**Tech Stack**

- Go 1.26.5, stdlib `net/http` + `encoding/json`; cobra CLI; no new dependencies.
- Tests: stdlib `testing` + `net/http/httptest`. `make ci` = `lint test build vuln`.
- Containers: Alpine base, non-root user `idrac` at uid 10001, busybox `wget` for
  the HEALTHCHECK. Docker Compose v2.
- Docs: MkDocs Material with an **explicit `nav:`** in `mkdocs.yml` — a new ADR
  must be added to it to be reachable from the site. That is a discoverability
  requirement, not a build gate: absent a `validation:` block, a docs file
  missing from `nav:` is an INFO notice and `--strict` still exits 0.

**Spec**

`/Users/fjacquet/Projects/obs_exporter/docs/superpowers/specs/2026-08-01-family-standard-catch-up-design.md`
— section "Plan E — `idrac_exporter`", plus "Canonical patterns", "Testing" and
"Documentation".

---

## Global Constraints

1. **`127.0.0.1`, never `localhost`, in every healthcheck.** Alpine's busybox
   `wget` resolves `localhost` to `::1` first; these exporters bind IPv4 only, so
   a `localhost`-based check fails with connection refused **at runtime while
   passing both `hadolint` and `docker compose config`**. This exact bug shipped
   once already.
2. **HEALTHCHECK timeout is `5s` in BOTH the Dockerfile and the compose
   `healthcheck:`.** The family effort shipped a 5s/10s mismatch in all eight
   repos and had to correct it in every final review. Same numbers everywhere:
   `interval 30s`, `timeout 5s`, `retries 3`, `start_period`/`--start-period 10s`.
3. **Port is 9348** everywhere in this repo (`RootConfig.Validate` defaults
   `c.Port = 9348`). No port change in this plan.
4. **hadolint findings are expected, not defects.** `DL3025` (shell-form CMD) is
   unavoidable given the required `… || exit 1` syntax; `DL3007` (unpinned
   `latest` base) and `DL3066` are standing family findings. **Do not add inline
   `# hadolint ignore=` suppressions** and do not treat these as blocking.
   hadolint is not in this repo's CI (`.github/workflows/ci.yml` only calls
   `fjacquet/ci` go-ci + go-security), so it is a local sanity check only.
5. **Verify by BUILDING AND RUNNING the image**, then asserting
   `docker inspect --format='{{.State.Health.Status}}' <container>` prints
   `healthy`. Reading the Dockerfile is not verification.
6. **Apple Silicon note:** building `Dockerfile.goreleaser` locally requires a
   binary built for the target platform. Build with
   `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build …` into `linux/arm64/` and
   pass `--build-arg TARGETPLATFORM=linux/arm64`, or the container dies with
   `exec format error` and the healthcheck never turns green.
7. **This repo's `ENTRYPOINT` is `/app/entrypoint.sh`**, not the binary
   (`entrypoint.sh` sources `/authconfig/$NODE_NAME` then `exec bin/idrac_exporter "$@"`).
   The HEALTHCHECK is unaffected — it runs as a separate exec — but do not assume
   the family's `ENTRYPOINT ["/app/bin/<binary>"]` shape when editing either
   Dockerfile.
8. **ADR number is confirmed by `ls docs/adr/` before writing**, never assumed.
   Nine ADRs exist today (0001-0009), so 0010 is expected — confirm it.
9. **The ADR needs a row in `docs/adr/index.md` AND an entry in `mkdocs.yml`
   `nav:`.** Both conventions exist here. Note this is for discoverability, not
   a build gate: a docs file missing from `nav:` is an INFO notice and
   `mkdocs build --strict` still exits 0 (verified empirically). `--strict` does
   fail on the reverse — a `nav:` entry pointing at a missing file — and on
   broken internal links.
10. **No inline `//nolint` or `# nosemgrep` suppressions** anywhere in this work.
11. Every task ends with a passing gate and a commit. Do not batch commits.

---

## File Structure

| Path | Action | What changes |
|---|---|---|
| `cmd/idrac_exporter/main.go` | Modify | Two new `http.HandleFunc` lines for `/livez` and `/readyz` |
| `cmd/idrac_exporter/handler.go` | Modify | New `staticOKHandler`; `healthHandler` gains the JSON body; new imports |
| `cmd/idrac_exporter/handler_test.go` | Modify | New tests: `staticOKHandler`, `/health` body with and without hosts |
| `internal/config/config.go` | Modify | New `(*RootConfig).TargetHosts()` under `Config.Mutex`; `sort` import |
| `internal/config/model.go` | Modify | New `HostHealth` struct |
| `internal/config/config_test.go` | Modify | New `TestTargetHosts` |
| `Dockerfile` | Modify | `alpine:3.23` → `alpine:latest`; add `HEALTHCHECK` |
| `Dockerfile.goreleaser` | Modify | `alpine:3.23` → `alpine:latest`; add `HEALTHCHECK` |
| `docker-compose.yml` | Modify | `healthcheck:` on the `idrac_exporter` service |
| `docker-compose.ghcr.yml` | Modify | `healthcheck:` on the `idrac_exporter` service |
| `docs/adr/0010-always-200-probes-and-container-healthcheck.md` | Create | The ADR |
| `docs/adr/index.md` | Modify | Table row for 0010 |
| `mkdocs.yml` | Modify | `nav:` entry for ADR 0010 |
| `README.md` | Modify | Endpoints table: `/livez`, `/readyz`, `/health` description |
| `docs/usage.md` | Modify | Endpoints table: same |
| `CLAUDE.md` | Modify | Endpoint list on line 29 |
| `charts/idrac-exporter/values.yaml` | Modify | Probes `/health` → `/livez` and `/readyz` |
| `CHANGELOG.md` | Modify | `## [Unreleased]` → `### Added` / `### Changed` entries |

---

### Task 1: `staticOKHandler` and the `/livez` + `/readyz` routes

**Files:**
- Modify: `/Users/fjacquet/Projects/idrac_exporter/cmd/idrac_exporter/handler.go`
- Modify: `/Users/fjacquet/Projects/idrac_exporter/cmd/idrac_exporter/handler_test.go`
- Modify: `/Users/fjacquet/Projects/idrac_exporter/cmd/idrac_exporter/main.go`

**Interfaces:**
- Consumes: nothing — the handler reads no config, no collector, no snapshot.
- Produces: `func staticOKHandler(rsp http.ResponseWriter, _ *http.Request)` in
  package `main`, used by both new routes.

Note on testing strategy: the tests call `staticOKHandler` directly with
`httptest`. They must **not** register routes on `http.DefaultServeMux` —
`http.HandleFunc` panics on a duplicate pattern, and `main.go` already owns those
registrations. That the routes are actually wired is verified end-to-end in
Task 4, where the container's own `HEALTHCHECK` hits `/livez` over the network.

- [x] **Step 1: Write the failing test.** Append to
      `/Users/fjacquet/Projects/idrac_exporter/cmd/idrac_exporter/handler_test.go`:

  ```go
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
  ```

  and replace the import block at the top of that file
  (`import "testing"`, line 3) with:

  ```go
  import (
  	"net/http"
  	"net/http/httptest"
  	"testing"
  )
  ```

- [x] **Step 2: Run it and watch it fail to compile.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && go test ./cmd/idrac_exporter/ -run TestStaticOKHandler
  ```

  Expect `undefined: staticOKHandler`.

- [x] **Step 3: Implement `staticOKHandler`.** In
      `/Users/fjacquet/Projects/idrac_exporter/cmd/idrac_exporter/handler.go`,
      insert immediately **after** `rootHandler` (which ends at line 47) and
      **before** `healthHandler`:

  ```go
  // staticOKHandler always answers 200. It reads no configuration, no collector
  // and no snapshot, so a probe wired here can never be the reason a healthy
  // process is restarted or pulled from rotation. /livez and /readyz both use
  // it; /health is the endpoint that describes what the exporter is configured
  // to scrape.
  //
  // Never point a probe at /metrics: this exporter collects a BMC per request,
  // so a probe tick would drive a full Redfish scrape and can block behind a
  // slow or unreachable BMC.
  func staticOKHandler(rsp http.ResponseWriter, _ *http.Request) {
  	rsp.WriteHeader(http.StatusOK)
  	_, _ = rsp.Write([]byte("ok"))
  }
  ```

- [x] **Step 4: Run it and watch it pass.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && go test ./cmd/idrac_exporter/ -run TestStaticOKHandler -v
  ```

- [x] **Step 5: Register the routes.** In
      `/Users/fjacquet/Projects/idrac_exporter/cmd/idrac_exporter/main.go`,
      replace the block at lines 86-91:

  ```go
  	http.HandleFunc("/discover", discoverHandler)
  	http.HandleFunc("/metrics", metricsHandler)
  	http.HandleFunc("/health", healthHandler)
  	http.HandleFunc("/reload", reloadHandler)
  	http.HandleFunc("/reset", resetHandler)
  	http.HandleFunc("/", rootHandler)
  ```

  with:

  ```go
  	http.HandleFunc("/discover", discoverHandler)
  	http.HandleFunc("/metrics", metricsHandler)
  	http.HandleFunc("/health", healthHandler)
  	http.HandleFunc("/livez", staticOKHandler)
  	http.HandleFunc("/readyz", staticOKHandler)
  	http.HandleFunc("/reload", reloadHandler)
  	http.HandleFunc("/reset", resetHandler)
  	http.HandleFunc("/", rootHandler)
  ```

  Nothing else in `main.go` changes. Leave `srv := &http.Server{...}` (lines
  98-101) exactly as it is — no `Handler:` field, no `http.NewServeMux()`.

- [x] **Step 6: Smoke-test the live routes.** In one terminal:

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && go run ./cmd/idrac_exporter --config default-config.yml
  ```

  In another:

  ```sh
  curl -sS -o /dev/null -w '%{http_code}\n' http://127.0.0.1:9348/livez
  curl -sS -o /dev/null -w '%{http_code}\n' http://127.0.0.1:9348/readyz
  ```

  Both must print `200`. Stop the server with Ctrl-C.

- [x] **Step 7: Gate and commit.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && make fmt-check vet test
  cd /Users/fjacquet/Projects/idrac_exporter && git add -A && git commit -m "feat(http): serve /livez and /readyz as always-200 probes"
  ```

---

### Task 2: `TargetHosts()` — the configured-BMC view `/health` needs

**Files:**
- Modify: `/Users/fjacquet/Projects/idrac_exporter/internal/config/model.go`
- Modify: `/Users/fjacquet/Projects/idrac_exporter/internal/config/config.go`
- Modify: `/Users/fjacquet/Projects/idrac_exporter/internal/config/config_test.go`

**Interfaces:**
- Consumes: `RootConfig.Hosts map[string]*AuthConfig`, `RootConfig.DefaultTarget`,
  `RootConfig.Mutex`.
- Produces: `func (c *RootConfig) TargetHosts() []HostHealth` and the exported
  `HostHealth` struct.

Why this lives in `internal/config` rather than in the handler: `Config.Hosts` is
mutated by `ReloadConfig` (`cmd/idrac_exporter/config.go` lines 33-49) under
`Config.Mutex`, so a `/health` request racing a SIGHUP reload would be a data
race caught by `go test -race`. `HasTargetHosts` (config.go lines 135-144) is the
existing precedent for taking that lock inside the package.

- [x] **Step 1: Write the failing test.** Append to
      `/Users/fjacquet/Projects/idrac_exporter/internal/config/config_test.go`:

  ```go
  func TestTargetHosts(t *testing.T) {
  	c := NewConfig()
  	c.DefaultTarget = "10.0.0.11"
  	c.Hosts["10.0.0.11"] = &AuthConfig{Username: "u", Password: "p", Scheme: "https"}
  	c.Hosts["10.0.0.10"] = &AuthConfig{Username: "u", Password: "p", Scheme: "http"}
  	// "default" is a credential fallback, not a BMC — it must not be reported.
  	c.Hosts["default"] = &AuthConfig{Username: "u", Password: "p", Scheme: "https"}

  	got := c.TargetHosts()

  	want := []HostHealth{
  		{Host: "10.0.0.10", Scheme: "http", Default: false},
  		{Host: "10.0.0.11", Scheme: "https", Default: true},
  	}
  	if len(got) != len(want) {
  		t.Fatalf("TargetHosts() = %+v, want %+v", got, want)
  	}
  	for i := range want {
  		if got[i] != want[i] {
  			t.Fatalf("TargetHosts()[%d] = %+v, want %+v", i, got[i], want[i])
  		}
  	}
  }

  func TestTargetHostsEmptyWhenOnlyDefaultCredential(t *testing.T) {
  	c := NewConfig()
  	c.Hosts["default"] = &AuthConfig{Username: "u", Password: "p", Scheme: "https"}

  	if got := c.TargetHosts(); len(got) != 0 {
  		t.Fatalf("TargetHosts() = %+v, want empty", got)
  	}
  }
  ```

  Check the existing import block of that file first; these tests need only
  `testing`, which is already imported.

- [x] **Step 2: Run it and watch it fail.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && go test ./internal/config/ -run TestTargetHosts
  ```

  Expect `c.TargetHosts undefined` / `undefined: HostHealth`.

- [x] **Step 3: Add the `HostHealth` type.** Append to
      `/Users/fjacquet/Projects/idrac_exporter/internal/config/model.go`:

  ```go
  // HostHealth describes one configured BMC target for the /health body. It is
  // configuration only — this exporter collects per request, so there is no
  // per-host collection state to report.
  type HostHealth struct {
  	Host    string
  	Scheme  string
  	Default bool
  }
  ```

- [x] **Step 4: Implement `TargetHosts`.** Append to
      `/Users/fjacquet/Projects/idrac_exporter/internal/config/config.go`,
      immediately after `HasTargetHosts` (which ends at line 144):

  ```go
  // TargetHosts returns every configured BMC host, sorted by host, for the
  // /health body. The "default" key is a credential fallback rather than a
  // target and is excluded, matching HasTargetHosts. Default marks the
  // deprecated default_target. Read under Config.Mutex: a SIGHUP reload can add
  // hosts while a /health request is in flight.
  func (c *RootConfig) TargetHosts() []HostHealth {
  	c.Mutex.Lock()
  	defer c.Mutex.Unlock()

  	out := make([]HostHealth, 0, len(c.Hosts))
  	for name, h := range c.Hosts {
  		if name == "default" {
  			continue
  		}
  		scheme := ""
  		if h != nil {
  			scheme = h.Scheme
  		}
  		out = append(out, HostHealth{
  			Host:    name,
  			Scheme:  scheme,
  			Default: name == c.DefaultTarget,
  		})
  	}
  	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
  	return out
  }
  ```

  Then add `"sort"` to the import block at the top of that file, which currently
  reads:

  ```go
  import (
  	"fmt"
  	"os"
  	"path/filepath"
  	"strings"

  	"github.com/fjacquet/idrac_exporter/internal/log"
  	"github.com/xhit/go-str2duration/v2"
  	"gopkg.in/yaml.v3"
  )
  ```

  making it:

  ```go
  import (
  	"fmt"
  	"os"
  	"path/filepath"
  	"sort"
  	"strings"

  	"github.com/fjacquet/idrac_exporter/internal/log"
  	"github.com/xhit/go-str2duration/v2"
  	"gopkg.in/yaml.v3"
  )
  ```

- [x] **Step 5: Run it and watch it pass.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && go test -race ./internal/config/ -run TestTargetHosts -v
  ```

- [x] **Step 6: Gate and commit.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && make fmt-check vet test
  cd /Users/fjacquet/Projects/idrac_exporter && git add -A && git commit -m "feat(config): add TargetHosts() view of configured BMC hosts"
  ```

---

### Task 3: Give `/health` the informational JSON body

**Files:**
- Modify: `/Users/fjacquet/Projects/idrac_exporter/cmd/idrac_exporter/handler.go`
- Modify: `/Users/fjacquet/Projects/idrac_exporter/cmd/idrac_exporter/handler_test.go`

**Interfaces:**
- Consumes: `config.Config` (`*config.RootConfig`) via `TargetHosts()` from Task 2;
  `version.Version` / `version.Revision`.
- Produces: `/health` → HTTP 200, `Content-Type: application/json`, this exact
  shape:

  ```json
  {
    "status": "ok",
    "version": "1.1.2",
    "revision": "b8f6212",
    "hosts": [
      {"host": "10.0.0.10", "scheme": "https"},
      {"host": "10.0.0.11", "scheme": "https", "default_target": true}
    ]
  }
  ```

  `status` is the constant string `"ok"` — the status code is 200
  unconditionally, and the field exists so the body is self-describing rather
  than to carry a failure. `hosts` is always present and is `[]` (never `null`)
  when nothing but the `default` credential is configured. `default_target` is
  omitted when false. There is deliberately **no** `last_scrape`, `ok`, or `err`
  field: this exporter has no background per-host collection whose result those
  could report.

- [x] **Step 1: Write the failing tests.** Append to
      `/Users/fjacquet/Projects/idrac_exporter/cmd/idrac_exporter/handler_test.go`:

  ```go
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
  ```

  and widen the import block of that file (which Task 1 left as
  `"net/http"`, `"net/http/httptest"`, `"testing"`) to:

  ```go
  import (
  	"encoding/json"
  	"net/http"
  	"net/http/httptest"
  	"strings"
  	"testing"

  	"github.com/fjacquet/idrac_exporter/internal/config"
  )
  ```

- [x] **Step 2: Run them and watch them fail.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && go test ./cmd/idrac_exporter/ -run 'TestHealthHandler'
  ```

  Expect `undefined: healthResponse`.

- [x] **Step 3: Implement the handler.** In
      `/Users/fjacquet/Projects/idrac_exporter/cmd/idrac_exporter/handler.go`,
      replace the existing no-op (lines 49-51):

  ```go
  func healthHandler(rsp http.ResponseWriter, req *http.Request) {
  	// just return a simple 200 for now
  }
  ```

  with:

  ```go
  // hostHealth is one configured BMC target in the /health body.
  type hostHealth struct {
  	Host    string `json:"host"`
  	Scheme  string `json:"scheme"`
  	Default bool   `json:"default_target,omitempty"`
  }

  // healthResponse is the informational /health body.
  //
  // It carries no per-host success state on purpose: this exporter collects a
  // BMC per request (/metrics?target=), so there is no background cycle whose
  // result a "last scrape" field could report. Whether a given BMC is reachable
  // is answered by scraping it — the idrac_up gauge — not by this endpoint.
  type healthResponse struct {
  	Status   string       `json:"status"`
  	Version  string       `json:"version"`
  	Revision string       `json:"revision"`
  	Hosts    []hostHealth `json:"hosts"`
  }

  // healthHandler always answers 200 with an informational body naming every
  // configured BMC host. The status code never depends on configuration or on
  // BMC reachability: /livez and /readyz are the probe endpoints, and a /health
  // that flips to 503 only ever removes a working exporter from rotation.
  func healthHandler(rsp http.ResponseWriter, _ *http.Request) {
  	out := healthResponse{
  		Status:   "ok",
  		Version:  version.Version,
  		Revision: version.Revision,
  		Hosts:    []hostHealth{},
  	}
  	if config.Config != nil {
  		for _, h := range config.Config.TargetHosts() {
  			out.Hosts = append(out.Hosts, hostHealth{
  				Host:    h.Host,
  				Scheme:  h.Scheme,
  				Default: h.Default,
  			})
  		}
  	}

  	rsp.Header().Set(contentTypeHeader, "application/json")
  	rsp.WriteHeader(http.StatusOK)
  	_ = json.NewEncoder(rsp).Encode(out)
  }
  ```

  Then add `"encoding/json"` to that file's import block, which currently reads:

  ```go
  import (
  	"compress/gzip"
  	"fmt"
  	"html/template"
  	"io"
  	"net/http"
  	"strings"
  	"sync"

  	"github.com/fjacquet/idrac_exporter/internal/collector"
  	"github.com/fjacquet/idrac_exporter/internal/config"
  	"github.com/fjacquet/idrac_exporter/internal/log"
  	"github.com/fjacquet/idrac_exporter/internal/version"
  )
  ```

  making it:

  ```go
  import (
  	"compress/gzip"
  	"encoding/json"
  	"fmt"
  	"html/template"
  	"io"
  	"net/http"
  	"strings"
  	"sync"

  	"github.com/fjacquet/idrac_exporter/internal/collector"
  	"github.com/fjacquet/idrac_exporter/internal/config"
  	"github.com/fjacquet/idrac_exporter/internal/log"
  	"github.com/fjacquet/idrac_exporter/internal/version"
  )
  ```

  `config` and `version` are already imported and used elsewhere in the file; no
  other import changes are needed.

- [x] **Step 4: Run them and watch them pass.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && go test -race ./cmd/idrac_exporter/ -run 'TestHealthHandler|TestStaticOKHandler' -v
  ```

- [x] **Step 5: Eyeball the real body.** In one terminal:

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && go run ./cmd/idrac_exporter --config config.yaml
  ```

  In another:

  ```sh
  curl -sS http://127.0.0.1:9348/health
  ```

  Confirm a JSON object with `status`, `version`, `revision`, `hosts`, and that
  no password or username appears anywhere in the output. Stop the server.

- [x] **Step 6: Gate and commit.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && make fmt-check vet test lint
  cd /Users/fjacquet/Projects/idrac_exporter && git add -A && git commit -m "feat(http): give /health an informational per-host JSON body"
  ```

---

### Task 4: `HEALTHCHECK` and `alpine:latest` in `./Dockerfile`

**Files:**
- Modify: `/Users/fjacquet/Projects/idrac_exporter/Dockerfile`

**Interfaces:**
- Consumes: `/livez` on port 9348 from Task 1.
- Produces: a local image whose `docker inspect` health status reaches `healthy`.

- [x] **Step 1: Switch the base image.** In
      `/Users/fjacquet/Projects/idrac_exporter/Dockerfile`, replace line 12:

  ```dockerfile
  FROM alpine:3.23 AS container
  ```

  with:

  ```dockerfile
  FROM alpine:latest AS container
  ```

  This drops the pin deliberately (spec decision 5): all fifteen family repos use
  unpinned `alpine:latest`, and uniformity was chosen over per-repo
  reproducibility. Do not re-pin it here; that is a fifteen-repo decision.

- [x] **Step 2: Add the HEALTHCHECK.** In the same file, insert between the
      `USER idrac` line (line 21) and the `ENTRYPOINT` line (line 23), so the
      tail of the file reads:

  ```dockerfile
  RUN adduser -D -u 10001 idrac && chown -R idrac /app
  USER idrac

  # 127.0.0.1, not localhost: busybox wget tries ::1 first and the exporter binds
  # IPv4 only, so a localhost check fails at runtime while passing every static
  # check. Timeout matches the compose healthcheck (5s) exactly.
  HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://127.0.0.1:9348/livez || exit 1

  ENTRYPOINT ["/app/entrypoint.sh"]
  ```

  Note this image's `ENTRYPOINT` is the shell wrapper `/app/entrypoint.sh`, not
  the binary. Leave it untouched — the HEALTHCHECK runs as its own exec and is
  unaffected.

- [x] **Step 3: Build the image.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && docker build -t idrac_exporter:hc-test .
  ```

- [x] **Step 4: Run it and assert it goes healthy.** The image ships
      `default-config.yml` at `/etc/prometheus/idrac.yml`, which configures only
      the `default` credential — enough for the process to start and serve.

  ```sh
  docker rm -f idrac_hc_test 2>/dev/null; \
  docker run -d --name idrac_hc_test -p 9348:9348 idrac_exporter:hc-test
  ```

  Wait for the start period plus one interval, then check:

  ```sh
  sleep 45 && docker inspect --format='{{.State.Health.Status}}' idrac_hc_test
  ```

  This **must** print `healthy`. If it prints `unhealthy`, read the probe output
  with `docker inspect --format='{{json .State.Health}}' idrac_hc_test` before
  changing anything — the usual cause is a `localhost` slip or a wrong port.

- [x] **Step 5: Confirm the endpoints from outside the container.**

  ```sh
  curl -sS -o /dev/null -w 'livez=%{http_code}\n' http://127.0.0.1:9348/livez
  curl -sS -o /dev/null -w 'readyz=%{http_code}\n' http://127.0.0.1:9348/readyz
  curl -sS http://127.0.0.1:9348/health
  ```

  Then clean up:

  ```sh
  docker rm -f idrac_hc_test
  ```

- [x] **Step 6: Run hadolint and record, do not fix.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && docker run --rm -i hadolint/hadolint < Dockerfile
  ```

  `DL3025` (shell-form CMD) and `DL3007` (unpinned `latest`) are expected here.
  Do not add suppressions and do not change the file to silence them.

- [x] **Step 7: Commit.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && git add -A && git commit -m "build(docker): unpin alpine and add a /livez HEALTHCHECK to the dev image"
  ```

---

### Task 5: `HEALTHCHECK` and `alpine:latest` in `Dockerfile.goreleaser`

**Files:**
- Modify: `/Users/fjacquet/Projects/idrac_exporter/Dockerfile.goreleaser`

**Interfaces:**
- Consumes: `/livez` on port 9348; a pre-built binary at
  `${TARGETPLATFORM}/idrac_exporter` in the build context (GoReleaser's buildx
  layout).
- Produces: the published GHCR image, health-checked.

- [x] **Step 1: Switch the base image.** In
      `/Users/fjacquet/Projects/idrac_exporter/Dockerfile.goreleaser`, replace
      line 4:

  ```dockerfile
  FROM alpine:3.23 AS container
  ```

  with:

  ```dockerfile
  FROM alpine:latest AS container
  ```

- [x] **Step 2: Add the HEALTHCHECK.** In the same file, insert between
      `USER idrac` (line 20) and `ENTRYPOINT` (line 22), so the tail reads:

  ```dockerfile
  EXPOSE 9348

  USER idrac

  # 127.0.0.1, not localhost: busybox wget tries ::1 first and the exporter binds
  # IPv4 only, so a localhost check fails at runtime while passing every static
  # check. Timeout matches the compose healthcheck (5s) exactly.
  HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://127.0.0.1:9348/livez || exit 1

  ENTRYPOINT ["/app/entrypoint.sh"]
  ```

  Do not touch `.goreleaser.yaml`: its `dockers_v2` block already lists
  `default-config.yml` and `entrypoint.sh` under `extra_files`, and the
  HEALTHCHECK needs no additional context file.

- [x] **Step 3: Stage a local build context.** This Dockerfile `COPY`s a
      pre-built binary, so build it first. On Apple Silicon, target arm64 —
      building an amd64 binary and running it locally gives `exec format error`
      and the healthcheck never turns green.

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && \
  mkdir -p linux/arm64 && \
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o linux/arm64/idrac_exporter ./cmd/idrac_exporter
  ```

  (On an amd64 host, use `GOARCH=amd64` and `linux/amd64` consistently in this
  step and the next.)

- [x] **Step 4: Build and run, then assert healthy.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && \
  docker build -f Dockerfile.goreleaser --build-arg TARGETPLATFORM=linux/arm64 -t idrac_exporter:release-hc-test .
  ```

  ```sh
  docker rm -f idrac_release_hc_test 2>/dev/null; \
  docker run -d --name idrac_release_hc_test -p 9348:9348 idrac_exporter:release-hc-test
  sleep 45 && docker inspect --format='{{.State.Health.Status}}' idrac_release_hc_test
  ```

  Must print `healthy`.

- [x] **Step 5: Clean up the staged context.** The `linux/` directory is a build
      artifact and must not be committed.

  ```sh
  docker rm -f idrac_release_hc_test
  cd /Users/fjacquet/Projects/idrac_exporter && rm -rf linux
  git status --porcelain
  ```

  `git status` must show only `Dockerfile.goreleaser` as modified.

- [x] **Step 6: Run hadolint and record, do not fix.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && docker run --rm -i hadolint/hadolint < Dockerfile.goreleaser
  ```

  `DL3025`, `DL3007` and `DL3066` are expected standing findings.

- [x] **Step 7: Commit.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && git add -A && git commit -m "build(docker): unpin alpine and add a /livez HEALTHCHECK to the release image"
  ```

---

### Task 6: `healthcheck:` in both compose files

**Files:**
- Modify: `/Users/fjacquet/Projects/idrac_exporter/docker-compose.yml`
- Modify: `/Users/fjacquet/Projects/idrac_exporter/docker-compose.ghcr.yml`

**Interfaces:**
- Consumes: `/livez` on port 9348.
- Produces: `docker compose ps` reporting `(healthy)` for the exporter service in
  both stacks.

- [x] **Step 1: Add the healthcheck to `docker-compose.yml`.** In the
      `idrac_exporter` service, insert a `healthcheck:` block immediately before
      `restart: unless-stopped` (line 32), so the service tail reads:

  ```yaml
      environment:
        - IDRAC1_HOST=${IDRAC1_HOST:-192.168.1.1}
        - IDRAC1_USERNAME=${IDRAC1_USERNAME:-root}
        - IDRAC1_PASSWORD=${IDRAC1_PASSWORD:-}
        # Second demo host. Its credentials default to the first host's unless set.
        - IDRAC2_HOST=${IDRAC2_HOST:-192.168.1.2}
        - IDRAC2_USERNAME=${IDRAC2_USERNAME:-${IDRAC1_USERNAME:-root}}
        - IDRAC2_PASSWORD=${IDRAC2_PASSWORD:-${IDRAC1_PASSWORD:-}}
      # 127.0.0.1, not localhost: busybox wget tries ::1 first and the exporter
      # binds IPv4 only. Timeout matches the Dockerfile HEALTHCHECK (5s) exactly.
      healthcheck:
        test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://127.0.0.1:9348/livez"]
        interval: 30s
        timeout: 5s
        retries: 3
        start_period: 10s
      restart: unless-stopped
  ```

- [x] **Step 2: Add the same block to `docker-compose.ghcr.yml`.** In its
      `idrac_exporter` service, insert immediately before
      `restart: unless-stopped` (line 26), so the service tail reads:

  ```yaml
      environment:
        - IDRAC1_HOST=${IDRAC1_HOST:-192.168.1.1}
        - IDRAC1_USERNAME=${IDRAC1_USERNAME:-root}
        - IDRAC1_PASSWORD=${IDRAC1_PASSWORD:-}
        # Second demo host. Its credentials default to the first host's unless set.
        - IDRAC2_HOST=${IDRAC2_HOST:-192.168.1.2}
        - IDRAC2_USERNAME=${IDRAC2_USERNAME:-${IDRAC1_USERNAME:-root}}
        - IDRAC2_PASSWORD=${IDRAC2_PASSWORD:-${IDRAC1_PASSWORD:-}}
      # 127.0.0.1, not localhost: busybox wget tries ::1 first and the exporter
      # binds IPv4 only. Timeout matches the Dockerfile HEALTHCHECK (5s) exactly.
      healthcheck:
        test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://127.0.0.1:9348/livez"]
        interval: 30s
        timeout: 5s
        retries: 3
        start_period: 10s
      restart: unless-stopped
  ```

  Do not add a `healthcheck:` to the `prometheus` or `grafana` services — they are
  out of scope.

- [x] **Step 3: Validate both files parse.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && docker compose -f docker-compose.yml config -q && docker compose -f docker-compose.ghcr.yml config -q && echo COMPOSE_OK
  ```

  Remember: `config -q` passing proves nothing about `localhost` vs `127.0.0.1`.
  Step 4 is the actual verification.

- [x] **Step 4: Bring the local stack up and assert healthy.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && docker compose up -d idrac_exporter
  sleep 45 && docker inspect --format='{{.State.Health.Status}}' idrac_exporter
  ```

  Must print `healthy`. Then:

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && docker compose down
  ```

  The GHCR stack cannot be verified until an image carrying the HEALTHCHECK is
  published; its block is byte-identical to the verified one, and the same
  HEALTHCHECK is baked into `Dockerfile.goreleaser` and verified in Task 5.

- [x] **Step 5: Commit.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && git add -A && git commit -m "build(compose): health-check the exporter service against /livez in both stacks"
  ```

---

### Task 7: ADR 0010 + index row + mkdocs nav

**Files:**
- Create: `/Users/fjacquet/Projects/idrac_exporter/docs/adr/0010-always-200-probes-and-container-healthcheck.md`
- Modify: `/Users/fjacquet/Projects/idrac_exporter/docs/adr/index.md`
- Modify: `/Users/fjacquet/Projects/idrac_exporter/mkdocs.yml`

**Interfaces:**
- Consumes: the decisions implemented in Tasks 1-6.
- Produces: a documented decision record reachable from the docs nav.

- [x] **Step 1: Confirm the ADR number.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && ls docs/adr/
  ```

  Nine records (`0001`-`0009`) plus `index.md` are expected, making **0010** the
  next free number. If the listing shows otherwise, use the actual next free
  number and adjust every filename and link below accordingly.

- [x] **Step 2: Write the ADR.** Create
      `/Users/fjacquet/Projects/idrac_exporter/docs/adr/0010-always-200-probes-and-container-healthcheck.md`:

  ```markdown
  # Always-200 `/livez` and `/readyz`, and a container `HEALTHCHECK`

  ## Status

  Accepted — implemented 2026-08-01. Adopts the family probe and container-image
  standards this repo was skipped by; see the family design note
  `2026-08-01-family-standard-catch-up-design.md`.

  ## Context

  `/health` was registered but empty: `healthHandler` wrote nothing and returned a
  bare 200. The Helm chart pointed both the liveness and readiness probes at it,
  and neither Dockerfile nor either compose file declared a health check, so a
  container that had stopped serving looked identical to a healthy one.

  This exporter is blackbox-style: `/metrics?target=` collects one BMC per
  request, and the `SnapshotStore` is only built when OTLP is enabled. There is
  therefore no background collection state a readiness gate could consult — and
  no honest way to express "not ready".

  ## Decision

  Three fixed paths, all unconditionally 200:

  - `/livez` and `/readyz` are wired to one `staticOKHandler` that reads no
    configuration, no collector and no snapshot. A probe here can never be the
    reason a working process is restarted or pulled from rotation.
  - `/health` keeps status 200 unconditionally and gains an informational JSON
    body: `status`, `version`, `revision`, and one `hosts[]` entry per configured
    BMC (`host`, `scheme`, and `default_target` on the deprecated
    `default_target`). The `default` map key is a credential fallback, not a
    target, and is excluded. There is no `last_scrape` or per-host `ok` field —
    per-host reachability is answered by `idrac_up` on a scrape.
  - Probes never point at `/metrics`: a probe tick would drive a real Redfish
    scrape and can block behind an unreachable BMC.

  The routes are registered with `http.HandleFunc` on `http.DefaultServeMux`,
  alongside the six existing routes. This repo's server is deliberately left on
  the default mux — matching the existing idiom was preferred over a refactor
  whose only purpose would be family cosmetics.

  Both Dockerfiles gain
  `HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3` running
  `wget --spider http://127.0.0.1:9348/livez`, and both compose files gain a
  matching `healthcheck:` with identical numbers. The address is `127.0.0.1`, not
  `localhost`: busybox `wget` resolves `localhost` to `::1` first and the exporter
  binds IPv4 only.

  The Alpine base tag is unpinned to `alpine:latest`, replacing `alpine:3.23`.

  ## Consequences

  Kubernetes and Docker probes stop depending on configuration or BMC
  reachability, so a transient BMC outage can no longer restart the exporter or
  remove it from a Service. `/health` becomes useful to a human — it says what the
  exporter is configured to scrape — while remaining useless as a gate, which is
  the point.

  Unpinning the Alpine tag cuts against [ADR 0001](0001-supply-chain-hardening.md),
  which pins GitHub Actions by SHA, the tool versions, and the Go builder: the base
  image becomes the one input whose contents can change between two builds of the
  same commit, which is what the SBOM and provenance attestations exist to nail
  down. Uniformity across the fifteen family repos was chosen over reproducibility
  on the three that pinned. Revisiting it is a family-wide decision, not a
  per-repo one.

  The Helm chart's liveness and readiness probes move from `/health` to `/livez`
  and `/readyz`. `/health` remains served and remains 200, so any external check
  pointed at it keeps working.
  ```

- [x] **Step 3: Add the index row.** In
      `/Users/fjacquet/Projects/idrac_exporter/docs/adr/index.md`, append after
      the 0009 row (line 17):

  ```markdown
  | [0010](0010-always-200-probes-and-container-healthcheck.md) | Always-200 `/livez` / `/readyz` and a container `HEALTHCHECK` | Accepted | 2026-08-01 |
  ```

- [x] **Step 4: Add the nav entry.** In
      `/Users/fjacquet/Projects/idrac_exporter/mkdocs.yml`, append under
      `Architecture Decisions:`, after the `0009` line:

  ```yaml
        - 0010 Always-200 probes & HEALTHCHECK: adr/0010-always-200-probes-and-container-healthcheck.md
  ```

- [x] **Step 5: Build the docs strictly.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && uvx --with mkdocs-material --with pymdown-extensions mkdocs build --strict --site-dir site
  ```

  Must exit 0. A missing nav entry or a broken relative link fails here.

- [x] **Step 6: Commit.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && git add -A && git commit -m "docs(adr): record the always-200 probes and container HEALTHCHECK as ADR-0010"
  ```

---

### Task 8: Sweep user-facing docs and the Helm chart

**Files:**
- Modify: `/Users/fjacquet/Projects/idrac_exporter/README.md`
- Modify: `/Users/fjacquet/Projects/idrac_exporter/docs/usage.md`
- Modify: `/Users/fjacquet/Projects/idrac_exporter/CLAUDE.md`
- Modify: `/Users/fjacquet/Projects/idrac_exporter/charts/idrac-exporter/values.yaml`

**Interfaces:**
- Consumes: the endpoints from Tasks 1 and 3.
- Produces: documentation and chart defaults that match what the binary serves.

Every repo in the family Alpine effort needed a post-review fix wave for exactly
this. Do the sweep now, not after review.

- [x] **Step 1: Find every stale claim.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && \
  grep -rn "alpine:3.23\|/health" --include="*.md" --include="*.yml" --include="*.yaml" . \
    | grep -v "^./site/" | grep -v "^./docs/superpowers/"
  ```

  Everything this prints outside `docs/adr/` (historical records stay as written)
  and `docs/superpowers/` (historical plans and specs stay as written) must be
  reconciled with the steps below.

- [x] **Step 2: Update the README endpoints table.** In
      `/Users/fjacquet/Projects/idrac_exporter/README.md`, replace the `/health`
      row (line 140) so the table reads:

  ```markdown
  | Endpoint    | Parameters | Description                                   |
  | ----------- | ---------- | --------------------------------------------- |
  | `/metrics`  | `target`   | Metrics for the specified target              |
  | `/reset`    | `target`   | Reset internal state for the specified target |
  | `/reload`   |            | Reload the configuration file                 |
  | `/discover` |            | Prometheus HTTP Service Discovery             |
  | `/livez`    |            | Liveness probe — always HTTP 200, reads no state |
  | `/readyz`   |            | Readiness probe — always HTTP 200, reads no state |
  | `/health`   |            | Always HTTP 200 with a JSON body listing the configured BMC hosts |
  | `/`         |            | Landing page                                  |
  ```

- [x] **Step 3: Update the docs endpoints table.** In
      `/Users/fjacquet/Projects/idrac_exporter/docs/usage.md`, replace the
      `/health` row (line 59) so the table reads:

  ```markdown
  | Endpoint    | Parameters | Description                                   |
  | ----------- | ---------- | --------------------------------------------- |
  | `/metrics`  | `target`   | Metrics for the specified target. With no `target` and no `default_target`, collects **all** configured hosts (each labeled `instance`/`system`); needs `honor_labels: true`. |
  | `/reset`    | `target`   | Reset internal state for the specified target |
  | `/reload`   |            | Reload the configuration file                 |
  | `/discover` |            | Prometheus HTTP Service Discovery             |
  | `/livez`    |            | Liveness probe — always HTTP 200, reads no state |
  | `/readyz`   |            | Readiness probe — always HTTP 200, reads no state |
  | `/health`   |            | Always HTTP 200 with a JSON body listing the configured BMC hosts |
  | `/`         |            | Landing page                                  |
  ```

  Then add this paragraph immediately after the paragraph that begins
  `/reset?target=` (it ends with "resets only the hosts whose credentials
  changed."):

  ~~~markdown
  Point Kubernetes and Docker probes at `/livez` and `/readyz`. Both are wired to a
  handler that reads no configuration and no collector state, so a probe can never
  restart a working exporter because a BMC went away. Never probe `/metrics`: it
  collects a BMC per request and can block behind an unreachable one. `/health` is
  informational — it is always 200 and its body names each configured host:

  ```json
  {"status":"ok","version":"1.1.2","revision":"b8f6212","hosts":[{"host":"10.0.0.10","scheme":"https"}]}
  ```
  ~~~

- [x] **Step 4: Update `CLAUDE.md`.** In
      `/Users/fjacquet/Projects/idrac_exporter/CLAUDE.md`, replace line 29:

  ```markdown
  1. `cmd/idrac_exporter/main.go` registers HTTP routes and starts the server. Endpoints: `/metrics` (needs `target`), `/discover` (Prometheus HTTP SD), `/reset`, `/reload`, `/health`, `/`.
  ```

  with:

  ```markdown
  1. `cmd/idrac_exporter/main.go` registers HTTP routes and starts the server. Endpoints: `/metrics` (needs `target`), `/discover` (Prometheus HTTP SD), `/reset`, `/reload`, `/livez`, `/readyz`, `/health`, `/`. Routes go on `http.DefaultServeMux` via top-level `http.HandleFunc` — this repo's idiom (ADR-0010); do not refactor to an explicit mux. `/livez` and `/readyz` share `staticOKHandler` and read no state; `/health` is always 200 with a JSON body naming the configured BMC hosts.
  ```

- [x] **Step 5: Repoint the chart probes.** In
      `/Users/fjacquet/Projects/idrac_exporter/charts/idrac-exporter/values.yaml`,
      replace the probe block (lines 50-57):

  ```yaml
  livenessProbe:
    httpGet:
      path: /health
      port: http
  readinessProbe:
    httpGet:
      path: /health
      port: http
  ```

  with:

  ```yaml
  # /livez and /readyz read no configuration and no collector state, so a probe can
  # never restart a working exporter because a BMC went away (ADR-0010). /health is
  # informational and also always 200, but it renders a JSON body per request.
  livenessProbe:
    httpGet:
      path: /livez
      port: http
  readinessProbe:
    httpGet:
      path: /readyz
      port: http
  ```

  `charts/idrac-exporter/templates/deployment.yaml` renders these values verbatim
  (`{{- toYaml .Values.livenessProbe | nindent 12 }}`) and needs no change.

- [x] **Step 6: Verify the chart still renders.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && helm template charts/idrac-exporter | grep -A4 -E "livenessProbe|readinessProbe"
  ```

  Confirm `/livez` and `/readyz` appear. If `helm` is unavailable, run
  `helm lint charts/idrac-exporter` or skip with a note — the chart CI workflow
  (`.github/workflows/helm-charts.yml`) covers it.

- [x] **Step 7: Re-run the grep from Step 1** and confirm the only remaining
      `/health` and `alpine:3.23` hits are inside `docs/adr/` and
      `docs/superpowers/` (historical records) or are the intentional `/health`
      rows added above.

- [x] **Step 8: Rebuild the docs and commit.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && uvx --with mkdocs-material --with pymdown-extensions mkdocs build --strict --site-dir site
  cd /Users/fjacquet/Projects/idrac_exporter && git add -A && git commit -m "docs: document /livez and /readyz, point the chart probes at them"
  ```

---

### Task 9: CHANGELOG

**Files:**
- Modify: `/Users/fjacquet/Projects/idrac_exporter/CHANGELOG.md`

**Interfaces:**
- Consumes: everything shipped in Tasks 1-8.
- Produces: `## [Unreleased]` entries in Keep a Changelog format.

- [x] **Step 1: Add the entries.** `## [Unreleased]` already exists at line 10,
      with an intro paragraph and `### Added` / `### Changed` / `### Notes`
      subsections. Append these bullets to the **existing** `### Added` list
      (which currently ends with the "MkDocs documentation site and Architecture
      Decision Records." bullet):

  ```markdown
  - `/livez` and `/readyz` probe endpoints, always HTTP 200 and reading no configuration or
    collector state, so a probe can never restart a working exporter because a BMC went away.
  - A `HEALTHCHECK` against `/livez` in both Dockerfiles and a matching `healthcheck:` in
    `docker-compose.yml` and `docker-compose.ghcr.yml`.
  ```

  and append these to the **existing** `### Changed` list (which currently ends
  with the "Renamed the module path…" bullet):

  ```markdown
  - `/health` now returns a JSON body — `status`, `version`, `revision`, and one entry per
    configured BMC host — instead of an empty 200. The status code is still 200
    unconditionally.
  - The Helm chart's liveness and readiness probes moved from `/health` to `/livez` and
    `/readyz`. `/health` is still served and still 200.
  - The container base image is `alpine:latest` instead of `alpine:3.23`, matching the rest of
    the exporter family.
  ```

- [x] **Step 2: Sanity-check the rendering.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && sed -n '10,50p' CHANGELOG.md
  ```

  Confirm the new bullets sit under the right `###` headings and that no duplicate
  heading was introduced.

- [x] **Step 3: Commit.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && git add -A && git commit -m "docs(changelog): record the probes, HEALTHCHECK and /health body"
  ```

---

### Task 10: Full gate

**Files:** none modified unless the gate fails.

**Interfaces:**
- Consumes: the complete change set.
- Produces: evidence that `make ci`, the docs build, both images and both compose
  files are green.

- [x] **Step 1: Run the CI gate.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && make fmt-check vet ci
  ```

  `ci` is `lint test build vuln`. All must pass. Note `make test` runs with
  `-race`; a `/health` test racing a config reload would surface here.

- [x] **Step 2: Rebuild and re-verify the dev image end to end.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && docker build -t idrac_exporter:hc-final . && \
  docker rm -f idrac_hc_final 2>/dev/null; \
  docker run -d --name idrac_hc_final -p 9348:9348 idrac_exporter:hc-final && \
  sleep 45 && docker inspect --format='{{.State.Health.Status}}' idrac_hc_final
  ```

  Must print `healthy`. Then `docker rm -f idrac_hc_final`.

- [x] **Step 3: Re-validate both compose files.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && docker compose -f docker-compose.yml config -q && docker compose -f docker-compose.ghcr.yml config -q && echo COMPOSE_OK
  ```

- [x] **Step 4: Rebuild the docs.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && uvx --with mkdocs-material --with pymdown-extensions mkdocs build --strict --site-dir site
  ```

- [x] **Step 5: Confirm the tree is clean and nothing stray was committed.**

  ```sh
  cd /Users/fjacquet/Projects/idrac_exporter && git status --porcelain && git log --oneline -9
  ```

  `git status` must be empty apart from ignored build output (`site/`, `bin/`,
  `coverage.out`). There must be **no** `linux/` directory from Task 5.

---

## Self-Review

Work through this list before declaring the plan's implementation complete. Each
item is a claim that must be backed by output you actually saw.

- [x] `/livez` and `/readyz` are registered in `main.go` with `http.HandleFunc`
      on `http.DefaultServeMux`, and `srv` still has **no** `Handler:` field.
      There is no `http.NewServeMux()` anywhere in `main.go`.
- [x] `staticOKHandler` references no package-level state — grep it: it touches
      neither `config.` nor `collector.` nor `version.`.
- [x] `/health` returns 200 in every case, including with zero configured hosts
      and with `config.Config == nil`.
- [x] `/health`'s `hosts` field serializes as `[]`, never `null`, when empty —
      `TestHealthHandlerNoHosts` asserts this on the raw body, not the decoded
      struct.
- [x] `/health` never leaks a username, password, or password-file path. Re-read
      the `hostHealth` struct and confirm only `host`, `scheme` and
      `default_target` are present.
- [x] The `default` map key does not appear in the `hosts` list — it is a
      credential fallback, not a BMC.
- [x] `TargetHosts()` takes `Config.Mutex`, and `go test -race ./...` is green.
- [x] Both Dockerfiles say `alpine:latest`; neither says `alpine:3.23`.
- [x] All four healthcheck definitions (2 Dockerfiles, 2 compose files) use
      `127.0.0.1`, port `9348`, path `/livez`. Grep for `localhost` in all four
      and confirm zero hits.
- [x] All four use `timeout 5s` — the Dockerfiles as `--timeout=5s`, the compose
      files as `timeout: 5s`. No 10s anywhere in a timeout position.
- [x] `docker inspect --format='{{.State.Health.Status}}'` printed `healthy` for
      an image built from `./Dockerfile` **and** for one built from
      `Dockerfile.goreleaser`. You saw both words.
- [x] No `# hadolint ignore=`, no `//nolint`, no `# nosemgrep` was added.
- [x] `ENTRYPOINT ["/app/entrypoint.sh"]` is unchanged in both Dockerfiles.
- [x] The ADR number was confirmed with `ls docs/adr/`, the file exists, it has a
      row in `docs/adr/index.md`, **and** an entry in `mkdocs.yml` `nav:`.
- [x] `mkdocs build --strict` exits 0.
- [x] The Helm chart's `livenessProbe` is `/livez` and `readinessProbe` is
      `/readyz`; `helm template` renders them.
- [x] `README.md`, `docs/usage.md` and `CLAUDE.md` list `/livez` and `/readyz`,
      and none of them still describes `/health` as "returns HTTP 200" with no
      body.
- [x] `CHANGELOG.md` `## [Unreleased]` records the probes, the `HEALTHCHECK`, the
      `/health` body, the chart probe move, and the base-image unpin.
- [x] `make ci` is green, and `git status --porcelain` shows nothing stray — in
      particular no `linux/` staging directory.
