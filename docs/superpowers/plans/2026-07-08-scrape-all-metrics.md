# Scrape-All `/metrics` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a bare `/metrics` (no `?target=`) collect every configured host in one response — each series labeled `instance="<bmc>"` and `system="<bmc>"`, plus a per-host `idrac_up` gauge — so Prometheus needs one static scrape config instead of the multi-target `?target=` + relabel pattern.

**Architecture:** Reuse the OTLP loop's existing collect-all machinery. Generalize the labeling primitives (`labelFamilies`/`upFamily`/`gatherTarget`) to take a `names []string` label-key list and a `coerceUntyped` flag. Extract the loop's fan-out into a shared `collectAllHosts(names, coerceUntyped)` that both the loop and a new exported `GatherAll()` call. The handler gains a pure `resolveMetricsMode` ladder that routes bare `/metrics` to `GatherAll()` when no `target`/`default_target` is set but hosts exist. `default_target` stays as a deprecated single-host override.

**Tech Stack:** Go, `prometheus/client_model` (`dto.MetricFamily`), `prometheus/common/expfmt`, cobra HTTP handlers, white-box `httptest` Redfish mock harness.

**Spec:** `docs/superpowers/specs/2026-07-08-scrape-all-metrics-design.md`

## Global Constraints

- `make ci` (`fmt-check`, `vet`, `golangci-lint`, `go test -race`, `govulncheck`) is the gate; every task ends green.
- The `?target=<host>` path and `/discover` HTTP SD are **unchanged** and stay first-class.
- No new **required** config, no toggle. Scrape-all is the new bare-`/metrics` default when `default_target` is empty and hosts are configured.
- Injected label names are the literals `"instance"` and `"system"` (both set to the host value). Not configurable in this work.
- Pull path leaves `UNTYPED` families untyped (`coerceUntyped=false`) so its exposition matches the `?target=` output; the OTLP loop keeps `coerceUntyped=true`.
- The OTLP loop's behavior is unchanged: it still injects only the single `config.Config.OTLP.IdentityLabel` (default `system`) and still coerces UNTYPED→GAUGE.
- Collector tests are white-box (`package collector`) and must **not** run in parallel — `config.Config` is a singleton (see `testhelpers_test.go`).

---

### Task 1: Generalize the labeling primitives to a `names []string` list

Change `labelFamilies`, `upFamily`, and `gatherTarget` to inject a list of label
keys (each set to the host value) and to take a `coerceUntyped` flag. Keep OTLP
behavior identical by having every existing caller pass a single-element list
(`[]string{"system"}` or `[]string{key}`) with `coerceUntyped=true`.

**Files:**
- Modify: `internal/collector/snapshot.go` (`labelFamilies` ~57-79, `upFamily` ~83-95)
- Modify: `internal/collector/loop.go` (`gatherTarget` ~85-106, and its one call site in `collectOnce` ~71)
- Test: `internal/collector/snapshot_test.go` (calls at lines 34, 67, 89)
- Test: `internal/collector/otlp_test.go` (calls at lines 32-33)

**Interfaces:**
- Produces (used by Tasks 2):
  - `func labelFamilies(families []*dto.MetricFamily, names []string, value string, coerceUntyped bool) []*dto.MetricFamily`
  - `func upFamily(names []string, target string, value float64) *dto.MetricFamily`
  - `func gatherTarget(target string, names []string, coerceUntyped bool) []*dto.MetricFamily`

- [ ] **Step 1: Migrate existing test calls and add the new-behavior tests**

In `internal/collector/snapshot_test.go`, update the three existing calls:
- Line 34: `out := labelFamilies(src, "system", "bmc1")` → `out := labelFamilies(src, []string{"system"}, "bmc1", true)`
- Line 67: `out := labelFamilies(src, "system", "bmc1")` → `out := labelFamilies(src, []string{"system"}, "bmc1", true)`
- Line 89: `mf := upFamily("system", "bmc1", 0)` → `mf := upFamily([]string{"system"}, "bmc1", 0)`

