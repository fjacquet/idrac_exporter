package collector

import (
	"testing"
	"time"

	"github.com/fjacquet/idrac_exporter/internal/config"
	dto "github.com/prometheus/client_model/go"
)

// upValueFor returns the idrac_up gauge value for a given identity-label value.
func upValueFor(t *testing.T, fams []*dto.MetricFamily, system string) (float64, bool) {
	t.Helper()
	for _, mf := range fams {
		if mf.GetName() != "idrac_up" {
			continue
		}
		for _, m := range mf.Metric {
			for _, l := range m.Label {
				if l.GetName() == "system" && l.GetValue() == system {
					return m.Gauge.GetValue(), true
				}
			}
		}
	}
	return 0, false
}

func systemHealthHasLabel(fams []*dto.MetricFamily, system string) bool {
	for _, mf := range fams {
		if mf.GetName() != "idrac_system_health" {
			continue
		}
		for _, m := range mf.Metric {
			for _, l := range m.Label {
				if l.GetName() == "system" && l.GetValue() == system {
					return true
				}
			}
		}
	}
	return false
}

func TestLoopCollectOnceDegradesPerHost(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Hosts["default"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "http"}
	cfg.Hosts["bmc1"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "http"}
	cfg.Hosts["bmc2"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "http"}
	cfg.Collect.System = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	config.SetConfig(cfg)

	good := mockRedfish(t, map[string]string{"/redfish/v1/Systems/1": "system.json"})
	defer good.Close()
	bad := mockRedfish(t, map[string]string{}) // 404s everything
	defer bad.Close()

	c1 := NewCollector()
	c1.client = testClient(good)
	c1.client.path.System = "/redfish/v1/Systems/1"
	c2 := NewCollector()
	c2.client = testClient(bad)
	c2.client.path.System = "/redfish/v1/Systems/1"

	mu.Lock()
	collectors["bmc1"] = c1
	collectors["bmc2"] = c2
	mu.Unlock()
	defer Reset("bmc1")
	defer Reset("bmc2")

	store := NewSnapshotStore()
	loop := NewLoop(store, time.Minute)
	loop.collectOnce()

	fams, err := store.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	if v, ok := upValueFor(t, fams, "bmc1"); !ok || v != 1 {
		t.Errorf("idrac_up{system=bmc1} = %v (found=%v), want 1", v, ok)
	}
	if v, ok := upValueFor(t, fams, "bmc2"); !ok || v != 0 {
		t.Errorf("idrac_up{system=bmc2} = %v (found=%v), want 0", v, ok)
	}
	if !systemHealthHasLabel(fams, "bmc1") {
		t.Errorf("idrac_system_health missing for healthy host bmc1")
	}
	if systemHealthHasLabel(fams, "bmc2") {
		t.Errorf("idrac_system_health present for failed host bmc2, want absent")
	}
}
