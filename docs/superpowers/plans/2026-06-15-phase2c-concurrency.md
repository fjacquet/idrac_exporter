# Phase 2c — Concurrency + Unchecked Collector Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `sync.WaitGroup` scrape fan-out with `errgroup`, add a `concurrency` config knob that bounds parallelism and the connection pool, make every refresh panic-safe, and convert the collector to an unchecked collector — all without changing metric output.

**Architecture:** `CollectServer` builds a slice of per-group task closures and hands them to a new `runLimited(limit, tasks)` helper that runs them through an `errgroup.Group`, calling `SetLimit(n)` only when `concurrency > 0` (0 = today's unlimited fan-out). Each task runs its `RefreshXxx` through a new panic-recovering `collector.refresh` helper, so a panic on malformed BMC JSON counts one error and degrades gracefully instead of crashing the process or hanging the group (errgroup does **not** recover panics — the `recover()` is load-bearing). When `concurrency > 0`, the Redfish transport's `MaxConnsPerHost`/`MaxIdleConnsPerHost` are sized to it. `Describe()` becomes a no-op (unchecked collector) because the metric name set is dynamic.

**Tech Stack:** Go 1.26.4, `golang.org/x/sync/errgroup`, `prometheus/client_golang`, existing `internal/collector` httptest harness.

**Branch:** `phase2c-concurrency` (off `main`, already created). Design: [Phase 2 plumbing spec §2c](../specs/2026-06-15-phase2-plumbing-design.md).

---

## File structure

- `internal/config/model.go` — **modify** add `Concurrency uint` to `RootConfig`.
- `internal/config/env.go` — **modify** add `CONFIG_CONCURRENCY` override.
- `internal/config/config_test.go` — **modify** (append) concurrency env + default tests.
- `internal/collector/collector.go` — **modify** add `runLimited` + `refresh`; rewrite `CollectServer`; wrap the PDU path in `Collect`; make `Describe` a no-op; add the `errgroup` import.
- `internal/collector/concurrency_test.go` — **create** `runLimited` bound test, `refresh` panic/failure/success tests, unchecked-`Describe` test.
- `internal/collector/redfish.go` — **modify** size the transport conn pool from `concurrency` in `NewRedfish`.
- `internal/collector/redfish_test.go` — **create** transport pool-sizing tests.

---

## Task 1: `concurrency` config option

**Files:**

- Modify: `internal/config/model.go`
- Modify: `internal/config/env.go:64-66`
- Test: `internal/config/config_test.go` (append)

- [ ] **Step 1: Write the failing tests** — append to `internal/config/config_test.go`:

```go
func TestConcurrencyFromEnvironment(t *testing.T) {
 t.Setenv("CONFIG_CONCURRENCY", "4")
 c := NewConfig()
 c.FromEnvironment()
 if c.Concurrency != 4 {
  t.Fatalf("Concurrency = %d, want 4", c.Concurrency)
 }
}

func TestConcurrencyDefaultsToUnlimited(t *testing.T) {
 c := NewConfig()
 c.Hosts["default"] = &AuthConfig{Username: "u", Password: "p", Scheme: "http"}
 if err := c.Validate(); err != nil {
  t.Fatalf("Validate: %v", err)
 }
 if c.Concurrency != 0 {
  t.Fatalf("Concurrency = %d, want 0 (unlimited default)", c.Concurrency)
 }
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/ -run TestConcurrency -v`
Expected: FAIL — `c.Concurrency undefined (type *RootConfig has no field or method Concurrency)`.

- [ ] **Step 3: Add the field** in `internal/config/model.go` `RootConfig`, immediately after the `Timeout` field (line 52):

```go
 Timeout       uint                   `yaml:"timeout"`
 Concurrency   uint                   `yaml:"concurrency"`
```

- [ ] **Step 4: Add the env override** in `internal/config/env.go`, in the `getEnvUint` block (after `CONFIG_TIMEOUT`, line 65):

```go
 getEnvUint("CONFIG_PORT", &c.Port)
 getEnvUint("CONFIG_TIMEOUT", &c.Timeout)
 getEnvUint("CONFIG_CONCURRENCY", &c.Concurrency)
 getEnvUint("CONFIG_DEFAULT_PORT", &port)
```

(No `Validate` default is needed: `0` is the zero value and means "unlimited", which is exactly the current behavior.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/config/ -run TestConcurrency -v`
Expected: PASS (both).

- [ ] **Step 6: Run the full gate**

Run: `make ci`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/config/model.go internal/config/env.go internal/config/config_test.go
git commit -m "feat(2c): add concurrency config option (0 = unlimited)"
```

---

## Task 2: errgroup fan-out + panic-safe refresh

**Files:**

- Modify: `internal/collector/collector.go` (import; `CollectServer` 576-700; `Collect` 702-716; add helpers)
- Test: `internal/collector/concurrency_test.go` (create)

- [ ] **Step 1: Add the dependency**

Run: `go get golang.org/x/sync/errgroup@latest && go mod tidy`
Expected: `golang.org/x/sync` becomes a direct dependency in `go.mod`.

- [ ] **Step 2: Write the failing tests** — create `internal/collector/concurrency_test.go`:

```go
package collector

import (
 "runtime"
 "sync/atomic"
 "testing"
)

// TestRunLimitedBoundsConcurrency asserts SetLimit(n) is honoured: with 8 tasks
// and a limit of 2, no more than 2 run at once.
func TestRunLimitedBoundsConcurrency(t *testing.T) {
 const limit = 2
 const n = 8
 var inFlight, maxSeen atomic.Int32
 release := make(chan struct{})

 tasks := make([]func(), 0, n)
 for i := 0; i < n; i++ {
  tasks = append(tasks, func() {
   cur := inFlight.Add(1)
   for {
    old := maxSeen.Load()
    if cur <= old || maxSeen.CompareAndSwap(old, cur) {
     break
    }
   }
   <-release // hold the slot so concurrency can be observed
   inFlight.Add(-1)
  })
 }

 done := make(chan struct{})
 go func() {
  runLimited(limit, tasks)
  close(done)
 }()

 // Wait until the limit is saturated, then drain everything.
 for inFlight.Load() < limit {
  runtime.Gosched()
 }
 close(release)
 <-done

 if got := maxSeen.Load(); got != limit {
  t.Fatalf("max concurrency = %d, want %d", got, limit)
 }
}

// TestRunLimitedUnlimitedRunsAllTasks asserts limit 0 runs every task.
func TestRunLimitedUnlimitedRunsAllTasks(t *testing.T) {
 var count atomic.Int32
 tasks := make([]func(), 0, 5)
 for i := 0; i < 5; i++ {
  tasks = append(tasks, func() { count.Add(1) })
 }
 runLimited(0, tasks)
 if got := count.Load(); got != 5 {
  t.Fatalf("ran %d tasks, want 5", got)
 }
}

// TestRefreshRecoversPanic asserts a panicking refresh counts one error and does
// not propagate (which would crash the process / hang the errgroup).
func TestRefreshRecoversPanic(t *testing.T) {
 var c Collector
 c.refresh("boom", func() bool { panic("kaboom") })
 if got := c.errors.Load(); got != 1 {
  t.Fatalf("errors = %d, want 1 after panic", got)
 }
}

// TestRefreshCountsFailure asserts a refresh returning false counts one error.
func TestRefreshCountsFailure(t *testing.T) {
 var c Collector
 c.refresh("fail", func() bool { return false })
 if got := c.errors.Load(); got != 1 {
  t.Fatalf("errors = %d, want 1 after false", got)
 }
}

// TestRefreshSuccessNoError asserts a successful refresh counts no error.
func TestRefreshSuccessNoError(t *testing.T) {
 var c Collector
 c.refresh("ok", func() bool { return true })
 if got := c.errors.Load(); got != 0 {
  t.Fatalf("errors = %d, want 0 on success", got)
 }
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/collector/ -run 'TestRunLimited|TestRefresh' -v`
Expected: FAIL — `undefined: runLimited` and `c.refresh undefined`.

- [ ] **Step 4: Add the `errgroup` import** to `internal/collector/collector.go`. In the import block, add:

```go
 "golang.org/x/sync/errgroup"
```

- [ ] **Step 5: Add the two helpers** to `internal/collector/collector.go` (place them immediately above `func (collector *Collector) CollectServer`):

```go
// runLimited runs each task in its own goroutine, bounding concurrency to limit
// when limit > 0. limit == 0 leaves the group unlimited, preserving the historical
// WaitGroup fan-out behavior. errgroup is used purely for SetLimit + join; the
// tasks never return an error (they account for failures via collector.errors).
func runLimited(limit uint, tasks []func()) {
 var g errgroup.Group
 if limit > 0 {
  g.SetLimit(int(limit))
 }
 for _, task := range tasks {
  task := task
  g.Go(func() error {
   task()
   return nil
  })
 }
 _ = g.Wait()
}

// refresh runs a single RefreshXxx call, recovering from panics so malformed BMC
// JSON degrades the scrape gracefully instead of crashing the process or hanging
// the errgroup (which does not recover panics). A panic or a false return each
// count exactly one scrape error.
func (collector *Collector) refresh(name string, fn func() bool) {
 defer func() {
  if r := recover(); r != nil {
   log.Error("Recovered from panic in %s collection: %v", name, r)
   collector.errors.Add(1)
  }
 }()
 if !fn() {
  collector.errors.Add(1)
 }
}
```

- [ ] **Step 6: Rewrite `CollectServer`** in `internal/collector/collector.go` — replace the entire function body (lines 576-700, the `var wg sync.WaitGroup` … `wg.Wait()` block) with:

```go
func (collector *Collector) CollectServer(ch chan<- prometheus.Metric) {
 collect := &config.Config.Collect
 client := collector.client
 var tasks []func()

 if collect.System {
  tasks = append(tasks, func() {
   collector.refresh("system", func() bool { return client.RefreshSystem(collector, ch) })
  })
 }

 if collect.Sensors {
  tasks = append(tasks, func() {
   collector.refresh("sensors", func() bool { return client.RefreshSensors(collector, ch) })
   // Voltage sensors live in the legacy Power resource. When the power
   // group is enabled they are emitted as part of that collection (at no
   // extra cost), otherwise they are fetched here.
   if !collect.Power {
    collector.refresh("voltages", func() bool { return client.RefreshVoltages(collector, ch) })
   }
  })
 }

 if collect.Power {
  tasks = append(tasks, func() {
   collector.refresh("power", func() bool { return client.RefreshPower(collector, ch) })
  })
 }

 if collect.Network {
  tasks = append(tasks, func() {
   collector.refresh("network", func() bool { return client.RefreshNetwork(collector, ch) })
  })
 }

 if collect.Events {
  tasks = append(tasks, func() {
   collector.refresh("events", func() bool { return client.RefreshEventLog(collector, ch) })
  })
 }

 if collect.Storage {
  tasks = append(tasks, func() {
   collector.refresh("storage", func() bool { return client.RefreshStorage(collector, ch) })
  })
 }

 if collect.Memory {
  tasks = append(tasks, func() {
   collector.refresh("memory", func() bool { return client.RefreshMemory(collector, ch) })
  })
 }

 if collect.Processors {
  tasks = append(tasks, func() {
   collector.refresh("processors", func() bool { return client.RefreshProcessors(collector, ch) })
  })
 }

 if collect.Manager {
  tasks = append(tasks, func() {
   collector.refresh("manager", func() bool { return client.RefreshManager(collector, ch) })
  })
 }

 if collect.Extra {
  tasks = append(tasks, func() {
   collector.refresh("extra", func() bool { return client.RefreshDell(collector, ch) })
  })
 }

 runLimited(config.Config.Concurrency, tasks)
}
```

- [ ] **Step 7: Make the PDU path panic-safe** in `internal/collector/collector.go` `Collect` (lines 705-712) — replace the `if len(...) > 0 { ok := … } else { … }` block with:

```go
 if len(collector.client.path.RackPDUs) > 0 {
  collector.refresh("pdus", func() bool { return collector.client.RefreshPDUs(collector, ch) })
 } else {
  collector.CollectServer(ch)
 }
```

- [ ] **Step 8: Run the new tests to verify they pass**

Run: `go test ./internal/collector/ -run 'TestRunLimited|TestRefresh' -v`
Expected: PASS (all five).

- [ ] **Step 9: Run the existing collector tests to confirm behavior is preserved**

Run: `go test ./internal/collector/ -run 'TestRefreshSystem|TestTrace' -race -v`
Expected: PASS — metric output unchanged.

- [ ] **Step 10: Run the full gate**

Run: `make ci`
Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add go.mod go.sum internal/collector/collector.go internal/collector/concurrency_test.go
git commit -m "feat(2c): errgroup fan-out with SetLimit and panic-safe refresh"
```

---

## Task 3: connection-pool tie-in

**Files:**

- Modify: `internal/collector/redfish.go:49-60` (`NewRedfish` transport)
- Test: `internal/collector/redfish_test.go` (create)

- [ ] **Step 1: Write the failing tests** — create `internal/collector/redfish_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/collector/ -run TestNewRedfishConnPool -v`
Expected: FAIL — `TestNewRedfishConnPoolFromConcurrency` gets `(20,10)` instead of `(4,3)`.

- [ ] **Step 3: Size the pool from concurrency** in `internal/collector/redfish.go` `NewRedfish`. Immediately before the `return &Redfish{...}` (line 41), add:

```go
 // Size the connection pool to the configured concurrency when set, so a
 // bounded fan-out does not open more connections than it can use (#189). The
 // 10/20 defaults preserve the historical unlimited behavior.
 maxIdle, maxConns := 10, 20
 if n := config.Config.Concurrency; n > 0 {
  maxIdle = int(n)
  maxConns = int(n) + 1
 }
```

Then in the `http.Transport` literal, replace the two hard-coded lines (53-54):

```go
    MaxIdleConnsPerHost:   maxIdle,                                            // Sized to concurrency when set
    MaxConnsPerHost:       maxConns,                                           // Sized to concurrency when set
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/collector/ -run TestNewRedfishConnPool -v`
Expected: PASS (both).

- [ ] **Step 5: Run the full gate**

Run: `make ci`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/collector/redfish.go internal/collector/redfish_test.go
git commit -m "feat(2c): size Redfish conn pool to configured concurrency"
```

---

## Task 4: unchecked collector

**Files:**

- Modify: `internal/collector/collector.go:502-574` (`Describe`)
- Test: `internal/collector/concurrency_test.go` (append)

- [ ] **Step 1: Write the failing test** — append to `internal/collector/concurrency_test.go`:

```go
// TestDescribeIsUnchecked asserts the collector is unchecked: Describe sends no
// descriptors (the metric name set is dynamic). Metric output is unaffected.
func TestDescribeIsUnchecked(t *testing.T) {
 mc := NewCollector()
 ch := make(chan *prometheus.Desc)
 go func() {
  mc.Describe(ch)
  close(ch)
 }()
 count := 0
 for range ch {
  count++
 }
 if count != 0 {
  t.Fatalf("Describe emitted %d descriptors, want 0 (unchecked collector)", count)
 }
}
```

Add `"github.com/prometheus/client_golang/prometheus"` to the test file's import block.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/collector/ -run TestDescribeIsUnchecked -v`
Expected: FAIL — `Describe emitted 73 descriptors, want 0`.

- [ ] **Step 3: Make `Describe` a no-op** in `internal/collector/collector.go` — replace the whole `Describe` body (the 70-plus `ch <- collector.Xxx` lines) with:

```go
func (collector *Collector) Describe(ch chan<- *prometheus.Desc) {
 // Unchecked collector: the emitted metric name set is built dynamically with
 // variable labels, so no descriptors are advertised at registration time. The
 // same metrics are still produced by Collect; only registration-time descriptor
 // validation is dropped. See Phase 2 plumbing spec §2c.
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/collector/ -run TestDescribeIsUnchecked -v`
Expected: PASS.

- [ ] **Step 5: Confirm registration + emission still work**

Run: `go test ./internal/collector/ -race -v`
Expected: PASS — `NewCollector` still registers, `TestRefreshSystem` still matches expected exposition (unchecked collectors register and gather normally).

- [ ] **Step 6: Run the full gate**

Run: `make ci`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/collector/collector.go internal/collector/concurrency_test.go
git commit -m "feat(2c): make Describe an unchecked collector"
```

---

## Task 5: manual verification + docs touch-up

**Files:**

- Modify: `sample-config.yml` (document `concurrency:`)

- [ ] **Step 1: Document the option** — add to `sample-config.yml` near the existing `timeout:` line, with a comment:

```yaml
# Maximum number of metric groups collected in parallel per scrape.
# 0 (default) = unlimited. >0 caps the fan-out and the per-host connection pool,
# useful for slow BMCs that choke on many concurrent Redfish requests.
concurrency: 0
```

(If `sample-config.yml` has no `timeout:` line, place it under the top-level root options alongside `port:`/`metrics_prefix:`.)

- [ ] **Step 2: Manual smoke — default behavior unchanged**

Run: `make cli && ./bin/idrac_exporter --once --config config.yml --debug 2>/dev/null | sort > /tmp/2c-after.txt`
Expected: non-empty sorted exposition; compare against a pre-2c capture if available — byte-identical metric set.

- [ ] **Step 3: Manual smoke — concurrency caps the fan-out**

Run: `CONFIG_CONCURRENCY=1 ./bin/idrac_exporter --once --config config.yml --trace 2>&1 | head -40`
Expected: requests are serialized (no overlap in trace timing); same metrics emitted; exporter does not crash if a host returns malformed JSON.

- [ ] **Step 4: Run the full gate one final time**

Run: `make ci`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sample-config.yml
git commit -m "docs(2c): document concurrency option in sample-config"
```

---

## Self-review notes

- **Spec coverage (§2c):**
  - *errgroup replaces WaitGroup* → Task 2 (`runLimited` + `CollectServer` rewrite). The PDU path has no internal fan-out (it is a sequential loop), so it only needs the panic wrapper — applied in Task 2 Step 7.
  - *`concurrency` config, 0 = unlimited, >0 SetLimit + conn pool* → Task 1 (field/env) + Task 2 (`SetLimit`) + Task 3 (`MaxConnsPerHost=n+1`, `MaxIdleConnsPerHost=n`).
  - *panic safety via `recover()` that logs + counts, supersedes #189's `defer wg.Done()`* → Task 2 `refresh`. errgroup does not recover panics, so the recover is load-bearing and wraps every spawned refresh **and** the PDU path.
  - *unchecked collector — `Describe()` sends nothing, no output change* → Task 4.
  - tests: *concurrency bounds parallelism* → `TestRunLimitedBoundsConcurrency` (deterministic; `CollectServer` is a thin builder over `runLimited`, so the bound it tests is the one production uses). *panicking refresh → counted error + successful Gather* → `TestRefreshRecoversPanic` (counted, no crash) + the preserved `TestRefreshSystem` (Gather still succeeds). *error total surfaces in `idrac_exporter_scrape_errors_total`* → unchanged emission path in `Collect` (line 715).
- **Placeholder scan:** none — every step shows the real code/commands.
- **Type consistency:** `runLimited(limit uint, tasks []func())` and `(*Collector) refresh(name string, fn func() bool)` are defined in Task 2 and used by `CollectServer`/`Collect` in the same task; `Concurrency uint` defined in Task 1 is read in Tasks 2 and 3; `transportOf`/`installConfig` helpers defined and used within `redfish_test.go` (Task 3). The errgroup task closures capture `ch` and `client` by closure — `client := collector.client` is bound once at the top of `CollectServer`.
- **Contract-neutral:** metric exposition is byte-identical. The only behavioral changes are (a) optional concurrency bounding, (b) panics counted instead of crashing, (c) dropped registration-time descriptor validation. Existing `TestRefreshSystem`/`trace_test` assert the output is unchanged.
- **Untestable-by-unit parts:** true wall-clock request overlap under `--trace` and conn-pool reuse against a live BMC are manual-verified (Task 5); the limiting mechanism (`runLimited`), the panic/error accounting (`refresh`), the pool sizing (`NewRedfish`), and the unchecked `Describe` are all unit-tested.
- **Out of scope (tracked separately):** the reader-writer race reading `config.Config.Concurrency`/`.Collect` without `Config.Mutex` is [issue #4]; this task follows the existing unlocked-read pattern rather than fixing it here.