In `internal/collector/otlp_test.go`, update lines 32-33:
```go
	labeled := labelFamilies(fams, []string{"system"}, "bmc1", true)
	host := append(labeled, upFamily([]string{"system"}, "bmc1", 1))
```

Append these two new tests to `internal/collector/snapshot_test.go`:
```go
func TestLabelFamiliesInjectsMultipleNames(t *testing.T) {
	src := []*dto.MetricFamily{sampleFamily("idrac_system_health")}
	out := labelFamilies(src, []string{"instance", "system"}, "bmc1", false)
	lbls := out[0].Metric[0].Label
	if len(lbls) != 2 {
		t.Fatalf("got %d labels, want 2: %+v", len(lbls), lbls)
	}
	got := map[string]string{}
	for _, l := range lbls {
		got[l.GetName()] = l.GetValue()
	}
	if got["instance"] != "bmc1" || got["system"] != "bmc1" {
		t.Fatalf("labels = %+v, want instance=bmc1 system=bmc1", got)
	}
}

func TestLabelFamiliesNoCoerceKeepsUntyped(t *testing.T) {
	src := []*dto.MetricFamily{{
		Name: proto.String("idrac_system_machine_info"),
		Type: dto.MetricType_UNTYPED.Enum(),
		Metric: []*dto.Metric{{
			Untyped: &dto.Untyped{Value: proto.Float64(1)},
		}},
	}}
	out := labelFamilies(src, []string{"instance"}, "bmc1", false)
	if out[0].GetType() != dto.MetricType_UNTYPED {
		t.Fatalf("type = %v, want UNTYPED (no coercion)", out[0].GetType())
	}
	if out[0].Metric[0].Untyped == nil {
		t.Fatal("Untyped cleared, want preserved")
	}
}
```

- [ ] **Step 2: Run the collector tests to verify they fail to compile**

Run: `go test ./internal/collector/ 2>&1 | head -20`
Expected: FAIL — build error, `too many arguments in call to labelFamilies` / `upFamily` (production signatures still old).

- [ ] **Step 3: Update `labelFamilies` and `upFamily` in `snapshot.go`**

Replace the body of `labelFamilies` (lines ~57-79) with:
```go
func labelFamilies(families []*dto.MetricFamily, names []string, value string, coerceUntyped bool) []*dto.MetricFamily {
	out := make([]*dto.MetricFamily, 0, len(families))
	for _, mf := range families {
		clone := proto.Clone(mf).(*dto.MetricFamily)
		if coerceUntyped && clone.GetType() == dto.MetricType_UNTYPED {
			clone.Type = dto.MetricType_GAUGE.Enum()
			for _, m := range clone.Metric {
				if m.Untyped != nil {
					m.Gauge = &dto.Gauge{Value: proto.Float64(m.Untyped.GetValue())}
					m.Untyped = nil
				}
			}
		}
		for _, m := range clone.Metric {
			for _, name := range names {
				m.Label = append(m.Label, &dto.LabelPair{
					Name:  proto.String(name),
					Value: proto.String(value),
				})
			}
		}
		out = append(out, clone)
	}
	return out
}
```

Replace the body of `upFamily` (lines ~83-95) with:
```go
func upFamily(names []string, target string, value float64) *dto.MetricFamily {
	name := prometheus.BuildFQName(config.Config.MetricsPrefix, "", "up")
	help := "Whether the last collection of the target succeeded (1) or failed (0)"
	labels := make([]*dto.LabelPair, 0, len(names))
	for _, n := range names {
		labels = append(labels, &dto.LabelPair{Name: proto.String(n), Value: proto.String(target)})
	}
	return &dto.MetricFamily{
		Name: proto.String(name),
		Help: proto.String(help),
		Type: dto.MetricType_GAUGE.Enum(),
		Metric: []*dto.Metric{{
			Label: labels,
			Gauge: &dto.Gauge{Value: proto.Float64(value)},
		}},
	}
}
```

Also update the two doc comments above these functions to say "every name in `names`" instead of "an identity label (key=value)".

