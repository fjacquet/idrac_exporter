# OTLP via the Prometheus bridge over a MetricFamily snapshot

## Status

Accepted — implemented in Phase 4. Refines [ADR 0002](0002-multi-target-with-optional-otlp.md).

## Context

The family reference OTLP path (pflex/pstore) collects into a typed snapshot of
samples and exposes it through OpenTelemetry *observable gauges*. `idrac_exporter`
is Prometheus-native: every metric is emitted with `prometheus.MustNewConstMetric`
into a per-target registry. Re-expressing each emitter as an observable gauge would
rewrite the entire collector and the on-demand path — contradicting ADR 0002's
"reuse the existing collector rather than reimplementing it."

## Decision

The optional background loop reuses the existing collectors via a coalescing-safe
`GatherFamilies()`, injects a configurable identity label (default `system`) and a
per-target `idrac_up` gauge, and publishes an immutable `[]*dto.MetricFamily`
snapshot. The OTLP path is `go.opentelemetry.io/contrib/bridges/prometheus`
`NewMetricProducer(WithGatherer(store))` feeding a `metric.PeriodicReader`. No
emitter is rewritten. The on-demand `/metrics?target=` path is unchanged.

## Consequences

A dependency on the contrib Prometheus bridge. The snapshot is gathered
`MetricFamily` rather than `[]Sample`; the bridge maps gauges → OTel gauges and
counters → monotonic sums (verified), dropping only `UNTYPED`/`GAUGE_HISTOGRAM`,
which this exporter never emits. The family `architecture.md` is updated to
recognize this "prometheus-native bridge" variant of the OTLP export path.
