# Phase 4 — Hybrid OTLP Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an optional, off-by-default background snapshot loop that polls the configured `hosts:` and pushes their metrics via OTLP, leaving the primary on-demand `/metrics?target=` path byte-for-byte unchanged.

**Architecture:** Reuse the existing per-target `*Collector`s. A new coalescing-safe `GatherFamilies()` returns gathered `[]*dto.MetricFamily`; a background `Loop` injects a configurable identity label + a per-target `idrac_up` gauge and publishes an immutable `Snapshot` to a `SnapshotStore` (atomic pointer-swap). `SnapshotStore` implements `prometheus.Gatherer`, so the OpenTelemetry Prometheus bridge (`contrib/bridges/prometheus` `MetricProducer`) feeds a `metric.PeriodicReader` → OTLP exporter. No metric emitter is rewritten.

**Tech Stack:** Go 1.26.4, `prometheus/client_golang`, `prometheus/client_model/go` (dto), `google.golang.org/protobuf/proto`, `go.opentelemetry.io/otel/sdk/metric`, `.../exporters/otlp/otlpmetric/otlpmetricgrpc` + `otlpmetrichttp`, `go.opentelemetry.io/contrib/bridges/prometheus`, cobra, errgroup.

**Reference spec:** `docs/superpowers/specs/2026-06-15-phase4-hybrid-otlp-design.md`

**Conventions:**
- Tests live in `package collector` (white-box). They must **not** run in parallel — `config.Config` is a process singleton. Use the existing harness in `internal/collector/testhelpers_test.go` (`testConfig`, `mockRedfish`, `testClient`).
- After each task, `make ci` must stay green.
- Two PRs: Tasks 1–3 = **PR 4a** (contract-neutral); Tasks 4–8 = **PR 4b** (the feature).

---

## PR 4a — infra + refactor (contract-neutral)

### Task 1: Coalescing-safe `GatherFamilies()` refactor

Split the gather/serialize so the loop can get structured families through the same `sync.Cond` coalescing the on-demand path uses. On-demand `/metrics` output stays byte-identical.

**Files:**
- Modify: `internal/collector/collector.go` (struct fields ~21-28; `NewCollector` ~493; `Gather` ~631-669; imports ~3-16)
- Test: `internal/collector/gather_families_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `internal/collector/gather_families_test.go`:

```go
package collector

import (
	"strings"
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestGatherFamiliesReturnsGatheredFamilies asserts GatherFamilies returns the
// same metric the registry produces, and that text Gather() is unchanged.
func TestGatherFamiliesReturnsGatheredFamilies(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.System = true })
	srv := mockRedfish(t, map[string]string{"/redfish/v1/Systems/1": "system.json"})
	defer srv.Close()

	mc := NewCollector()
	mc.client = testClient(srv)
	mc.client.path.System = "/redfish/v1/Systems/1"

	fams, err := mc.GatherFamilies()
	if err != nil {
		t.Fatalf("GatherFamilies: %v", err)
	}
	var found bool
	for _, mf := range fams {
		if mf.GetName() == "idrac_system_health" {
			found = true
		}
	}
	if !found {
		t.Fatalf("idrac_system_health not in gathered families")
	}

	// Text path must still match the existing exposition exactly.
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

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collector/ -run TestGatherFamiliesReturnsGatheredFamilies -v`
Expected: FAIL — `mc.GatherFamilies undefined`.

- [ ] **Step 3: Refactor `collector.go`**

In the import block, add the dto import:

```go
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	dto "github.com/prometheus/client_model/go"
	"golang.org/x/sync/errgroup"
```

In the `Collector` struct, replace the `builder` field with a cached families field:

```go
	client     *Client
	registry   *prometheus.Registry
	collected  *sync.Cond
	collecting bool
	errors     atomic.Uint64
	families   []*dto.MetricFamily
```

In `NewCollector`, delete the line `collector.builder = new(strings.Builder)` (keep the `collected`/`registry` setup lines).

Replace the whole `Gather` method (lines ~631-669) with:

```go
// GatherFamilies gathers the registry into metric families, coalescing
// concurrent callers via sync.Cond so a single collection serves them all.
// The returned slice is shared with other coalesced callers — callers that
// mutate it (e.g. the snapshot loop) must clone first.
func (collector *Collector) GatherFamilies() ([]*dto.MetricFamily, error) {
	collector.collected.L.Lock()

	// A collection is already in progress: wait and return its cached families.
	if collector.collecting {
		collector.collected.Wait()
		families := collector.families
		collector.collected.L.Unlock()
		return families, nil
	}

	collector.collecting = true
	collector.collected.L.Unlock()

	defer func() {
		collector.collected.L.Lock()
		collector.collected.Broadcast()
		collector.collecting = false
		collector.collected.L.Unlock()
	}()

	m, err := collector.registry.Gather()
	if err != nil {
		return nil, err
	}
	collector.families = m
	return m, nil
}

