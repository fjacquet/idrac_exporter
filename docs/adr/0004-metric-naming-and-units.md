# Metric naming, units, and the `idrac_` prefix

## Status

Accepted — applies across Phases 3–5.

## Context

The family convention uses a per-repo metric prefix and unit-explicit names, and treats
per-second values as gauges. This exporter has an established public contract: the `idrac_`
prefix (configurable via `metrics_prefix`) and port `9348`, already consumed by existing
dashboards, alerts, and scrape configs.

## Decision

- Keep the `idrac_` prefix. Like `obs_exporter`'s deliberate `ecs_`, it is a vocabulary
  keep, not drift — the metrics are BMC/Redfish vocabulary and the name is the public
  contract. Changing it would break every downstream dashboard and alert.
- Keep port `9348` (the registered default for this exporter).
- Be unit-explicit in metric names and stable per generation; per-second values (where
  added) are gauges aggregated with `sum`/`avg` in PromQL, never `rate()`.

## Consequences

No breaking change to the public metric surface across the fork. New metrics follow the
unit-explicit naming rule. Grafana panels use `sum`/`avg` on gauges.
