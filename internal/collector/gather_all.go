package collector

import (
	"strings"

	"github.com/prometheus/common/expfmt"
)

// GatherAll collects every configured host (minus the "default" credential
// fallback) concurrently and returns the merged Prometheus text exposition.
// Each series carries instance="<host>" and system="<host>", plus a per-host
// <prefix>_up gauge (1 if the host produced metrics, 0 if it was unreachable or
// errored). UNTYPED families are left untyped so the output matches the
// per-target /metrics?target= exposition. An individual unreachable host never
// fails the call — it contributes only up=0.
func GatherAll() (string, error) {
	snap := collectAllHosts([]string{"instance", "system"}, false)
	var b strings.Builder
	for _, mf := range snap.families {
		if _, err := expfmt.MetricFamilyToText(&b, mf); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}
