# Phase 3b — Payload Realignment + Absent-Not-Zero Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Apply the audit-confirmed payload realignment — never-panic hardening, the one metric-affecting field fix (`SensorNumber`), dead-field cleanup, and absent-not-zero guards on the fake-zero emitters.

**Architecture:** Driven by the committed audit ([findings](../specs/2026-06-15-phase3b-audit-findings.md)). Ordered **never-panic → field drift → absent-not-zero** so a zero-guard never masks a parse bug. Only the absent-not-zero step changes metric output (documented per ADR 0008); everything else is contract-neutral.

**Tech Stack:** Go 1.26.4, existing `internal/collector` httptest + `prometheus/testutil` harness.

**Branch:** `phase3b-payload` (off `main`, already created; findings doc already committed).

---

## Task 1: never-panic hardening

**Files:** `internal/collector/unmarshal.go`, `internal/collector/client.go`; tests in `internal/collector/unmarshal_test.go` (create).

- [ ] **Step 1 — failing test** `internal/collector/unmarshal_test.go` (package `collector`):

```go
package collector

import (
 "encoding/json"
 "testing"
)

func TestXstringEmptyArrayDoesNotPanic(t *testing.T) {
 var x xstring
 // An empty JSON array must not panic (issue: list[0] on empty slice).
 if err := json.Unmarshal([]byte(`[]`), &x); err != nil {
  t.Fatalf("unmarshal []: %v", err)
 }
 if x.String() != "" {
  t.Fatalf("xstring = %q, want empty", x.String())
 }
}

func TestXstringMemberArray(t *testing.T) {
 var x xstring
 if err := json.Unmarshal([]byte(`[{"Member":"v1"}]`), &x); err != nil {
  t.Fatalf("unmarshal: %v", err)
 }
 if x.String() != "v1" {
  t.Fatalf("xstring = %q, want v1", x.String())
 }
}
```

- [ ] **Step 2 — run** `go test ./internal/collector/ -run TestXstring -v` → `TestXstringEmptyArrayDoesNotPanic` panics/fails (index out of range at `unmarshal.go:60`).
- [ ] **Step 3 — fix** `internal/collector/unmarshal.go`: immediately after `list, ok := x.([]any)` / its `if !ok { return nil }`, before `dict, ok := list[0].(map[string]any)`, add:

```go
 if len(list) == 0 {
  return nil
 }
```

- [ ] **Step 4 — run** `go test ./internal/collector/ -run TestXstring -v` → PASS (both).
- [ ] **Step 5 — `client.go` Members[0] guards.** At `internal/collector/client.go:108`, `:117`, `:173` (the `group.Members[0].OdataId` accesses for Systems/Chassis/Managers — verify current line numbers), guard each block with a length check so an empty `Members` collection is skipped rather than panicking, e.g.:

```go
 if len(group.Members) > 0 {
  client.path.System = group.Members[0].OdataId
 }
```

Apply the equivalent guard at each of the three sites (System, Chassis, Manager). Keep behavior identical when `Members` is non-empty.

- [ ] **Step 6 — run** `make ci` → PASS. **Commit** `fix(3b): never-panic guards for empty xstring array and empty Members`.

---

## Task 2: SensorNumber field fix (the one metric-affecting realignment)

**Files:** `internal/collector/model.go`; test in `internal/collector/model_test.go` (create).

- [ ] **Step 1 — failing test** `internal/collector/model_test.go` (package `collector`):

```go
package collector

import (
 "encoding/json"
 "testing"
)

func TestTemperatureSensorNumberId(t *testing.T) {
 // Spec property is "SensorNumber"; GetId must use it as the fallback when
 // MemberId is absent (was json:"Number", which never parsed).
 var temp Temperature
 if err := json.Unmarshal([]byte(`{"SensorNumber":7,"ReadingCelsius":40}`), &temp); err != nil {
  t.Fatalf("unmarshal: %v", err)
 }
 if got := temp.GetId(99); got != "7" {
  t.Fatalf("GetId = %q, want \"7\" (SensorNumber, not array fallback)", got)
 }
}
```

- [ ] **Step 2 — run** `go test ./internal/collector/ -run TestTemperatureSensorNumberId -v` → FAIL (GetId returns "99" because `Number` never parsed from `SensorNumber`).
- [ ] **Step 3 — fix** in `internal/collector/model.go` `Temperature` struct: rename the field and tag

```go
 SensorNumber        int     `json:"SensorNumber"`
```

(replacing `Number int`json:"Number"``), and update `GetId` to use `t.SensorNumber` (`if t.SensorNumber > 0 { return strconv.Itoa(t.SensorNumber) }`). Grep for any other `.Number` reference on a `Temperature` value and update it.

- [ ] **Step 4 — run** `go test ./internal/collector/ -run TestTemperature -v` → PASS.
- [ ] **Step 5 — run** `make ci` → PASS. **Commit** `fix(3b): Temperature.SensorNumber json tag (was Number) — fixes label drift`.

---

## Task 3: dead-field cleanup (audit B1–B4, contract-neutral)

**Files:** `internal/collector/model.go`. No new test (these fields are read by no emitter; the guard is `make ci` + existing tests staying green).

Per [findings §B](../specs/2026-06-15-phase3b-audit-findings.md):

