# Multi-target collection with optional OTLP

## Status

Accepted — family-novel. Optional OTLP loop implemented in Phase 4.

## Context

The family reference architecture is a *snapshot* model: a background loop polls one
appliance on an interval, swaps an immutable snapshot, and both `/metrics` and an OTLP
push read it. `idrac_exporter` is different: it is a **multi-target** exporter scraped as
`/metrics?target=<bmc>` (the idiomatic Prometheus relabel pattern, like snmp_exporter and
blackbox_exporter). BMC scrapes can take minutes, so a single fleet-wide poll loop on a
fixed interval is operationally worse than letting Prometheus parallelize per target.

## Decision

Keep the on-demand `?target=` path as the primary collection model (per-target collector
cache with concurrent-scrape coalescing). Add an **optional, off-by-default** background
snapshot loop that polls only the configured `hosts:` and OTLP-pushes them. This is a new
"multi-target exporter" class that the family `architecture.md` is updated to recognize.

## Consequences

Two collection paths to maintain. On-demand is never removed, so the relabel pattern and
the `/discover` endpoint keep working. OTLP is available for environments that want push,
without forcing a fleet-wide poll cadence. The snapshot loop reuses the existing collector
rather than reimplementing it.
