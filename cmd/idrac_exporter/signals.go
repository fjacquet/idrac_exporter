package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/fjacquet/idrac_exporter/internal/log"
)

// handleSignals reloads the configuration on SIGHUP for the lifetime of the
// process.
func handleSignals(cfgPath string) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)
	for range c {
		log.Info("Received SIGHUP, reloading configuration")
		ReloadConfig(cfgPath)
	}
}
