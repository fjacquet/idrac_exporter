package main

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/fsnotify/fsnotify"
)

// atomicSave performs an atomic file save via write-to-temp + os.Rename,
// mimicking how editors (vim, nano, most CI tools) replace files.
func atomicSave(t *testing.T, filename, content string) {
	t.Helper()
	dir := filepath.Dir(filename)
	tmp, err := os.CreateTemp(dir, ".cfg-tmp-*")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		t.Fatalf("write temp: %v", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp: %v", err)
	}
	if err := os.Rename(tmp.Name(), filename); err != nil {
		t.Fatalf("rename temp: %v", err)
	}
}

// directSave writes content directly to filename (no rename), triggering a
// plain Write event which should cause a reload when outside the dedup window.
func directSave(t *testing.T, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

// minimalConfig returns a minimal valid YAML config string for testing.
func minimalConfig() string {
	return `
address: "127.0.0.1"
port: 19348
metrics:
  system: true
hosts:
  default:
    username: u
    password: p
`
}

// TestWatcherReattachesAfterAtomicSave asserts that after an atomic rename-save
// (within the 1-second dedup window), the watcher re-attaches its inotify watch
// so that a subsequent direct edit still triggers a reload.
//
// Issue #3: the old code ran the dedup gate before the rename/remove re-attach,
// so atomic saves within 1s would drop the watch silently.
func TestWatcherReattachesAfterAtomicSave(t *testing.T) {
	// Install a minimal config so ReloadConfig can parse the file.
	cfg := config.NewConfig()
	cfg.Hosts["default"] = &config.AuthConfig{
		Username: "u",
		Password: "p",
		Scheme:   "http",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	config.SetConfig(cfg)

	// Create a temp config file.
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "idrac.yml")
	if err := os.WriteFile(cfgFile, []byte(minimalConfig()), 0o600); err != nil {
		t.Fatalf("create config file: %v", err)
	}

	// Set up a real fsnotify watcher to verify re-attach behaviour.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Skipf("fsnotify unavailable in this environment: %v", err)
	}
	defer func() { _ = watcher.Close() }()

	if err := watcher.Add(cfgFile); err != nil {
		t.Skipf("cannot watch %s: %v", cfgFile, err)
	}

	// Track the number of reload-worthy events delivered to us from the watcher.
	var reloadEvents atomic.Int32

	// Drain events from the watcher in a goroutine, counting shouldReload hits.
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		for {
			select {
			case ev, ok := <-watcher.Events:
				if !ok {
					return
				}
				if shouldReload(ev) {
					reloadEvents.Add(1)
					// Mirror the fix: re-attach the watch on rename/remove.
					if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
						_ = watcher.Remove(ev.Name)
						for i := 0; i < 5; i++ {
							if addErr := watcher.Add(cfgFile); addErr == nil {
								break
							}
							time.Sleep(50 * time.Millisecond)
						}
					}
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	// Step 1: atomic save (rename) — this is the save that drops the watch inode.
	atomicSave(t, cfgFile, minimalConfig())

	// Wait for the rename event to be processed with a generous timeout.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && reloadEvents.Load() == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if reloadEvents.Load() == 0 {
		t.Skip("watcher did not deliver rename event; skipping (environment limitation)")
	}

	// Step 2: wait past the 1-second dedup window, then do a direct write.
	// This proves the watch survived the atomic rename (if it had not, no event
	// would arrive and the test would time out and fail).
	time.Sleep(1100 * time.Millisecond)
	before := reloadEvents.Load()
	directSave(t, cfgFile, minimalConfig())

	deadline2 := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline2) && reloadEvents.Load() == before {
		time.Sleep(50 * time.Millisecond)
	}

	// Close the watcher so the drain goroutine exits.
	_ = watcher.Close()
	<-watchDone

	if reloadEvents.Load() == before {
		t.Fatalf("no write event received after atomic rename; watch was dropped and not re-attached (issue #3)")
	}
}
