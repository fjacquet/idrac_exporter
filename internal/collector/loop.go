package collector

import (
	"context"
	"sync"
	"time"

	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/fjacquet/idrac_exporter/internal/log"
	dto "github.com/prometheus/client_model/go"
)

// Loop is the optional background collection loop. Each cycle it polls every
// configured host, builds an immutable Snapshot, and publishes it to the store
// for the OTLP exporter to read. The on-demand /metrics path is unaffected.
type Loop struct {
	store    *SnapshotStore
	interval time.Duration
}

func NewLoop(store *SnapshotStore, interval time.Duration) *Loop {
	return &Loop{store: store, interval: interval}
}

// Run collects once immediately (so the snapshot populates without waiting a
// full interval), then on every tick until ctx is cancelled.
func (l *Loop) Run(ctx context.Context) {
	l.collectOnce()
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.collectOnce()
		}
	}
}

// hostTargets returns the configured host keys excluding the "default"
// credentials fallback (which is not a real target).
func hostTargets() []string {
	config.Config.Mutex.Lock()
	defer config.Config.Mutex.Unlock()
	targets := make([]string, 0, len(config.Config.Hosts))
	for target := range config.Config.Hosts {
		if target == "default" {
			continue
		}
		targets = append(targets, target)
	}
	return targets
}

// collectAllHosts collects every configured host (minus the "default"
// credential fallback) concurrently and returns the merged, sorted Snapshot.
// names are the identity label keys injected per host (each set to the host
// value); coerceUntyped controls whether UNTYPED families are converted to
// GAUGE (true for OTLP, false for the pull exposition path).
func collectAllHosts(names []string, coerceUntyped bool) *Snapshot {
	config.Config.Mutex.Lock()
	concurrency := config.Config.Concurrency
	config.Config.Mutex.Unlock()

	targets := hostTargets()

	var accMu sync.Mutex
	perHost := make([][]*dto.MetricFamily, 0, len(targets))

	tasks := make([]func(), 0, len(targets))
	for _, target := range targets {
		target := target
		tasks = append(tasks, func() {
			fams := gatherTarget(target, names, coerceUntyped)
			accMu.Lock()
			perHost = append(perHost, fams)
			accMu.Unlock()
		})
	}
	runLimited(concurrency, tasks)

	return buildSnapshot(perHost)
}

func (l *Loop) collectOnce() {
	config.Config.Mutex.Lock()
	key := config.Config.OTLP.IdentityLabel
	config.Config.Mutex.Unlock()

	l.store.Store(collectAllHosts([]string{key}, true))
}

// gatherTarget collects one host and returns its families with every name in
// `names` applied as a label plus the <prefix>_up gauge. An unreachable host,
// a gather error, or a cycle that produced no real metric yields only up=0.
func gatherTarget(target string, names []string, coerceUntyped bool) []*dto.MetricFamily {
	collector, err := GetCollector(target, "")
	if err != nil {
		log.Error("snapshot: get collector for %s: %v", target, err)
		return []*dto.MetricFamily{upFamily(names, target, 0)}
	}
	families, err := collector.GatherFamilies()
	if err != nil {
		log.Error("snapshot: gather %s: %v", target, err)
		return []*dto.MetricFamily{upFamily(names, target, 0)}
	}
	// A nil error is not a freshness guarantee: coalesced waiters always return
	// the last cached families with err==nil even if the leader just failed (see
	// GatherFamilies). The hasRealMetric check below is the real gate.
	if !hasRealMetric(families) {
		return []*dto.MetricFamily{upFamily(names, target, 0)}
	}
	labeled := labelFamilies(families, names, target, coerceUntyped)
	return append(labeled, upFamily(names, target, 1))
}
