# iDRAC Exporter — Family-Standard Recovery (Program Overview)

**Status:** Draft · 2026-06-15
**Type:** Program-level umbrella spec (each phase gets its own spec → plan → implementation cycle)

## Goal

Bring `idrac_exporter` (a fork of `mrlhansen/idrac_exporter`, a multi-vendor Redfish/BMC
metrics exporter) into conformance with Fred's exporter-standards family — the universal
layer in full, and the metrics layer via a documented multi-target variant. The Grafana
dashboards are already strong and are kept; they only need wiring into the family layout.

## Context

`idrac_exporter` is **not** an original member of the family table. It is a deliberate
**adoption**. It diverges from the family in two distinct ways:

- **Universal layer** (Go version, Makefile, CI/CD, supply-chain, Docker, docs, ADRs,
  tests) — plain drift, fully recoverable.
- **Metrics layer** — it is a *multi-target on-demand* exporter (`?target=`), idiomatic
  for a BMC fleet (snmp/blackbox-style), not the family's *snapshot + OTLP appliance*
  model. The README warns BMC scrapes can take minutes, so a single snapshot loop polling
  the whole fleet on a fixed interval is operationally worse than per-target on-demand.

## Settled decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | **Metrics model = hybrid.** Keep on-demand `/metrics?target=` as primary; add an *optional* background snapshot loop over the configured `hosts:` that OTLP-pushes. | Preserves the idiomatic multi-target pattern; adds dual-export coverage without forcing a fleet-wide poll loop. |
| D2 | **Hard fork.** Diverge fully from upstream; stop tracking it. | Full family conformance requires invasive core migrations (cobra/logrus/errgroup/resty/OTLP) that would conflict forever with upstream merges. Accepts losing upstream's ongoing multi-vendor BMC fixes. |
| D3 | **Module path rename** `github.com/mrlhansen/idrac_exporter` → `github.com/fjacquet/idrac_exporter`. | Matches family GHCR / Homebrew / GoReleaser conventions. *(Assumption — confirm.)* |
| D4 | **Client stays hand-rolled, transport → `resty/v2`.** `docs/swagger/` (Dell iDRAC 10 + DMTF Redfish OpenAPI, ~4.7 MB) is a **payload-realignment reference, not a codegen source.** | Codegen off a 2.8 MB / hundreds-of-schema spec is a monstrous dep for an exporter touching ~15 resources (fails the no-regression criterion). |
| D5 | **Public contract preserved:** metric prefix `idrac_` (configurable) and port `9348`. | Like `obs`'s `ecs_`, the prefix is a deliberate vocabulary keep, not drift. Changing it breaks every existing dashboard/alert/scrape. |

## Upstream PR harvest (before cutting ties)

Upstream's 3 open PRs were triaged. None are verbatim cherry-picks (we rewrite the
surrounding code); harvest the **ideas**:

- **#189 configurable concurrency limit** → fold intent into Phase 2 as the
  `errgroup.SetLimit` migration + a `concurrency:` config option + conn-pool tie-in. Its
  `defer wg.Done()` fixes a **real latent bug** in our tree (a panic in any `RefreshXxx`
  hangs the `WaitGroup` forever) — eliminated for free by errgroup.
- **#148 watcher resilience** → fold the `fsnotify.Rename` handling into the config-reload
  rework (atomic-rename-on-save is currently missed). Reimplement cleanly; the PR leaks
  goroutines and blocks its event loop.
- **#138 firmware inventory metrics** (draft) → **backlog feature**, revisit post-recovery.

## Phase map

Each phase is an independent, shippable PR with its own spec. `make ci` (introduced in
Phase 1) is the gate for every phase after it.

| Phase | Scope | Touches collector core? | Exit criteria |
|---|---|---|---|
| **1 — Universal layer** | Go `1.26.4` pin; full Makefile contract; CI trio + `.goreleaser.yaml`; supply-chain (semgrep, govulncheck, CycloneDX SBOM); SHA-pinned actions + dependabot + workflow hardening; **non-root Dockerfile** + `Dockerfile.goreleaser`; module-path rename; MkDocs skeleton; README badges; **all ADRs written here**. | No | `make ci` green; release-snapshot builds; image runs non-root; Pages deploys. |
| **2 — Plumbing migrations** | `flag`→cobra (`--config --debug --once --trace`); logger→logrus; `WaitGroup`→errgroup+`SetLimit` (+`concurrency:` config, #189 intent); SIGHUP + `passwordFile` + godotenv; identity label + per-target `idrac_up` gauge; unchecked collector; first tests (httptest Redfish mock). | Yes (mechanical) | Behavior-preserving; registry-gather tests pass; `--once --debug` dumps samples; `--trace` is token-safe. |
| **3 — Payload realignment** | Validate `model.go` structs against `docs/swagger/`; finish absent-not-zero hardening of `xstring`/`asFloat64`; localize tolerant types. | Yes | Unparseable fields yield absent samples (never `0`); spec-validated fields documented in `docs/metrics.md`. |
| **4 — Hybrid OTLP loop** | `snapshot.go` (immutable, RWMutex swap) + optional background loop over configured `hosts:` on `collection.interval` (errgroup `SetLimit`, serve HTTP before first cycle, graceful per-target degradation); `otlp.go` observable gauges + periodic reader off the snapshot. On-demand `/metrics` stays primary. | Additive | Dual-export tests pass via **both** registry gather and OTLP `ManualReader`; OTLP loop is opt-in and off by default. |
| **5 — Quickstart + dashboards** | `docker-compose.yml` + `.ghcr.yml`; `prometheus.yml` + `deploy/prometheus/idrac.rules.yml`; `grafana/provisioning/{datasources,dashboards}` + `system` template var; `docs/dashboards.md` + `docs/deployment/docker.md` in mkdocs nav. | No | One-command demo stack runs; panels use `sum`/`avg` (no `rate()` on per-second gauges). |

## ADRs (written in Phase 1)

`docs/adr/NNNN-title.md` (Status/Context/Decision/Consequences + `index.md`), reusing
sibling templates: `0001` supply-chain hardening · `0002` multi-target + optional OTLP
(family-novel — also fold a "multi-target exporter" class back into the skill's
`architecture.md`) · `0003` hand-rolled resty client + swagger-as-reference · `0004`
naming/units · `0005` label-key invariant · `0006` token-auth + retry-excludes-4xx ·
`0007` config hot reload · `0008` absent-not-zero parsing.

## Non-goals

- No snapshot-only refactor; on-demand `/metrics` is never removed.
- No metric prefix/port change; no breaking of the existing public metric contract.
- No Redfish client codegen from the OpenAPI specs.
- Firmware-inventory metrics (#138) and any new metric groups are out of scope (backlog).
- The Helm chart is retained as-is (a bonus over the family baseline), not reworked.

## Sequencing & dependencies

Strictly ordered 1 → 5. Phase 1 must land first (it establishes the `make ci` gate and the
release pipeline). Phases 2 and 3 both touch the collector core and should not be in flight
simultaneously. Phase 4 depends on Phase 2 (errgroup/config) and Phase 3 (clean parsing).
Phase 5 depends on Phase 1 (image) and is otherwise independent.

## Risks

- **Loss of upstream vendor fixes** (accepted under D2) — mitigated by the PR harvest above
  and by keeping `docs/swagger/` as the realignment reference.
- **Core-migration regressions** (Phase 2/3) — mitigated by introducing tests in Phase 2
  before the deeper changes, and by the `--once --debug` sample-diff tooling.
- **Module rename blast radius** (D3) — import paths, ldflags, GHCR image, Homebrew cask;
  contained to Phase 1.
