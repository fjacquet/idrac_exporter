# Wire the Demo Quickstart for Scrape-All (Design)

**Status:** Draft · 2026-07-08
**Parent:** [scrape-all /metrics design](2026-07-08-scrape-all-metrics-design.md)
**Scope:** v1.1.0 shipped the scrape-all `/metrics` code and its reference docs, but the one-command demo stack (`docker-compose.yml`, `config.yaml`, `prometheus.yml`) and the quickstart docs still demonstrate the **single-host** pattern. A user who adds a second host to `hosts:` still sees only one host on the Grafana dashboards. This spec rewires the demo to exercise scrape-all end-to-end and updates the quickstart docs to match. **Docs + config only — no Go code.** One branch + PR; released as `v1.1.1`.

## Context

The Grafana dashboards key every panel on the **`system`** label (e.g. the `System` variable is `label_values(idrac_system_machine_info, system)`; the Memory panel filters `idrac_memory_module_info{system=~"$system"}`). In the current demo:

- `config.yaml` sets `default_target: ${IDRAC1_HOST}` → the container's bare `/metrics` returns **only that one host** (the deprecated single-host override path), never scrape-all.
- `prometheus.yml` scrapes bare `/metrics` and stamps a **static** `labels: { system: demo-bmc }` on the whole scrape, plus `job_name: idrac`.

Net effect: one host, one static `system` value. Adding a second host to `hosts:` does nothing, because Prometheus only ever receives the single `default_target`'s metrics. This is the root cause of the reported "System dropdown shows only one value / Memory: No data" symptom.

v1.1.0's scrape-all path already injects `instance="<bmc>"` and `system="<bmc>"` per host on bare `/metrics` when `default_target` is empty (see [scrape-all design](2026-07-08-scrape-all-metrics-design.md)). The demo simply never opts into it.

## Settled decisions

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | **Demo defaults to scrape-all.** `config.yaml` ships `default_target: ""`; bare `/metrics` collects all hosts. | Makes the runnable demo showcase the headline feature; multi-host "just works". |
| D2 | **Two hosts with per-host `IDRAC1_*` / `IDRAC2_*` env creds** under `hosts:` (env-expanded keys). | Shows a real multi-host fleet in the runnable stack (not docs-only). Per-host creds cover mixed-credential fleets. |
| D3 | **`prometheus.yml`: `honor_labels: true`, drop the static `system` label, keep bare `/metrics`.** Keep the multi-target relabel snippet in comments as the documented alternative. | `honor_labels: true` preserves the exporter's per-host `instance`/`system`; the static `system` label would otherwise override the per-host one. |
| D4 | **Both compose files** (`docker-compose.yml`, `docker-compose.ghcr.yml`) gain `IDRAC2_*` env with defaults. | Keep the two demo entrypoints consistent. |
| D5 | **Dashboard JSON is out of scope.** If a panel (e.g. Memory) still shows "No data" after correct wiring, it is a genuine dashboard-query bug filed as a **separate** follow-up. | Keeps this change focused on wiring + docs, per the agreed scope. |
| D6 | **Released as `v1.1.1`** (patch). | No API/behavior change to the exporter itself — only demo config + docs. |

## Files & changes

### 1. `config.yaml`
- `default_target: ""` (was `${IDRAC1_HOST}`). Comment: empty ⇒ bare `/metrics` collects all `hosts:` (scrape-all); set a single host here only for the legacy single-target behavior.
- `hosts:` — two entries with env-expanded keys and per-host creds:
  ```yaml
  hosts:
    ${IDRAC1_HOST}:
      username: ${IDRAC1_USERNAME}
      password: ${IDRAC1_PASSWORD}
    ${IDRAC2_HOST}:
      username: ${IDRAC2_USERNAME}
      password: ${IDRAC2_PASSWORD}
  ```
  (The config loader runs `os.ExpandEnv` over the whole file, so `${IDRAC1_HOST}:` expands as a map key.)
- Keep `metrics: { all: true }`, `address`, `port`, `timeout`.
- Comment: a single-BMC user should set `IDRAC2_HOST` to a real host or delete the second block; otherwise the second host reports `idrac_up{...}=0` (a live demo of the per-host failure semantics).

### 2. `prometheus.yml`
- The `idrac` scrape job becomes:
  ```yaml
  scrape_configs:
    - job_name: idrac
      honor_labels: true            # keep the exporter's per-host instance/system=<bmc>
      metrics_path: /metrics
      static_configs:
        - targets: ["idrac_exporter:9348"]
  ```
- Remove the static `labels: { system: demo-bmc }`.
- Keep (in comments) the multi-target relabel alternative (`__param_target` → `system`/`instance`, rewrite `__address__` → `idrac_exporter:9348`) for operators who prefer per-target scraping.

### 3. `docker-compose.yml` and `docker-compose.ghcr.yml`
- Add to the `idrac_exporter` service `environment:` (both files):
  ```yaml
  - IDRAC2_HOST=${IDRAC2_HOST:-192.168.1.2}
  - IDRAC2_USERNAME=${IDRAC2_USERNAME:-${IDRAC1_USERNAME:-root}}
  - IDRAC2_PASSWORD=${IDRAC2_PASSWORD:-${IDRAC1_PASSWORD:-}}
  ```
  (Second host defaults its creds to the first host's, so a same-credential fleet only needs `IDRAC2_HOST`.) **Verify** the nested defaults resolve via `docker compose config`; if the Compose version in use does not interpolate nested `${A:-${B:-c}}`, fall back to flat defaults (`IDRAC2_USERNAME=${IDRAC2_USERNAME:-root}`, `IDRAC2_PASSWORD=${IDRAC2_PASSWORD:-}`).
- Update each file's header comment block to show the two-host invocation and note that bare `/metrics` now returns all hosts.

### 4. Docs
- `README.md`: update the quickstart so it reflects scrape-all (empty `default_target`, both hosts, `honor_labels: true`), and the two-host `docker compose up` invocation.
- `docs/deployment/docker.md`: same — the Docker Compose quickstart page describes the scrape-all wiring, the `IDRAC2_*` env, and the `honor_labels` requirement; keep the single-target relabel as the documented alternative.
- `docs/dashboards.md` and `docs/installation.md`: fix any single-host framing so the dashboards are described as multi-host / keyed on `system`.
- `docs/configuration.md` and `docs/usage.md` already carry the scrape-all reference (v1.1.0) — no change needed unless they still contradict the new demo default.

## Verification

Docs + config only, so the Go suite is unaffected; the gates are:

- `config.yaml` and `prometheus.yml` are valid YAML.
- `docker compose config` (and `docker compose -f docker-compose.ghcr.yml config`) interpolate cleanly with and without `IDRAC2_*` set (no empty-key `:` in the rendered config).
- `make ci` stays green (no Go changes; confirms nothing else broke).
- `mkdocs build --strict` succeeds (docs render, no broken links) — or, if mkdocs is unavailable locally, the docs.yml workflow on the PR is the gate.
- **Manual (post-merge, on a live stack):** `docker compose up`, open Grafana, confirm the `System` variable lists both hosts and panels populate per host. If Memory still shows "No data", open a separate dashboard-bug follow-up (D5).

## Out of scope

- Grafana dashboard JSON query fixes (D5).
- Any change to the exporter's Go code or the scrape-all behavior itself.
- Removing `default_target` (still deprecated, still honored).
- A generic "N hosts from one env var" mechanism — the demo shows exactly two.
