package collector

import (
	"strings"
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRefreshSystem(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.System = true })
	srv := mockRedfish(t, map[string]string{"/redfish/v1/Systems/1": "system.json"})
	defer srv.Close()

	mc := NewCollector()
	mc.client = testClient(srv)
	mc.client.path.System = "/redfish/v1/Systems/1"

	const want = `
# HELP idrac_system_health Health status of the system
# TYPE idrac_system_health gauge
idrac_system_health{status="OK"} 0
`
	if err := testutil.CollectAndCompare(mc, strings.NewReader(want), "idrac_system_health"); err != nil {
		t.Fatalf("unexpected metrics: %v", err)
	}
}
