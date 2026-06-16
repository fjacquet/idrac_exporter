package collector

import (
	"sync"
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
)

// TestCollectConfigSnapshotNoRace runs a gather while a goroutine concurrently
// mutates config.Config.Collect and config.Config.Event under Config.Mutex.
// It must be clean under go test -race (the CI gate) and the gather must
// succeed (fixing issue #4).
func TestCollectConfigSnapshotNoRace(t *testing.T) {
	// Minimal config: enable System + Events so both Collect and Event fields
	// are read during the gather, maximising the race-detector coverage.
	testConfig(t, func(c *config.CollectConfig) {
		c.System = true
		c.Events = true
	})

	srv := mockRedfish(t, map[string]string{
		"/redfish/v1/Systems/1": "system.json",
	})
	defer srv.Close()

	mc := NewCollector()
	mc.client = testClient(srv)
	mc.client.path.System = "/redfish/v1/Systems/1"

	var wg sync.WaitGroup

	// Mutator goroutine: repeatedly write Collect/Event under the mutex,
	// simulating a concurrent config reload.
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				config.Config.Mutex.Lock()
				config.Config.Collect.System = true
				config.Config.Collect.Events = true
				config.Config.Event.SeverityLevel = 1
				config.Config.Event.MaxAgeSeconds = 604800
				config.Config.Mutex.Unlock()
			}
		}
	}()

	// Run several gathers while the mutator is running.
	for i := 0; i < 5; i++ {
		_, err := mc.Gather()
		if err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("Gather() returned error: %v", err)
		}
	}

	close(stop)
	wg.Wait()
}
