package main

import (
	"time"

	"github.com/fjacquet/idrac_exporter/internal/collector"
	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/fjacquet/idrac_exporter/internal/log"
	"github.com/fsnotify/fsnotify"
)

func ReloadConfig(filename string) {
	cfg := config.NewConfig()
	old := config.Config

	log.Info("Configuration reload was triggered")

	if len(filename) > 0 {
		err := cfg.FromFile(filename)
		if err != nil {
			log.Error("Failed to %v", err)
			return
		}
	}

	cfg.FromEnvironment()
	err := cfg.Validate()
	if err != nil {
		log.Error("Invalid configuration: %v", err)
		return
	}

	old.Mutex.Lock()
	defer old.Mutex.Unlock()

	old.Collect = cfg.Collect
	old.Event = cfg.Event

	for k, v := range cfg.Hosts {
		h, ok := old.Hosts[k]
		if ok {
			if h.Username != v.Username || h.Password != v.Password || h.Scheme != v.Scheme {
				old.Hosts[k] = v
				collector.Reset(k)
			}
		} else {
			old.Hosts[k] = v
		}
	}

	log.Info("Configuration reload was successful")
}

func WatchConfig(filename string) {
	lastReload := time.Now()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Error("Failed to start file watcher: %v", err)
		return
	}
	defer func() { _ = watcher.Close() }()

	err = watcher.Add(filename)
	if err != nil {
		log.Error("Failed to watch configuration file: %v", err)
		return
	}

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
}

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

func LoadConfig(filename string, watch bool) {
	cfg := config.NewConfig()

	if len(filename) > 0 {
		err := cfg.FromFile(filename)
		if err != nil {
			log.Fatal("Failed to %v", err)
		}
		log.Info("Loaded configuration file: %s", filename)
	}

	cfg.FromEnvironment()
	err := cfg.Validate()
	if err != nil {
		log.Fatal("Invalid configuration: %v", err)
	}

	config.SetConfig(cfg)

	if watch && len(filename) > 0 {
		go WatchConfig(filename)
	}
}
