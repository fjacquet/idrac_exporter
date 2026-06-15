# Hand-rolled Redfish client; OpenAPI specs as reference

## Status

Accepted — transport migration to `resty/v2` in Phase 2.

## Context

The family client rule: use the official vendor Go SDK if available **and** useful, else
hand-roll a lean `resty/v2` client. Redfish has no single Go SDK that models the multi-
vendor surface this exporter scrapes (Dell, HPE, Lenovo, Supermicro, …). The repo ships
the Dell iDRAC 10 and DMTF Redfish OpenAPI documents in `docs/swagger/` (~4.7 MB).

## Decision

Hand-roll the client. Migrate the existing `net/http` transport to `resty/v2` (retry
excluding 4xx, TLS min 1.2, bearer/session header) to match the family. Treat the OpenAPI
specs as a **payload-realignment reference, not a codegen source**: generating a client
from a 2.8 MB / hundreds-of-schema spec would pull in a monstrous dependency tree for an
exporter that touches ~15 resources, failing the "no regression" criterion.

## Consequences

The lean client stays readable and vendor-tolerant. The specs are used to validate the
hand-written response structs and to harden parsing (see [0008]). New metrics map to
hand-written structs; there is no generated surface to maintain.
