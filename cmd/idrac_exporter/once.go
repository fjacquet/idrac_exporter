package main

import (
	"fmt"
	"io"
	"sort"

	"github.com/fjacquet/idrac_exporter/internal/collector"
	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/fjacquet/idrac_exporter/internal/log"
)

// runOnce collects every configured host (except the "default" credential
// fallback) exactly once and writes their exposition to w, sorted by target so
// the output is diffable. It is the live-validation path behind --once.
func runOnce(w io.Writer) error {
	targets := make([]string, 0, len(config.Config.Hosts))
	for t := range config.Config.Hosts {
		if t == "default" {
			continue
		}
		targets = append(targets, t)
	}
	sort.Strings(targets)

	for _, target := range targets {
		c, err := collector.GetCollector(target, "")
		if err != nil {
			log.Error("once: collector for %s: %v", target, err)
			continue
		}
		metrics, err := c.Gather()
		if err != nil {
			log.Error("once: gather %s: %v", target, err)
			continue
		}
		_, _ = fmt.Fprintf(w, "# target %s\n%s", target, metrics)
	}
	return nil
}
