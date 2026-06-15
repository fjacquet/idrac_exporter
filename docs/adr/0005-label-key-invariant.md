# Label-key consistency invariant

## Status

Accepted — enforced from Phase 2.

## Context

The family load-bearing invariant: a metric name must carry **one** label-key set across
all of its series. Prometheus rejects inconsistent label sets within a metric family, and
this exporter emits some families from more than one code path (e.g. the new vs legacy
Redfish resource variants, `RefreshSensorsNew`/`RefreshSensorsOld`).

## Decision

When a metric family is produced by two paths, emit a **union label set in a fixed
canonical order**, filling empty values for keys that do not apply on a given path. Add an
identity label to every series (`instance`/`system`) so one exporter process can serve
multiple targets. A test enforces the invariant.

## Consequences

Mixed-path metric families stay valid. The identity label complements (does not replace)
the Prometheus `instance` relabel, so snapshot/OTLP series are attributable to a target.