// Gather serializes the gathered families to the Prometheus text exposition
// format. Output is byte-identical to the pre-refactor implementation.
func (collector *Collector) Gather() (string, error) {
	m, err := collector.GatherFamilies()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i := range m {
		if _, err := expfmt.MetricFamilyToText(&b, m[i]); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/collector/ -run 'TestGatherFamilies|TestRefreshSystem' -v`
Expected: PASS (the existing `TestRefreshSystem` confirms exposition is unchanged).

- [ ] **Step 5: Full package + vet**

Run: `go vet ./... && go test ./internal/collector/ -count=1`
Expected: PASS, no vet errors (confirms `strings` is still used and no `builder` references remain).

- [ ] **Step 6: Commit**

```bash
git add internal/collector/collector.go internal/collector/gather_families_test.go
git commit -m "refactor(4a): coalescing-safe GatherFamilies; text Gather derives from it"
```

---

### Task 2: `collection:` and `otlp:` config

Add the two config blocks, env overrides, and `Validate` defaults. No consumer yet.

**Files:**
- Modify: `internal/config/model.go` (add structs; extend `RootConfig`)
- Modify: `internal/config/config.go` (`Validate` ~111-182)
- Modify: `internal/config/env.go` (`FromEnvironment` ~44-116)
- Test: `internal/config/otlp_config_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `internal/config/otlp_config_test.go`:

```go
package config

import "testing"

func TestOTLPConfigDefaults(t *testing.T) {
	c := NewConfig()
	c.Hosts["default"] = &AuthConfig{Username: "u", Password: "p"}
	if err := c.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if c.OTLP.IdentityLabel != "system" {
		t.Errorf("identity_label default = %q, want system", c.OTLP.IdentityLabel)
	}
	if c.OTLP.Protocol != "grpc" {
		t.Errorf("protocol default = %q, want grpc", c.OTLP.Protocol)
	}
	if c.OTLP.Endpoint != "localhost:4317" {
		t.Errorf("endpoint default = %q, want localhost:4317", c.OTLP.Endpoint)
	}
	if c.OTLP.Insecure {
		t.Errorf("insecure default = true, want false (secure by default)")
	}
	if c.Collection.IntervalSeconds != 60 {
		t.Errorf("collection interval = %v, want 60", c.Collection.IntervalSeconds)
	}
	if c.OTLP.IntervalSeconds != 60 {
		t.Errorf("otlp interval default = %v, want 60 (= collection interval)", c.OTLP.IntervalSeconds)
	}
}

func TestOTLPConfigInvalidProtocol(t *testing.T) {
	c := NewConfig()
	c.Hosts["default"] = &AuthConfig{Username: "u", Password: "p"}
	c.OTLP.Protocol = "carrier-pigeon"
	if err := c.Validate(); err == nil {
		t.Fatalf("expected error for invalid protocol")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestOTLPConfig -v`
Expected: FAIL — `c.OTLP undefined`.

- [ ] **Step 3: Add the config structs (`model.go`)**

After the `TLSConfig` struct, add:

```go
type CollectionConfig struct {
	Interval        string `yaml:"interval"`
	IntervalSeconds float64
}

type OTLPConfig struct {
	Enabled         bool              `yaml:"enabled"`
	Endpoint        string            `yaml:"endpoint"`
	Protocol        string            `yaml:"protocol"`
	Insecure        bool              `yaml:"insecure"`
	Interval        string            `yaml:"interval"`
	IdentityLabel   string            `yaml:"identity_label"`
	Headers         map[string]string `yaml:"headers"`
	IntervalSeconds float64
}
```

In `RootConfig`, add two fields (after `Concurrency`):

```go
	Concurrency   uint                   `yaml:"concurrency"`
	Collection    CollectionConfig       `yaml:"collection"`
	OTLP          OTLPConfig             `yaml:"otlp"`
	Hosts         map[string]*AuthConfig `yaml:"hosts"`
```

- [ ] **Step 4: Add `Validate` defaults (`config.go`)**

In `Validate`, just before the final `return nil`, add:

```go
	// collection + otlp
	if c.OTLP.IdentityLabel == "" {
		c.OTLP.IdentityLabel = "system"
	}
	switch c.OTLP.Protocol {
	case "":
		c.OTLP.Protocol = "grpc"
	case "grpc", "http":
	default:
		return fmt.Errorf("invalid otlp protocol: %s", c.OTLP.Protocol)
	}
	if c.OTLP.Endpoint == "" {
		c.OTLP.Endpoint = "localhost:4317"
	}
	if c.Collection.Interval == "" {
		c.Collection.Interval = "60s"
	}
	ci, err := str2duration.ParseDuration(c.Collection.Interval)
	if err != nil {
		return fmt.Errorf("parse collection interval: %v", err)
	}
	c.Collection.IntervalSeconds = ci.Seconds()
	if c.OTLP.Interval == "" {
		c.OTLP.IntervalSeconds = c.Collection.IntervalSeconds
	} else {
		oi, err := str2duration.ParseDuration(c.OTLP.Interval)
		if err != nil {
			return fmt.Errorf("parse otlp interval: %v", err)
		}
		c.OTLP.IntervalSeconds = oi.Seconds()
	}
```

(`str2duration` and `fmt` are already imported in `config.go`.)

- [ ] **Step 5: Add env overrides (`env.go`)**

In `FromEnvironment`, after the existing `getEnvUint("CONFIG_CONCURRENCY", ...)` line add:

```go
	getEnvBool("CONFIG_OTLP_ENABLED", &c.OTLP.Enabled)
	getEnvBool("CONFIG_OTLP_INSECURE", &c.OTLP.Insecure)
	getEnvString("CONFIG_OTLP_ENDPOINT", &c.OTLP.Endpoint)
	getEnvString("CONFIG_OTLP_PROTOCOL", &c.OTLP.Protocol)
	getEnvString("CONFIG_OTLP_INTERVAL", &c.OTLP.Interval)
	getEnvString("CONFIG_OTLP_IDENTITY_LABEL", &c.OTLP.IdentityLabel)
	getEnvString("CONFIG_COLLECTION_INTERVAL", &c.Collection.Interval)
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestOTLPConfig -v`
Expected: PASS (both cases).

- [ ] **Step 7: Commit**

```bash
git add internal/config/model.go internal/config/config.go internal/config/env.go internal/config/otlp_config_test.go
git commit -m "feat(4a): collection + otlp config blocks with defaults and env overrides"
```

---

### Task 3: Graceful shutdown (`serve` helper + signal context)

Replace the bare `ListenAndServe` with a context-driven server that shuts down on SIGINT/SIGTERM. This is the lifecycle seam Task 7 plugs the loop/OTLP into.

**Files:**
- Create: `cmd/idrac_exporter/server.go`
- Modify: `cmd/idrac_exporter/main.go` (`run` ~88-100; imports)
- Test: `cmd/idrac_exporter/server_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `cmd/idrac_exporter/server_test.go`:

```go
package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestServeShutsDownOnContextCancel asserts serve returns nil promptly once the
// context is cancelled, having gracefully shut the server down.
func TestServeShutsDownOnContextCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: http.NewServeMux()}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, srv, ln) }()

	// Give the server a moment to start, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return after context cancel")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/idrac_exporter/ -run TestServeShutsDown -v`
