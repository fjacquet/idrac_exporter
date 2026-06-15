# Phase 2a — CLI + Logging + Test Harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate the CLI to cobra and logging to logrus (TTY-aware), add `--once`/`--trace`, and stand up the first test harness — all without changing metric output.

**Architecture:** Keep the existing package-level `internal/log` API but back it with logrus behind a TTY-aware formatter. Wrap the current `main()` server path in a cobra `rootCmd`. Add a white-box `httptest`-based test harness in `internal/collector` and verify metrics with `prometheus/client_golang/prometheus/testutil`. The Redfish transport stays `net/http` (resty is Phase 3).

**Tech Stack:** Go 1.26.4, `spf13/cobra`, `sirupsen/logrus`, `golang.org/x/term`, `prometheus/client_golang/testutil`, `httptest`.

**Branch:** `phase2-plumbing` (already created). Spec: `docs/superpowers/specs/2026-06-15-phase2-plumbing-design.md`.

---

## File structure

- `internal/log/logger.go` — **rewrite** internals to logrus; keep `Logger` type + method names.
- `internal/log/default.go` — **modify** singleton construction only.
- `internal/log/logger_test.go` — **create** logger tests.
- `internal/collector/testhelpers_test.go` — **create** mock Redfish server + client/config helpers.
- `internal/collector/refresh_test.go` — **create** first collector test (`RefreshSystem`).
- `internal/collector/testdata/system.json` — **create** fixture.
- `internal/collector/redfish.go` — **modify** `Get`/`Exists` to add token-safe `--trace` logging.
- `internal/config/model.go` — **modify** add `Trace bool` to `RootConfig`-adjacent debug flags (via existing `config.Debug` sibling).
- `internal/config/config.go` — **modify** add a package-level `Trace` flag next to `Debug`.
- `cmd/idrac_exporter/main.go` — **rewrite** to a cobra `rootCmd`; keep server path in `RunE`.
- `cmd/idrac_exporter/once.go` — **create** `--once` logic.
- `Makefile` — **modify** `RUNFLAGS` to double-dash.
- `README.md` — **modify** any `-config` references to `--config`.

---

## Task 1: Test-harness foundation + first collector test

**Files:**

- Create: `internal/collector/testdata/system.json`
- Create: `internal/collector/testhelpers_test.go`
- Create: `internal/collector/refresh_test.go`

- [ ] **Step 1: Create the fixture**

`internal/collector/testdata/system.json`:

```json
{
  "@odata.id": "/redfish/v1/Systems/1",
  "Id": "System.Embedded.1",
  "Manufacturer": "Dell Inc.",
  "Model": "PowerEdge R660",
  "PowerState": "On",
  "Status": { "Health": "OK", "State": "Enabled" }
}
```

- [ ] **Step 2: Write the test harness helpers**

`internal/collector/testhelpers_test.go`:

```go
package collector

import (
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
  body, err := os.ReadFile("testdata/" + file)
  if err != nil {
   t.Fatalf("read fixture %s: %v", file, err)
  }
  mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
   w.Header().Set("Content-Type", "application/json")
   _, _ = w.Write(body)
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
```

- [ ] **Step 3: Write the failing test**

`internal/collector/refresh_test.go`:

```go
package collector

import (
 "strings"
 "testing"

 "github.com/fjacquet/idrac_exporter/internal/config"
 "github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRefreshSystem(t *testing.T) {
 testConfig(t, func(c *config.CollectConfig) { c.System = true })
 srv := mockRedfish(t, map[string]string{"/redfish/v1/Systems/1": "system.json"})
 defer srv.Close()

 mc := NewCollector()
 mc.client = testClient(srv)
 mc.client.path.System = "/redfish/v1/Systems/1"

 const want = `
# HELP idrac_system_health Health status of the system
# TYPE idrac_system_health gauge
idrac_system_health{status="OK"} 0
`
 if err := testutil.CollectAndCompare(mc, strings.NewReader(want), "idrac_system_health"); err != nil {
  t.Fatalf("unexpected metrics: %v", err)
 }
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/collector/ -run TestRefreshSystem -v`
Expected: PASS. (If `RefreshSystem` emits a metric the fixture leaves zero-valued and `CollectAndCompare` complains, the `metricNames` filter `"idrac_system_health"` scopes the comparison — only that family is compared.)

