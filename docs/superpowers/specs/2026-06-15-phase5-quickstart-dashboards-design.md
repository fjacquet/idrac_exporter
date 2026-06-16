# Phase 5 — Quickstart + Dashboards (Design)

**Status:** Draft · 2026-06-15
**Parent:** [program overview](2026-06-15-idrac-family-recovery-overview-design.md)
**Scope:** Bring the already-scaffolded demo stack (compose, Prometheus, Grafana provisioning, dashboards, docs) into family-standard shape and **align + extend** the dashboards/alerts to the metric surface Phases 2–4 created. No exporter code changes; this phase touches only deploy assets, dashboards, and docs.

One reviewable PR. `make ci` is the gate (it lints docs/compose where applicable). The public contract (metric prefix `idrac_`, port `9348`, on-demand exposition) is unchanged.

## Context

Unlike Phases 1–4, almost every Phase 5 artifact already exists in the tree: both `docker-compose.yml` (build) and `docker-compose.ghcr.yml` (pull), `prometheus.yml`, `deploy/prometheus/idrac.rules.yml`, `grafana/provisioning/{datasources,dashboards}/`, three dashboards, and `docs/{dashboards.md,deployment/docker.md}` already wired into the mkdocs nav. No dashboard uses `rate()` (the exit criterion's main hazard is already satisfied).

So this is **not a build** — it is a hardening, alignment, and coverage pass. The driving discovery: the two export paths label hosts differently. The OTLP/snapshot path injects an identity label (default key `system`, per Phase 4 D4); the on-demand scrape path has only Prometheus's `instance`/`job` and **no `system` label**. The existing dashboards key on `instance`/`job` (and one uses a stray `DS_PROMETHEUS` datasource var), so they neither honor the program's `system`-var exit criterion nor work uniformly across both paths.

## Settled decisions

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | **Scope = align + extend coverage.** Validate the one-command stack, fix dashboard/alert drift, AND add curated panels/alerts for the metric groups Phases 2–4 introduced. Not a dashboard rewrite. | The scaffolding is sound; the gap is correctness + coverage of newer metrics, not net-new design. |
| D2 | **Unify on `system` as the single host-identity label** across both export paths. OTLP already injects it; the quickstart scrape and the documented fleet relabel are changed to populate it too. Dashboards use one `system` template var. | Makes the program's "`system` template var" exit criterion real on **both** paths and gives the demo a populated selector. Avoids a brittle dual-source var. |
| D3 | **Quickstart requires a reachable BMC** (no mock-BMC service, no seeded sample data). "Runs" = the stack starts and Prometheus/Grafana provision cleanly; BMC-dependent panels render empty / show target-down when no BMC is reachable. | Matches sibling exporters; zero extra maintained artifacts. A mock Redfish responder tracking ~15 discovered endpoints is scope creep beyond the family baseline. |
| D4 | **PDU gets a dedicated `grafana/pdu.json`** rather than being wedged into the server-detail dashboard. | PDUs are a distinct device class (RackPDU discovery path), with their own `idrac_pdu_*` metrics; mixing them into the BMC server view is confusing. |
| D5 | **Dashboard health signal is path-aware.** Scrape path uses Prometheus's own `up{job="idrac"}`; OTLP/snapshot path uses `idrac_up` (emitted only there — confirmed in `snapshot.go`/`loop.go`). The `system` template var keys on an always-present per-host metric (`idrac_system_machine_info`), never on `idrac_up`. | `idrac_up` does not exist on the on-demand scrape path, so a var or sole health panel built on it would be empty in the default demo. |

---

## Work items

### 1. Unified `system` label (cross-cutting)

- **Quickstart `prometheus.yml`:** attach a static `system` label to the scrape target:

  ```yaml
  static_configs:
    - targets: ["idrac_exporter:9348"]
      labels: { system: demo-bmc }
  ```

- **Fleet (multi-target):** document the relabel that sets it per BMC:

  ```yaml
  - source_labels: [__param_target]
    target_label: system
  ```

- **OTLP path:** unchanged — already injects `system` (default `otlp.identity_label`).

Net effect: every series carries `system` regardless of path, so a single dashboard var works everywhere.

### 2. Dashboards — align (all three)

- Standardize the datasource on **one** `datasource`-typed template var (default value the provisioned uid `prometheus`). Eliminates the `DS_PROMETHEUS`-vs-`datasource` split in `status-alternative.json`; all panel `datasource` refs use `${datasource}`.
- Replace `job`/`instance` host selectors with a single **`system`** var: `label_values(idrac_system_machine_info, system)`.
- Preserve the no-`rate()` invariant (already clean) — re-verify after edits.
- Keep the existing panels and layout otherwise (dashboards are "kept", per the program overview).

### 3. Dashboards — extend (curated, not panel-per-metric)

- **`idrac_overview.json` (fleet):** add a health row — target-up (`up{job="idrac"}` scrape / `idrac_up` OTLP), `idrac_exporter_scrape_errors_total`, and storage/manager health rollups (`idrac_storage_health`, `idrac_manager_health`).
- **`idrac.json` (per-machine detail):** add panels for **manager health** (`idrac_manager_health`/`_info`), **storage controller + volume** (`idrac_storage_controller_health`/`_cache_health`, `idrac_storage_volume_health`/`_capacity_bytes`), **voltage** (`idrac_sensors_voltage`, `idrac_cpu_voltage`), **PSU capacity/efficiency** (`idrac_power_supply_capacity_watts`/`_efficiency_percent`), and **power-control min/max** (`idrac_power_control_min_consumed_watts`/`_max_consumed_watts`).
- **`grafana/pdu.json` (new, D4):** a dedicated PDU dashboard — `idrac_pdu_health`/`_info`, `idrac_pdu_power_watts`, `idrac_pdu_power_apparent_va`, `idrac_pdu_power_factor`, `idrac_pdu_energy_kwh`. Same `datasource` + `system` var conventions; no `rate()`.
- Mount `pdu.json` in both compose files and reference it from the dashboards provider directory.

> Out of scope unless trivially free: Dell OEM extras (`idrac_dell_*`), per-drive/system indicator-active gauges, `power_control_interval_in_minutes`. Surfacing them is optional polish, not required coverage.

### 4. Alerts — reconcile + extend (`deploy/prometheus/idrac.rules.yml`)

- Keep the existing six rules (verified to reference real metric names against the live catalog).
- Add an **OTLP-path health alert**: `idrac_up == 0` (the existing `up{job="idrac"} == 0` only covers the scrape path).
- Re-validate every expr against the current metric catalog so none references a renamed/absent series.

### 5. Docs & nav

- **`docs/deployment/docker.md`:** document the `system` label (quickstart static label + fleet relabel snippet); keep the real-BMC requirement explicit.
- **`docs/dashboards.md`:** drop the `system`/`instance` hedge — state `system` as canonical; add the new PDU dashboard to the table.
- **`mkdocs.yml` nav:** add the PDU dashboard reference if it warrants a doc entry (the dashboards page table is sufficient; no new nav node required).

## Validation (defines "the stack runs")

Without a BMC, prove the stack starts and provisions:

- `docker compose config -q` lints `docker-compose.yml` and `docker-compose.ghcr.yml`.
- Bring the stack up and assert:
  - exporter `/health` → 200;
  - Prometheus rules load (`/api/v1/rules` lists the `idrac-health` group; no parse error);
  - Grafana datasource health-check is green and **all four** dashboards appear via provisioning search.
- BMC-dependent panels render empty and the target shows down — a passing "runs" under D3.
- Dashboard JSON sanity: `grep -L 'rate(' grafana/*.json` (no `rate()`); each dashboard parses as JSON and exposes the `datasource` + `system` vars.

## Exit criteria

- `make ci` green; both compose files lint; one-command stack starts and fully provisions (datasource green, four dashboards loaded, rules parsed) against a reachable BMC.
- Every dashboard uses the `system` host var and the shared `datasource` var; no `rate()` on per-second gauges.
- Alerts cover both the scrape (`up{job}`) and OTLP (`idrac_up`) health paths, with all exprs validated against the live catalog.
- `docs/{dashboards.md,deployment/docker.md}` describe the `system` model and the PDU dashboard; mkdocs builds.

## Non-goals

- No mock BMC, no seeded/sample metrics (real BMC required — D3).
- No metric prefix/port change; no new exporter metrics or collector code changes.
- No dashboard rewrite beyond var alignment + the curated additions above.
- Firmware-inventory metrics (#138) and Dell OEM dashboard panels remain backlog.
