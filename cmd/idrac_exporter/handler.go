package main

import (
	"compress/gzip"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/fjacquet/idrac_exporter/internal/collector"
	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/fjacquet/idrac_exporter/internal/log"
	"github.com/fjacquet/idrac_exporter/internal/version"
)

const (
	contentTypeHeader     = "Content-Type"
	contentEncodingHeader = "Content-Encoding"
	acceptEncodingHeader  = "Accept-Encoding"
)

var gzipPool = sync.Pool{
	New: func() any {
		return gzip.NewWriter(nil)
	},
}

const landingPageTemplate = `<html lang="en">
<head><title>iDRAC Exporter</title></head>
<body style="font-family: sans-serif">
<h2>iDRAC Exporter</h2>
<div>Build information: version={{.Version}} revision={{.Revision}}</div>
<ul><li><a href="/metrics">Metrics</a> (needs <code>target</code> parameter)</li></ul>
</body>
</html>
`

var landingPage = template.Must(template.New("landing").Parse(landingPageTemplate))

func rootHandler(rsp http.ResponseWriter, req *http.Request) {
	_ = landingPage.Execute(rsp, struct {
		Version  string
		Revision string
	}{version.Version, version.Revision})
}

func healthHandler(rsp http.ResponseWriter, req *http.Request) {
	// just return a simple 200 for now
}

func reloadHandler(rsp http.ResponseWriter, req *http.Request) {
	ReloadConfig(flagConfig)
}

func resetHandler(rsp http.ResponseWriter, req *http.Request) {
	target := req.URL.Query().Get("target")
	if target == "" {
		target = config.Config.DefaultTarget
		if target == "" {
			log.Error("Received request from %s without 'target' parameter", req.Host)
			http.Error(rsp, "Query parameter 'target' is mandatory", http.StatusBadRequest)
			return
		}
	}
	log.Debug("Handling reset request from %s for host %s", req.Host, target)

	collector.Reset(target)
}

func discoverHandler(rsp http.ResponseWriter, req *http.Request) {
	rsp.Header().Set(contentTypeHeader, "application/json")
	w := io.Writer(rsp)
	_, _ = io.WriteString(w, config.GetDiscover())
}

type metricsMode int

const (
	modeSingleTarget metricsMode = iota // collect one host (the returned target)
	modeScrapeAll                       // collect every configured host
	modeError                           // 400: nothing resolvable
)

// resolveMetricsMode implements the /metrics routing ladder. hasHosts reports
// whether any non-"default" host is configured.
func resolveMetricsMode(target, defaultTarget string, hasHosts bool) (metricsMode, string) {
	if target != "" {
		return modeSingleTarget, target
	}
	if defaultTarget != "" {
		return modeSingleTarget, defaultTarget
	}
	if hasHosts {
		return modeScrapeAll, ""
	}
	return modeError, ""
}

func metricsHandler(rsp http.ResponseWriter, req *http.Request) {
	target := req.URL.Query().Get("target")
	mode, target := resolveMetricsMode(target, config.Config.DefaultTarget, config.Config.HasTargetHosts())

	switch mode {
	case modeError:
		log.Error("Received request from %s without 'target' parameter and no hosts configured", req.Host)
		http.Error(rsp, "Query parameter 'target' is mandatory", http.StatusBadRequest)
		return
	case modeScrapeAll:
		log.Debug("Handling scrape-all metrics request from %s", req.Host)
		metrics, err := collector.GatherAll()
		if err != nil {
			errorMsg := fmt.Sprintf("Error collecting metrics for all hosts: %v", err)
			log.Error("%v", errorMsg)
			http.Error(rsp, errorMsg, http.StatusInternalServerError)
			return
		}
		writeMetrics(rsp, req, metrics)
		return
	}

	// modeSingleTarget
	auth := req.URL.Query().Get("auth")
	log.Debug("Handling metrics request from %s for host %s", req.Host, target)

	c, err := collector.GetCollector(target, auth)
	if err != nil {
		errorMsg := fmt.Sprintf("Error instantiating metrics collector for host %s: %v", target, err)
		log.Error("%v", errorMsg)
		http.Error(rsp, errorMsg, http.StatusInternalServerError)
		return
	}

	log.Debug("Collecting metrics for host %s", target)

	metrics, err := c.Gather()
	if err != nil {
		errorMsg := fmt.Sprintf("Error collecting metrics for host %s: %v", target, err)
		log.Error("%v", errorMsg)
		http.Error(rsp, errorMsg, http.StatusInternalServerError)
		return
	}

	log.Debug("Metrics for host %s collected", target)
	writeMetrics(rsp, req, metrics)
}

// writeMetrics writes the exposition text to the response, gzipping when the
// client accepts it. Shared by the single-target and scrape-all paths.
func writeMetrics(rsp http.ResponseWriter, req *http.Request, metrics string) {
	header := rsp.Header()
	header.Set(contentTypeHeader, "text/plain")

	// Code inspired by the official Prometheus metrics http handler
	w := io.Writer(rsp)
	if gzipAccepted(req.Header) {
		header.Set(contentEncodingHeader, "gzip")
		gz := gzipPool.Get().(*gzip.Writer)
		defer gzipPool.Put(gz)

		gz.Reset(w)
		defer func() { _ = gz.Close() }()

		w = gz
	}

	_, _ = io.WriteString(w, metrics)
}

// gzipAccepted returns whether the client will accept gzip-encoded content.
func gzipAccepted(header http.Header) bool {
	a := header.Get(acceptEncodingHeader)
	parts := strings.Split(a, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "gzip" || strings.HasPrefix(part, "gzip;") {
			return true
		}
	}
	return false
}
