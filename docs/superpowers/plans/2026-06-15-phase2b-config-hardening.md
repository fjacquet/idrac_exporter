# Phase 2b — Config Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add `.env` loading, `passwordFile` secrets, a SIGHUP reload trigger, and editor-rename-safe file watching — without changing metric output.

**Architecture:** `.env` is loaded (godotenv, never overriding real env) before the config's `${VAR}` interpolation. `passwordFile` is read during `AuthConfig.Validate`. Reload is triggerable by `/reload`, SIGHUP, and a file watch that now also handles `fsnotify.Rename`.

**Tech Stack:** Go 1.26.4, `github.com/joho/godotenv`, `os/signal`+`syscall`, `fsnotify`.

**Branch:** `phase2b-config` (off `main`, already created). Design: Phase 2 spec §2b.

---

## File structure
- `internal/config/dotenv.go` — **create** `LoadDotEnv`.
- `internal/config/dotenv_test.go` — **create**.
- `internal/config/model.go` — **modify** add `PasswordFile` to `AuthConfig`.
- `internal/config/config.go` — **modify** `AuthConfig.Validate` reads `PasswordFile`.
- `internal/config/config_test.go` — **create** passwordFile test.
- `cmd/idrac_exporter/main.go` — **modify** call `config.LoadDotEnv` before `LoadConfig`; start SIGHUP handler.
- `cmd/idrac_exporter/signals.go` — **create** `handleSignals`.
- `cmd/idrac_exporter/config.go` — **modify** `WatchConfig` to handle `Rename` + bounded re-add; add `shouldReload`.
- `cmd/idrac_exporter/config_test.go` — **create** `shouldReload` test.

---

## Task 1: godotenv `.env` loading

**Files:** Create `internal/config/dotenv.go`, `internal/config/dotenv_test.go`; modify `cmd/idrac_exporter/main.go`.

- [ ] **Step 1: add dep** — Run `go get github.com/joho/godotenv@latest` then `go mod tidy` (keep it a direct dep).

- [ ] **Step 2: write the failing test** `internal/config/dotenv_test.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvDoesNotOverrideRealEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("IDRAC_2B_ONLY=fromfile\nIDRAC_2B_BOTH=fromfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IDRAC_2B_BOTH", "fromenv") // real env must win
	defer os.Unsetenv("IDRAC_2B_ONLY")

	LoadDotEnv(filepath.Join(dir, "idrac.yml")) // loads <dir>/.env

	if got := os.Getenv("IDRAC_2B_ONLY"); got != "fromfile" {
		t.Fatalf("IDRAC_2B_ONLY = %q, want fromfile", got)
	}
	if got := os.Getenv("IDRAC_2B_BOTH"); got != "fromenv" {
		t.Fatalf("IDRAC_2B_BOTH = %q, want fromenv (real env must not be overridden)", got)
	}
}
```

- [ ] **Step 3: run** `go test ./internal/config/ -run TestLoadDotEnv -v` — expect FAIL (`LoadDotEnv` undefined).

- [ ] **Step 4: implement** `internal/config/dotenv.go`:
```go
package config

import (
	"os"
	"path/filepath"

	"github.com/fjacquet/idrac_exporter/internal/log"
	"github.com/joho/godotenv"
)

// LoadDotEnv loads .env files BEFORE the config's ${VAR} interpolation so those
// references resolve. It checks the current directory first, then the config
// file's directory. godotenv.Load never overrides an already-set environment
// variable, so real secret injection always wins (.env is nice, env is the way).
func LoadDotEnv(cfgPath string) {
	candidates := []string{".env"}
	if cfgPath != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(cfgPath), ".env"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := godotenv.Load(p); err != nil {
			log.Warn("Failed to load %s: %v", p, err)
			continue
		}
		log.Info("Loaded environment from %s", p)
	}
}
```

- [ ] **Step 5: wire it in `cmd/idrac_exporter/main.go`** — in `run()`, immediately BEFORE `LoadConfig(flagConfig, flagWatch)`, add:
```go
	config.LoadDotEnv(flagConfig)
```

- [ ] **Step 6: run** `go test ./internal/config/ -run TestLoadDotEnv -v` — expect PASS.
- [ ] **Step 7: run** `make ci` — expect PASS.
- [ ] **Step 8: commit** `feat(2b): load .env at startup before config interpolation`.

---

## Task 2: `passwordFile`

**Files:** Modify `internal/config/model.go`, `internal/config/config.go`; create `internal/config/config_test.go`.

- [ ] **Step 1: write the failing test** `internal/config/config_test.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPasswordFilePopulatesPassword(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "pw")
	if err := os.WriteFile(secret, []byte("s3cr3t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &AuthConfig{Username: "u", PasswordFile: secret, Scheme: "https"}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if c.Password != "s3cr3t" {
		t.Fatalf("Password = %q, want s3cr3t (trimmed)", c.Password)
	}
}

func TestPasswordFileMissingErrors(t *testing.T) {
	c := &AuthConfig{Username: "u", PasswordFile: "/no/such/file", Scheme: "https"}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for unreadable password_file, got nil")
	}
}
```

- [ ] **Step 2: run** `go test ./internal/config/ -run TestPasswordFile -v` — expect FAIL (no `PasswordFile` field).

- [ ] **Step 3: add the field** in `internal/config/model.go` `AuthConfig` (after `Verify`):
```go
	PasswordFile string `yaml:"password_file"`
```

