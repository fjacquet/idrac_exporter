# Config hot reload (SIGHUP + file watch)

## Status

Accepted — extended in Phase 2.

## Context

The exporter already supports reload via an HTTP `/reload` endpoint and an optional
`fsnotify` file watcher (`-config-watch`). The family standard is a thread-safe
rebuild-and-swap reload triggered by **both SIGHUP and a file watch**. The current watcher
also misses atomic-rename-on-save (editors write a temp file then rename), which upstream
PR #148 set out to fix.

## Decision

Keep the rebuild-and-swap reload. Add a `SIGHUP` handler alongside the file watch. Handle
`fsnotify.Rename` in addition to `Write`/`Remove`, and make watcher re-add resilient — but
reimplement cleanly (no goroutine-leaking `go WatchConfig()` recursion, no in-loop
`time.Sleep` blocking events) rather than copying PR #148 verbatim.

## Consequences

Reload works from a signal or a file change, including editor atomic saves. Credentials
that change are swapped and the affected target's collector is reset, as today.
