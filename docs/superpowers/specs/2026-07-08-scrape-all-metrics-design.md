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
| D4 | **Label each host `instance="<bmc>"`**; document `honor_labels: true`. | Standard aggregating-exporter idiom — each BMC becomes its own `instance`, so existing dashboards/alerts that group by `instance` keep working. `instance` is hardcoded (not a config knob) for now. |
| D5 | **Extract the loop's collect-all core into a shared package helper**; both the `Loop` and the new handler call it. | No duplicated fan-out/merge logic; the OTLP loop's behavior is unchanged. |
| D6 | **Parameterize `labelFamilies`' `UNTYPED→GAUGE` coercion** so the pull path skips it. | That coercion exists only for the OTLP bridge. Skipping it keeps the pull exposition byte-compatible with the per-target `?target=` output (`*_info`/`build_info` stay `untyped`). |
| D7 | **No new required config, no toggle.** Scrape-all is simply the new bare-`/metrics` default when `default_target` is empty and hosts are configured. | YAGNI. |

## 1. Request-routing ladder (`cmd/idrac_exporter/handler.go`)

`metricsHandler` gains a scrape-all fallback. Precedence, most-specific first:

| Request | Behavior |
|---|---|
| `/metrics?target=<bmc>` | Single host — **unchanged**. Primary multi-target path. |
| `/metrics`, `default_target` set | Single host = `default_target` (deprecated — warning logged at config load, see §4). Backward-compatible. |
| `/metrics`, no `default_target`, `hosts:` configured (excluding `default`) | **NEW: scrape-all** — every configured host. |
| `/metrics`, no target, no `default_target`, no hosts | `400` (as today). |

`resetHandler`'s `default_target` fallback is left as-is (out of scope; `/reset` is per-target).

## 2. Shared collect-all helper (`internal/collector/`)

Extract the core of `loop.go`'s `collectOnce()` into a package helper, e.g.:

```go
// collectAllHosts collects every configured host (minus the "default"
// credential fallback) concurrently and returns the merged, sorted Snapshot.
// label is the identity label name injected per host; coerceUntyped controls
// whether UNTYPED families are converted to GAUGE (true for OTLP, false for
// the pull exposition path).
func collectAllHosts(label string, coerceUntyped bool) *Snapshot
```

It performs: `hostTargets()` → `runLimited(concurrency, tasks)` of `gatherTarget(target, label, coerceUntyped)` → `buildSnapshot(perHost)`. `Loop.collectOnce()` is rewritten to call `collectAllHosts(config.Config.OTLP.IdentityLabel, true)` and `Store()` the result — no behavior change. `gatherTarget` gains the `coerceUntyped` parameter, threaded into the labeling step (§3).

## 3. Labeling & exposition

- **Label injection:** `labelFamilies(families, key, value)` gains a `coerceUntyped bool` parameter. When `false`, it appends the identity label but leaves `UNTYPED` families untyped (skips the `UNTYPED→GAUGE` rewrite). The deep-clone behavior (so the per-target cache stays clean) is retained. OTLP passes `true`; the pull path passes `false`.
- **Per-host `idrac_up`:** unchanged — `upFamily(key, target, 1|0)` is appended by `gatherTarget`. A host that produced no real metric (down/error) yields **only** `idrac_up=0`.
- **Merge & sort:** unchanged — `buildSnapshot` concatenates same-named families across hosts and sorts by name. Each host's series are disambiguated by their distinct `instance` value, so no duplicate-series collision.
- **Rendering:** a new handler helper renders `[]*dto.MetricFamily` → text with the same `expfmt.MetricFamilyToText` loop `Collector.Gather()` uses, reusing the existing gzip path in `metricsHandler`.

The injected label name for the pull path is the string literal `"instance"`.

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
    honor_labels: true          # keep the exporter's instance="<bmc>"
    scrape_timeout: 60s
    static_configs:
      - targets: ['idrac-exporter:9348']
```

No `?target=`, no `relabel_configs`, no `/discover`. The `?target=` + relabel and `/discover` HTTP-SD patterns remain fully supported for operators who prefer them.

## 7. Testing

White-box handler tests on the existing `mockRedfish` / `testClient` / `testConfig` harness (`internal/collector/testhelpers_test.go`), driving `metricsHandler`:

- **Two healthy hosts** → bare `/metrics` returns metrics from both, each carrying its own `instance="<host>"` label and `idrac_up{instance=…}=1`.
- **One host down** → its `idrac_up=0` is present and the scrape still succeeds (200) with the healthy host's metrics intact.
- **`default_target` set** → bare `/metrics` returns exactly that one host (compat), and the deprecation warning fires.
- **`?target=<host>`** → unchanged single-host output; no `instance` injected by the exporter (Prometheus relabeling owns it on that path).
- **No hosts, no `default_target`** → `400`.
- Exposition for the `?target=` path is byte-identical before/after (guards D6).

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
- Making the injected `instance` label name configurable.
- Removing `default_target` (a later release, after the deprecation window).
