# Scrape-All `/metrics` (Design)

**Status:** Draft · 2026-07-08
**Parent:** [program overview](2026-06-15-idrac-family-recovery-overview-design.md)
**Scope:** Give bare `/metrics` (no `?target=`) a scrape-all mode that collects every configured host in one response, each series labeled `instance="<bmc>"`, so Prometheus needs a single static scrape config instead of the multi-target `?target=` + relabeling pattern. Additive and backward-compatible: the `?target=` path is untouched; `default_target` is deprecated, not removed. Reuses the existing OTLP-loop collection machinery. One branch + PR; `make ci` is the gate.

## Context

The exporter is on-demand and multi-target ([ADR 0002](../../adr/0002-multi-target-with-optional-otlp.md)): Prometheus passes each BMC via `?target=` and relabels, so scraping N hosts means N relabeled targets. `default_target` (`RootConfig.DefaultTarget`, a single string) only lets bare `/metrics` fall back to **one** host. Operators with a handful of BMCs find the `?target=` + `relabel_configs` construct heavy for what they want: "point Prometheus at the exporter and get all my hosts."

The machinery to collect *every* configured host already exists and is used in three places:

- `--once` (`cmd/idrac_exporter/once.go`) — collect all hosts, print combined exposition.
- The OTLP push loop (`internal/collector/loop.go`) — `collectOnce()` fans out `gatherTarget()` over `hostTargets()` under `runLimited(concurrency, …)`, merges via `buildSnapshot()`, and stores an immutable `Snapshot`. `gatherTarget` already injects a per-host identity label (via `labelFamilies`) and an `idrac_up` gauge (via `upFamily`).
- `/discover` (`internal/config/discover.go`) — Prometheus HTTP SD listing all hosts.

What is missing is a **pull** door onto that same collection: a bare `/metrics` scrape that returns all hosts at once. This spec adds it as a live fan-out per scrape, reusing the loop's collection code.

## Settled decisions

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | **Overload bare `/metrics`** to return all configured hosts; no new endpoint. | User wants one well-known path. `?target=` stays primary and unchanged. |
| D2 | **Live fan-out per scrape** (not snapshot-backed). Each bare `/metrics` scrape collects all hosts right then, concurrently. | Freshest data; preserves the on-demand model (ADR 0002); reuses the per-target collector cache + `sync.Cond` coalescing; no new always-on goroutine. Snapshot-backed pull is explicitly deferred (§Out of scope). |
| D3 | **Routing ladder** with `default_target` as a deprecated single-host override. See §1. | Fully backward-compatible: existing single-default users are unaffected; scrape-all is opt-in by leaving `default_target` empty. |
| D4 | **Label each host with BOTH `instance="<bmc>"` and `system="<bmc>"`** (same host value); document `honor_labels: true`. | `instance` is the standard aggregating-exporter idiom (each BMC becomes its own `instance`); `system` is what this repo's bundled Grafana dashboards and the OTLP loop already key on (`docs/configuration.md` per-target relabel sets both). Injecting both makes standard dashboards *and* the bundled ones work from scrape-all. The two names are hardcoded (not a config knob) for now. |
| D5 | **Extract the loop's collect-all core into a shared package helper**; both the `Loop` and the new handler call it. | No duplicated fan-out/merge logic; the OTLP loop's behavior is unchanged. |
| D6 | **Parameterize `labelFamilies`' `UNTYPED→GAUGE` coercion** so the pull path skips it, and **generalize `labelFamilies`/`upFamily`/`gatherTarget` to take a `names []string` label-key list** (all set to the host value). | The coercion exists only for the OTLP bridge; skipping it keeps the pull exposition byte-compatible with the per-target `?target=` output (`*_info`/`build_info` stay `untyped`). The `[]string` generalization is the minimal way to inject one label (`{system}`, OTLP) or two (`{instance, system}`, pull) without duplicating the labeling code. |
| D7 | **No new required config, no toggle.** Scrape-all is simply the new bare-`/metrics` default when `default_target` is empty and hosts are configured. | YAGNI. |