Expected: FAIL — `undefined: serve`.

- [ ] **Step 3: Create `cmd/idrac_exporter/server.go`**

```go
package main

import (
	"context"
	"net"
	"net/http"
	"time"
)

// serve runs srv on ln until it errors or ctx is cancelled. On cancellation it
// gracefully shuts the server down with a bounded timeout and returns nil.
func serve(ctx context.Context, srv *http.Server, ln net.Listener) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shCtx)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/idrac_exporter/ -run TestServeShutsDown -v`
Expected: PASS.

- [ ] **Step 5: Wire `run` to use `serve` with a signal context**

In `main.go`, update the imports to add `context`, `crypto/tls`, `os/signal`, `syscall` (keep the rest):

```go
import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/fjacquet/idrac_exporter/internal/log"
	"github.com/fjacquet/idrac_exporter/internal/version"
	"github.com/spf13/cobra"
)
```

Replace the server-construction tail of `run` (from `srv := &http.Server{...}` to the final `return srv.ListenAndServe()`) with:

```go
	srv := &http.Server{
		Addr:              bind,
		ReadHeaderTimeout: 10 * time.Second, // mitigate Slowloris
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", bind)
	if err != nil {
		return err
	}
	if config.Config.TLS.Enabled {
		cert, err := tls.LoadX509KeyPair(config.Config.TLS.CertFile, config.Config.TLS.KeyFile)
		if err != nil {
			return err
		}
		ln = tls.NewListener(ln, &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		})
	}

	return serve(ctx, srv, ln)
```

- [ ] **Step 6: Run the full command package + build**

Run: `go test ./cmd/idrac_exporter/ -count=1 && go build ./...`
Expected: PASS and a clean build.

- [ ] **Step 7: Commit**

```bash
git add cmd/idrac_exporter/server.go cmd/idrac_exporter/server_test.go cmd/idrac_exporter/main.go
git commit -m "feat(4a): graceful shutdown via signal context and serve helper"
```

- [ ] **Step 8: Gate — run CI**