- [ ] **Step 5: Run the full suite + lint**

Run: `make ci`
Expected: PASS (gofmt, vet, lint, race, vuln all green; one test).

- [ ] **Step 6: Commit**

```bash
git add internal/collector/testdata/system.json internal/collector/testhelpers_test.go internal/collector/refresh_test.go
git commit -m "test(2a): add httptest Redfish harness and first RefreshSystem test"
```

---

## Task 2: logrus-backed logger (TTY-aware), behind the existing API

**Files:**

- Modify: `internal/log/logger.go`
- Modify: `internal/log/default.go`
- Create: `internal/log/logger_test.go`

- [ ] **Step 1: Add dependencies**

Run:

```bash
go get github.com/sirupsen/logrus@latest
go get golang.org/x/term@latest
```

Expected: `go.mod` gains both requires.

- [ ] **Step 2: Write the failing test**

`internal/log/logger_test.go`:

```go
package log

import (
 "bytes"
 "encoding/json"
 "strings"
 "testing"
)

func TestNonTTYEmitsJSON(t *testing.T) {
 var buf bytes.Buffer
 l := NewLoggerWithOutput(LevelInfo, &buf) // buffer is not a TTY -> JSON
 l.Info("hello %s", "world")

 var entry map[string]any
 if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
  t.Fatalf("expected JSON log line, got %q: %v", buf.String(), err)
 }
 if entry["msg"] != "hello world" {
  t.Fatalf("msg = %v, want %q", entry["msg"], "hello world")
 }
}

func TestLevelFiltering(t *testing.T) {
 var buf bytes.Buffer
 l := NewLoggerWithOutput(LevelWarn, &buf)
 l.Info("should be filtered")
 l.Warn("should appear")
 out := buf.String()
 if strings.Contains(out, "should be filtered") {
  t.Fatalf("info line leaked at warn level: %q", out)
 }
 if !strings.Contains(out, "should appear") {
  t.Fatalf("warn line missing: %q", out)
 }
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/log/ -run TestNonTTYEmitsJSON -v`
Expected: FAIL — `NewLoggerWithOutput` undefined.

- [ ] **Step 4: Rewrite the logger internals**

Replace the entire contents of `internal/log/logger.go` with:

```go
package log

import (
 "io"
 "os"

 "github.com/sirupsen/logrus"
 "golang.org/x/term"
)

const (
 LevelFatal = iota
 LevelError
 LevelWarn
 LevelInfo
 LevelDebug
)

var levelToLogrus = map[int]logrus.Level{
 LevelFatal: logrus.FatalLevel,
 LevelError: logrus.ErrorLevel,
 LevelWarn:  logrus.WarnLevel,
 LevelInfo:  logrus.InfoLevel,
 LevelDebug: logrus.DebugLevel,
}

const timestampFormat = "2006-01-02T15:04:05.000"

// Logger wraps logrus, preserving the package's printf-style API.
type Logger struct {
 l *logrus.Logger
}

func formatterFor(w io.Writer) logrus.Formatter {
 if f, ok := w.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
  return &logrus.TextFormatter{FullTimestamp: true, TimestampFormat: timestampFormat}
 }
 return &logrus.JSONFormatter{TimestampFormat: timestampFormat}
}

func NewLoggerWithOutput(level int, w io.Writer) *Logger {
 l := logrus.New()
 l.SetOutput(w)
 l.SetFormatter(formatterFor(w))
 l.SetLevel(levelToLogrus[level])
 return &Logger{l: l}
}

// NewLogger builds a logger writing to stdout. The console arg is retained for
// API compatibility and ignored (formatter selection is TTY-aware).
func NewLogger(level int, _ bool) *Logger {
 return NewLoggerWithOutput(level, os.Stdout)
}

func (log *Logger) SetLevel(level int) { log.l.SetLevel(levelToLogrus[level]) }

func (log *Logger) SetLogFile(path string) error {
 f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
 if err != nil {
  return err
 }
 log.l.SetOutput(f)
 log.l.SetFormatter(formatterFor(f))
 return nil
}

func (log *Logger) Fatal(format string, args ...any) { log.l.Fatalf(format, args...) }
func (log *Logger) Error(format string, args ...any) { log.l.Errorf(format, args...) }
func (log *Logger) Warn(format string, args ...any)  { log.l.Warnf(format, args...) }
func (log *Logger) Info(format string, args ...any)  { log.l.Infof(format, args...) }
func (log *Logger) Debug(format string, args ...any) { log.l.Debugf(format, args...) }
```

