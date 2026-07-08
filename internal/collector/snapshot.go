package collector

import (
	"sort"
	"sync/atomic"

	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"
)

// Snapshot is an immutable set of metric families aggregated across all
// configured hosts. It is published to a SnapshotStore by the background loop
// and read by the OTLP bridge.
type Snapshot struct {
	families []*dto.MetricFamily
}

// SnapshotStore holds the latest Snapshot behind an atomic pointer swap and
// implements prometheus.Gatherer so the OTLP MetricProducer can read it.
type SnapshotStore struct {
	ptr atomic.Pointer[Snapshot]
}

func NewSnapshotStore() *SnapshotStore {
	s := &SnapshotStore{}
	s.ptr.Store(&Snapshot{})
	return s
}

func (s *SnapshotStore) Store(snap *Snapshot) {
	s.ptr.Store(snap)
}

// Gather implements prometheus.Gatherer.
// It returns the families slice from the current snapshot directly — no
// defensive copy is made. Callers MUST NOT mutate the returned slice or any
// *dto.MetricFamily within it: the snapshot is shared and immutable, and
// mutation would race with a concurrent Store() swap. If you need to annotate
// or transform the families, use labelFamilies (which deep-clones them) or
// build your own copy first.
func (s *SnapshotStore) Gather() ([]*dto.MetricFamily, error) {
	snap := s.ptr.Load()
	if snap == nil {
		return nil, nil
	}
	return snap.families, nil
}

// labelFamilies returns a deep copy of families ready for the OTLP snapshot:
// every name in `names` is appended as a label (set to value) on every metric,
// and, when coerceUntyped is set, UNTYPED families (the exporter's *_info and
// build_info metrics) are converted to GAUGE so the OpenTelemetry Prometheus
// bridge maps them instead of dropping them. The source families are never
// mutated, so the collector's cached gather output stays clean for the
// on-demand path.
func labelFamilies(families []*dto.MetricFamily, names []string, value string, coerceUntyped bool) []*dto.MetricFamily {
	out := make([]*dto.MetricFamily, 0, len(families))
	for _, mf := range families {
		clone := proto.Clone(mf).(*dto.MetricFamily)
		if coerceUntyped && clone.GetType() == dto.MetricType_UNTYPED {
			clone.Type = dto.MetricType_GAUGE.Enum()
			for _, m := range clone.Metric {
				if m.Untyped != nil {
					m.Gauge = &dto.Gauge{Value: proto.Float64(m.Untyped.GetValue())}
					m.Untyped = nil
				}
			}
		}
		for _, m := range clone.Metric {
			for _, name := range names {
				m.Label = append(m.Label, &dto.LabelPair{
					Name:  proto.String(name),
					Value: proto.String(value),
				})
			}
		}
		out = append(out, clone)
	}
	return out
}

// upFamily builds the <prefix>_up metric family for one target, carrying
// every name in `names` as a label.
func upFamily(names []string, target string, value float64) *dto.MetricFamily {
	name := prometheus.BuildFQName(config.Config.MetricsPrefix, "", "up")
	help := "Whether the last collection of the target succeeded (1) or failed (0)"
	labels := make([]*dto.LabelPair, 0, len(names))
	for _, n := range names {
		labels = append(labels, &dto.LabelPair{Name: proto.String(n), Value: proto.String(target)})
	}
	return &dto.MetricFamily{
		Name: proto.String(name),
		Help: proto.String(help),
		Type: dto.MetricType_GAUGE.Enum(),
		Metric: []*dto.Metric{{
			Label: labels,
			Gauge: &dto.Gauge{Value: proto.Float64(value)},
		}},
	}
}

// hasRealMetric reports whether families contains any non-meta metric family —
// i.e. the target produced at least one collected metric this cycle. Used to
// decide idrac_up: a target that returns only the build_info / scrape_errors
// bookkeeping metrics is treated as down.
func hasRealMetric(families []*dto.MetricFamily) bool {
	prefix := config.Config.MetricsPrefix
	buildInfo := prometheus.BuildFQName(prefix, "exporter", "build_info")
	scrapeErrors := prometheus.BuildFQName(prefix, "exporter", "scrape_errors_total")
	for _, mf := range families {
		name := mf.GetName()
		if name == buildInfo || name == scrapeErrors {
			continue
		}
		if len(mf.Metric) > 0 {
			return true
		}
	}
	return false
}

// buildSnapshot merges per-host families into one Snapshot. Families sharing a
// name across hosts have their Metric slices concatenated; the result is sorted
// by name for stable output.
//
// Caller contract: each family in perHost must be owned (not shared/cached),
// since merging mutates the first-seen family in place by appending the metrics
// of later same-named families. Callers satisfy this by passing the output of
// labelFamilies (deep clones) or upFamily (freshly allocated).
func buildSnapshot(perHost [][]*dto.MetricFamily) *Snapshot {
	merged := map[string]*dto.MetricFamily{}
	for _, host := range perHost {
		for _, mf := range host {
			name := mf.GetName()
			if existing, ok := merged[name]; ok {
				existing.Metric = append(existing.Metric, mf.Metric...)
			} else {
				merged[name] = mf
			}
		}
	}
	out := make([]*dto.MetricFamily, 0, len(merged))
	for _, mf := range merged {
		out = append(out, mf)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GetName() < out[j].GetName() })
	return &Snapshot{families: out}
}

// Ensure SnapshotStore implements prometheus.Gatherer at compile time.
var _ prometheus.Gatherer = (*SnapshotStore)(nil)
