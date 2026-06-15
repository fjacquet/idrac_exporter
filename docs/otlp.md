# OTLP push (optional)

The exporter's primary mode is on-demand: Prometheus scrapes
`/metrics?target=<bmc>`. Optionally, an off-by-default background loop polls the
configured `hosts:` on a fixed interval and pushes their metrics via OTLP.

```yaml
collection:
  interval: 60s            # snapshot-loop cadence (default 60s when otlp.enabled)
otlp:
  enabled: false           # master switch — the loop runs only when true
  endpoint: "localhost:4317"
  protocol: grpc           # grpc | http
  insecure: false          # set true for a plaintext local collector
  interval: 0s             # OTLP push cadence; 0 = use collection.interval
  identity_label: system   # per-series target label (system | instance | ...)
  headers: {}              # optional static exporter headers
```

Every pushed series carries `<identity_label>=<target>` (the OTLP path has no
Prometheus relabel to supply `instance`). A per-target `idrac_up` gauge reports
`1` when the last cycle collected metrics and `0` when the target was unreachable
or produced nothing. Host/credential changes hot-reload via SIGHUP; changing OTLP
transport settings (`endpoint`/`protocol`/`interval`/`enabled`) requires a
restart. Environment overrides: `CONFIG_OTLP_ENABLED`, `CONFIG_OTLP_ENDPOINT`,
`CONFIG_OTLP_PROTOCOL`, `CONFIG_OTLP_INSECURE`, `CONFIG_OTLP_INTERVAL`,
`CONFIG_OTLP_IDENTITY_LABEL`, `CONFIG_COLLECTION_INTERVAL`.