Note: logrus `Fatalf` now calls `os.Exit(1)` — this is an intentional fix (the old `Fatal` logged but did not exit, which could let a failed config load continue with a nil config).

- [ ] **Step 5: Update the singleton constructor**

In `internal/log/default.go`, replace the `var logger = &Logger{...}` block with:

```go
var logger = NewLogger(LevelInfo, true)
```

Leave the package-level `SetDefaultLogger/SetLogFile/SetLevel/Fatal/Error/Warn/Info/Debug` functions unchanged — they delegate to `logger`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/log/ -v`
Expected: PASS (both tests).

- [ ] **Step 7: Run the full gate**

Run: `make ci`
Expected: PASS. Fix any call sites if vet/lint flags an unused field (the old `Logger` fields are gone).

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum internal/log/
git commit -m "feat(2a): back internal/log with logrus, TTY-aware text/JSON formatter"
```

---

## Task 3: cobra root command

**Files:**

- Modify: `cmd/idrac_exporter/main.go`

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/spf13/cobra@latest`
Expected: `go.mod` gains cobra.

- [ ] **Step 2: Rewrite main.go around a cobra rootCmd**

Replace the contents of `cmd/idrac_exporter/main.go` with:

```go
package main

import (
 "fmt"
 "net"
 "net/http"
 "os"
 "runtime"
 "strings"
 "time"

 "github.com/fjacquet/idrac_exporter/internal/config"
 "github.com/fjacquet/idrac_exporter/internal/log"
 "github.com/fjacquet/idrac_exporter/internal/version"
 "github.com/spf13/cobra"
)

var (
 flagVerbose bool
 flagDebug   bool
 flagTrace   bool
 flagOnce    bool
 flagConfig  string
 flagWatch   bool
 flagVersion bool
)

func main() {
 rootCmd := &cobra.Command{
  Use:           "idrac_exporter",
  Short:         "Redfish (iDRAC, iLO, XClarity, ...) exporter for Prometheus",
  SilenceUsage:  true,
  SilenceErrors: true,
  RunE:          run,
 }

 f := rootCmd.PersistentFlags()
 f.StringVar(&flagConfig, "config", "/etc/prometheus/idrac.yml", "Path to the configuration file")
 f.BoolVar(&flagVerbose, "verbose", false, "Enable more verbose logging")
 f.BoolVar(&flagDebug, "debug", false, "Dump JSON responses from Redfish requests (implies --verbose)")
 f.BoolVar(&flagTrace, "trace", false, "Log each Redfish request (method, path, status) without credentials")
 f.BoolVar(&flagOnce, "once", false, "Collect every configured host once, print exposition, and exit")
 f.BoolVar(&flagWatch, "config-watch", false, "Watch the configuration file and reload on change")
 f.BoolVar(&flagVersion, "version", false, "Show version and exit")

 if err := rootCmd.Execute(); err != nil {
  log.Fatal("%v", err)
 }
}

