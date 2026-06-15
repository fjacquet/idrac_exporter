# Dashboards

The repository ships Grafana dashboards under `grafana/`, auto-provisioned by the
[Docker Compose quickstart](deployment/docker.md) against the `Prometheus` datasource.

| Dashboard | File | Focus |
|-----------|------|-------|
| iDRAC | `grafana/idrac.json` | Detailed per-machine view |
| iDRAC Overview | `grafana/idrac_overview.json` | Global overview of all machines |
| Status (alternative) | `grafana/status-alternative.json` | Detailed per-machine status |

The overview and detail dashboards were contributed by
[@7840vz](https://github.com/7840vz).

## Conventions

- Panels should read from the provisioned `Prometheus` datasource and key off a
  `system`/`instance` template variable so one stack can show many machines.
- Health metrics map `0 = OK`, `1 = Warning`, `2 = Critical`.
- Any per-second gauge is aggregated with `sum`/`avg` in PromQL — never `rate()` (the
  exporter already exposes instantaneous values).
