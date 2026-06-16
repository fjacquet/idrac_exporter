# Family Helm Charts — Generation Sweep (Design)

**Status:** Draft · 2026-06-16
**Scope:** Give the 10 chartless exporter-family members a Helm chart modeled on `idrac_exporter`'s, plus the tag-driven lockstep publishing workflow. Cross-repo program: one PR per repo. `idrac_exporter` is the template source (it is the only member that currently has a chart, inherited from upstream).

## Context

A corrected probe of `fjacquet/*_exporter` shows **only `idrac_exporter` has a `charts/` directory and a helm workflow**. The other 10 — `pflex, pmax, pstore, pscale, obs, cee, ppdd, ppdm, nbu, nsr` — have `release.yml` + GoReleaser but no chart. Most are the "snapshot + OTLP appliance" model (one process polls many backends), unlike idrac's multi-target on-demand model, so the chart must not assume Prometheus scraping.

The user chose a **big-bang** rollout (all 10 at once) over a piloted one; the safety net is per-PR review of every rendered chart before merge.

## Settled decisions

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | **All 10 in one sweep**, one PR per repo, via Sonnet subagents. | User chose big-bang. |
| D2 | **Template = idrac's chart + lockstep workflow**, generalized. | Only existing chart; structure is already values-driven. |
| D3 | **Prometheus-Operator CRDs (`servicemonitor`/`scrapeconfig`/`prometheusrule`) ship but default OFF.** | OTLP-push exporters aren't scraped; opt-in keeps the chart valid for both models. |
| D4 | **Config is a passthrough Secret**, values key standardized to **`config:`**. | Each exporter has a different config schema; passthrough absorbs all. (idrac keeps `idracConfig:`; optional future alignment.) |
| D5 | **Lockstep publishing**: PR lint + publish on `v*` tags with `helm package --version/--app-version` from the tag. All charts → `oci://ghcr.io/fjacquet/charts`. | Matches the idrac pattern; no manual `Chart.yaml` bump. |
| D6 | **Per-PR review before merge; merge incrementally.** | Big-bang has no pilot; review is the safety net. |

## The portable template (from `idrac_exporter`)

Reference (local): `/Users/fjacquet/Projects/idrac_exporter/charts/idrac-exporter/` and `.github/workflows/helm-charts.yml`.

Each generated chart contains:
- `Chart.yaml` — name `<repo with _→->`, baseline `version`/`appVersion` (repo's latest release tag stripped of `v`, else `0.1.0`); a comment that CI overrides both from the release tag.
- `values.yaml` — `image` (`ghcr.io/fjacquet/<repo>`), `service.port`, probes, `config:` passthrough default, `prometheus.{monitor,rules,scrapeConfig}.enabled: false`.
- `templates/` — `deployment`, `service`, `serviceaccount`, `config` (Secret from `config:`), `_helpers.tpl`, and the three optional CRD templates.
- `.helmignore`.
- `.github/workflows/helm-charts.yml` — PR lint (`helm lint` + `helm template`) on `charts/**`; publish on `v*` tags with version from the tag, push to `oci://ghcr.io/fjacquet/charts`.

## Per-repo adaptation (auto-discovered)

Each subagent reads its target repo to determine:
- **Container port** (e.g. pflex `9445`, idrac `9348`) — from `config.yaml`/README/code.
- **Config path + `--config` flag** — how the exporter loads config (Dockerfile CMD / main flag / README); the deployment mounts the Secret there and passes the flag.
- **Default `config:` blob** — a minimal valid config from the repo's `config.yaml`/`sample-config`/README, with secrets as placeholders.
- **Probe target** — `/health` if the exporter exposes it, else `/metrics` httpGet (or TCP).
- **Chart name + image repo** — derived from the repo name.

If any of these is ambiguous, pick a sensible default and note it in the PR body.

## Execution

Fan out one Sonnet subagent per repo. Each: clone the repo to an isolated dir → discover the adaptation points → generate the chart from the idrac template → `helm lint` + `helm template` (must pass) → branch → commit → push → open a PR (base = repo default branch). Returns a summary (adaptation choices, lint result, PR URL). The orchestrator reviews every PR + rendered chart before any merge.

## Exit criteria

- All 10 repos have an open PR adding `charts/<name>/` + `helm-charts.yml`, each passing `helm lint`/`helm template`.
- Every chart: image `ghcr.io/fjacquet/<repo>`, correct port, config path + flag, working probes, CRDs default-off, `config:` passthrough.
- Publishing is tag-driven lockstep to `oci://ghcr.io/fjacquet/charts`.
- Each PR reviewed; merged incrementally.

## Non-goals

- No exporter code, port, or config-schema changes.
- Helm only — not aligning other config (e.g., GoReleaser SBOM); idrac's SBOM divergence is a separate optional cleanup.
- CRDs not enabled by default.
- Not auto-merging — each PR is human-reviewed.