func run(_ *cobra.Command, _ []string) error {
 if flagVersion {
  fmt.Printf("version: %s\n", version.Version)
  fmt.Printf("revision: %s\n", version.Revision)
  fmt.Printf("goversion: %s\n", runtime.Version())
  fmt.Printf("platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
  return nil
 }

 log.Info("Build information: version=%s revision=%s", version.Version, version.Revision)
 LoadConfig(flagConfig, flagWatch)

 if flagDebug {
  config.Debug = true
  flagVerbose = true
 }
 if flagTrace {
  config.Trace = true
 }
 if flagVerbose {
  log.SetLevel(log.LevelDebug)
 }

 if flagOnce {
  return runOnce(os.Stdout)
 }

 http.HandleFunc("/discover", discoverHandler)
 http.HandleFunc("/metrics", metricsHandler)
 http.HandleFunc("/health", healthHandler)
 http.HandleFunc("/reload", reloadHandler)
 http.HandleFunc("/reset", resetHandler)
 http.HandleFunc("/", rootHandler)

 port := fmt.Sprintf("%d", config.Config.Port)
 host := strings.Trim(config.Config.Address, "[]")
 bind := net.JoinHostPort(host, port)
 log.Info("Server listening on %s (TLS: %v)", bind, config.Config.TLS.Enabled)

 srv := &http.Server{Addr: bind, ReadHeaderTimeout: 10 * time.Second}
 if config.Config.TLS.Enabled {
  return srv.ListenAndServeTLS(config.Config.TLS.CertFile, config.Config.TLS.KeyFile)
 }
 return srv.ListenAndServe()
}
```

- [ ] **Step 3: Build and smoke-test the CLI**

Run:

```bash
go build ./... && go run ./cmd/idrac_exporter --version
```

Expected: prints version/revision/goversion/platform and exits 0.

- [ ] **Step 4: Verify flags parse**

Run: `go run ./cmd/idrac_exporter --help`
Expected: usage lists `--config --debug --trace --once --verbose --config-watch --version`.

- [ ] **Step 5: Run the gate**

Run: `make ci`
Expected: PASS. (`runOnce` is referenced but defined in Task 4 — if implementing strictly task-by-task, add a temporary `func runOnce(w io.Writer) error { return nil }` stub in `once.go` now and flesh it out in Task 4, or implement Task 4 before building.)

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum cmd/idrac_exporter/main.go
git commit -m "feat(2a): migrate CLI to cobra (rootCmd + --once/--trace flags)"
```

---

## Task 4: `--once` (collect all hosts, sorted exposition)

**Files:**

- Create: `cmd/idrac_exporter/once.go`

- [ ] **Step 1: Implement runOnce**

`cmd/idrac_exporter/once.go`:

```go
package main

import (
 "fmt"
 "io"
 "sort"

 "github.com/fjacquet/idrac_exporter/internal/collector"
 "github.com/fjacquet/idrac_exporter/internal/config"
 "github.com/fjacquet/idrac_exporter/internal/log"
)

// runOnce collects every configured host (except the "default" credential
// fallback) exactly once and writes their exposition to w, sorted by target so
// the output is diffable. It is the live-validation path behind --once.
func runOnce(w io.Writer) error {
 targets := make([]string, 0, len(config.Config.Hosts))
 for t := range config.Config.Hosts {
  if t == "default" {
   continue
  }
  targets = append(targets, t)
 }
 sort.Strings(targets)

 for _, target := range targets {
  c, err := collector.GetCollector(target, "")
  if err != nil {
   log.Error("once: collector for %s: %v", target, err)
   continue
  }
  metrics, err := c.Gather()
  if err != nil {
   log.Error("once: gather %s: %v", target, err)
   continue
  }
  fmt.Fprintf(w, "# target %s\n%s", target, metrics)
 }
 return nil
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success (resolves the Task 3 reference to `runOnce`).

- [ ] **Step 3: Manual verification note**

`--once` drives real Redfish discovery against each configured host, so it is verified manually against a live BMC or the compose stack: `go run ./cmd/idrac_exporter --config config.yml --once --debug | sort`. The exposition is grouped per `# target` and otherwise byte-identical to a `/metrics?target=` scrape. (A full end-to-end automated test would require mocking complete discovery for a host; deferred — the per-`Refresh` tests in Task 1 cover collection correctness.)

- [ ] **Step 4: Run the gate**

Run: `make ci`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/idrac_exporter/once.go
git commit -m "feat(2a): --once collects every configured host and prints sorted exposition"
```

---

## Task 5: `--trace` (token-safe Redfish request logging)

**Files:**

- Modify: `internal/config/config.go` (add `Trace` flag)
- Modify: `internal/collector/redfish.go` (log requests when tracing)
- Create: `internal/collector/trace_test.go`

- [ ] **Step 1: Add the package-level Trace flag**

In `internal/config/config.go`, next to `var Debug bool = false`, add:

```go
var Trace bool = false
```

- [ ] **Step 2: Write the failing test**

`internal/collector/trace_test.go`:

```go
package collector

import (
 "bytes"
 "testing"

 "github.com/fjacquet/idrac_exporter/internal/config"
 "github.com/fjacquet/idrac_exporter/internal/log"
)

func TestTraceNeverLeaksToken(t *testing.T) {
 testConfig(t, func(c *config.CollectConfig) {})
 srv := mockRedfish(t, map[string]string{"/redfish/v1/Systems/1": "system.json"})
 defer srv.Close()

 var buf bytes.Buffer
 log.SetDefaultLogger(log.NewLoggerWithOutput(log.LevelDebug, &buf))
 config.Trace = true
 defer func() { config.Trace = false }()

 c := testClient(srv)
 c.redfish.session.token = "SUPERSECRET-TOKEN"

 var out struct{ Id string }
 c.redfish.Get("/redfish/v1/Systems/1", &out)

 logged := buf.String()
 if !bytes.Contains(buf.Bytes(), []byte("/redfish/v1/Systems/1")) {
  t.Fatalf("trace did not log the request path: %q", logged)
 }
 if bytes.Contains(buf.Bytes(), []byte("SUPERSECRET-TOKEN")) {
  t.Fatalf("trace leaked the auth token: %q", logged)
 }
}
```

- [ ] **Step 3: (no change) confirm the constructor is exported**

`NewLoggerWithOutput` was exported in Task 2, so the collector test installs a buffer-backed logger via `log.NewLoggerWithOutput`. Nothing to rename — just verify it compiles.

- [ ] **Step 4: Run the test to verify it fails**

Run: `go test ./internal/collector/ -run TestTraceNeverLeaksToken -v`
Expected: FAIL — no trace line is logged yet (path assertion fails).

- [ ] **Step 5: Add token-safe trace logging to Get**

In `internal/collector/redfish.go`, inside `func (r *Redfish) Get(...)`, immediately after the request succeeds and the status is known (right after the `resp, err := r.http.Do(req)` error check, before reading the body), add:

```go
 if config.Trace {
  log.Info("trace: GET %s -> %d", path, resp.StatusCode)
 }
```

Apply the same one-liner in `Exists` (`HEAD %s -> %d`). Do **not** log headers or the request body — the `X-Auth-Token` header therefore never reaches the log. (Bodies are still available under `--debug` via the existing `config.Debug` dump, which logs response bodies only — never request headers.)

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/collector/ -run TestTraceNeverLeaksToken -v`
Expected: PASS.

- [ ] **Step 7: Run the gate**

Run: `make ci`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/config/config.go internal/collector/redfish.go internal/collector/trace_test.go internal/log/
git commit -m "feat(2a): token-safe --trace request logging"
```

---

## Task 6: Update callers to double-dash flags

**Files:**

- Modify: `Makefile`
- Modify: `README.md`

- [ ] **Step 1: Update RUNFLAGS**

In `Makefile`, change:

```make
RUNFLAGS ?= -config config.yml -verbose
```

to:

```make
RUNFLAGS ?= --config config.yml --verbose
```

- [ ] **Step 2: Update README flag references**

Run: `grep -n -- '-config ' README.md`
For each hit that shows a CLI invocation, change `-config` to `--config` (leave Prometheus relabel/config YAML untouched). If none, skip.

- [ ] **Step 3: Verify run target works**

Run: `make run` (Ctrl-C after it logs "Server listening"). Expected: starts with `--config`/`--verbose` parsed by cobra.

- [ ] **Step 4: Run the gate**

Run: `make ci`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add Makefile README.md
git commit -m "chore(2a): move RUNFLAGS and docs to double-dash flags"
```

---

## Self-review notes

- **Spec coverage:** cobra (T3), TTY-aware logrus behind the API (T2), `--once`=all hosts (T4), token-safe `--trace` (T5), httptest harness + first test (T1), double-dash caller update (T6). All 2a spec bullets are covered. `--config-watch`/`--verbose` retained (T3 flags).
- **Type consistency:** `NewLoggerWithOutput` is exported in T2 and reused by the T5 collector test via `log.NewLoggerWithOutput`. `testClient`/`mockRedfish`/`testConfig` signatures are used consistently across T1/T5. `runOnce(io.Writer) error` matches the call in `main.go` (T3) and the definition (T4).
- **Ordering caveat:** T3 references `runOnce` from T4 — implement T4 immediately after T3 (or stub as noted) so the tree always builds before each commit.
- **Contract-neutral:** no task adds/removes/relabels a metric; T1 asserts the existing `idrac_system_health` exposition unchanged.