## 1. Request-routing ladder (`cmd/idrac_exporter/handler.go`)

`metricsHandler` gains a scrape-all fallback. Precedence, most-specific first:

| Request | Behavior |
|---|---|
| `/metrics?target=<bmc>` | Single host — **unchanged**. Primary multi-target path. |
| `/metrics`, `default_target` set | Single host = `default_target` (deprecated — warning logged at config load, see §4). Backward-compatible. |
| `/metrics`, no `default_target`, `hosts:` configured (excluding `default`) | **NEW: scrape-all** — every configured host. |
| `/metrics`, no target, no `default_target`, no hosts | `400` (as today). |

The decision is extracted into a pure, network-free function so it can be unit-tested in isolation:

```go
type metricsMode int
const (
    modeSingleTarget metricsMode = iota // collect one host (the returned target)
    modeScrapeAll                       // collect every configured host
    modeError                           // 400: nothing resolvable
)

// resolveMetricsMode implements the ladder above. hasHosts is whether any
// non-"default" host is configured.
func resolveMetricsMode(target, defaultTarget string, hasHosts bool) (metricsMode, string)
```

`metricsHandler` calls `resolveMetricsMode`, then dispatches: `modeSingleTarget` → existing `GetCollector(target).Gather()` path; `modeScrapeAll` → `collector.GatherAll()`; `modeError` → `400`. Whether any target host is configured is answered by a new mutex-guarded predicate `config.Config.HasTargetHosts() bool`.

`resetHandler`'s `default_target` fallback is left as-is (out of scope; `/reset` is per-target).

## 2. Shared collect-all helper (`internal/collector/`)

Extract the core of `loop.go`'s `collectOnce()` into a package helper, e.g.:

```go
// collectAllHosts collects every configured host (minus the "default"
// credential fallback) concurrently and returns the merged, sorted Snapshot.
// names are the identity label keys injected per host (each set to the host
// value); coerceUntyped controls whether UNTYPED families are converted to
// GAUGE (true for OTLP, false for the pull exposition path).
func collectAllHosts(names []string, coerceUntyped bool) *Snapshot
```

It performs: `hostTargets()` → `runLimited(concurrency, tasks)` of `gatherTarget(target, names, coerceUntyped)` → `buildSnapshot(perHost)`. `Loop.collectOnce()` is rewritten to call `collectAllHosts([]string{config.Config.OTLP.IdentityLabel}, true)` and `Store()` the result — no behavior change (still one `system` label, still coerced). `gatherTarget` gains the `names []string` and `coerceUntyped` parameters, threaded into the labeling step (§3).

The exported pull entry point (called by the handler in `package main`) is:

```go
// GatherAll collects every configured host and returns the merged Prometheus
// text exposition. Each series carries instance="<host>" and system="<host>",
// plus a per-host <prefix>_up gauge. UNTYPED families are left untyped.
func GatherAll() (string, error)
```

`GatherAll` calls `collectAllHosts([]string{"instance", "system"}, false)` and renders the snapshot families to text.

## 3. Labeling & exposition

- **Label injection:** `labelFamilies(families, names, value)` gains `names []string` (was a single `key`) and a `coerceUntyped bool` parameter. It appends every `name=value` pair; when `coerceUntyped` is `false` it leaves `UNTYPED` families untyped (skips the `UNTYPED→GAUGE` rewrite). The deep-clone behavior (so the per-target cache stays clean) is retained. OTLP passes `names=[]string{"system"}, coerceUntyped=true`; the pull path passes `names=[]string{"instance","system"}, coerceUntyped=false`.
- **Per-host `up` gauge:** `upFamily(names, target, 1|0)` similarly gains `names []string` and emits one metric carrying every `name=target` pair. Appended by `gatherTarget`. A host that produced no real metric (down/error) yields **only** the `up` gauge = 0.
- **Merge & sort:** unchanged — `buildSnapshot` concatenates same-named families across hosts and sorts by name. Each host's series are disambiguated by their distinct `instance`/`system` values, so no duplicate-series collision.
- **Rendering:** `GatherAll` renders `[]*dto.MetricFamily` → text with the same `expfmt.MetricFamilyToText` loop `Collector.Gather()` uses; `metricsHandler` writes the result through its existing gzip path.