- [ ] **Step 4: Update `gatherTarget` and its call site in `loop.go`**

Replace `gatherTarget` (lines ~85-106) with:
```go
func gatherTarget(target string, names []string, coerceUntyped bool) []*dto.MetricFamily {
	collector, err := GetCollector(target, "")
	if err != nil {
		log.Error("snapshot: get collector for %s: %v", target, err)
		return []*dto.MetricFamily{upFamily(names, target, 0)}
	}
	families, err := collector.GatherFamilies()
	if err != nil {
		log.Error("snapshot: gather %s: %v", target, err)
		return []*dto.MetricFamily{upFamily(names, target, 0)}
	}
	// A nil error is not a freshness guarantee: coalesced waiters always return
	// the last cached families with err==nil even if the leader just failed (see
	// GatherFamilies). The hasRealMetric check below is the real gate.
	if !hasRealMetric(families) {
		return []*dto.MetricFamily{upFamily(names, target, 0)}
	}
	labeled := labelFamilies(families, names, target, coerceUntyped)
	return append(labeled, upFamily(names, target, 1))
}
```

In `collectOnce` (line ~71), update the call:
```go
			fams := gatherTarget(target, []string{key}, true)
```

- [ ] **Step 5: Run the collector tests to verify they pass**

Run: `go test ./internal/collector/ 2>&1 | tail -5`
Expected: PASS (`ok  github.com/fjacquet/idrac_exporter/internal/collector`). `TestLoopCollectOnceDegradesPerHost` still asserts `idrac_up{system=bmc1}=1` / `{system=bmc2}=0` — proves OTLP behavior unchanged.

- [ ] **Step 6: Commit**

```bash
git add internal/collector/snapshot.go internal/collector/loop.go internal/collector/snapshot_test.go internal/collector/otlp_test.go
git commit -m "refactor(collector): generalize labeling to a names []string list

labelFamilies/upFamily/gatherTarget now take a list of identity label keys
(each set to the host value) plus a coerceUntyped flag. Existing OTLP callers
pass a single-element list with coerceUntyped=true, so behavior is unchanged.
Prepares the two-label (instance+system), untyped-preserving scrape-all path.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Add `collectAllHosts` + exported `GatherAll` pull entry point

Extract the loop's fan-out into a shared `collectAllHosts(names, coerceUntyped)`
returning a `*Snapshot`; rewire `collectOnce` to use it; add the exported
`GatherAll()` that the handler calls, rendering the merged families to text.

**Files:**
- Modify: `internal/collector/loop.go` (add `collectAllHosts`; rewrite `collectOnce` ~56-80)
- Create: `internal/collector/gather_all.go`
- Test: Create `internal/collector/gather_all_test.go`

**Interfaces:**
- Consumes (from Task 1): `gatherTarget(target string, names []string, coerceUntyped bool)`, `buildSnapshot`, `hostTargets`, `runLimited`.
- Produces (used by Task 4): `func GatherAll() (string, error)` in `package collector`.

- [ ] **Step 1: Write the failing test**

Create `internal/collector/gather_all_test.go`:
```go
package collector

import (
	"strings"
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
)

