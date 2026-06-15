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

func metricsHandler(rsp http.ResponseWriter, req *http.Request) {
	target := req.URL.Query().Get("target")
	if target == "" {
		target = config.Config.DefaultTarget
		if target == "" {
			log.Error("Received request from %s without 'target' parameter", req.Host)
			http.Error(rsp, "Query parameter 'target' is mandatory", http.StatusBadRequest)
			return
		}
	}
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
