# Phase 4 — Hybrid OTLP Loop (Design)

**Status:** Draft · 2026-06-15
**Parent:** [program overview](2026-06-15-idrac-family-recovery-overview-design.md)
**Scope:** Add an optional, off-by-default background snapshot loop that polls the configured `hosts:` on a fixed interval and pushes their metrics via OTLP — while leaving the primary on-demand `/metrics?target=` path completely unchanged. Introduces the deferred identity label and a per-target `idrac_up` gauge (Phase 2 D6), both scoped to the snapshot/OTLP path.

Two reviewable PRs in sequence: **4a infra + refactor** (contract-neutral), **4b the OTLP feature**. `make ci` is the gate for each. The public contract (metric prefix `idrac_`, port `9348`, on-demand exposition) is unchanged.

## Settled decisions

| # | Decision |
|---|----------|
| D1 | **Approach = Prometheus→OTLP bridge over a MetricFamily snapshot.** The background loop reuses the existing per-target `Collector`s; each cycle gathers their `[]*dto.MetricFamily`, injects an identity label + `idrac_up`, and stores an immutable snapshot. `go.opentelemetry.io/contrib/bridges/prometheus` `NewMetricProducer(WithGatherer(store))` feeds a `metric.PeriodicReader` → OTLP exporter. **No emitter (`metrics.go`) is rewritten.** This deliberately deviates from the family's literal observable-gauge pattern (recorded in ADR 0009), justified by the prometheus-native collector and ADR 0002's "reuse the existing collector rather than reimplementing it." |
| D2 | **On-demand path is untouched and primary.** `/metrics?target=` keeps its per-target collector cache, coalescing, and byte-identical exposition. The snapshot loop is purely additive. |
| D3 | **Coalescing-safe `GatherFamilies()`.** `collector.go` is refactored so the `sync.Cond`-coalesced gather caches `[]*dto.MetricFamily`; the existing text `Gather()` derives from it via `expfmt.MetricFamilyToText`. The loop calls `GatherFamilies()` (never `Collect()` directly), so loop and on-demand scrapes share one BMC collection when they overlap, with no double-registration race. On-demand output stays byte-identical. |
| D4 | **Identity label is configurable, default `system`.** Every series in the snapshot/OTLP path carries `<identity_label>=<target>` (default key `system`, overridable to `instance`). It is a per-series metric label, not an OTLP Resource attribute, because one `MeterProvider` pushes all targets. On-demand series do **not** gain it (Prometheus supplies `instance` via relabel there — Phase 2 D6). |
| D5 | **`idrac_up{<identity_label>=target}`** is emitted per host in the snapshot path: `1` on a successful cycle, `0` on failure. It is the only new metric series, and only on the snapshot/OTLP path. |
| D6 | **Loop is gated on `otlp.enabled` (off by default).** No decoupled "snapshot-only" mode (YAGNI); the loop exists to feed OTLP. Default config behaves exactly as today — no loop, no push. |
| D7 | **Graceful shutdown is added** (currently absent). `signal.NotifyContext(SIGINT, SIGTERM)` drives `srv.Shutdown` → loop stop → `meterProvider.Shutdown` (final flush). This is a family-standard improvement required by the loop's lifecycle. |
| D8 | **Per-host graceful degradation.** A host that errors in a cycle yields `idrac_up=0` and **no other metrics that cycle** (no stale carry-over — consistent with absent-not-zero, ADR 0008). One bad BMC never fails the whole cycle. |

## Library additions

`go.opentelemetry.io/otel`, `go.opentelemetry.io/otel/sdk/metric`,
`go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc` (+ `otlpmetrichttp`),
`go.opentelemetry.io/contrib/bridges/prometheus`. No codegen tooling is added.

**Bridge fidelity (verified against `bridges/prometheus/producer.go`):** Prometheus gauges → OTel `Gauge`; counters → monotonic cumulative `Sum` (so `idrac_exporter_scrape_errors_total` is correct); metric/label names copied **as-is** (no `_total` stripping); only `UNTYPED`/`GAUGE_HISTOGRAM` are dropped-with-error via `otel.Handle` — and the exporter emits none of those.

---

## 4a — infra + refactor *(contract-neutral)*

Stand up the plumbing the feature builds on, changing **no metric output**.

