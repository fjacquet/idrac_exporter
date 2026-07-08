package collector

import (
	"strings"
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
)

// TestGatherAllLabelsAndUpPerHost drives GatherAll across a healthy and a down
// host using the pre-populated collectors-map seam (bypasses Redfish discovery),
// mirroring TestLoopCollectOnceDegradesPerHost.
func TestGatherAllLabelsAndUpPerHost(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.System = true })
	config.Config.Hosts["bmc1"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "http"}
	config.Config.Hosts["bmc2"] = &config.AuthConfig{Username: "u", Password: "p", Scheme: "http"}

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

	out, err := GatherAll()
	if err != nil {
		t.Fatalf("GatherAll: %v", err)
	}

	// Label order follows the []string{"instance","system"} construction order in upFamily/labelFamilies (expfmt preserves input order, it does not sort).
	if !strings.Contains(out, `idrac_up{instance="bmc1",system="bmc1"} 1`) {
		t.Errorf("missing up=1 for healthy bmc1:\n%s", out)
	}
	if !strings.Contains(out, `idrac_up{instance="bmc2",system="bmc2"} 0`) {
		t.Errorf("missing up=0 for down bmc2:\n%s", out)
	}
	// healthy host's real metrics carry both identity labels.
	if !strings.Contains(out, `instance="bmc1"`) || !strings.Contains(out, `system="bmc1"`) {
		t.Errorf("bmc1 identity labels missing:\n%s", out)
	}
}
