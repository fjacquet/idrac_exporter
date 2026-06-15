# Phase 2 — Plumbing Migrations (Design)

**Status:** Draft · 2026-06-15
**Parent:** [program overview](2026-06-15-idrac-family-recovery-overview-design.md)
**Scope:** Migrate the CLI, logging, config, and concurrency plumbing to the family stack.
**Contract-neutral:** metric output is unchanged. Three reviewable, TDD sub-PRs: **2a CLI +
logging + test harness**, **2b config hardening**, **2c concurrency**. Sequence 2a → 2b → 2c
(2a stands up the test harness the others build on).

## Settled decisions

| # | Decision |
|---|----------|
| D1 | **Logs: TTY-aware** — logrus `TextFormatter` when stdout is a terminal, `JSONFormatter` when piped/redirected. |
| D2 | **`--once` collects every configured host** (each entry under `hosts:` except `default`), prints sorted exposition, exits. |
| D3 | **Keep `--verbose` and `--config-watch`** as long flags (no functional removal). |
| D4 | **Double-dash migration, no compat shim** — cobra/pflag means single-dash `-config` stops parsing; update our own callers (`Makefile`, docs, compose). External `-config` users move to `--config`. |
| D5 | **`concurrency` default `0` = unlimited** (= today's behavior); `>0` caps the fan-out and ties the conn pool. |
| D6 | **Identity label, `idrac_up`, snapshot/OTLP are NOT in Phase 2** — deferred to Phase 4 (they are redundant/conflicting in the on-demand model, where Prometheus already supplies `instance` via relabel and the built-in `up` reflects per-target health). |

## Library additions (`stack.md` canon)

`github.com/spf13/cobra`, `github.com/sirupsen/logrus`, `golang.org/x/sync/errgroup`,
`github.com/joho/godotenv`. Each lands in the sub-PR that first uses it.

---

## 2a — CLI + logging + test harness

### cobra

- `rootCmd` whose `RunE` is the current `main` server path. `PersistentFlags`:
  `--config` (default `/etc/prometheus/idrac.yml`), `--debug`, `--trace`, `--once`,
  `--verbose`, `--config-watch`; `--version` preserved (flag or `version` subcommand).
- `--verbose` is an alias that raises the log level to debug; `--debug` additionally enables
  the raw-JSON Redfish dump (`config.Debug`) as today.
- **Update callers** to `--`: `Makefile` `RUNFLAGS` (`--config config.yml --verbose`), the
  README/docs, and any compose/docs references. `entrypoint.sh` passes no flags (uses the
  default config path), so it is unaffected.

### logging (logrus behind the existing API)

- Keep the package-level `internal/log` API (`Info/Warn/Error/Debug/Fatal`, `SetLevel`,
  `SetLogFile`, `SetDefaultLogger`) so call sites barely change; reimplement the internals
  with logrus. The printf-style signatures map to logrus `*f` formatting.
- TTY detection on the configured output selects Text vs JSON formatter (D1). Level maps
  `LevelFatal…LevelDebug` → logrus levels.

### `--once` and `--trace`

- **`--once`**: build a client + collector for every configured host (D2), run one `Gather`
  each, write the exposition **sorted** to stdout, then exit — no HTTP server. This is the
  live-validation tool (`--once --debug` output diffs against the future `docs/metrics.md`).
- **`--trace`**: when set, log each Redfish request as method · path · status (body only
  under `--debug`). **Token-safe:** never log the `X-Auth-Token` header; the session-create
  response carries the token in a header, so header logging stays off for every request.

### test-harness foundation

- `internal/collector/*_test.go` with an `httptest` mock Redfish server serving minimal
  canned JSON (ServiceRoot → Systems → System, Chassis → Thermal). A small seam points the
  client's base URL at the test server (scheme `http`, host = test server address).
- First collector test: enable `system` + `sensors`, `Gather`, assert via the Prometheus
  registry that e.g. `idrac_system_health` and `idrac_sensors_temperature` are present with
  expected values. Fixtures live under `internal/collector/testdata/`.

### 2a exit criteria

`make ci` green; `--config/--debug/--once/--trace/--verbose/--config-watch/--version` all
work; `--once --debug` prints sorted samples; `--trace` never leaks a token; the harness +
first test pass; metric output byte-identical to before.

---

## 2b — config hardening

- **Reload**: add a `SIGHUP` handler that runs the existing `ReloadConfig`, alongside the
  retained file-watch. Reimplement the watcher cleanly to also handle `fsnotify.Rename`
  (editor atomic-save), with a bounded re-add retry and **no** `go WatchConfig()` recursion
  or in-loop `time.Sleep` (the smells from upstream #148).
- **`passwordFile`**: a per-host/auth field; when set, `Validate` reads the secret from the
  file (fails fast if unreadable). Coexists with `password` / `${ENV}`.
- **godotenv**: load `.env` at startup (CWD, then the config file's dir) **before** the
  `os.ExpandEnv` interpolation in `FromFile`, never overriding already-set env vars.
- tests: reload swaps config and resets only changed hosts; `passwordFile` populates the
  secret; `.env` fills `${VAR}` but a real env var wins.

### 2b exit criteria

`make ci` green; `SIGHUP` and a file edit (incl. rename-save) both reload; `passwordFile`
and `.env` resolve credentials; metric output unchanged.

---

## 2c — concurrency + unchecked collector

- **errgroup**: replace the `sync.WaitGroup` fan-out in `CollectServer` (and the PDU path)
  with `errgroup.Group`. Add a `concurrency` root-config option: `0` = unlimited (current
  behavior, no `SetLimit`); `>0` calls `SetLimit(n)` and sets `MaxConnsPerHost = n+1`,
  `MaxIdleConnsPerHost = n` on the Redfish transport (#189's intent for slow BMCs).
- **panic safety**: each group goroutine wraps its `RefreshXxx` in a `recover()` that logs
  and increments the existing error counter, so a panic on malformed BMC JSON degrades the
  scrape gracefully instead of crashing or hanging (supersedes #189's `defer wg.Done()`).
- **unchecked collector**: `Describe()` sends nothing (dynamic name set). No output change —
  the same metrics are emitted; only registration-time descriptor validation is dropped.
- tests: with `concurrency: 2` no more than 2 groups run at once; a `RefreshXxx` that panics
  yields a counted error and a still-successful `Gather`; the error total surfaces in
  `idrac_exporter_scrape_errors_total`.

### 2c exit criteria

`make ci` green; default build behaves exactly as today; `concurrency` bounds parallelism
and the conn pool; a panicking refresh is contained; metric output unchanged.

---

## Non-goals (Phase 2)

No identity label, no `idrac_up`, no snapshot/OTLP loop (all Phase 4); no new metrics; no
`docs/metrics.md` catalog (Phase 3). The metric exposition stays byte-identical across all
three sub-PRs.

**Resty deferral:** Phase 2 keeps the existing `net/http` transport. The `resty/v2` rework
(ADR 0003) is **moved to Phase 3**, where it pairs naturally with the payload realignment
that already reworks the client and response structs — doing both at once avoids touching
`redfish.go`/`model.go` twice. ADR 0003 currently reads "Phase 2"; it needs a one-line
update to "Phase 3" (tracked as a follow-up to this spec).

## Risks

- **Double-dash break (D4)** — contained to our callers; external `-config` users adjust.
  Mitigated by clear release notes.
- **logrus output format change** — TTY-aware keeps local runs readable; piped/aggregated
  runs get JSON. Operators parsing the old text format in pipelines must switch to JSON.
- **errgroup panic semantics** — errgroup does not recover panics; the explicit per-group
  `recover()` is load-bearing and must wrap every spawned refresh.
- **test seam** — pointing the client at an `httptest` URL may need a tiny constructor tweak
  (base-URL injection); keep it minimal and test-only.
