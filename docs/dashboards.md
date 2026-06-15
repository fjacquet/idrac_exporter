# Dashboards

The repository ships Grafana dashboards under `grafana/`, auto-provisioned by the
[Docker Compose quickstart](deployment/docker.md) against the `Prometheus` datasource.

| Dashboard | File | Focus |
|-----------|------|-------|
| iDRAC | `grafana/idrac.json` | Detailed per-machine view |
| iDRAC Overview | `grafana/idrac_overview.json` | Global overview of all machines |
| Status (alternative) | `grafana/status-alternative.json` | Detailed per-machine status |
| PDU | `grafana/pdu.json` | Rack PDU power, energy, and health |

The overview and detail dashboards were contributed by
[@7840vz](https://github.com/7840vz).

## Conventions

- Panels read from the provisioned `Prometheus` datasource and key off a single **`system`**
  template variable (`label_values(idrac_system_machine_info, system)`) so one stack can show
  many machines.  `system` is the canonical host-identity label across both export paths:
  - **OTLP/snapshot path:** the exporter injects it automatically (configurable via
    `otlp.identity_label`, default `system`).
  - **On-demand scrape path:** attach a `system` label to each Prometheus target via a static
    label or relabel rule — see the [Docker Compose quickstart](deployment/docker.md) for
    details.
- Health metrics map `0 = OK`, `1 = Warning`, `2 = Critical`.
- Any per-second gauge is aggregated with `sum`/`avg` in PromQL — never `rate()` (the
  exporter already exposes instantaneous values).
