# Config Reload Concurrency Hardening (Design)

**Status:** Draft · 2026-06-16
**Parent:** [program overview](2026-06-15-idrac-family-recovery-overview-design.md)
**Closes:** [#4](https://github.com/fjacquet/idrac_exporter/issues/4) (data race), [#3](https://github.com/fjacquet/idrac_exporter/issues/3) (watcher re-add)
**Scope:** Two pre-existing config-reload correctness bugs flagged by the Phase 2b review (PR #2) and tracked as issues. No public-contract change; no new metrics. One branch + PR; `make ci` (incl. `-race`) is the gate.

## Context

Phase 2 wired up multiple concurrent config-reload triggers (SIGHUP goroutine, `--config-watch` file watcher, `/reload` endpoint), all mutating the global `config.Config` singleton under `config.Config.Mutex`. Two correctness gaps remain on the **reader** and **watcher** sides:

- **#4 — reader-writer data race.** The collector reads `config.Config.Collect` and `config.Config.Event` **without** holding `Config.Mutex`, while reloads mutate them under it. A scrape running concurrently with a reload reads torn/stale state (detectable under `go test -race`). Read sites: `internal/collector/collector.go` (`collect := &config.Config.Collect`) and ~6 sites in `internal/collector/client.go` (Collect flags + Event severity/maxage).
- **#3 — watcher silently drops the watch.** In `cmd/idrac_exporter/config.go`, the 1-second dedup gate runs **before** the rename/remove re-add. Editors save atomically (write → rename temp over target); when the `Rename`/`Remove` arrives within 1s of a prior reload, the `break` skips the re-add. The kernel has already dropped the inotify watch on the renamed inode, so future edits stop triggering reloads — silently.

Both are pre-existing (present at the merge base); Phase 2b correctly scoped around them and filed the trackers.

## Settled decisions

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | **Fix both in one PR.** | Same theme (config access/reload correctness), both small, both from the same review. |
| D2 | **#4 = per-gather snapshot, not per-site locking.** Add `config.Snapshot()` (copies `Collect` + `Event` under `Config.Mutex`); capture it **once** per gather and read through the copy. | One lock acquisition per scrape; the whole scrape sees one consistent config. Avoids per-read lock churn and the chance of two sites observing different snapshots mid-scrape. Matches the issue's suggested fix. |
| D3 | **Snapshot is stashed on the `Client`** (`client.cfg`), set at the top of `Collector.Collect()`. | The `Client` is per-target and reused; concurrent scrapes of the same target already coalesce via `sync.Cond`, so exactly one gather writes `client.cfg` at a time. No method-signature churn through `client.go`. |
| D4 | **#3 = re-add always; dedup only the reload.** Reorder the watcher event handler so the rename/remove re-attach runs regardless of the 1s gate; the gate suppresses only the redundant `ReloadConfig` call. | The watch re-attach is not a "burst"; it must never be deduplicated away. |

## Work items

### 1. `config.Snapshot()` (`internal/config/`)

- Add an exported value type, e.g.:
  ```go
  type Snapshot struct {
      Collect CollectConfig
      Event   EventConfig
  }
  ```
  `CollectConfig` (all bools) and `EventConfig` (`Severity`, `MaxAge` strings + `SeverityLevel int`, `MaxAgeSeconds float64`) contain no maps/slices/pointers, so a struct copy is a safe deep copy.
- Add `func Snapshot() Snapshot` that locks `Config.Mutex`, copies the two fields, unlocks, and returns the value.

### 2. Read through the snapshot (`internal/collector/`)

- Add a field to `Client`: `cfg config.Snapshot`.
- At the top of `Collector.Collect()` (before the fan-out / `RefreshPDUs`), set `collector.client.cfg = config.Snapshot()`.
- Repoint every read site off the global:
  - `collector.go`: `collect := &collector.client.cfg.Collect`.
  - `client.go`: `c.cfg.Collect.*` and `c.cfg.Event.*` at the ~6 sites.
- After this change, no `internal/collector` code reads `config.Config.Collect` / `config.Config.Event` directly. (Other `config.Config` reads — hosts, OTLP, concurrency — are out of scope here; the loop's reads were already mutex-guarded.)

### 3. Watcher reorder (`cmd/idrac_exporter/config.go`)

Reorder the `watcher.Events` handler so the re-add precedes the dedup gate:
```go
if !shouldReload(event) {
    break
}
// Always re-attach the watch on rename/remove — atomic saves swap the inode,
// and the kernel drops the watch on the old one. This must not be deduplicated.
if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
    _ = watcher.Remove(event.Name)
    if !readd(watcher, filename) {
        // existing failure handling
    }
}
if time.Since(lastReload) < time.Second {
    break // dedup only the reload itself
}
lastReload = time.Now()
ReloadConfig(filename)
```

## Testing

- **#4:** a `-race` test using the existing `httptest` Redfish mock harness (`testhelpers_test.go`) that runs a gather while a goroutine concurrently mutates `config.Config.Collect`/`.Event` under `Config.Mutex`. Must be clean under `go test -race` (the CI gate). Asserts the gather still succeeds.
- **#3:** a temp-file + real `fsnotify` integration test: start the watcher on a temp config, perform an atomic save (write temp → rename over target) within the 1s window, then make a later edit and assert a reload still fires (the watch survived). Use generous, tolerance-based timing to avoid flakiness; skip if the watcher cannot attach in the test environment.

## Exit criteria

- `make ci` green, including `go test -race ./...`.
- No `internal/collector` code reads `config.Config.Collect`/`.Event` outside `config.Snapshot()`.
- The watcher re-attaches its watch across an atomic save within the dedup window, and subsequent edits still trigger reloads.
- Issues #3 and #4 closed by the merged PR.

## Non-goals

- No change to the public metric contract, endpoints, or config schema.
- No broader audit of every `config.Config` access (only the `Collect`/`Event` race in #4 and the watcher in #3). Other reads are already guarded or out of scope.
- No change to the reload triggers themselves (SIGHUP / `/reload` / watcher remain as-is, just corrected).
