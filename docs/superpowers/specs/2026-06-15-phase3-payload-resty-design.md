# Phase 3 — Payload Realignment + resty (Design)

**Status:** Draft · 2026-06-15
**Parent:** [program overview](2026-06-15-idrac-family-recovery-overview-design.md)
**Scope:** Migrate the Redfish transport to `resty/v2`, validate the response structs against the `docs/swagger/` OpenAPI reference, finish the absent-not-zero parsing hardening, and publish a `docs/metrics.md` catalog.

Three reviewable PRs in sequence: **3a resty transport**, **3b payload realignment + absent-not-zero**, **3c metrics catalog**. `make ci` is the gate for each. The public contract (metric prefix `idrac_`, port `9348`) is unchanged.

## Settled decisions

| # | Decision |
|---|----------|
| D1 | **Transport → `resty/v2`** (ADR 0003), hand-rolled client preserved. Every existing behavior is kept; the swagger specs are a realignment reference, never a codegen source. |
| D2 | **Retry: idempotent GET/HEAD only.** `Get`/`Exists` retry (~2 attempts, short backoff), **excluding 4xx**. The session-create `POST` is **never** retried (avoids duplicate BMC sessions). This is the only behavioral *addition* in 3a. |
| D3 | **3a is contract-neutral; 3b is contract-improving.** 3a keeps metric output byte-identical. 3b *intentionally* changes output where it was wrong: a malformed/absent field that today emits a fake `0` will emit **no sample** instead (ADR 0008). These removals are documented. |
| D4 | **3b is an exhaustive field-by-field audit** of every `model.go` struct against the Dell iDRAC 10 (`docs/swagger/11017-1.30.xx.json`) and DMTF Redfish (`docs/swagger/openapi-7.xx.yaml`) documents. |
| D5 | **`UnmarshalJSON` must never panic** (ADR 0008). The known empty-slice `list[0]` dereference in `unmarshal.go` is fixed, plus a sweep for similar. |
| D6 | **`docs/metrics.md` is its own 3c PR**, verified against `--once --debug` exposition and wired into mkdocs nav. |

## Library additions

`github.com/go-resty/resty/v2` (lands in 3a). No codegen tooling is added.

---

## 3a — resty/v2 transport migration *(contract-neutral)*

Rework `internal/collector/redfish.go` (currently the only `net/http` user in the collector) onto a `resty/v2` client, **preserving every behavior**:

- **Methods reimplemented on resty:** `Get`, `Exists`, `CreateSession`, `RefreshSession`, `DeleteSession`. All quirks kept:
  - the `SessionService/Sessions` → `Sessions` fallback on `405` (iDRAC 8),
  - the iLO 4 session id parsed from the `Location` header when the body omits it,
  - `\r` stripping before unmarshal (#192),
  - `X-Auth-Token` header when a session exists, HTTP basic-auth fallback otherwise,
  - `config.Trace` request logging (method · path · status, **token-safe** — never log `X-Auth-Token`) and `config.Debug` raw-body dump.
- **TLS unchanged:** `InsecureSkipVerify: !auth.Verify`, `MinVersion: tls.VersionTLS12`.
- **Connection pool:** the 2c sizing (`MaxConnsPerHost = n+1`, `MaxIdleConnsPerHost = n` when `concurrency > 0`, else 20/10) is carried onto resty's underlying transport.
- **Retry (D2):** resty retry on `Get`/`Exists` only — ~2 retries, short backoff, retry condition **excludes 4xx** (a `404`/`401` is a real answer, not a transient failure, and the session-refresh path already handles `401`/`404` explicitly). The session-create `POST` runs with retries disabled.
- **Timeouts:** preserve the existing `config.Config.Timeout`-derived client timeout and response-header timeout.

### 3a tests

Extend the `httptest` harness:

- a flaky GET (first attempt 503, second 200) is retried and succeeds;
- a GET returning 404/400 is **not** retried (single request observed);
- the session `POST` is issued exactly once even when it fails;
- `--trace` output never contains a token; `\r` is still stripped.

### 3a exit criteria

`make ci` green; metric output byte-identical to `main`; the four behaviors above are asserted; no `net/http` client construction remains in `redfish.go` (resty owns the transport).

---

## 3b — payload realignment + absent-not-zero *(contract-improving, documented)*

- **Never-panic fix (D5):** guard the empty-slice dereference at `internal/collector/unmarshal.go:60` (`list[0]` when `list` is `[]`), and sweep `model.go`/`unmarshal.go` for any other custom `UnmarshalJSON` that can index/convert without a length/type check.
- **Exhaustive struct validation (D4):** for every Redfish resource modelled in `model.go`, verify field names, JSON tags, and value shapes against the two `docs/swagger/` documents; correct drift. Because the audit spans ~15 resources × 4.7 MB of specs, the 3b *plan* may fan out one worker per resource (multi-agent — requires explicit opt-in at plan time).
- **Absent-not-zero audit (D3):** review the `metrics.go` emitters. Where a missing or unparseable field currently yields `MustNewConstMetric(..., 0)` — capacity, energy, error counts, health, speeds — emit **no sample** instead. Record each newly-absent-on-malformed metric in the PR description; these are the only output changes in Phase 3.

### 3b tests

- Table-driven `xstring`/`asFloat64` cases: `null`, string, integer, float, `[{"Member": …}]`, **empty list `[]`**, `"N/A"`, stray `\r`.
- Emitter tests: an absent/garbled source field produces **no** sample (not a `0`), while a valid field still emits.
- Existing exposition assertions updated only where a fake-zero was removed (documented).

### 3b exit criteria

`make ci` green; no `UnmarshalJSON` can panic on any of the fuzzed shapes; spec-validated field set is the basis for 3c; every emitter either produces a correct sample or none.

---

## 3c — `docs/metrics.md` catalog *(docs-only)*

- New `docs/metrics.md`: a table of every exported metric — name, Prometheus type, labels, and source Redfish resource — derived from the 3b-validated structs and verified against `--once --debug` exposition.
- Add it to the mkdocs nav.

### 3c exit criteria

`make ci` green; `docs/metrics.md` lists every metric `Describe`/`Collect` can emit; Pages builds; the catalog matches a live `--once` sample.

---

## Non-goals (Phase 3)

No new metric groups; no firmware-inventory metrics (#138, backlog); no snapshot/OTLP loop (Phase 4); no metric prefix or port change; no Redfish client codegen from the OpenAPI specs.

## Sequencing & dependencies

3a → 3b → 3c, strictly ordered. 3a (transport) is independent and behavior-preserving. 3b depends on nothing in 3a but must not be in flight simultaneously (both touch the collector). 3c depends on 3b's validated field set.

## Risks

- **resty behavior drift (3a)** — the session quirks (405 fallback, iLO 4 Location, basic-auth fallback) are subtle; mitigated by behavior-preserving tests and `--once --debug` sample diffing against `main`.
- **Retry masking real failures (3a)** — bounded to GET/HEAD, excludes 4xx, short backoff; a persistently-down BMC still fails fast after the bounded attempts.
- **Output changes from absent-not-zero (3b)** — dashboards relying on a fake `0` will see gaps; mitigated by documenting each removed-on-malformed metric and noting it in release notes.
- **Audit scale (3b)** — 4.7 MB of specs × ~15 resources; mitigated by per-resource fan-out at plan time and by treating the specs as a name/shape reference rather than a line-by-line diff.