- **`GatherFamilies()` refactor (D3):** extract a coalescing-safe `(*Collector).GatherFamilies() ([]*dto.MetricFamily, error)` from the current `Gather()`. The `sync.Cond` coalescing now caches the gathered families; `Gather()` becomes `MetricFamilyToText(GatherFamilies())`. Behavior-preserving: `/metrics` exposition is byte-identical (a sample diff against `main` confirms).
- **Config structs (`internal/config/`):** add `Collection` (`interval`) and `OTLP` (`enabled`, `endpoint`, `protocol`, `insecure`, `interval`, `identity_label`, `headers`) to `RootConfig`. Wire `${ENV}` expansion and `CONFIG_OTLP_*` / `CONFIG_COLLECTION_*` env overrides in `env.go`. `Validate()`: `protocol ∈ {grpc,http}` (default `grpc`); `endpoint` default `localhost:4317`; `identity_label` default `system`; `insecure` default **`false` (secure-by-default; the compose demo sets `true` for a plaintext local collector)**; `Collection.Interval` default `60s`; `OTLP.Interval == 0` → use `Collection.Interval`.
- **Graceful-shutdown scaffolding (`cmd/idrac_exporter/main.go`, D7):** root `signal.NotifyContext(ctx, SIGINT, SIGTERM)`; serve HTTP in a goroutine; on cancel, `srv.Shutdown(timeoutCtx)`. No loop/OTLP yet — just the lifecycle seam (a no-op shutdown hook the feature plugs into). SIGHUP reload is unchanged.

### 4a tests

- `GatherFamilies()` returns the same families the registry produces; `Gather()` text is byte-identical to the pre-refactor output for the mock fixture.
- Config: `otlp:`/`collection:` parse from YAML and env; `Validate()` fills every default above; an invalid `protocol` is rejected.
- Shutdown: SIGTERM causes `srv.Shutdown` to return and the process to exit cleanly (no leaked listener).

### 4a exit criteria

`make ci` green; `/metrics` byte-identical to `main`; config round-trips with defaults; SIGTERM shuts the server down gracefully; no behavioral change visible to a scraper.

---

## 4b — the OTLP feature *(additive)*

Three new files in `internal/collector/`, plus lifecycle wiring.

