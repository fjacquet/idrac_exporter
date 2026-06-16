package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/fjacquet/idrac_exporter/internal/collector"
	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/fjacquet/idrac_exporter/internal/log"
	"github.com/fjacquet/idrac_exporter/internal/version"
	"github.com/spf13/cobra"
)

var (
	flagVerbose bool
	flagDebug   bool
	flagTrace   bool
	flagOnce    bool
	flagConfig  string
	flagWatch   bool
	flagVersion bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:           "idrac_exporter",
		Short:         "Redfish (iDRAC, iLO, XClarity, ...) exporter for Prometheus",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          run,
	}

	f := rootCmd.PersistentFlags()
	f.StringVar(&flagConfig, "config", "/etc/prometheus/idrac.yml", "Path to the configuration file")
	f.BoolVar(&flagVerbose, "verbose", false, "Enable more verbose logging")
	f.BoolVar(&flagDebug, "debug", false, "Dump JSON responses from Redfish requests (implies --verbose)")
	f.BoolVar(&flagTrace, "trace", false, "Log each Redfish request (method, path, status) without credentials")
	f.BoolVar(&flagOnce, "once", false, "Collect every configured host once, print exposition, and exit")
	f.BoolVar(&flagWatch, "config-watch", false, "Watch the configuration file and reload on change")
	f.BoolVar(&flagVersion, "version", false, "Show version and exit")

	if err := rootCmd.Execute(); err != nil {
		log.Fatal("%v", err)
	}
}

func run(_ *cobra.Command, _ []string) error {
	if flagVersion {
		fmt.Printf("version: %s\n", version.Version)
		fmt.Printf("revision: %s\n", version.Revision)
		fmt.Printf("goversion: %s\n", runtime.Version())
		fmt.Printf("platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		return nil
	}

	log.Info("Build information: version=%s revision=%s", version.Version, version.Revision)
	config.LoadDotEnv(flagConfig)
	LoadConfig(flagConfig, flagWatch)

	if flagDebug {
		config.Debug = true
		flagVerbose = true
	}
	if flagTrace {
		config.Trace = true
	}
	if flagVerbose {
		log.SetLevel(log.LevelDebug)
	}

	go handleSignals(flagConfig)

	if flagOnce {
		return runOnce(os.Stdout)
	}

	http.HandleFunc("/discover", discoverHandler)
	http.HandleFunc("/metrics", metricsHandler)
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/reload", reloadHandler)
	http.HandleFunc("/reset", resetHandler)
	http.HandleFunc("/", rootHandler)

	port := fmt.Sprintf("%d", config.Config.Port)
	host := strings.Trim(config.Config.Address, "[]")
	bind := net.JoinHostPort(host, port)
	log.Info("Server listening on %s (TLS: %v)", bind, config.Config.TLS.Enabled)

	srv := &http.Server{
		Addr:              bind,
		ReadHeaderTimeout: 10 * time.Second, // mitigate Slowloris
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", bind)
	if err != nil {
		return err
	}
	if config.Config.TLS.Enabled {
		cert, err := tls.LoadX509KeyPair(config.Config.TLS.CertFile, config.Config.TLS.KeyFile)
		if err != nil {
			return err
		}
		ln = tls.NewListener(ln, &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		})
	}

	if config.Config.OTLP.Enabled {
		store := collector.NewSnapshotStore()

		// Build the OTLP pipeline before starting the loop, so a failed exporter
		// setup never leaves an orphaned collection goroutine running.
		otlp, err := collector.NewOTLP(ctx, store)
		if err != nil {
			return err
		}
		defer func() {
			shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := otlp.Shutdown(shCtx); err != nil {
				log.Error("OTLP shutdown: %v", err)
			}
		}()

		interval := time.Duration(config.Config.Collection.IntervalSeconds * float64(time.Second))
		loop := collector.NewLoop(store, interval)
		go loop.Run(ctx)

		log.Info("OTLP push enabled: endpoint=%s protocol=%s interval=%vs",
			config.Config.OTLP.Endpoint, config.Config.OTLP.Protocol, config.Config.OTLP.IntervalSeconds)
	}

	return serve(ctx, srv, ln)
}
