# Phase 1 — Universal Layer (Design)

**Status:** Draft · 2026-06-15
**Parent:** [program overview](2026-06-15-idrac-family-recovery-overview-design.md)
**Scope:** Bring the build, toolchain, CI/CD, supply-chain, container, and docs scaffolding
to exporter-standards family conformance. **No change to metric output or collection
behavior** (those are Phases 2–4) — the one structural exception is the TLS-config refactor
in `redfish.go` (see Risks), which preserves the existing skip-verify default. Split into two
reviewable PRs: **1a build foundation**, **1b CI/CD + docs**.

## Goal & exit definition

"Phase 1 done" = the repo builds under the new module path on Go 1.26.4, `make ci` and
`make release-snapshot` are green **locally**, and the container runs non-root. The publish
paths (GHCR push, Pages deploy, Homebrew cask) are wired but verified later, once
`github.com/fjacquet/idrac_exporter` exists with Pages enabled and `HOMEBREW_TAP_GITHUB_TOKEN`
set. The cask self-skips when that secret is absent, so releases never break meanwhile.

---

## PR 1a — Build foundation

### Module rename

- `go mod edit -module github.com/fjacquet/idrac_exporter`; rewrite every
  `github.com/mrlhansen/idrac_exporter` import across all `.go` files.
- Update `Makefile` `REPOSITORY` var and the ldflags path.
- **Keep idrac's `internal/version.{Version,Revision}` injection** (do *not* move to the
  family's `main.version`): the `idrac_exporter_build_info` metric is emitted from
  `internal/collector`, which cannot import `main`. ldflags become
  `-X github.com/fjacquet/idrac_exporter/internal/version.Version=… -X …/internal/version.Revision=…`.
  This is a deliberate, documented deviation from the family's `main.version` convention.

### Go & Makefile

- `go.mod`: `go 1.26.4` (patch-pinned, not bare `1.26`).
- Full Makefile contract: `tools fmt-check fmt vet lint test test-race test-coverage vuln
  ci sure cli sbom release release-snapshot docker run-cli clean`. `make tools` installs
  pinned `golangci-lint`, `cyclonedx-gomod`, `govulncheck`. `make cli` builds `bin/idrac_exporter`
  with the ldflags above. `CGO_ENABLED=0` for release builds.

### Dockerfile

- Multi-stage; **non-root `USER`** via `adduser -D -u 10001`.
- `COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt`
  — **do not** `apk add ca-certificates` (fails behind a corporate MITM proxy).
- The release image is built from this same `./Dockerfile` by the `release.yml`
  `docker/build-push-action` job (the pflex/ppdd hand-rolled reference pattern) — **no
  `Dockerfile.goreleaser`** (that is the SDK-backed `pstore` GoReleaser `dockers_v2` variant).
- Retain `entrypoint.sh` (the `/authconfig/$NODE_NAME` credential injection is a k8s feature)
  — verify the `/authconfig` mount is readable by uid 10001 under the non-root user.

### .gitignore

- Expand to the canonical family set: build artifacts (`idrac_exporter`, `bin/`,
  `coverage.html`, `dist/`), logs, `site/`, local secrets (`.env`, `*.local.yaml`,
  `.claude.local.md`, `.claude/settings.local.json`), editor/OS noise, `.rtk/`.

### 1a exit criteria

- `make build` and `make cli` produce a runnable binary under the new module path.
- `make sure` (fmt + vet + test + build + lint) is green — see the **golangci-lint** risk below.
- `docker build` succeeds; `docker run … id -u` reports `10001`.

---

## PR 1b — CI/CD + supply-chain + docs

### Workflow trio (+ kept Helm)

- **`ci.yml`** — PRs + push to `main`: `make ci` (gofmt check, `go vet`, golangci-lint,
  `go test -race`, govulncheck) + CycloneDX SBOM artifact + Semgrep. `cache: true` (speed).
- **`release.yml`** — `v*` tags: GoReleaser job (binaries + SBOM + GitHub Release) **+**
  multi-arch GHCR image job (`linux/amd64,arm64`, `sbom: true`, `provenance: mode=max`).
  **`cache: false`** on `setup-go` here.
- **`docs.yml`** — push to `main` (`docs/**`): MkDocs Material → GitHub Pages,
  `build_type: workflow`.
- **`helm-charts.yml`** — **kept as-is** (chart-path-triggered). Its checkout that pushes
  the packaged chart to the pages branch must **keep `persist-credentials: true`** (branch-push
  needs the token) — the documented exception to the hardening rule, same class as nbu's
  `static.yml`.
- Replaced & deleted: `go-binaries.yml` (→ GoReleaser), `docker-images.yml` (→ image job).