Run: `make ci`
Expected: green. This closes PR 4a (open it with `gh pr create` per the repo's phase-PR workflow).

---

## PR 4b — the OTLP feature (additive)

### Task 4: Snapshot store + family helpers (`snapshot.go`)

**Files:**
- Create: `internal/collector/snapshot.go`
- Test: `internal/collector/snapshot_test.go`

- [ ] **Step 1: Promote dto/proto to direct deps**

Run:
```bash
go get github.com/prometheus/client_model/go@v0.6.2
go get google.golang.org/protobuf@v1.36.11
```
Expected: `go.mod` now lists both as direct (no `// indirect`).

- [ ] **Step 2: Write the failing test**

Create `internal/collector/snapshot_test.go`:

```go
package collector

import (
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"
)

func sampleFamily(name string) *dto.MetricFamily {
	return &dto.MetricFamily{
		Name: proto.String(name),
		Type: dto.MetricType_GAUGE.Enum(),
		Metric: []*dto.Metric{{
			Gauge: &dto.Gauge{Value: proto.Float64(1)},
		}},
	}
}

func TestSnapshotStoreEmptyGather(t *testing.T) {
	s := NewSnapshotStore()
	fams, err := s.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	if len(fams) != 0 {
		t.Fatalf("empty store gathered %d families, want 0", len(fams))
	}
}

func TestLabelFamiliesDoesNotMutateSource(t *testing.T) {
	src := []*dto.MetricFamily{sampleFamily("idrac_system_health")}
	out := labelFamilies(src, "system", "bmc1")

	if got := len(src[0].Metric[0].Label); got != 0 {
		t.Fatalf("source mutated: %d labels, want 0", got)
	}
	lbls := out[0].Metric[0].Label
	if len(lbls) != 1 || lbls[0].GetName() != "system" || lbls[0].GetValue() != "bmc1" {
		t.Fatalf("identity label not applied to clone: %+v", lbls)
	}
}

func TestBuildSnapshotMergesByName(t *testing.T) {
	host1 := []*dto.MetricFamily{sampleFamily("idrac_system_health")}
	host2 := []*dto.MetricFamily{sampleFamily("idrac_system_health")}
	snap := buildSnapshot([][]*dto.MetricFamily{host1, host2})
	if len(snap.families) != 1 {
		t.Fatalf("merged into %d families, want 1", len(snap.families))
	}
	if got := len(snap.families[0].Metric); got != 2 {
		t.Fatalf("merged family has %d metrics, want 2", got)
	}
}

func TestUpFamilyCarriesIdentityLabel(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.System = true })
	mf := upFamily("system", "bmc1", 0)
	if mf.GetName() != "idrac_up" {
		t.Fatalf("up name = %q, want idrac_up", mf.GetName())
	}
	m := mf.Metric[0]
	if m.Gauge.GetValue() != 0 {
		t.Fatalf("up value = %v, want 0", m.Gauge.GetValue())
	}
	if m.Label[0].GetName() != "system" || m.Label[0].GetValue() != "bmc1" {
		t.Fatalf("up label = %+v, want system=bmc1", m.Label)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/collector/ -run 'TestSnapshot|TestLabelFamilies|TestBuildSnapshot|TestUpFamily' -v`
Expected: FAIL — `NewSnapshotStore undefined`.

- [ ] **Step 4: Create `internal/collector/snapshot.go`**

```go
package collector

import (
	"sort"
	"sync/atomic"

	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"
)

// Snapshot is an immutable set of metric families aggregated across all
// configured hosts. It is published to a SnapshotStore by the background loop
// and read by the OTLP bridge.
type Snapshot struct {
	families []*dto.MetricFamily
}

// SnapshotStore holds the latest Snapshot behind an atomic pointer swap and
// implements prometheus.Gatherer so the OTLP MetricProducer can read it.
type SnapshotStore struct {
	ptr atomic.Pointer[Snapshot]
}

func NewSnapshotStore() *SnapshotStore {
	s := &SnapshotStore{}
	s.ptr.Store(&Snapshot{})
	return s
}

func (s *SnapshotStore) Store(snap *Snapshot) {
	s.ptr.Store(snap)
}

// Gather implements prometheus.Gatherer.
func (s *SnapshotStore) Gather() ([]*dto.MetricFamily, error) {
	snap := s.ptr.Load()
	if snap == nil {
		return nil, nil
	}
	return snap.families, nil
}

// labelFamilies returns a deep copy of families with an identity label
// (key=value) appended to every metric. The source families are never mutated,
// so the collector's cached gather output stays clean for the on-demand path.
func labelFamilies(families []*dto.MetricFamily, key, value string) []*dto.MetricFamily {
	out := make([]*dto.MetricFamily, 0, len(families))
	for _, mf := range families {
		clone := proto.Clone(mf).(*dto.MetricFamily)
		for _, m := range clone.Metric {
			m.Label = append(m.Label, &dto.LabelPair{
				Name:  proto.String(key),
				Value: proto.String(value),
			})
		}
		out = append(out, clone)
	}
	return out
}

// upFamily builds the <prefix>_up metric family for one target, carrying the
// identity label.
func upFamily(key, target string, value float64) *dto.MetricFamily {
	name := prometheus.BuildFQName(config.Config.MetricsPrefix, "", "up")
	help := "Whether the last collection of the target succeeded (1) or failed (0)"
	return &dto.MetricFamily{
		Name: proto.String(name),
		Help: proto.String(help),
		Type: dto.MetricType_GAUGE.Enum(),
		Metric: []*dto.Metric{{
			Label: []*dto.LabelPair{{Name: proto.String(key), Value: proto.String(target)}},
			Gauge: &dto.Gauge{Value: proto.Float64(value)},
		}},
	}
}

// hasRealMetric reports whether families contains any non-meta metric family —
// i.e. the target produced at least one collected metric this cycle. Used to
// decide idrac_up: a target that returns only the build_info / scrape_errors
// bookkeeping metrics is treated as down.
func hasRealMetric(families []*dto.MetricFamily) bool {
	prefix := config.Config.MetricsPrefix
	buildInfo := prometheus.BuildFQName(prefix, "exporter", "build_info")
	scrapeErrors := prometheus.BuildFQName(prefix, "exporter", "scrape_errors_total")
	for _, mf := range families {
		name := mf.GetName()
		if name == buildInfo || name == scrapeErrors {
			continue
		}
		if len(mf.Metric) > 0 {
			return true
		}
	}
	return false
}

// buildSnapshot merges per-host families into one Snapshot. Families sharing a
// name across hosts have their Metric slices concatenated; the result is sorted
// by name for stable output.
func buildSnapshot(perHost [][]*dto.MetricFamily) *Snapshot {
	merged := map[string]*dto.MetricFamily{}
	for _, host := range perHost {
		for _, mf := range host {
			name := mf.GetName()
			if existing, ok := merged[name]; ok {
				existing.Metric = append(existing.Metric, mf.Metric...)
			} else {
				merged[name] = mf
			}
		}
	}
	out := make([]*dto.MetricFamily, 0, len(merged))
	for _, mf := range merged {
		out = append(out, mf)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GetName() < out[j].GetName() })
	return &Snapshot{families: out}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/collector/ -run 'TestSnapshot|TestLabelFamilies|TestBuildSnapshot|TestUpFamily' -v`
Expected: PASS (all four).

- [ ] **Step 6: Commit**

```bash
git add internal/collector/snapshot.go internal/collector/snapshot_test.go go.mod go.sum
git commit -m "feat(4b): snapshot store, identity-label injection, idrac_up, merge helpers"
```

---

### Task 5: Background loop (`loop.go`)

**Files:**
- Create: `internal/collector/loop.go`
- Test: `internal/collector/loop_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/collector/loop_test.go`:

```go
package collector

import (
	"testing"
	"time"

	"github.com/fjacquet/idrac_exporter/internal/config"
	dto "github.com/prometheus/client_model/go"
)

// upValueFor returns the idrac_up gauge value for a given identity-label value.
func upValueFor(t *testing.T, fams []*dto.MetricFamily, system string) (float64, bool) {
	t.Helper()
	for _, mf := range fams {
		if mf.GetName() != "idrac_up" {
			continue
		}
		for _, m := range mf.Metric {
			for _, l := range m.Label {
				if l.GetName() == "system" && l.GetValue() == system {
					return m.Gauge.GetValue(), true
				}
			}
		}
	}
	return 0, false
}

func systemHealthHasLabel(fams []*dto.MetricFamily, system string) bool {
	for _, mf := range fams {
		if mf.GetName() != "idrac_system_health" {
			continue
		}
		for _, m := range mf.Metric {
			for _, l := range m.Label {
				if l.GetName() == "system" && l.GetValue() == system {
					return true
				}
			}
		}
	}
	return false
}

func TestLoopCollectOnceDegradesPerHost(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Hosts["default"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "http"}
	cfg.Hosts["bmc1"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "http"}
	cfg.Hosts["bmc2"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "http"}
	cfg.Collect.System = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	config.SetConfig(cfg)

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

	store := NewSnapshotStore()
	loop := NewLoop(store, time.Minute)
	loop.collectOnce()

	fams, err := store.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	if v, ok := upValueFor(t, fams, "bmc1"); !ok || v != 1 {
		t.Errorf("idrac_up{system=bmc1} = %v (found=%v), want 1", v, ok)
	}
	if v, ok := upValueFor(t, fams, "bmc2"); !ok || v != 0 {
		t.Errorf("idrac_up{system=bmc2} = %v (found=%v), want 0", v, ok)
	}
	if !systemHealthHasLabel(fams, "bmc1") {
		t.Errorf("idrac_system_health missing for healthy host bmc1")
	}
	if systemHealthHasLabel(fams, "bmc2") {
		t.Errorf("idrac_system_health present for failed host bmc2, want absent")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collector/ -run TestLoopCollectOnce -v`
Expected: FAIL — `NewLoop undefined`.

- [ ] **Step 3: Create `internal/collector/loop.go`**

```go
package collector

import (
	"context"
	"sync"
	"time"

	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/fjacquet/idrac_exporter/internal/log"
	dto "github.com/prometheus/client_model/go"
)

// Loop is the optional background collection loop. Each cycle it polls every
// configured host, builds an immutable Snapshot, and publishes it to the store
// for the OTLP exporter to read. The on-demand /metrics path is unaffected.
type Loop struct {
	store    *SnapshotStore
	interval time.Duration
}

func NewLoop(store *SnapshotStore, interval time.Duration) *Loop {
	return &Loop{store: store, interval: interval}
}

// Run collects once immediately (so the snapshot populates without waiting a
// full interval), then on every tick until ctx is cancelled.
func (l *Loop) Run(ctx context.Context) {
	l.collectOnce()
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.collectOnce()
		}
	}
}

// hostTargets returns the configured host keys excluding the "default"
// credentials fallback (which is not a real target).
func hostTargets() []string {
	config.Config.Mutex.Lock()
	defer config.Config.Mutex.Unlock()
	targets := make([]string, 0, len(config.Config.Hosts))
	for target := range config.Config.Hosts {
		if target == "default" {
			continue
		}
		targets = append(targets, target)
	}
	return targets
}

func (l *Loop) collectOnce() {
	key := config.Config.OTLP.IdentityLabel
	targets := hostTargets()

	var mu sync.Mutex
	perHost := make([][]*dto.MetricFamily, 0, len(targets))

	tasks := make([]func(), 0, len(targets))
	for _, target := range targets {
		target := target
		tasks = append(tasks, func() {
			fams := gatherTarget(target, key)
			mu.Lock()
			perHost = append(perHost, fams)
			mu.Unlock()
		})
	}
	runLimited(config.Config.Concurrency, tasks)

	l.store.Store(buildSnapshot(perHost))
}

// gatherTarget collects one host and returns its families with the identity
// label applied plus the <prefix>_up gauge. An unreachable host, a gather
// error, or a cycle that produced no real metric yields only up=0.
func gatherTarget(target, key string) []*dto.MetricFamily {
	collector, err := GetCollector(target, "")
	if err != nil {
		log.Error("snapshot: get collector for %s: %v", target, err)
		return []*dto.MetricFamily{upFamily(key, target, 0)}
	}
	families, err := collector.GatherFamilies()
	if err != nil {
		log.Error("snapshot: gather %s: %v", target, err)
		return []*dto.MetricFamily{upFamily(key, target, 0)}
	}
	if !hasRealMetric(families) {
		return []*dto.MetricFamily{upFamily(key, target, 0)}
	}
	labeled := labelFamilies(families, key, target)
	return append(labeled, upFamily(key, target, 1))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/collector/ -run TestLoopCollectOnce -v`
Expected: PASS.

- [ ] **Step 5: Run with the race detector (loop fans out concurrently)**

Run: `go test ./internal/collector/ -run TestLoopCollectOnce -race -count=1`
Expected: PASS, no race (confirms the `perHost` accumulation mutex and clone-before-label are correct).

- [ ] **Step 6: Commit**

```bash
git add internal/collector/loop.go internal/collector/loop_test.go
git commit -m "feat(4b): background snapshot loop with per-host graceful degradation"
```

---

### Task 6: OTLP exporter + dual-export test (`otlp.go`)

**Files:**
- Create: `internal/collector/otlp.go`
- Test: `internal/collector/otlp_test.go`

- [ ] **Step 1: Add the OpenTelemetry dependencies**

Run:
```bash
go get go.opentelemetry.io/otel/sdk/metric@latest
go get go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc@latest
go get go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp@latest
go get go.opentelemetry.io/contrib/bridges/prometheus@latest
go mod tidy
```
Expected: the four modules appear in `go.mod` `require`.

- [ ] **Step 2: Write the failing dual-export test**

Create `internal/collector/otlp_test.go`:

```go
package collector

import (
	"context"
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
	prombridge "go.opentelemetry.io/contrib/bridges/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestSnapshotDualExport asserts the snapshot is readable through BOTH the
// Prometheus gatherer and an OTLP ManualReader via the bridge — the family
// "assert via both paths" requirement.
func TestSnapshotDualExport(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.System = true })
	srv := mockRedfish(t, map[string]string{"/redfish/v1/Systems/1": "system.json"})
	defer srv.Close()

	mc := NewCollector()
	mc.client = testClient(srv)
	mc.client.path.System = "/redfish/v1/Systems/1"
	fams, err := mc.GatherFamilies()
	if err != nil {
		t.Fatalf("gather families: %v", err)
	}

	store := NewSnapshotStore()
	labeled := labelFamilies(fams, "system", "bmc1")
	host := append(labeled, upFamily("system", "bmc1", 1))
	store.Store(buildSnapshot([][]*dto.MetricFamily{host}))

	// (a) Prometheus gatherer path.
	if !systemHealthHasLabel(mustGather(t, store), "bmc1") {
		t.Fatalf("registry path: idrac_system_health{system=bmc1} missing")
	}

	// (b) OTLP ManualReader path through the bridge.
	producer := prombridge.NewMetricProducer(prombridge.WithGatherer(store))
	reader := metric.NewManualReader(metric.WithProducer(producer))
	_ = metric.NewMeterProvider(metric.WithReader(reader))

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("manual reader collect: %v", err)
	}
	if !otlpHasMetric(rm, "idrac_system_health") {
		t.Fatalf("OTLP path: idrac_system_health missing")
	}
	if !otlpHasMetric(rm, "idrac_up") {
		t.Fatalf("OTLP path: idrac_up missing")
	}
}

func mustGather(t *testing.T, store *SnapshotStore) []*dto.MetricFamily {
	t.Helper()
	fams, err := store.Gather()
	if err != nil {
		t.Fatalf("store gather: %v", err)
	}
	return fams
}

func otlpHasMetric(rm metricdata.ResourceMetrics, name string) bool {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return true
			}
		}
	}
	return false
}
```

(The `dto`, `upValueFor`, `systemHealthHasLabel` helpers come from `snapshot_test.go` / `loop_test.go` in the same package.)

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/collector/ -run TestSnapshotDualExport -v`
Expected: FAIL — compiles but only if `otlp.go` is absent it still builds (test uses only existing symbols + otel). It should PASS already for the bridge path. If it passes, that is acceptable — the test asserts the dual-export contract. Proceed to add `otlp.go` for the exporter wiring (Step 4), which Task 7 needs.

- [ ] **Step 4: Create `internal/collector/otlp.go`**

```go
package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/fjacquet/idrac_exporter/internal/config"
	prombridge "go.opentelemetry.io/contrib/bridges/prometheus"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/sdk/metric"
)

