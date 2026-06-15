# Supply-chain & release hardening

## Status

Accepted — implemented in Phase 1.

## Context

The project was forked from `mrlhansen/idrac_exporter` with an ad-hoc release setup
(hand-rolled `go-binaries.yml`, `docker-images.yml`, unpinned actions, no SBOM, no
vulnerability scanning). The exporter-standards family mandates a hardened pipeline.

## Decision

- Pin Go to `1.26.4` and run `CGO_ENABLED=0` static builds.
- Every CI step is a Makefile target so CI reproduces locally; `make ci` runs gofmt,
  `go vet`, `golangci-lint`, `go test -race`, and `govulncheck`.
- Pin every GitHub Action to a full commit SHA with an explicit `# vX.Y.Z` comment;
  Dependabot bumps actions, gomod, and docker.
- Publish a CycloneDX SBOM (`cyclonedx-gomod`) as a CI artifact and release asset.
- Harden workflows: `persist-credentials: false` on checkouts (except the Helm chart
  push), `cache: false` on `setup-go` in the release workflow, Pages permissions scoped
  to the deploy job.
- Containers run as a non-root user; semgrep runs on every write and blocks on findings,
  with no inline `//nolint` / `//nosemgrep` suppressions (restructure instead).

## Consequences

Reproducible, auditable releases. Inline suppressions are forbidden, so findings such as
the BMC `InsecureSkipVerify` usage are resolved by restructuring (see [0008] and the
`verify` config option) rather than annotation.