The injected label names for the pull path are the string literals `"instance"` and `"system"`.

## 4. Config changes (`internal/config/`)

- `RootConfig.DefaultTarget`: field kept. Mark **deprecated** in `sample-config.yml` and docs. Emit a deprecation warning in `Validate()` when the field is non-empty — deterministic and once per config load/reload, not per request.
- No new config fields, no default changes, no toggle.

## 5. Failure & timeout semantics

- A down or slow host does **not** fail the scrape: `gatherTarget` catches the error and returns just `idrac_up=0` for that host (existing behavior).
- Wall-clock ≈ slowest host, bounded by `concurrency` and the per-host `timeout`. Docs instruct raising Prometheus `scrape_timeout` for large fleets.
- Threading the inbound request context into collection for early cancellation is **out of scope** (noted as a future nicety).

## 6. Prometheus configuration (the payoff)

```yaml
scrape_configs:
  - job_name: idrac
    honor_labels: true          # keep the exporter's instance/system="<bmc>"
    scrape_timeout: 60s
    static_configs:
      - targets: ['idrac-exporter:9348']
```

No `?target=`, no `relabel_configs`, no `/discover`. The `?target=` + relabel and `/discover` HTTP-SD patterns remain fully supported for operators who prefer them.

## 7. Testing

White-box handler tests on the existing `mockRedfish` / `testClient` / `testConfig` harness (`internal/collector/testhelpers_test.go`), driving `metricsHandler`:

Two layers of tests:

**Collector level (`package collector`, using the mock harness):** exercise `GatherAll`/`collectAllHosts` via the pre-populated `collectors`-map pattern from `TestLoopCollectOnceDegradesPerHost` (bypasses real Redfish discovery):

- **Two healthy hosts** → `GatherAll()` text contains metrics from both, each series carrying `instance="<host>"` AND `system="<host>"`, plus `idrac_up{instance="<host>",system="<host>"}=1` for each.
- **One host down** → its `idrac_up=…0` is present and `GatherAll` still succeeds with the healthy host's metrics intact.
- Updated `labelFamilies`/`upFamily` unit tests (in `snapshot_test.go`) for the new `names []string` signatures, including a two-name case and the `coerceUntyped=false` (no `UNTYPED→GAUGE`) case.

**Handler routing (`package main`):** the routing ladder is extracted into a pure function `resolveMetricsMode(target, defaultTarget string, hasHosts bool) (metricsMode, string)` (see §1) and unit-tested for all four ladder rows without any network — `?target=` → single; empty+default → single(default); empty+no-default+hosts → scrape-all; empty+no-default+no-hosts → error.

`make ci` (`fmt-check`, `vet`, `golangci-lint`, `go test -race`, `govulncheck`) is the gate.

## 8. Documentation

- `sample-config.yml`: deprecate `default_target` with a comment pointing at scrape-all.
- MkDocs config/usage pages: document the scrape-all model, the routing ladder, the `honor_labels: true` `prometheus.yml` example, and the `instance` labeling.
- Note that `?target=` and `/discover` remain first-class.

## 9. Out of scope (YAGNI)

- Snapshot-backed pull mode (serve the last background `Snapshot`) — deferred; revisit only if fleets grow large enough that a single scrape blows past `scrape_timeout`.
- A separate `/metrics/all` endpoint.
- Per-request context/timeout threading into collection.
- Curated host subsets (`default_target` as a list).
- Making the injected label names (`instance`, `system`) configurable.
- Removing `default_target` (a later release, after the deprecation window).
