package collector

import (
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
)

// TestRunLimitedBoundsConcurrency asserts SetLimit(n) is honoured: with 8 tasks
// and a limit of 2, no more than 2 run at once.
func TestRunLimitedBoundsConcurrency(t *testing.T) {
	const limit = 2
	const n = 8
	var inFlight, maxSeen atomic.Int32
	release := make(chan struct{})

	tasks := make([]func(), 0, n)
	for i := 0; i < n; i++ {
		tasks = append(tasks, func() {
			cur := inFlight.Add(1)
			for {
				old := maxSeen.Load()
				if cur <= old || maxSeen.CompareAndSwap(old, cur) {
					break
				}
			}
			<-release // hold the slot so concurrency can be observed
			inFlight.Add(-1)
		})
	}

	done := make(chan struct{})
	go func() {
		runLimited(limit, tasks)
		close(done)
	}()

	// Wait until the limit is saturated, then drain everything.
	for inFlight.Load() < limit {
		runtime.Gosched()
	}
	close(release)
	<-done

	if got := maxSeen.Load(); got != limit {
		t.Fatalf("max concurrency = %d, want %d", got, limit)
	}
}

// TestRunLimitedUnlimitedRunsAllTasks asserts limit 0 runs every task.
func TestRunLimitedUnlimitedRunsAllTasks(t *testing.T) {
	var count atomic.Int32
	tasks := make([]func(), 0, 5)
	for i := 0; i < 5; i++ {
		tasks = append(tasks, func() { count.Add(1) })
	}
	runLimited(0, tasks)
	if got := count.Load(); got != 5 {
		t.Fatalf("ran %d tasks, want 5", got)
	}
}

// TestRefreshRecoversPanic asserts a panicking refresh counts one error and does
// not propagate (which would crash the process / hang the errgroup).
func TestRefreshRecoversPanic(t *testing.T) {
	var c Collector
	c.refresh("boom", func() bool { panic("kaboom") })
	if got := c.errors.Load(); got != 1 {
		t.Fatalf("errors = %d, want 1 after panic", got)
	}
}

// TestRefreshCountsFailure asserts a refresh returning false counts one error.
func TestRefreshCountsFailure(t *testing.T) {
	var c Collector
	c.refresh("fail", func() bool { return false })
	if got := c.errors.Load(); got != 1 {
		t.Fatalf("errors = %d, want 1 after false", got)
	}
}

// TestRefreshSuccessNoError asserts a successful refresh counts no error.
func TestRefreshSuccessNoError(t *testing.T) {
	var c Collector
	c.refresh("ok", func() bool { return true })
	if got := c.errors.Load(); got != 0 {
		t.Fatalf("errors = %d, want 0 on success", got)
	}
}

// TestDescribeIsUnchecked asserts the collector is unchecked: Describe sends no
// descriptors (the metric name set is dynamic). Metric output is unaffected.
func TestDescribeIsUnchecked(t *testing.T) {
	// Initialize config so NewCollector can succeed.
	testConfig(t, func(c *config.CollectConfig) { c.System = true })

	mc := NewCollector()
	ch := make(chan *prometheus.Desc)
	go func() {
		mc.Describe(ch)
		close(ch)
	}()
	count := 0
	for range ch {
		count++
	}
	if count != 0 {
		t.Fatalf("Describe emitted %d descriptors, want 0 (unchecked collector)", count)
	}
}
