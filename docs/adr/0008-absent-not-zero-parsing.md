# Defensive payload parsing: absent, never zero

## Status

Accepted — hardened in Phase 3 against the `docs/swagger/` reference.

## Context

BMC Redfish payloads are unreliable: string-typed numbers, per-vendor value shapes (the
`[{"Member": …}]` form), `"N/A"` strings, and stray `\r`. The exporter already strips `\r`
and uses tolerant types (`xstring`, `asFloat64`); a recent fix ensured `xstring`
unmarshalling cannot panic. The family rule: an unparseable value yields an **absent
sample, never a zero** — a fake `0` on a capacity or error metric silently corrupts
dashboards and alerts.

## Decision

Keep tolerant parsing localized (the `unmarshal.go` helpers). Custom `UnmarshalJSON` must
never panic. When a field cannot be parsed, emit **no sample** for it rather than a zero.
Validate the response structs in `model.go` against the Dell iDRAC 10 and DMTF Redfish
OpenAPI documents in `docs/swagger/` during the Phase 3 realignment pass.

## Consequences

Missing/garbled fields disappear from `/metrics` instead of reporting misleading zeros.
The OpenAPI specs are the authority for field names and shapes.
