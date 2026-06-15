# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

A Prometheus exporter that scrapes hardware health/telemetry from BMCs (Dell iDRAC, HPE iLO, Lenovo XClarity, Supermicro, and others) over the **Redfish API**. Despite the name, it is vendor-agnostic because it relies on the Redfish standard. Metrics are collected **on-demand**: Prometheus passes the BMC address via the `?target=` query parameter, and the exporter opens a Redfish session to that host at scrape time.

## Build / Run / Test

```sh
make cli              # build bin/idrac_exporter with version/revision ldflags (CGO off)
make ci               # the gate: fmt-check, vet, golangci-lint, go test -race, govulncheck
make sure             # local convenience: fmt, vet, test, build, lint
make tools            # install pinned golangci-lint, cyclonedx-gomod, govulncheck
make run              # go run with RUNFLAGS (default: --config config.yml --verbose)
make sbom             # CycloneDX module SBOM
make release-snapshot # GoReleaser local dry-run (no publish)
```

CI runs `make ci` — every CI step is a Makefile target, so it reproduces locally. Tests use a white-box `httptest` Redfish mock harness in `internal/collector` (`testhelpers_test.go`: `mockRedfish`/`testClient`/`testConfig`); `go test -race ./...` runs them and collector assertions go through `prometheus/testutil`. Also verify against a real or mocked Redfish endpoint — use `--debug` to dump every raw Redfish JSON response (implies `--verbose`), or `--trace` to log each request (method/path/status, token-safe). `--once` collects every configured host once and prints sorted exposition.

Key flags: `--config <path>` (default `/etc/prometheus/idrac.yml`), `--config-watch` (hot-reload on file change via fsnotify), `--verbose`, `--debug`, `--version`.

`VERSION` in the Makefile comes from a `vX.Y.Z` tag on HEAD (else `dev`), so locally-built binaries report `version=dev` unless HEAD is tagged.

## Request flow (the core mental model)

1. `cmd/idrac_exporter/main.go` registers HTTP routes and starts the server. Endpoints: `/metrics` (needs `target`), `/discover` (Prometheus HTTP SD), `/reset`, `/reload`, `/health`, `/`.
2. `metricsHandler` (`handler.go`) → `collector.GetCollector(target, auth)`.
3. `GetCollector` (`internal/collector/collector.go`) keeps a **per-target `*Collector` cache** (`collectors` map). On first request for a target it builds a `Client`, which **discovers all Redfish endpoint paths and detects the vendor** (`client.findAllEndpoints`). This discovery is the expensive, vendor-sensitive step.
4. `Collector.Gather()` serializes metrics to Prometheus text. Concurrent scrapes of the **same** target coalesce via a `sync.Cond`: only one collection runs, the others block and receive the same cached output.
5. `Collector.Collect()` fans out **one goroutine per enabled metric group** (`CollectServer`), each calling a `client.RefreshXxx` method. If the host is a PDU (RackPDUs discovered at root), it takes the separate `RefreshPDUs` path instead.

`/reset?target=` drops a target's cached collector (forces fresh discovery + session). `/reload` re-reads config and resets only hosts whose credentials changed.

## Package layout

- `cmd/idrac_exporter/` — `main.go` (flags, routing, listener), `handler.go` (HTTP handlers, gzip), `config.go` (load/reload/watch wiring).
- `internal/config/` — `model.go` (config structs), `config.go` (file load + `Validate`), `env.go` (env-var overrides), `discover.go` (Prometheus SD JSON). Global singleton `config.Config` guarded by `Config.Mutex`.
- `internal/collector/` — the bulk of the code:
  - `collector.go` — implements `prometheus.Collector`; declares every metric `*prometheus.Desc`; per-target cache; collection concurrency/coalescing.
  - `client.go` — Redfish endpoint discovery, **vendor constants** (`DELL`, `HPE`, …), and all `RefreshXxx` collection methods.
  - `redfish.go` — HTTP transport, Redfish session auth (with HTTP basic-auth fallback), `Get`/`Exists`.
  - `metrics.go` — `NewXxx` helpers that translate parsed structs into `prometheus.MustNewConstMetric` emissions; value mappers like `health2value` (OK=0, Warning=1, Critical=2).
  - `model.go` — Go structs mirroring Redfish JSON responses.
  - `unmarshal.go` — `xstring` (tolerant JSON field that may be null/string/number/`[{"Member":…}]`) and coercion helpers.