### Workflow hardening (all workflows)

- `persist-credentials: false` on every `actions/checkout` **except** the Helm chart-push
  checkout.
- Pages perms scoped to the deploy job (`contents: read` at workflow level; `pages: write` +
  `id-token: write` on `jobs.deploy.permissions`).
- **Every action SHA-pinned** with an explicit `# vX.Y.Z` comment (resolve via
  `gh api repos/<owner>/<action>/commits/<tag> --jq .sha`). `semgrep/semgrep` container may
  keep its rolling tag.
- `.github/dependabot.yml`: `github-actions` + `gomod` + `docker`.

### `.goreleaser.yaml` (`version: 2`)

- `builds`: `CGO_ENABLED=0`, `goos:[linux,darwin,windows]`, `goarch:[amd64,arm64]`,
  `-trimpath`, `ldflags: -s -w -X …/internal/version.Version={{.Version}} -X …/internal/version.Revision={{.Commit}}`,
  `mod_timestamp: {{.CommitTimestamp}}`.
- `archives`: `tar.gz` incl. `LICENSE README.md config.yaml`; Windows `zip` override.
- `checksum`: sha256 → `checksums.txt`. `sboms`: `cyclonedx-gomod` (module path `../`).
- `homebrew_casks`: `fjacquet/homebrew-tap` via `HOMEBREW_TAP_GITHUB_TOKEN`; `skip_upload`
  when the secret is empty; post-install quarantine-strip. `changelog: github-native`.
- Validate with `goreleaser check`; dry-run with `make release-snapshot`.

### Docs & ADRs

- MkDocs Material skeleton: `mkdocs.yml` + `docs/index.md` + nav (ADR section now; `metrics.md`,
  `dashboards.md`, `deployment/` are placeholders filled in later phases).
- `docs/adr/` with `index.md` and **8 ADRs** (`NNNN-title.md`,
  Status/Context/Decision/Consequences). They record decisions already made; each notes its
  implementing phase in Consequences:
  `0001` supply-chain hardening (Phase 1) · `0002` multi-target + optional OTLP, family-novel
  — also fold a "multi-target exporter" class back into the skill's `architecture.md` (Phase 4)
  · `0003` hand-rolled resty client + swagger-as-reference (Phase 2) · `0004` naming/units +
  `idrac_` prefix keep (Phases 3–5) · `0005` label-key invariant (Phase 2) · `0006` token-auth +
  retry-excludes-4xx (Phase 2) · `0007` config hot reload, SIGHUP + watch (Phase 2) · `0008`
  absent-not-zero parsing (Phase 3).
- README: 6 canonical badges (CI status, latest release, Go Report Card, Go version, license, docs).

### 1b exit criteria

- `make ci` green locally; `goreleaser check` passes; `make release-snapshot` emits `dist/`
  binaries + `checksums.txt` + SBOM.
- Semgrep clean (see TLS risk below); `mkdocs build --strict` succeeds; ADR `index.md` complete.
- Publish paths (GHCR/Pages/Homebrew) deferred to live-repo verification.

---

## Risks & mitigations

- **golangci-lint over never-linted code** will likely surface findings that fail `make ci`.
  Mitigation: add `.golangci.yml` with the family linter set; fix findings by restructuring
  (the **no inline `//nolint` suppressions** rule holds). Budget a cleanup sub-task in 1a/1b.
- **Semgrep will flag `InsecureSkipVerify: true`** in `redfish.go`. It is intentional (BMCs
  use self-signed certs), but inline suppression is banned. Mitigation: **restructure** —
  drive TLS verification from config (a per-host `insecure` bool, defaulting to skip for BMC
  compatibility) so no hardcoded `true` literal trips the rule. This lightly touches
  `redfish.go`/config; it is the one core-adjacent edit Phase 1 carries, and it doubles as a
  real feature (optional cert verification). If it proves larger than expected, defer the
  semgrep gate's strict-fail to Phase 2 and track the finding explicitly.
- **Module-rename miss** → build break; caught by `make build` in 1a.
- **Go 1.26.4 availability** — follow the family canon; if the toolchain isn't installed,
  `make tools`/CI `setup-go` provisions it.

## Non-goals (Phase 1)

No cobra/logrus/errgroup/resty migration (Phase 2); no OTLP/snapshot (Phase 4); no new
metrics; no `docs/metrics.md` catalog yet (Phase 3); no unit tests yet (Phase 2 — `go test
-race` passes trivially meanwhile); Helm chart unchanged.

## Verification

Infra phase — verification is running the make targets, `goreleaser check`, and
`docker run … id -u`, not unit tests. Each of 1a and 1b lands as its own PR gated by the
new `make ci`.