// TestGatherAllLabelsAndUpPerHost drives GatherAll across a healthy and a down
// host using the pre-populated collectors-map seam (bypasses Redfish discovery),
// mirroring TestLoopCollectOnceDegradesPerHost.
func TestGatherAllLabelsAndUpPerHost(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.System = true })
	config.Config.Hosts["bmc1"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "http"}
	config.Config.Hosts["bmc2"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "http"}

	good := mockRedfish(t, map[string]string{"/redfish/v1/Systems/1": "system.json"})
	defer good.Close()
	bad := mockRedfish(t, map[string]string{}) // 404s everything
	defer bad.Close()

	c1 := NewCollector()
	c1.client = testClient(good)
	c1.client.path.System = "/redfish/v1/Systems/1"
	c2 := NewCollector()
	c2.client = testClient(bad)
	c2.client.path.System = "/redfish/v1/Systems/1"

	mu.Lock()
	collectors["bmc1"] = c1
	collectors["bmc2"] = c2
	mu.Unlock()
	defer Reset("bmc1")
	defer Reset("bmc2")

	out, err := GatherAll()
	if err != nil {
		t.Fatalf("GatherAll: %v", err)
	}

	// expfmt sorts labels by name: instance before system.
	if !strings.Contains(out, `idrac_up{instance="bmc1",system="bmc1"} 1`) {
		t.Errorf("missing up=1 for healthy bmc1:\n%s", out)
	}
	if !strings.Contains(out, `idrac_up{instance="bmc2",system="bmc2"} 0`) {
		t.Errorf("missing up=0 for down bmc2:\n%s", out)
	}
	// healthy host's real metrics carry both identity labels.
	if !strings.Contains(out, `instance="bmc1"`) || !strings.Contains(out, `system="bmc1"`) {
		t.Errorf("bmc1 identity labels missing:\n%s", out)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/collector/ -run TestGatherAllLabelsAndUpPerHost 2>&1 | head -10`
Expected: FAIL — build error, `undefined: GatherAll`.

- [ ] **Step 3: Add `collectAllHosts` and rewrite `collectOnce` in `loop.go`**

Add `collectAllHosts` (place it just above `collectOnce`):
```go
// collectAllHosts collects every configured host (minus the "default"
// credential fallback) concurrently and returns the merged, sorted Snapshot.
// names are the identity label keys injected per host (each set to the host
// value); coerceUntyped controls whether UNTYPED families are converted to
// GAUGE (true for OTLP, false for the pull exposition path).
func collectAllHosts(names []string, coerceUntyped bool) *Snapshot {
	config.Config.Mutex.Lock()
	concurrency := config.Config.Concurrency
	config.Config.Mutex.Unlock()

	targets := hostTargets()

	var accMu sync.Mutex
	perHost := make([][]*dto.MetricFamily, 0, len(targets))

	tasks := make([]func(), 0, len(targets))
	for _, target := range targets {
		target := target
		tasks = append(tasks, func() {
			fams := gatherTarget(target, names, coerceUntyped)
			accMu.Lock()
			perHost = append(perHost, fams)
			accMu.Unlock()
		})
	}
	runLimited(concurrency, tasks)

	return buildSnapshot(perHost)
}
```

Replace the whole body of `collectOnce` (lines ~56-80) with:
```go
func (l *Loop) collectOnce() {
	config.Config.Mutex.Lock()
	key := config.Config.OTLP.IdentityLabel
	config.Config.Mutex.Unlock()

	l.store.Store(collectAllHosts([]string{key}, true))
}
```

(The `sync` and `dto` imports in `loop.go` are still used by `collectAllHosts`, so the import block is unchanged.)

- [ ] **Step 4: Create `internal/collector/gather_all.go`**

```go
package collector

import (
	"strings"

	"github.com/prometheus/common/expfmt"
)

// GatherAll collects every configured host (minus the "default" credential
// fallback) concurrently and returns the merged Prometheus text exposition.
// Each series carries instance="<host>" and system="<host>", plus a per-host
// <prefix>_up gauge (1 if the host produced metrics, 0 if it was unreachable or
// errored). UNTYPED families are left untyped so the output matches the
// per-target /metrics?target= exposition. An individual unreachable host never
// fails the call — it contributes only up=0.
func GatherAll() (string, error) {
	snap := collectAllHosts([]string{"instance", "system"}, false)
	var b strings.Builder
	for _, mf := range snap.families {
		if _, err := expfmt.MetricFamilyToText(&b, mf); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}
```

- [ ] **Step 5: Run the collector tests to verify they pass**

Run: `go test ./internal/collector/ 2>&1 | tail -5`
Expected: PASS. The new `TestGatherAllLabelsAndUpPerHost` passes, and existing `TestLoopCollectOnceDegradesPerHost` / OTLP tests still pass (collectOnce refactor is behavior-preserving).

- [ ] **Step 6: Commit**

```bash
git add internal/collector/loop.go internal/collector/gather_all.go internal/collector/gather_all_test.go
git commit -m "feat(collector): add GatherAll scrape-all pull entry point

Extract the loop's fan-out into collectAllHosts(names, coerceUntyped) and add
exported GatherAll() rendering all hosts to text with instance+system labels and
a per-host up gauge. collectOnce now delegates to collectAllHosts (unchanged
OTLP behavior). Unreachable hosts contribute up=0 without failing the scrape.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Config — `HasTargetHosts()` predicate + `default_target` deprecation warning

**Files:**
- Modify: `internal/config/config.go` (add `HasTargetHosts` method; add warning in `Validate` after the `MetricsPrefix` default ~149)
- Test: Create `internal/config/target_hosts_test.go`

**Interfaces:**
- Produces (used by Task 4): `func (c *RootConfig) HasTargetHosts() bool`.

- [ ] **Step 1: Write the failing test**

Create `internal/config/target_hosts_test.go`:
```go
package config

import "testing"

func TestHasTargetHosts(t *testing.T) {
	c := NewConfig()
	c.Hosts["default"] = &AuthConfig{Username: "u", Password: "p"}
	if c.HasTargetHosts() {
		t.Fatal("only 'default' configured, want false")
	}
	c.Hosts["10.0.0.1"] = &AuthConfig{Username: "u", Password: "p"}
	if !c.HasTargetHosts() {
		t.Fatal("a real host is configured, want true")
	}
}

func TestValidateAcceptsDeprecatedDefaultTarget(t *testing.T) {
	c := NewConfig()
	c.Hosts["default"] = &AuthConfig{Username: "u", Password: "p", Scheme: "http"}
	c.DefaultTarget = "192.168.1.1"
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate with default_target set returned error: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/ -run 'TestHasTargetHosts|TestValidateAcceptsDeprecatedDefaultTarget' 2>&1 | head -10`
Expected: FAIL — build error, `c.HasTargetHosts undefined`.

- [ ] **Step 3: Add `HasTargetHosts` and the deprecation warning**

Add this method to `internal/config/config.go` (e.g. just below `Validate`):
```go
// HasTargetHosts reports whether any host other than the "default" credential
// fallback is configured. Read under Config.Mutex.
func (c *RootConfig) HasTargetHosts() bool {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	for k := range c.Hosts {
		if k != "default" {
			return true
		}
	}
	return false
}
```

In `Validate()`, immediately after the `MetricsPrefix` default block (after line ~149, before the `// hosts` loop), add:
```go
	if c.DefaultTarget != "" {
		log.Warn("config: 'default_target' is deprecated and will be removed in a future release; leave it empty to have a bare /metrics scrape collect all configured hosts")
	}
```
(`internal/log` is already imported in `config.go` — no new import.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/config/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/target_hosts_test.go
git commit -m "feat(config): add HasTargetHosts predicate and deprecate default_target

HasTargetHosts() reports whether any non-default host is configured (drives the
scrape-all routing decision). Validate() logs a deprecation warning when
default_target is set, once per config load. Non-breaking: Validate still accepts it.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Handler — `resolveMetricsMode` ladder + wire bare `/metrics` to `GatherAll`

**Files:**
- Modify: `cmd/idrac_exporter/handler.go` (`metricsHandler` ~78-129; add `metricsMode`/`resolveMetricsMode`; extract `writeMetrics`)
- Test: Create `cmd/idrac_exporter/handler_test.go`

**Interfaces:**
- Consumes: `collector.GatherAll()` (Task 2), `config.Config.HasTargetHosts()` (Task 3).
- Produces: `resolveMetricsMode(target, defaultTarget string, hasHosts bool) (metricsMode, string)` and the `metricsMode` constants.

- [ ] **Step 1: Write the failing test**

Create `cmd/idrac_exporter/handler_test.go`:
```go
package main

import "testing"

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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/idrac_exporter/ -run TestResolveMetricsMode 2>&1 | head -10`
Expected: FAIL — build error, `undefined: metricsMode` / `resolveMetricsMode`.

- [ ] **Step 3: Add the mode type + `resolveMetricsMode` and rewire the handler**

In `cmd/idrac_exporter/handler.go`, add above `metricsHandler`:
```go
type metricsMode int

const (
	modeSingleTarget metricsMode = iota // collect one host (the returned target)
	modeScrapeAll                       // collect every configured host
	modeError                           // 400: nothing resolvable
)

// resolveMetricsMode implements the /metrics routing ladder. hasHosts reports
// whether any non-"default" host is configured.
func resolveMetricsMode(target, defaultTarget string, hasHosts bool) (metricsMode, string) {
	if target != "" {
		return modeSingleTarget, target
	}
	if defaultTarget != "" {
		return modeSingleTarget, defaultTarget
	}
	if hasHosts {
		return modeScrapeAll, ""
	}
	return modeError, ""
}
```

Replace `metricsHandler` (lines ~78-129) with:
```go
func metricsHandler(rsp http.ResponseWriter, req *http.Request) {
	target := req.URL.Query().Get("target")
	mode, target := resolveMetricsMode(target, config.Config.DefaultTarget, config.Config.HasTargetHosts())

	switch mode {
	case modeError:
		log.Error("Received request from %s without 'target' parameter and no hosts configured", req.Host)
		http.Error(rsp, "Query parameter 'target' is mandatory", http.StatusBadRequest)
		return
	case modeScrapeAll:
		log.Debug("Handling scrape-all metrics request from %s", req.Host)
		metrics, err := collector.GatherAll()
		if err != nil {
			errorMsg := fmt.Sprintf("Error collecting metrics for all hosts: %v", err)
			log.Error("%v", errorMsg)
			http.Error(rsp, errorMsg, http.StatusInternalServerError)
			return
		}
		writeMetrics(rsp, req, metrics)
		return
	}

	// modeSingleTarget
	auth := req.URL.Query().Get("auth")
	log.Debug("Handling metrics request from %s for host %s", req.Host, target)

	c, err := collector.GetCollector(target, auth)
	if err != nil {
		errorMsg := fmt.Sprintf("Error instantiating metrics collector for host %s: %v", target, err)
		log.Error("%v", errorMsg)
		http.Error(rsp, errorMsg, http.StatusInternalServerError)
		return
	}

	log.Debug("Collecting metrics for host %s", target)

	metrics, err := c.Gather()
	if err != nil {
		errorMsg := fmt.Sprintf("Error collecting metrics for host %s: %v", target, err)
		log.Error("%v", errorMsg)
		http.Error(rsp, errorMsg, http.StatusInternalServerError)
		return
	}

	log.Debug("Metrics for host %s collected", target)
	writeMetrics(rsp, req, metrics)
}

// writeMetrics writes the exposition text to the response, gzipping when the
// client accepts it. Shared by the single-target and scrape-all paths.
func writeMetrics(rsp http.ResponseWriter, req *http.Request, metrics string) {
	header := rsp.Header()
	header.Set(contentTypeHeader, "text/plain")

	// Code inspired by the official Prometheus metrics http handler
	w := io.Writer(rsp)
	if gzipAccepted(req.Header) {
		header.Set(contentEncodingHeader, "gzip")
		gz := gzipPool.Get().(*gzip.Writer)
		defer gzipPool.Put(gz)

		gz.Reset(w)
		defer func() { _ = gz.Close() }()

		w = gz
	}

	_, _ = io.WriteString(w, metrics)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/idrac_exporter/ 2>&1 | tail -5`
Expected: PASS (`TestResolveMetricsMode` and the existing cmd tests).

- [ ] **Step 5: Build to confirm the binary compiles**

Run: `go build ./... 2>&1 | tail -5`
Expected: no output (success).

- [ ] **Step 6: Commit**

```bash
git add cmd/idrac_exporter/handler.go cmd/idrac_exporter/handler_test.go
git commit -m "feat(handler): bare /metrics collects all hosts via GatherAll

Add resolveMetricsMode ladder: ?target= -> single host; default_target ->
single host (deprecated); else all configured hosts -> collector.GatherAll();
else 400. Extract writeMetrics gzip helper shared by both paths.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Docs — deprecate `default_target`, document scrape-all + `honor_labels`

**Files:**
- Modify: `sample-config.yml` (the `default_target` block ~50-53)
- Modify: `docs/configuration.md` (append a scrape-all subsection after the `http_sd_configs` example, ~line 100)
- Modify: `docs/usage.md` (endpoints table `/metrics` row ~55)

**Interfaces:** none (documentation only).

- [ ] **Step 1: Update `sample-config.yml`**

Replace the `default_target` block (the two comment lines + the value at line ~50-53) with:
```yaml
# DEPRECATED: 'default_target' is retained for backward compatibility and will be
# removed in a future release. When set, a bare /metrics (no ?target=) returns
# ONLY this single host. Leave it empty (or omit it) to have a bare /metrics
# collect ALL configured hosts at once, each series labeled instance/system.
# Environment variable: CONFIG_DEFAULT_TARGET=192.168.1.1
default_target: ""
```

- [ ] **Step 2: Add the scrape-all section to `docs/configuration.md`**

Append after the `http_sd_configs` example (after line ~100):
```markdown
### Scrape all hosts from one static target

If you leave `default_target` empty, a bare `/metrics` (no `target` parameter)
collects **every** configured host in one response, each series labeled
`instance="<bmc>"` and `system="<bmc>"`. Point Prometheus at the exporter with a
single static target and `honor_labels: true` so those labels survive scraping:

```yaml
scrape_configs:
  - job_name: idrac
    honor_labels: true          # keep the exporter's instance/system="<bmc>"
    scrape_timeout: 60s
    static_configs:
      - targets: ['exporter:9348']
```

No `?target=`, no `relabel_configs`, no `/discover`. A down or unreachable BMC
does not fail the scrape — it contributes only
`idrac_up{instance="<bmc>",system="<bmc>"} 0`. Because one scrape collects every
host (bounded by `concurrency`), give Prometheus a generous `scrape_timeout` for
large fleets. The `?target=` and `/discover` patterns above remain fully
supported for operators who prefer per-target scraping.
```

- [ ] **Step 3: Update the endpoints table in `docs/usage.md`**

Change the `/metrics` row (line ~55) from:
```markdown
| `/metrics`  | `target`   | Metrics for the specified target              |
```
to:
```markdown
| `/metrics`  | `target`   | Metrics for the specified target. With no `target` and no `default_target`, collects **all** configured hosts (each labeled `instance`/`system`); needs `honor_labels: true`. |
```

- [ ] **Step 4: Verify docs reference the new behavior and config is consistent**

Run:
```bash
grep -n "honor_labels" docs/configuration.md docs/usage.md && grep -n 'default_target: ""' sample-config.yml
```
Expected: matches in `docs/configuration.md` (and the `usage.md` row), and `default_target: ""` in `sample-config.yml`.

- [ ] **Step 5: Commit**

```bash
git add sample-config.yml docs/configuration.md docs/usage.md
git commit -m "docs: document scrape-all /metrics and deprecate default_target

Add the single-static-target + honor_labels scrape-all example, note the
per-host instance/system labels and idrac_up=0 failure semantics, and mark
default_target deprecated (empty in sample-config).

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Final gate: run `make ci`

- [ ] **Step 1: Run the full CI gate**

Run: `make ci`
Expected: PASS — `fmt-check`, `vet`, `golangci-lint`, `go test -race ./...`, `govulncheck` all succeed.

- [ ] **Step 2: Manual live check (optional but recommended)**

With two BMC entries under `hosts:` and `default_target` empty, run
`bin/idrac_exporter --config config.yml --once` (or start the server and
`curl -s localhost:9348/metrics`) and confirm both hosts appear, each series
carries `instance=`/`system=`, and any unreachable host shows
`idrac_up{...} 0`.
```