- `internal/version/`, `internal/log/` — build-info vars and a small leveled logger.

## Conventions when extending

**Adding a metric** touches four places, in order:

1. `model.go` — add fields to the relevant Redfish response struct.
2. `collector.go` — add a `*prometheus.Desc` field to `Collector`, initialize it in `NewCollector()` (use `prometheus.BuildFQName(prefix, subsystem, name)`), and emit it in `Describe()`.
3. `metrics.go` — add a `NewXxx(ch, …)` emitter method.
4. `client.go` — call the emitter from the appropriate `RefreshXxx` method.

**New vs legacy Redfish resources:** newer BMCs expose `ThermalSubsystem`/`PowerSubsystem`; older ones use the deprecated `Thermal`/`Power` resources. The `Refresh*` dispatchers (`RefreshSensors`, `RefreshPower`) branch on whether the subsystem path was discovered (`...New` vs `...Old` variants). Voltage sensors live in the legacy `Power` resource, so they're emitted with the power group when enabled, otherwise fetched separately.

**Vendor handling:** `client.vendor` is set by matching `system.Manufacturer` substrings during discovery and drives quirks — vendor-specific event-log paths, the Inspur `Storages`→`Storage` fix, iLO 4 path overrides, Dell OEM extras. Per the project's `CONTRIBUTING.md`, prefer metrics that work **across vendors**; large single-vendor features are generally rejected. Small vendor hacks are acceptable.

**BMC JSON is unreliable.** BMCs return inconsistent types, stray `\r`, and odd shapes. `redfish.Get` strips `\r` before unmarshalling; use `xstring` / `asFloat64` for fields whose type varies. Custom `UnmarshalJSON` must never panic (see recent history). TLS verification is skipped by default because BMCs use self-signed certs, but can be enforced per-host with `verify: true` in the config (`InsecureSkipVerify: !verify`, minimum TLS 1.2).

## Configuration

YAML config (`sample-config.yml` is the documented reference) is loaded, then **environment variables override** file values (`CONFIG_*`), then `Validate()` fills defaults. `${VAR}` references inside the YAML are expanded from the environment. Credentials live under `hosts:` (keyed by target IP/hostname, with a `default:` fallback) or `auths:` (named groups referenced via the `?auth=` query param). Metric groups are toggled under `metrics:` (`all: true` enables everything).

## Release / CI

GitHub Actions (`.github/workflows/`): `ci.yml` (`make ci` + SBOM + semgrep on PRs/push), `release.yml` (GoReleaser binaries/SBOM/Release + multi-arch GHCR image on `v*` tags), `docs.yml` (MkDocs Material → Pages), and `helm-charts.yml` (chart publishing on `charts/**`). Actions are SHA-pinned; releases are driven by `.goreleaser.yaml`. The Helm chart lives in `charts/idrac-exporter/`.

## Family-standard recovery (in progress)

This fork is being brought to the exporter-standards family conformance. Design specs live in `docs/superpowers/specs/` (program overview + per-phase) and decisions in `docs/adr/`. Phase 1 (build/CI/docs scaffolding) is landing; Phases 2–5 follow: cobra/logrus/errgroup/resty migration, payload realignment against `docs/swagger/`, an optional OTLP snapshot loop, and the compose quickstart. The on-demand `?target=` collection model is preserved ([ADR 0002](docs/adr/0002-multi-target-with-optional-otlp.md)).

<!-- rtk-instructions v2 -->
# RTK (Rust Token Killer) - Token-Optimized Commands

## Golden Rule

**Always prefix commands with `rtk`**. If RTK has a dedicated filter, it uses it. If not, it passes through unchanged. This means RTK is always safe to use.

**Important**: Even in command chains with `&&`, use `rtk`:
```bash
# ❌ Wrong
git add . && git commit -m "msg" && git push

# ✅ Correct
rtk git add . && rtk git commit -m "msg" && rtk git push
```

## RTK Commands by Workflow

### Build & Compile (80-90% savings)
```bash
rtk cargo build         # Cargo build output
rtk cargo check         # Cargo check output
rtk cargo clippy        # Clippy warnings grouped by file (80%)
rtk tsc                 # TypeScript errors grouped by file/code (83%)
rtk lint                # ESLint/Biome violations grouped (84%)
rtk prettier --check    # Files needing format only (70%)
rtk next build          # Next.js build with route metrics (87%)
```