- [ ] **Step 1 — B1:** remove `PowerWatts` from `ThermalMetrics`.
- [ ] **Step 2 — B2:** remove `ChassisPowerConsumption` and `NodePowerConsumption` from `PowerControlUnit`.
- [ ] **Step 3 — B3:** in `Processor`, rename `TDPWatts` → `MaxTDPWatts` with `json:"MaxTDPWatts"`.
- [ ] **Step 4 — B4:** remove the spurious `MaxLevels` field from the `DeepOperations` struct inside `V1Response.ProtocolFeaturesSupported` (leave `ExpandQuery.MaxLevels` untouched).
- [ ] **Step 5 — verify** each removed/renamed field is referenced by no other code: `grep -rn 'PowerWatts\|ChassisPowerConsumption\|NodePowerConsumption\|TDPWatts' internal/collector/*.go` and confirm the only hits are the definitions you changed (and the legitimately-different `PowerDistributionMetrics.PowerWatts` / `Oem.TsFujitsu.ChassisPowerConsumption`, which must NOT be touched). **B5/B6 (Redundancy `*int`/`RedundancyEnabled`) are intentionally NOT done** (deferred, unread fields).
- [ ] **Step 6 — run** `make ci` → PASS (build + all existing tests green, proving nothing read these). **Commit** `refactor(3b): drop phantom/spec-absent struct fields per swagger audit`.

---

## Task 4: absent-not-zero guards (documented output change)

**Files:** `internal/collector/metrics.go`; tests in `internal/collector/absent_test.go` (create).

The ~13 fake-zero emitters are listed in [findings §D](../specs/2026-06-15-phase3b-audit-findings.md). Each currently emits unconditionally; add the established guard so an absent/zero source field yields **no sample**. The pattern is the existing `NewPowerSupplyEfficiencyPercent`:

```go
 if value == 0 {
  return
 }
```

For emitters whose value is computed internally (e.g. `NewNetworkPortCurrentSpeed` with its fallback chain), guard the **final** computed value the same way (skip emit when it resolves to 0).

- [ ] **Step 1 — failing tests** `internal/collector/absent_test.go` (package `collector`). Use `prometheus/testutil` to assert a 0-value emit produces NO sample and a non-zero one does. Representative coverage (one per family is enough — the change is uniform):

```go
package collector

import (
 "strings"
 "testing"

 "github.com/prometheus/client_golang/prometheus/testutil"
)

func TestAbsentNotZero_StorageDriveCapacity(t *testing.T) {
 testConfig(t, func(c *config.CollectConfig) {})
 mc := NewCollector()
 // value 0 → no sample
 if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
  mc.NewStorageDriveCapacity(ch, 0, "drive-0")
 })); n != 0 {
  t.Fatalf("0-value emitted %d samples, want 0", n)
 }
 // value > 0 → one sample
 if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
  mc.NewStorageDriveCapacity(ch, 1024, "drive-0")
 })); n != 1 {
  t.Fatalf("nonzero emitted %d samples, want 1", n)
 }
}
```

> **Note for implementer:** add the small `funcCollector` adapter (a `prometheus.Collector` whose `Collect` runs the closure and `Describe` is a no-op) and the needed imports (`config`, `prometheus`). Write **one such paired test per fake-zero emitter family** (power-supply, power-control, storage capacity, network speed) — not necessarily all 13, but cover each distinct emitter signature. If a clean unit harness for a given emitter is impractical, note it and rely on the guard + `make ci`.

- [ ] **Step 2 — run** the new tests → FAIL (0-value currently emits a sample).
- [ ] **Step 3 — add the guard** to each of the ~13 emitters in [findings §D](../specs/2026-06-15-phase3b-audit-findings.md), matching `NewPowerSupplyEfficiencyPercent`. Do NOT touch the already-guarded emitters.
- [ ] **Step 4 — run** the new tests → PASS.
- [ ] **Step 5 — guard against masking the SensorNumber-style bug:** confirm no guarded emitter's source field was itself a parse bug (the audit cleared this — only `SensorNumber`, already fixed in Task 2, and it is not one of these emitters).
- [ ] **Step 6 — run** `make ci` and `go test ./internal/collector/ -race` → PASS. Confirm `TestRefreshSystem` still matches (its sample values are non-zero, so guards don't change them). **Commit** `feat(3b): absent-not-zero — omit fake-zero samples for absent fields`.

---

## Self-review notes

- **Spec coverage (§3b):** never-panic (Task 1, findings §C), field drift (Task 2 = the 1 metric-affecting §A1; Task 3 = §B1–B4), absent-not-zero (Task 4, §D, zero-as-absent guards). The exhaustive struct audit itself is **done** (committed findings doc) — this plan applies its confirmed output.
- **Deferred (recorded):** Redundancy `*int`/`RedundancyEnabled` (§B5/B6) — unread, YAGNI.
- **Contract:** Tasks 1–3 contract-neutral; Task 4 intentionally omits fake-zero samples (the one documented output change). `TestRefreshSystem`'s exposition is unaffected (its values are non-zero).
- **Placeholder scan:** target lists are in the committed findings doc (not placeholders — concrete, line-referenced); the absent-not-zero guard is one uniform pattern shown in full.
