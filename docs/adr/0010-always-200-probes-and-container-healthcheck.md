# Always-200 `/livez` and `/readyz`, and a container `HEALTHCHECK`

## Status

Accepted — implemented 2026-08-01. Adopts the family probe and container-image
standards this repo was skipped by; see the family catch-up plan
[`2026-08-01-family-catch-up.md`](../superpowers/plans/2026-08-01-family-catch-up.md).

## Context

`/health` was registered but empty: `healthHandler` wrote nothing and returned a
bare 200. The Helm chart pointed both the liveness and readiness probes at it,
and neither Dockerfile nor either compose file declared a health check, so a
container that had stopped serving looked identical to a healthy one.

This exporter is blackbox-style: `/metrics?target=` collects one BMC per
request, and the `SnapshotStore` is only built when OTLP is enabled. There is
therefore no background collection state a readiness gate could consult — and
no honest way to express "not ready".

## Decision

Three fixed paths, all unconditionally 200:

- `/livez` and `/readyz` are wired to one `staticOKHandler` that reads no
  configuration, no collector and no snapshot. A probe here can never be the
  reason a working process is restarted or pulled from rotation.
- `/health` keeps status 200 unconditionally and gains an informational JSON
  body: `status`, `version`, `revision`, and one `hosts[]` entry per configured
  BMC (`host`, `scheme`, and `default_target`, a bool flagging whether that
  host matches the deprecated root-level `default_target` config setting).
  The `default` map key is a credential fallback, not a target, and is
  excluded. There is no `last_scrape` or per-host `ok` field — per-host
  reachability is answered by `idrac_up` on a scrape.
- Probes never point at `/metrics`: a probe tick would drive a real Redfish
  scrape and can block behind an unreachable BMC.

The routes are registered with `http.HandleFunc` on `http.DefaultServeMux`,
alongside the six existing routes. This repo's server is deliberately left on
the default mux — matching the existing idiom was preferred over a refactor
whose only purpose would be family cosmetics.

Both Dockerfiles gain
`HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3` running
`wget --spider http://127.0.0.1:9348/livez`, and both compose files gain a
matching `healthcheck:` with identical numbers. The address is `127.0.0.1`, not
`localhost`: busybox `wget` resolves `localhost` to `::1` first and the exporter
binds IPv4 only.

The Alpine base tag is unpinned to `alpine:latest`, replacing `alpine:3.23`.

## Consequences

Kubernetes and Docker probes stop depending on configuration or BMC
reachability, so a transient BMC outage can no longer restart the exporter or
remove it from a Service. `/health` becomes useful to a human — it says what the
exporter is configured to scrape — while remaining useless as a gate, which is
the point.

Unpinning the Alpine tag cuts against [ADR 0001](0001-supply-chain-hardening.md),
which pins GitHub Actions by SHA, the tool versions, and the Go builder: the base
image becomes the one input whose contents can change between two builds of the
same commit, which is what the SBOM and provenance attestations exist to nail
down. Uniformity across the fifteen family repos was chosen over reproducibility
on the three that pinned. Revisiting it is a family-wide decision, not a
per-repo one.

The Helm chart's liveness and readiness probes move from `/health` to `/livez`
and `/readyz`. `/health` remains served and remains 200, so any external check
pointed at it keeps working.