### Test (60-99% savings)
```bash
rtk cargo test          # Cargo test failures only (90%)
rtk go test             # Go test failures only (90%)
rtk jest                # Jest failures only (99.5%)
rtk vitest              # Vitest failures only (99.5%)
rtk playwright test     # Playwright failures only (94%)
rtk pytest              # Python test failures only (90%)
rtk rake test           # Ruby test failures only (90%)
rtk rspec               # RSpec test failures only (60%)
rtk test <cmd>          # Generic test wrapper - failures only
```

### Git (59-80% savings)
```bash
rtk git status          # Compact status
rtk git log             # Compact log (works with all git flags)
rtk git diff            # Compact diff (80%)
rtk git show            # Compact show (80%)
rtk git add             # Ultra-compact confirmations (59%)
rtk git commit          # Ultra-compact confirmations (59%)
rtk git push            # Ultra-compact confirmations
rtk git pull            # Ultra-compact confirmations
rtk git branch          # Compact branch list
rtk git fetch           # Compact fetch
rtk git stash           # Compact stash
rtk git worktree        # Compact worktree
```

Note: Git passthrough works for ALL subcommands, even those not explicitly listed.

### GitHub (26-87% savings)
```bash
rtk gh pr view <num>    # Compact PR view (87%)
rtk gh pr checks        # Compact PR checks (79%)
rtk gh run list         # Compact workflow runs (82%)
rtk gh issue list       # Compact issue list (80%)
rtk gh api              # Compact API responses (26%)
```

### JavaScript/TypeScript Tooling (70-90% savings)
```bash
rtk pnpm list           # Compact dependency tree (70%)
rtk pnpm outdated       # Compact outdated packages (80%)
rtk pnpm install        # Compact install output (90%)
rtk npm run <script>    # Compact npm script output
rtk npx <cmd>           # Compact npx command output
rtk prisma              # Prisma without ASCII art (88%)
```

### Files & Search (60-75% savings)
```bash
rtk ls <path>           # Tree format, compact (65%)
rtk read <file>         # Code reading with filtering (60%)
rtk grep <pattern>      # Search grouped by file (75%). Format flags (-c, -l, -L, -o, -Z) run raw.
rtk find <pattern>      # Find grouped by directory (70%)
```

### Analysis & Debug (70-90% savings)
```bash
rtk err <cmd>           # Filter errors only from any command
rtk log <file>          # Deduplicated logs with counts
rtk json <file>         # JSON structure without values
rtk deps                # Dependency overview
rtk env                 # Environment variables compact
rtk summary <cmd>       # Smart summary of command output
rtk diff                # Ultra-compact diffs
```

### Infrastructure (85% savings)
```bash
rtk docker ps           # Compact container list
rtk docker images       # Compact image list
rtk docker logs <c>     # Deduplicated logs
rtk kubectl get         # Compact resource list
rtk kubectl logs        # Deduplicated pod logs
```

### Network (65-70% savings)
```bash
rtk curl <url>          # Compact HTTP responses (70%)
rtk wget <url>          # Compact download output (65%)
```

### Meta Commands
```bash
rtk gain                # View token savings statistics
rtk gain --history      # View command history with savings
rtk discover            # Analyze Claude Code sessions for missed RTK usage
rtk proxy <cmd>         # Run command without filtering (for debugging)
rtk init                # Add RTK instructions to CLAUDE.md
rtk init --global       # Add RTK to ~/.claude/CLAUDE.md
```

## Token Savings Overview

| Category | Commands | Typical Savings |
|----------|----------|-----------------|
| Tests | vitest, playwright, cargo test | 90-99% |
| Build | next, tsc, lint, prettier | 70-87% |
| Git | status, log, diff, add, commit | 59-80% |
| GitHub | gh pr, gh run, gh issue | 26-87% |
| Package Managers | pnpm, npm, npx | 70-90% |
| Files | ls, read, grep, find | 60-75% |
| Infrastructure | docker, kubectl | 85% |
| Network | curl, wget | 65-70% |

Overall average: **60-90% token reduction** on common development operations.
<!-- /rtk-instructions -->