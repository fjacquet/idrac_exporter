package collector

import (
	"context"
	"fmt"
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

// Shutdown stops the reader and flushes a final export. A failed final flush
// (e.g. the collector is unreachable at shutdown) is returned to the caller,
// which logs it — OTLP delivery is best-effort and must not be silently dropped.
func (o *OTLP) Shutdown(ctx context.Context) error {
	return o.provider.Shutdown(ctx)
}