- [ ] **Step 4: read it in `internal/config/config.go` `AuthConfig.Validate`** — insert BEFORE the `if c.Password == ""` check:
```go
	if c.PasswordFile != "" {
		data, err := os.ReadFile(c.PasswordFile)
		if err != nil {
			return fmt.Errorf("read password_file: %v", err)
		}
		c.Password = strings.TrimSpace(string(data))
	}
```
(`os`, `strings`, `fmt` are already imported in `config.go`.)

- [ ] **Step 5: run** `go test ./internal/config/ -run TestPasswordFile -v` — expect PASS (both).
- [ ] **Step 6: run** `make ci` — expect PASS.
- [ ] **Step 7: commit** `feat(2b): support per-host password_file for secrets`.

---

## Task 3: reload triggers — SIGHUP + rename-safe watcher

**Files:** Create `cmd/idrac_exporter/signals.go`; modify `cmd/idrac_exporter/main.go`, `cmd/idrac_exporter/config.go`; create `cmd/idrac_exporter/config_test.go`.

- [ ] **Step 1: write the failing test** `cmd/idrac_exporter/config_test.go` (unit-tests the watcher's reload decision, the part that is pure):
```go
package main

import (
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestShouldReload(t *testing.T) {
	cases := []struct {
		op   fsnotify.Op
		want bool
	}{
		{fsnotify.Write, true},
		{fsnotify.Remove, true},
		{fsnotify.Rename, true},
		{fsnotify.Chmod, false},
	}
	for _, tc := range cases {
		if got := shouldReload(fsnotify.Event{Op: tc.op}); got != tc.want {
			t.Fatalf("shouldReload(%v) = %v, want %v", tc.op, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: run** `go test ./cmd/idrac_exporter/ -run TestShouldReload -v` — expect FAIL (`shouldReload` undefined).

- [ ] **Step 3: rewrite the watcher loop in `cmd/idrac_exporter/config.go`.** Add the helper functions and replace the `for { select … }` body of `WatchConfig` so the Remove branch also handles Rename with a bounded re-add. The new `WatchConfig` (keep the existing setup — `fsnotify.NewWatcher`, `defer watcher.Close()`, initial `watcher.Add`) and replace the event loop with:
```go
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if time.Since(lastReload) < time.Second {
				break // deduplicate bursts of write events
			}
			if !shouldReload(event) {
				break
			}
			// Editors save via rename/replace, which drops the watch; re-add it.
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				_ = watcher.Remove(event.Name)
				if !readd(watcher, filename) {
					log.Error("Stopped watching %s after repeated re-add failures", filename)
					return
				}
			}
			lastReload = time.Now()
			ReloadConfig(filename)
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Error("File watcher error: %v", err)
		}
	}
```
and add these two functions to the file:
```go
// shouldReload reports whether a watcher event warrants a config reload.
func shouldReload(event fsnotify.Event) bool {
	return event.Has(fsnotify.Write) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename)
}

// readd re-attaches the watch after a rename/remove, with a bounded retry. It
// does NOT recurse or spawn goroutines (unlike upstream PR #148).
func readd(watcher *fsnotify.Watcher, filename string) bool {
	for i := 0; i < 5; i++ {
		if err := watcher.Add(filename); err == nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
```

- [ ] **Step 4: run** `go test ./cmd/idrac_exporter/ -run TestShouldReload -v` — expect PASS.

- [ ] **Step 5: add the SIGHUP handler** — create `cmd/idrac_exporter/signals.go`:
```go
package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/fjacquet/idrac_exporter/internal/log"
)

// handleSignals reloads the configuration on SIGHUP for the lifetime of the
// process.
func handleSignals(cfgPath string) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)
	for range c {
		log.Info("Received SIGHUP, reloading configuration")
		ReloadConfig(cfgPath)
	}
}
```

- [ ] **Step 6: start it in `cmd/idrac_exporter/main.go`** — in `run()`, after `LoadConfig(...)` and the debug/trace/verbose block, before the `--once` check, add:
```go
	go handleSignals(flagConfig)
```

- [ ] **Step 7: build + smoke** — `go build ./...`; manual: `make run`, then in another shell `kill -HUP <pid>` → logs "Received SIGHUP, reloading configuration"; and `touch`/rename the config file → reload logged. (The watcher rename path and SIGHUP delivery are integration-verified manually; `shouldReload` is unit-tested.)

- [ ] **Step 8: run** `make ci` — expect PASS.
- [ ] **Step 9: commit** `feat(2b): SIGHUP reload + rename-safe config watcher`.

---

## Self-review notes
- **Spec coverage:** godotenv (T1), passwordFile (T2), SIGHUP + watcher Rename/clean-readd (T3) — all of Phase 2 spec §2b. The #148 smells (goroutine-leaking recursion, in-loop sleep on every event) are avoided: `readd` sleeps only on the rare failure path and never recurses.
- **Placeholder scan:** none — all code is complete.
- **Type consistency:** `shouldReload(fsnotify.Event) bool` and `readd(*fsnotify.Watcher, string) bool` are defined in `cmd/.../config.go` and used in `WatchConfig`; `handleSignals(string)` defined in `signals.go`, called in `main.go`; `LoadDotEnv(string)` defined in `internal/config/dotenv.go`, called in `main.go`.
- **Contract-neutral:** no metric/collector code touched; metric output unchanged.
- **Untestable-by-unit parts** (SIGHUP delivery, fsnotify timing) are manual-verified; the pure decision (`shouldReload`) and the secret/env handling (`passwordFile`, `LoadDotEnv`) are unit-tested.
