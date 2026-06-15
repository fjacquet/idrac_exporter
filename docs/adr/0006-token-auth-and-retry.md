# Session/token auth with retry excluding 4xx

## Status

Accepted — formalized in the Phase 2 `resty/v2` migration.

## Context

Redfish authenticates via a session token (`X-Auth-Token` from `SessionService/Sessions`),
with HTTP Basic auth as a fallback. The exporter already creates, refreshes, and deletes
sessions and disables session auth on failure. The family rule: modern session/token flow,
and retries must **exclude 4xx** so authentication failures are never retried.

## Decision

Preserve the session-first, basic-auth-fallback flow. When the transport moves to
`resty/v2`, configure retry to exclude all 4xx responses (auth failures fail fast). Session
creation/refresh stays idempotent: a 401/404 on refresh recreates the session, and repeated
failure disables session auth for the host.

## Consequences

No retry storms against a BMC returning 401/403. Token handling is centralized in the
client. `--trace` logging must skip any response whose body or headers carry the token.
