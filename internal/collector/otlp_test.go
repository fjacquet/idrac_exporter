package collector

import (
	"context"
	"testing"
	"time"

	"github.com/fjacquet/idrac_exporter/internal/config"
	dto "github.com/prometheus/client_model/go"
	prombridge "go.opentelemetry.io/contrib/bridges/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestSnapshotDualExport asserts the snapshot is readable through BOTH the
// Prometheus gatherer and an OTLP ManualReader via the bridge — the family
// "assert via both paths" requirement.
func TestSnapshotDualExport(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.System = true })
	srv := mockRedfish(t, map[string]string{"/redfish/v1/Systems/1": "system.json"})
	defer srv.Close()

	mc := NewCollector()
	mc.client = testClient(srv)
	mc.client.path.System = "/redfish/v1/Systems/1"
	fams, err := mc.GatherFamilies()
	if err != nil {
		t.Fatalf("gather families: %v", err)
	}

	store := NewSnapshotStore()
	labeled := labelFamilies(fams, "system", "bmc1")
	host := append(labeled, upFamily("system", "bmc1", 1))
	store.Store(buildSnapshot([][]*dto.MetricFamily{host}))

	// (a) Prometheus gatherer path.
	if !systemHealthHasLabel(mustGather(t, store), "bmc1") {
		t.Fatalf("registry path: idrac_system_health{system=bmc1} missing")
	}

	// (b) OTLP ManualReader path through the bridge.
	producer := prombridge.NewMetricProducer(prombridge.WithGatherer(store))
	reader := metric.NewManualReader(metric.WithProducer(producer))
	_ = metric.NewMeterProvider(metric.WithReader(reader))

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("manual reader collect: %v", err)
	}
	if !otlpHasMetric(rm, "idrac_system_health") {
		t.Fatalf("OTLP path: idrac_system_health missing")
	}
	if !otlpHasMetric(rm, "idrac_up") {
		t.Fatalf("OTLP path: idrac_up missing")
	}
}

func mustGather(t *testing.T, store *SnapshotStore) []*dto.MetricFamily {
	t.Helper()
	fams, err := store.Gather()
	if err != nil {
		t.Fatalf("store gather: %v", err)
	}
	return fams
}

func otlpHasMetric(rm metricdata.ResourceMetrics, name string) bool {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return true
			}
		}
	}
	return false
}

func TestNewOTLPConstructsAndShuts(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.System = true })
	config.Config.OTLP.Endpoint = "localhost:4317"
	config.Config.OTLP.Protocol = "grpc"
	config.Config.OTLP.Insecure = true
	config.Config.OTLP.IntervalSeconds = 60

	store := NewSnapshotStore()
	o, err := NewOTLP(context.Background(), store)
	if err != nil {
		t.Fatalf("NewOTLP: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := o.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}
