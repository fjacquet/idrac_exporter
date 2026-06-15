# Architecture Decision Records

Decisions for `idrac_exporter` as it is brought into the exporter-standards family.
Each record uses the `NNNN-title.md` convention with Status / Context / Decision /
Consequences sections.

| ADR | Title | Status | Implemented |
|-----|-------|--------|-------------|
| [0001](0001-supply-chain-hardening.md) | Supply-chain & release hardening | Accepted | Phase 1 |
| [0002](0002-multi-target-with-optional-otlp.md) | Multi-target collection with optional OTLP | Accepted | Phase 4 |
| [0003](0003-handrolled-redfish-client.md) | Hand-rolled Redfish client; OpenAPI specs as reference | Accepted | Phase 3 |
| [0004](0004-metric-naming-and-units.md) | Metric naming, units, and the `idrac_` prefix | Accepted | Phases 3–5 |
| [0005](0005-label-key-invariant.md) | Label-key consistency invariant | Accepted | Phase 2 |
| [0006](0006-token-auth-and-retry.md) | Session/token auth with retry excluding 4xx | Accepted | Phase 2 |
| [0007](0007-config-hot-reload.md) | Config hot reload (SIGHUP + file watch) | Accepted | Phase 2 |
| [0008](0008-absent-not-zero-parsing.md) | Defensive payload parsing: absent, never zero | Accepted | Phase 3 |