// OTLP wraps the OpenTelemetry MeterProvider that pushes the snapshot via OTLP.
// Its only metric source is the Prometheus bridge reading the SnapshotStore; it
// owns no instruments of its own.
type OTLP struct {
	provider *metric.MeterProvider
}

// NewOTLP builds the OTLP push pipeline: an exporter (gRPC or HTTP per config),
// a periodic reader on otlp.interval, and a MeterProvider fed by the bridge.
func NewOTLP(ctx context.Context, store *SnapshotStore) (*OTLP, error) {
	o := &config.Config.OTLP

	var (
		exporter metric.Exporter
		err      error
	)
	switch o.Protocol {
	case "http":
		opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(o.Endpoint)}
		if o.Insecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		if len(o.Headers) > 0 {
			opts = append(opts, otlpmetrichttp.WithHeaders(o.Headers))
		}
		exporter, err = otlpmetrichttp.New(ctx, opts...)
	default: // grpc
		opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(o.Endpoint)}
		if o.Insecure {
			opts = append(opts, otlpmetricgrpc.WithInsecure())
		}
		if len(o.Headers) > 0 {
			opts = append(opts, otlpmetricgrpc.WithHeaders(o.Headers))
		}
		exporter, err = otlpmetricgrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	producer := prombridge.NewMetricProducer(prombridge.WithGatherer(store))
	reader := metric.NewPeriodicReader(
		exporter,
		metric.WithInterval(time.Duration(o.IntervalSeconds*float64(time.Second))),
		metric.WithProducer(producer),
	)
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	return &OTLP{provider: provider}, nil
}

