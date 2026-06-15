# Phase 3b — Payload Audit Findings (model.go ↔ docs/swagger)

**Status:** Reference · 2026-06-15
**Parent:** [Phase 3 design](2026-06-15-phase3-payload-resty-design.md)
**Source:** Multi-agent fan-out audit (14 resource families, one auditor + adversarial verifier each; ~30 agents, Sonnet). Read-only comparison of every `internal/collector/model.go` struct against `docs/swagger/11017-1.30.xx.json` (Dell iDRAC 10) and `docs/swagger/openapi-7.xx.yaml` (DMTF Redfish). Plus a prior read-only Explore pass for panic/absent-not-zero surface.

> This file records a costly audit so the work survives context resets. **8 confirmed struct-vs-spec divergences** (only **1** affects emitted metrics) + the never-panic and absent-not-zero surface. It is the input to the Phase 3b implementation plan.

---

## A. Metric-affecting field drift (must fix)

### A1 — `Temperature.Number` → `SensorNumber` · severity medium · affectsMetric ✅
- **File:** `internal/collector/model.go` (`Temperature` struct, field `Number int` `json:"Number"`, ~line 271).
- **Spec:** Both swagger files (DMTF `Thermal.v1_7_3.Temperature` examples; Dell Thermal example) name the property **`SensorNumber`**, not `Number`. The sibling `Voltages` struct already uses `SensorNumber int` `json:"SensorNumber"` — the intended pattern.
- **Impact:** The wrong tag means `Number` never unmarshals (stays `0`). `Temperature.GetId()` (model.go ~281-289) uses `t.Number` as the fallback when `MemberId` is empty (consumed at `client.go:324` `t.GetId(n)`). So on BMCs that omit `MemberId` (older firmware), temperature metric label IDs fall back to the **array index** instead of the real sensor number → silent label drift across scrapes if array order changes.
- **Fix:** rename the Go field `Number`→`SensorNumber` and tag `json:"SensorNumber"`; update `GetId()` and any reference. Test: a Temperature payload with `SensorNumber` populates the label id.

---

## B. Correctness cleanup — dead / phantom / nullable fields (affectsMetric ❌, low severity)

All verified as spec-divergent but read by **no** emitter, so zero current metric impact. Removing/correcting them realigns the structs with the spec.

| # | Struct.Field (model.go) | Issue | Spec evidence | Proposed fix |
|---|---|---|---|---|
| B1 | `ThermalMetrics.PowerWatts` (~317) | phantom — not on `ThermalMetrics` in either spec (belongs to EnvironmentMetrics/PDU) | DMTF `ThermalMetrics.v1_3_2` + Dell example show only `TemperatureReadingsCelsius`/`TemperatureSummaryCelsius` | remove field |
| B2 | `PowerControlUnit.ChassisPowerConsumption` + `NodePowerConsumption` (~643-644) | spec-absent under `PowerControl` array item | neither spec defines them under PowerControl; the `client.go:587` reference is `Oem.TsFujitsu.*`, a different struct | remove both fields |
| B3 | `Processor.TDPWatts` (~151) | wrong tag — spec is `MaxTDPWatts` | Dell `Processor.v1_20_1` example shows `MaxTDPWatts` | retag `json:"MaxTDPWatts"` (rename field `MaxTDPWatts`) |
| B4 | `V1Response …ProtocolFeaturesSupported.DeepOperations.MaxLevels` (~105) | wrong shape — `MaxLevels` belongs under `ExpandQuery` (already captured), not `DeepOperations` (only `DeepPATCH`/`DeepPOST`) | both specs: `DeepOperations` has only `DeepPATCH`,`DeepPOST` | remove the spurious `MaxLevels` from `DeepOperations` |
| B5 | `Redundancy.MaxNumSupported` / `MinNumNeeded` (~58-59) | nullable in spec; Go `int` coerces `null`→`0` | both specs show these as `null` in some examples | `int`→`*int` **(DEFERRED — YAGNI: unread fields)** |
| B6 | `Redundancy.RedundancyEnabled` (~61) | dropped in Dell `Redundancy.v1_5_0` (present only in DMTF `v1_4_2`) | absent from all Dell v1_5_0 examples | leave or drop **(DEFERRED — unread)** |

**Decision:** B1–B4 are in the Phase 3b plan (clean, safe spec realignment). **B5/B6 are deferred** — unread fields, zero metric impact; adding unused `*int` pointers is over-engineering. Recorded here so the decision isn't re-litigated.

---

## C. Never-panic surface (from the Explore pass)

ADR 0008: custom `UnmarshalJSON` must never panic.

- **C1 (real bug):** `internal/collector/unmarshal.go:60` — `dict, ok := list[0].(map[string]any)` indexes `list[0]` without checking `len(list) > 0`. An empty JSON array `[]` for an `xstring` field panics. **Fix:** guard `if len(list) == 0 { return nil }`.
- **C2 (defensive):** `internal/collector/client.go:108`, `:117`, `:173` — `group.Members[0]` accessed without a length guard (the `Systems`/`Chassis`/`Managers` collection first member). The Phase 2c `recover()` catches a panic here, but explicit `if len(group.Members) > 0` guards are cleaner and avoid counting a spurious scrape error.

---

## D. Absent-not-zero targets (from the Explore pass)

Decision: **zero-as-absent guards** (matching the ~11 emitters that already do `if value == 0 { return }` / nil-pointer guards). The ~13 fake-zero emitters in `internal/collector/metrics.go`:

- Power/energy: `NewPowerSupplyInputWatts` (~200), `NewPowerSupplyInputVoltage` (~209), `NewPowerSupplyOutputWatts` (~218), `NewPowerSupplyCapacityWatts` (~227), `NewPowerControlConsumedWatts` (~248), `NewPowerControlCapacityWatts` (~258), `NewPowerControlMinConsumedWatts` (~268), `NewPowerControlMaxConsumedWatts` (~278), `NewPowerControlAvgConsumedWatts` (~288), `NewPowerControlInterval` (~298)
- Storage capacity: `NewStorageDriveCapacity` (~383), `NewStorageVolumeCapacity` (~546)
- Network: `NewNetworkPortCurrentSpeed` (~651)

Already-guarded (follow their pattern): `NewPowerSupplyEfficiencyPercent`, `NewStorageControllerSpeed`, `NewStorageControllerCacheSize`, `NewMemoryModuleCapacity`, `NewMemoryModuleSpeed`, `NewNetworkPortMaxSpeed`, `NewCpuMaxSpeed`, `NewStorageVolumeMediaSpan`, `NewSystemMemorySize`, `NewStorageDriveLifeLeft`, `NewPowerSupplyHealth`.

> **Output change (documented):** these guards omit a sample when the source field is `0`/absent instead of emitting a misleading `0`. Per ADR 0008. Line numbers are approximate — verify against the file before editing.

---

## E. Reassurance

38 structs audited; 1 metric-affecting issue, 7 cosmetic. The hand-written model layer is substantially spec-accurate — the realignment is small and targeted, not a rewrite.
