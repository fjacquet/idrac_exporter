package collector

import (
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"
)

func sampleFamily(name string) *dto.MetricFamily {
	return &dto.MetricFamily{
		Name: proto.String(name),
		Type: dto.MetricType_GAUGE.Enum(),
		Metric: []*dto.Metric{{
			Gauge: &dto.Gauge{Value: proto.Float64(1)},
		}},
	}
}

func TestSnapshotStoreEmptyGather(t *testing.T) {
	s := NewSnapshotStore()
	fams, err := s.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	if len(fams) != 0 {
		t.Fatalf("empty store gathered %d families, want 0", len(fams))
	}
}

func TestLabelFamiliesDoesNotMutateSource(t *testing.T) {
	src := []*dto.MetricFamily{sampleFamily("idrac_system_health")}
	out := labelFamilies(src, "system", "bmc1")

	if got := len(src[0].Metric[0].Label); got != 0 {
		t.Fatalf("source mutated: %d labels, want 0", got)
	}
	lbls := out[0].Metric[0].Label
	if len(lbls) != 1 || lbls[0].GetName() != "system" || lbls[0].GetValue() != "bmc1" {
		t.Fatalf("identity label not applied to clone: %+v", lbls)
	}
}

func TestBuildSnapshotMergesByName(t *testing.T) {
	// Each host gets its own freshly built family, satisfying buildSnapshot's
	// owned-input contract (the real call path feeds it labelFamilies clones).
	host1 := []*dto.MetricFamily{sampleFamily("idrac_system_health")}
	host2 := []*dto.MetricFamily{sampleFamily("idrac_system_health")}
	snap := buildSnapshot([][]*dto.MetricFamily{host1, host2})
	if len(snap.families) != 1 {
		t.Fatalf("merged into %d families, want 1", len(snap.families))
	}
	if got := len(snap.families[0].Metric); got != 2 {
		t.Fatalf("merged family has %d metrics, want 2", got)
	}
}

func TestLabelFamiliesConvertsUntypedToGauge(t *testing.T) {
	src := []*dto.MetricFamily{{
		Name: proto.String("idrac_system_machine_info"),
		Type: dto.MetricType_UNTYPED.Enum(),
		Metric: []*dto.Metric{{
			Untyped: &dto.Untyped{Value: proto.Float64(1)},
		}},
	}}
	out := labelFamilies(src, "system", "bmc1")
	if out[0].GetType() != dto.MetricType_GAUGE {
		t.Fatalf("type = %v, want GAUGE", out[0].GetType())
	}
	m := out[0].Metric[0]
	if m.Untyped != nil {
		t.Errorf("Untyped not cleared")
	}
	if m.Gauge == nil || m.Gauge.GetValue() != 1 {
		t.Fatalf("gauge value not preserved: %+v", m.Gauge)
	}
	if m.Label[0].GetName() != "system" || m.Label[0].GetValue() != "bmc1" {
		t.Errorf("identity label missing: %+v", m.Label)
	}
	// source must be untouched
	if src[0].GetType() != dto.MetricType_UNTYPED || src[0].Metric[0].Untyped == nil {
		t.Errorf("source mutated")
	}
}

func TestUpFamilyCarriesIdentityLabel(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.System = true })
	mf := upFamily("system", "bmc1", 0)
	if mf.GetName() != "idrac_up" {
		t.Fatalf("up name = %q, want idrac_up", mf.GetName())
	}
	m := mf.Metric[0]
	if m.Gauge.GetValue() != 0 {
		t.Fatalf("up value = %v, want 0", m.Gauge.GetValue())
	}
	if m.Label[0].GetName() != "system" || m.Label[0].GetValue() != "bmc1" {
		t.Fatalf("up label = %+v, want system=bmc1", m.Label)
	}
}