// Shutdown stops the reader and flushes a final export.
func (o *OTLP) Shutdown(ctx context.Context) error {
	return o.provider.Shutdown(ctx)
}
```

- [ ] **Step 5: Add a construction smoke test**

Append to `internal/collector/otlp_test.go`:

```go
func TestNewOTLPConstructsAndShuts(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.System = true })
	config.Config.OTLP.Endpoint = "localhost:4317"
	config.Config.OTLP.Protocol = "grpc"
	config.Config.OTLP.Insecure = true
	config.Config.OTLP.IntervalSeconds = 60

	store := NewSnapshotStore()
	o, err := NewOTLP(context.Background(), store)
	if err != nil {
		t.Fatalf("NewOTLP: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := o.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}
```

Add `"time"` to the test imports.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/collector/ -run 'TestSnapshotDualExport|TestNewOTLP' -v`
Expected: PASS. (`otlpmetricgrpc.New` does not dial on construction, so no live collector is needed; `Shutdown` flushes against an unreachable endpoint and returns within the timeout.)

- [ ] **Step 7: Commit**

```bash
git add internal/collector/otlp.go internal/collector/otlp_test.go go.mod go.sum
git commit -m "feat(4b): OTLP exporter via prometheus bridge; dual-export test"
```

---

### Task 7: Lifecycle wiring (`main.go`)

Start the loop + OTLP when enabled, and shut them down with the server.

**Files:**
- Modify: `cmd/idrac_exporter/main.go` (`run`)
- Test: manual (`make ci` + a smoke run)

- [ ] **Step 1: Wire the loop/OTLP into `run`**

In `main.go`, replace the final `return serve(ctx, srv, ln)` (added in Task 3) with:

```go
	if config.Config.OTLP.Enabled {
		store := collector.NewSnapshotStore()
		interval := time.Duration(config.Config.Collection.IntervalSeconds * float64(time.Second))
		loop := collector.NewLoop(store, interval)
		go loop.Run(ctx)

		otlp, err := collector.NewOTLP(ctx, store)
		if err != nil {
			return err
		}
		defer func() {
			shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := otlp.Shutdown(shCtx); err != nil {
				log.Error("OTLP shutdown: %v", err)
			}
		}()
		log.Info("OTLP push enabled: endpoint=%s protocol=%s interval=%vs",
			config.Config.OTLP.Endpoint, config.Config.OTLP.Protocol, config.Config.OTLP.IntervalSeconds)
	}

	return serve(ctx, srv, ln)
```

Add the collector import to `main.go`:

```go
	"github.com/fjacquet/idrac_exporter/internal/collector"
```

- [ ] **Step 2: Build and vet**

Run: `go build ./... && go vet ./...`
Expected: clean build (the loop goroutine stops on ctx cancel; OTLP shuts down via the deferred flush before `serve` returns).

- [ ] **Step 3: Smoke test — default config does nothing new**

Run: `go test ./... -count=1`
Expected: PASS. With `otlp.enabled` unset, no loop/OTLP starts and behavior equals today's.

- [ ] **Step 4: Commit**

```bash
git add cmd/idrac_exporter/main.go
git commit -m "feat(4b): start snapshot loop + OTLP push when enabled; flush on shutdown"
```

---

### Task 8: ADR-0009, docs, sample config

**Files:**
- Create: `docs/adr/0009-otlp-via-prometheus-bridge.md`
- Modify: `docs/adr/index.md`
- Create: `docs/otlp.md`
- Modify: `mkdocs.yml` (nav)
- Modify: `sample-config.yml`

- [ ] **Step 1: Write ADR-0009**

Create `docs/adr/0009-otlp-via-prometheus-bridge.md`:

```markdown
# OTLP via the Prometheus bridge over a MetricFamily snapshot

## Status

Accepted — implemented in Phase 4. Refines [ADR 0002](0002-multi-target-with-optional-otlp.md).

## Context

The family reference OTLP path (pflex/pstore) collects into a typed snapshot of
samples and exposes it through OpenTelemetry *observable gauges*. `idrac_exporter`
is Prometheus-native: every metric is emitted with `prometheus.MustNewConstMetric`
into a per-target registry. Re-expressing each emitter as an observable gauge would
rewrite the entire collector and the on-demand path — contradicting ADR 0002's
"reuse the existing collector rather than reimplementing it."

## Decision

The optional background loop reuses the existing collectors via a coalescing-safe
`GatherFamilies()`, injects a configurable identity label (default `system`) and a
per-target `idrac_up` gauge, and publishes an immutable `[]*dto.MetricFamily`
snapshot. The OTLP path is `go.opentelemetry.io/contrib/bridges/prometheus`
`NewMetricProducer(WithGatherer(store))` feeding a `metric.PeriodicReader`. No
emitter is rewritten. The on-demand `/metrics?target=` path is unchanged.

## Consequences

A dependency on the contrib Prometheus bridge. The snapshot is gathered
`MetricFamily` rather than `[]Sample`; the bridge maps gauges → OTel gauges and
counters → monotonic sums (verified), dropping only `UNTYPED`/`GAUGE_HISTOGRAM`,
which this exporter never emits. The family `architecture.md` is updated to
recognize this "prometheus-native bridge" variant of the OTLP export path.
```

- [ ] **Step 2: Add ADR-0009 to the index**

In `docs/adr/index.md`, add a list entry for `0009` mirroring the existing entries' format (one line linking `0009-otlp-via-prometheus-bridge.md` with a one-line summary).

- [ ] **Step 3: Write `docs/otlp.md`**

Create `docs/otlp.md`:

```markdown
# OTLP push (optional)

The exporter's primary mode is on-demand: Prometheus scrapes
`/metrics?target=<bmc>`. Optionally, an off-by-default background loop polls the
configured `hosts:` on a fixed interval and pushes their metrics via OTLP.

```yaml
collection:
  interval: 60s            # snapshot-loop cadence (default 60s when otlp.enabled)
otlp:
  enabled: false           # master switch — the loop runs only when true
  endpoint: "localhost:4317"
  protocol: grpc           # grpc | http
  insecure: false          # set true for a plaintext local collector
  interval: 0s             # OTLP push cadence; 0 = use collection.interval
  identity_label: system   # per-series target label (system | instance | ...)
  headers: {}              # optional static exporter headers
```

Every pushed series carries `<identity_label>=<target>` (the OTLP path has no
Prometheus relabel to supply `instance`). A per-target `idrac_up` gauge reports
`1` when the last cycle collected metrics and `0` when the target was unreachable
or produced nothing. Host/credential changes hot-reload via SIGHUP; changing OTLP
transport settings (`endpoint`/`protocol`/`interval`/`enabled`) requires a
restart. Environment overrides: `CONFIG_OTLP_ENABLED`, `CONFIG_OTLP_ENDPOINT`,
`CONFIG_OTLP_PROTOCOL`, `CONFIG_OTLP_INSECURE`, `CONFIG_OTLP_INTERVAL`,
`CONFIG_OTLP_IDENTITY_LABEL`, `CONFIG_COLLECTION_INTERVAL`.
```

- [ ] **Step 4: Add `docs/otlp.md` to the mkdocs nav**

In `mkdocs.yml`, add `- OTLP push: otlp.md` to the `nav:` list (next to the metrics catalog entry; match the existing indentation).

- [ ] **Step 5: Document the blocks in `sample-config.yml`**

Append to `sample-config.yml` (commented, off by default):

```yaml
# Optional background snapshot loop + OTLP push (off by default).
# collection:
#   interval: 60s
# otlp:
#   enabled: false
#   endpoint: "localhost:4317"
#   protocol: grpc
#   insecure: false
#   interval: 0s
#   identity_label: system
#   headers: {}
```

- [ ] **Step 6: Verify docs build**

Run: `mkdocs build --strict` (or the repo's docs target if defined)
Expected: build succeeds, no broken-nav warnings.

- [ ] **Step 7: Commit**

```bash
git add docs/adr/0009-otlp-via-prometheus-bridge.md docs/adr/index.md docs/otlp.md mkdocs.yml sample-config.yml
git commit -m "docs(4b): ADR-0009, OTLP config docs, sample-config blocks"
```

- [ ] **Step 8: Gate — run CI**

Run: `make ci`
Expected: green. This closes PR 4b.

---

## Self-Review (completed by plan author)

**Spec coverage:**
- snapshot.go (immutable + RWMutex/atomic swap) → Task 4 ✓
- background loop over `hosts:` on `collection.interval`, errgroup `runLimited` → Task 5 ✓
- serve-HTTP-before-first-cycle → Task 7 (loop in goroutine after server starts) ✓
- per-target graceful degradation (up=0, no other metrics) → Task 5 (`gatherTarget` + `TestLoopCollectOnceDegradesPerHost`) ✓
- otlp.go observable export via bridge + periodic reader → Task 6 ✓
- configurable identity label (default `system`) + per-target `idrac_up` → Tasks 2, 4, 5 ✓
- config schema (`collection`/`otlp`) + env + defaults → Task 2 ✓
- graceful shutdown / final flush → Tasks 3, 7 ✓
- dual-export test (registry gather + OTLP ManualReader) → Task 6 ✓
- on-demand byte-identical → Task 1 (`TestRefreshSystem` retained) ✓
- ADR-0009 + docs → Task 8 ✓

**Deviation from spec:** `otlp.insecure` defaults to **false (secure)**, not `true` as the spec's config block showed — secure-by-default; the compose demo sets `insecure: true`. The spec file is updated to match.

**Placeholder scan:** none — every code/test step has full content.

**Type consistency:** `GatherFamilies()`, `SnapshotStore`, `NewSnapshotStore`, `Store`, `Gather`, `labelFamilies`, `upFamily`, `hasRealMetric`, `buildSnapshot`, `NewLoop`, `Loop.Run`, `collectOnce`, `gatherTarget`, `NewOTLP`, `OTLP.Shutdown`, `serve` are defined once and referenced with identical signatures across tasks. `IntervalSeconds float64` is consistently converted via `time.Duration(x * float64(time.Second))`. Test helpers `upValueFor`/`systemHealthHasLabel`/`otlpHasMetric`/`mustGather` are each defined once in the package.
```
