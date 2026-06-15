package collector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fjacquet/idrac_exporter/internal/config"
	prombridge "go.opentelemetry.io/contrib/bridges/prometheus"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/sdk/metric"
)

// OTLP wraps the OpenTelemetry MeterProvider that pushes the snapshot via OTLP.
// Its only metric source is the Prometheus bridge reading the SnapshotStore; it
// owns no instruments of its own.
type OTLP struct {
	provider *metric.MeterProvider
}

// NewOTLP builds the OTLP push pipeline: an exporter (gRPC or HTTP per config),
// a periodic reader on otlp.interval, and a MeterProvider fed by the bridge.
func NewOTLP(ctx context.Context, store *SnapshotStore) (*OTLP, error) {
	o := &config.Config.OTLP

	var (
		exporter metric.Exporter
		err      error
	)
	switch o.Protocol {
	case "http":
		opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(o.Endpoint)}
		if o.Insecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		if len(o.Headers) > 0 {
			opts = append(opts, otlpmetrichttp.WithHeaders(o.Headers))
		}
		exporter, err = otlpmetrichttp.New(ctx, opts...)
	default: // grpc
		opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(o.Endpoint)}
		if o.Insecure {
			opts = append(opts, otlpmetricgrpc.WithInsecure())
		}
		if len(o.Headers) > 0 {
			opts = append(opts, otlpmetricgrpc.WithHeaders(o.Headers))
		}
		exporter, err = otlpmetricgrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	producer := prombridge.NewMetricProducer(prombridge.WithGatherer(store))
	reader := metric.NewPeriodicReader(
		exporter,
		metric.WithInterval(time.Duration(o.IntervalSeconds*float64(time.Second))),
		metric.WithProducer(producer),
	)
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	return &OTLP{provider: provider}, nil
}

// Shutdown stops the reader and attempts a best-effort final export. Export
// delivery errors (e.g. unreachable collector endpoint) are silently dropped;
// the caller cannot remediate them and OTLP is explicitly best-effort.
func (o *OTLP) Shutdown(ctx context.Context) error {
	err := o.provider.Shutdown(ctx)
	if err == nil {
		return nil
	}
	// OTel wraps export failures as "failed to upload metrics: …"; treat these
	// as best-effort and do not propagate.
	if strings.HasPrefix(err.Error(), "failed to upload metrics") {
		return nil
	}
	return err
}
