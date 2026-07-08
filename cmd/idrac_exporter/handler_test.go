package main

import "testing"

func TestResolveMetricsMode(t *testing.T) {
	tests := []struct {
		name          string
		target        string
		defaultTarget string
		hasHosts      bool
		wantMode      metricsMode
		wantTarget    string
	}{
		{"explicit target", "10.0.0.5", "", true, modeSingleTarget, "10.0.0.5"},
		{"explicit target beats default", "10.0.0.5", "1.2.3.4", true, modeSingleTarget, "10.0.0.5"},
		{"default target fallback", "", "1.2.3.4", true, modeSingleTarget, "1.2.3.4"},
		{"scrape all when hosts but no target/default", "", "", true, modeScrapeAll, ""},
		{"error when nothing resolvable", "", "", false, modeError, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, target := resolveMetricsMode(tt.target, tt.defaultTarget, tt.hasHosts)
			if mode != tt.wantMode || target != tt.wantTarget {
				t.Fatalf("resolveMetricsMode(%q,%q,%v) = (%v,%q), want (%v,%q)",
					tt.target, tt.defaultTarget, tt.hasHosts, mode, target, tt.wantMode, tt.wantTarget)
			}
		})
	}
}