- **`snapshot.go`:** an immutable `Snapshot` (a merged `map[name]*dto.MetricFamily` across all hosts, plus per-host up state) and a `SnapshotStore` holding it behind an atomic pointer-swap. `SnapshotStore` implements `prometheus.Gatherer` (returns the snapshot's families, sorted), so the bridge reads it with zero copying on the hot path.
- **`loop.go`:** `Loop.Run(ctx)` — a ticker on `collection.interval`. Each cycle fans out over `config.Config.Hosts` (every entry except `default`) via the existing `runLimited(config.Config.Concurrency, …)`. Per host:
  - `GetCollector(target, auth)` → `GatherFamilies()`;
  - inject `<identity_label>=<target>` onto every `Metric` of every family;
  - append `idrac_up{<identity_label>=target} = 1`;
  - on error: emit only `idrac_up = 0` for that host (D8).
  Merge all hosts' families by name into a new immutable `Snapshot`; `store.Store(snapshot)`. The loop reads `config.Config.Hosts` fresh each cycle, so SIGHUP host/credential changes are picked up automatically.
- **`otlp.go`:** `NewOTLP(store, cfg)` builds the OTLP exporter (`otlpmetricgrpc` default, `otlpmetrichttp` when `protocol: http`; `insecure`, `endpoint`, `headers` applied), `prometheus.NewMetricProducer(prometheus.WithGatherer(store))`, a `metric.NewPeriodicReader(exporter, metric.WithInterval(otlp.interval), metric.WithProducer(producer))`, and a `metric.NewMeterProvider(metric.WithReader(reader))` that owns **no instruments of its own**. Exposes `Start()` and `Shutdown(ctx)` (the latter flushes via `MeterProvider.Shutdown`).
- **Lifecycle wiring (`main.go`):** when `otlp.enabled`, after the HTTP server is serving, start the `MeterProvider` and `go loop.Run(ctx)`. The first cycle runs in the background; the snapshot is empty until it completes, so the bridge gathers nothing and the first push is empty/no-op (serve-before-first-cycle, no panic). On ctx cancel: stop the loop, then `otlp.Shutdown(ctx)` for a final flush. OTLP **transport** changes (endpoint/protocol/interval/enabled) are **restart-required**, documented; host/credential changes hot-reload via the per-cycle config read.

### 4b tests (assert via *both* export paths)

- **Dual-export:** an httptest Redfish mock; run one loop cycle; assert (a) via the `SnapshotStore` gatherer that families carry `system=<target>` and `idrac_up`, and (b) via an OTLP `metric.NewManualReader` wired to the bridge over the snapshot that `Collect` yields the same metrics with the identity attribute and correct types (gauge vs monotonic sum).
- **Degradation:** two hosts, one failing → `idrac_up=1` + metrics for the good host, `idrac_up=0` + no other metrics for the bad host; the cycle still completes.
- **Concurrency:** with `concurrency: 2`, no more than two host gathers run at once.
- **Empty snapshot:** the bridge gathers an empty `SnapshotStore` before cycle 1 without error/panic.
- **Shutdown flush:** ctx cancel stops the loop and `MeterProvider.Shutdown` returns (final export attempted).
- **Configurable label:** `identity_label: instance` makes every series carry `instance` instead of `system`.

### 4b exit criteria

`make ci` green; default config = today's behavior (no loop, no push); with `otlp.enabled`, the loop polls `hosts:` and pushes via OTLP; dual-export tests pass through both registry gather and OTLP `ManualReader`; `/metrics` byte-identical; SIGTERM flushes; ADR 0009 written; `docs/` documents the `otlp:`/`collection:` config and the snapshot/OTLP model.

---

## ADRs

- **ADR 0002** (Accepted) already records the hybrid multi-target + optional-OTLP decision — no change needed.
- **ADR 0009 — new:** "OTLP via the Prometheus bridge over a MetricFamily snapshot." Records the deliberate deviation from the family's observable-gauge pattern (`architecture.md`), its justification (prometheus-native collector; ADR 0002's reuse mandate; zero emitter rewrites), and the consequences (a dependency on the contrib bridge; the snapshot is gathered `MetricFamily` rather than `[]Sample`). Per `decisions.md`, the family `architecture.md` is updated to recognize a "prometheus-native bridge" variant of the OTLP export path.

## Non-goals (Phase 4)

- No change to the on-demand `/metrics?target=` exposition.
- Metrics only — no OTLP traces or logs.
- No new metric **groups**; `idrac_up` is the only new series, and only on the snapshot/OTLP path.
- No per-host collection interval; one global `collection.interval`.
- No decimation / skip-slow-stat knobs (a future optimization, not needed here).
- No live rebuild of the OTLP exporter pipeline on SIGHUP (transport changes are restart-required).
- Firmware-inventory metrics (#138, backlog) remain out of scope.

## Sequencing & dependencies

4a → 4b, strictly ordered. 4a is contract-neutral and independent (refactor + config + lifecycle seam). 4b depends on 4a's `GatherFamilies()` and config structs. Phase 4 as a whole depends on Phase 2 (errgroup `runLimited`, `concurrency`) and Phase 3 (clean absent-not-zero parsing), both landed. Phase 5 (quickstart/dashboards) consumes Phase 4's `idrac_up` for its alerting rules and may template dashboards on the `system` identity label.

## Risks

- **Bridge type-mapping surprises** — mitigated by the verified gauge/counter mapping and the dual-export `ManualReader` test asserting OTel types; any `UNTYPED` metric would be dropped, but the exporter emits none.
- **Snapshot loop adding BMC load** — the loop is off by default; when on, it polls only configured `hosts:` and coalesces with concurrent on-demand scrapes; `concurrency` bounds the fan-out and conn pool.
- **Identity-label / dashboard drift** — default `system` differs from the current dashboards' `instance` var; reconciled in Phase 5 (which re-templates dashboards) and softened by the configurable key.
- **Graceful-shutdown regressions** — the new signal/Shutdown path is covered by a 4a test; the loop and OTLP plug into a single cancel context to avoid leaked goroutines.
- **OTLP dependency tree growth** — accepted: OTLP is a required family capability; the bridge avoids the larger cost of reimplementing every emitter as an observable gauge.
