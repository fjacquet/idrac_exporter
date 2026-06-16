# iDRAC Exporter

A Redfish exporter for [Prometheus](https://prometheus.io) that scrapes hardware health and
telemetry from server BMCs over the Redfish API. Despite the name it is vendor-agnostic.
Metrics are collected **on demand** — Prometheus passes the BMC address as the `target` query
parameter and the exporter opens a Redfish session to that host at scrape time:

```text
http://localhost:9348/metrics?target=192.168.1.1
```

_Forked from [mrlhansen/idrac_exporter](https://github.com/mrlhansen/idrac_exporter)._

## Supported systems

Any Redfish-compliant BMC should work. The exporter is tested on:

- Dell iDRAC
- HPE iLO
- Lenovo XClarity
- Supermicro BMC

## Export modes

Two modes can run side by side:

- **On-demand scrape (primary).** Prometheus scrapes `/metrics?target=<bmc>`; the exporter
  opens a Redfish session per scrape. This is the default and the recommended model for a BMC
  fleet.
- **Optional OTLP push.** A background loop polls the configured hosts on a fixed interval and
  pushes their metrics over OTLP. It is **off by default**, enabled under the `otlp:` block,
  and leaves the on-demand path unchanged. See the [OTLP guide](otlp.md).

## Where to go next

- [Installation](installation.md) — build, Docker, Helm
- [Configuration](configuration.md) — config reference and Prometheus setup
- [Usage](usage.md) — command-line flags, `--once`/`--trace`, and endpoints
- [Docker Compose](deployment/docker.md) — one-command demo stack
- [Metrics](metrics.md) — full metric catalog
- [Dashboards](dashboards.md) — bundled Grafana dashboards
- [Architecture Decisions](adr/index.md) — the decisions behind the design
