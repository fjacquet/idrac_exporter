# iDRAC Exporter

[![CI](https://github.com/fjacquet/idrac_exporter/actions/workflows/ci.yml/badge.svg)](https://github.com/fjacquet/idrac_exporter/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/fjacquet/idrac_exporter?include_prereleases&sort=semver)](https://github.com/fjacquet/idrac_exporter/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/fjacquet/idrac_exporter)](https://goreportcard.com/report/github.com/fjacquet/idrac_exporter)
[![Go version](https://img.shields.io/github/go-mod/go-version/fjacquet/idrac_exporter)](go.mod)
[![License](https://img.shields.io/github/license/fjacquet/idrac_exporter)](LICENSE)
[![Docs](https://img.shields.io/badge/docs-mkdocs-blue)](https://fjacquet.github.io/idrac_exporter)

_Forked from [mrlhansen/idrac_exporter](https://github.com/mrlhansen/idrac_exporter)._

A Redfish exporter for [Prometheus](https://prometheus.io) that scrapes hardware health and telemetry from server BMCs — Dell iDRAC, HPE iLO, Lenovo XClarity, Supermicro and other Redfish-compliant systems. Metrics are collected **on-demand**: Prometheus passes the BMC address via the `target` parameter and the exporter opens a Redfish session to that host at scrape time.

```text
http://localhost:9348/metrics?target=192.168.1.1
```

If the target is unreachable or authentication fails, the exporter returns status `500` with an error message.

> 📖 **Full documentation:** <https://fjacquet.github.io/idrac_exporter/> —
> [Metrics catalog](https://fjacquet.github.io/idrac_exporter/metrics/) ·
> [Docker Compose](https://fjacquet.github.io/idrac_exporter/deployment/docker/) ·
> [Dashboards](https://fjacquet.github.io/idrac_exporter/dashboards/) ·
> [OTLP push](https://fjacquet.github.io/idrac_exporter/otlp/) ·
> [Architecture Decisions](https://fjacquet.github.io/idrac_exporter/adr/)

## Export modes

Two modes can run side by side:

* **On-demand scrape (primary).** Prometheus scrapes `/metrics?target=<bmc>`; the exporter opens a Redfish session per scrape. This is the default and the recommended model for a BMC fleet.
* **Optional OTLP push.** A background loop polls the configured hosts on a fixed interval and pushes their metrics over OTLP. It is **off by default**, enabled under the `otlp:` block, and leaves the on-demand path unchanged. See the [OTLP guide](https://fjacquet.github.io/idrac_exporter/otlp/).

## Installation

### Build from source

```sh
git clone https://github.com/fjacquet/idrac_exporter.git
cd idrac_exporter
make cli            # builds bin/idrac_exporter
```

### Homebrew (macOS)

```sh
brew install fjacquet/tap/idrac_exporter
```

Pre-built binaries for Linux, macOS and Windows (`amd64`/`arm64`) are attached to each [release](https://github.com/fjacquet/idrac_exporter/releases).

### Docker

Pre-built images are published on GHCR:

```sh
docker pull ghcr.io/fjacquet/idrac_exporter
```

Set the listen address to `0.0.0.0` when running in a container.

### Docker Compose quickstart

A one-command demo stack — exporter, Prometheus (with alert rules) and Grafana (datasource + dashboards auto-provisioned):

```sh
IDRAC1_HOST=10.0.0.10 IDRAC1_USERNAME=monitor IDRAC1_PASSWORD='secret' docker compose up -d
```

Grafana is then on <http://localhost:3000> (`admin`/`admin`), Prometheus on <http://localhost:9090>. A reachable BMC is required. Full walkthrough: [Docker Compose](https://fjacquet.github.io/idrac_exporter/deployment/docker/).

### Helm

```sh
helm install idrac-exporter oci://ghcr.io/fjacquet/charts/idrac-exporter
```

## Configuration

By default the exporter reads `/etc/prometheus/idrac.yml` (override with `--config`). A minimal configuration:

```yaml
address: 127.0.0.1   # listen address (use 0.0.0.0 in a container)
port: 9348           # listen port
timeout: 60          # Redfish HTTP timeout, in seconds (BMC calls can be slow)
hosts:
  default:           # fallback credentials for any unmatched target
    username: user
    password: pass
metrics:
  all: true          # enable every metric group
```

Per-host credentials can be keyed by IP/hostname under `hosts:`, or grouped under `auths:` and selected per request with `&auth=<group>`. File values are overridden by `CONFIG_*` environment variables, and `${VAR}` references in the YAML are expanded from the environment. The full reference — every option, env var, and the `collection:` / `otlp:` blocks — is in [sample-config.yml](sample-config.yml) and the [configuration docs](https://fjacquet.github.io/idrac_exporter/).

Because metrics are collected on-demand, a scrape can take a while depending on the enabled groups — set a generous Prometheus `scrape_timeout`.

## Usage

```sh
idrac_exporter --config /etc/prometheus/idrac.yml
```

| Flag             | Description                                                          |
| ---------------- | ------------------------------------------------------------------- |
| `--config`       | Path to the configuration file (default `/etc/prometheus/idrac.yml`)|
| `--config-watch` | Watch the configuration file and hot-reload on change               |
| `--verbose`      | More verbose logging                                                 |
| `--debug`        | Dump raw Redfish JSON responses (implies `--verbose`)               |
| `--trace`        | Log each Redfish request — method, path, status — without credentials|
| `--once`         | Collect every configured host once, print the exposition, and exit  |
| `--version`      | Print version and exit                                               |

**Validate against a real BMC without Prometheus** — `--once` collects every configured host a single time and writes the metrics exposition (sorted by target) to stdout, which is handy for a quick check or a diff:

```sh
idrac_exporter --config config.yml --once
```

**Trace what the exporter does to a BMC** — `--trace` logs every Redfish request (method, path, status) with no credentials, and `--debug` additionally dumps the raw JSON of each response:

```sh
idrac_exporter --config config.yml --once --trace   # request-level trace
idrac_exporter --config config.yml --once --debug   # full JSON dump
```

## Endpoints

| Endpoint    | Parameters | Description                                   |
| ----------- | ---------- | --------------------------------------------- |
| `/metrics`  | `target`   | Metrics for the specified target              |
| `/reset`    | `target`   | Reset internal state for the specified target |
| `/reload`   |            | Reload the configuration file                 |
| `/discover` |            | Prometheus HTTP Service Discovery             |
| `/health`   |            | Returns HTTP 200                              |
| `/`         |            | Landing page                                  |

## Prometheus configuration

For a single exporter fronting many BMCs, rewrite `target` per host with relabeling (`exporter:9348` is where the exporter runs):

```yaml
scrape_configs:
  - job_name: idrac
    static_configs:
      - targets: ['192.168.1.1', '192.168.1.2']
    relabel_configs:
      - source_labels: [__param_target]
        target_label: __address__
        # placeholder; the next rules set the real param + address
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      # Expose the BMC as `system` too — the bundled Grafana dashboards key on it.
      - source_labels: [__param_target]
        target_label: system
      - target_label: __address__
        replacement: exporter:9348
```

The exporter's `/discover` endpoint can replace the static target list — see the [documentation](https://fjacquet.github.io/idrac_exporter/).

## Metrics

Every metric is prefixed `idrac_` (configurable). All `<name>_health` metrics map `0 = OK`, `1 = Warning`, `2 = Critical`. Groups: **System, Sensors, Power, Processors, Memory, Network, Storage, Manager, Event Log, PDU** (experimental), Dell OEM extras, and **Exporter** self-metrics. The OTLP/snapshot path additionally emits `idrac_up` (per-host collection health).

See the full **[metrics catalog](https://fjacquet.github.io/idrac_exporter/metrics/)** ([docs/metrics.md](docs/metrics.md)) for every metric name and its labels.

## Dashboards

Grafana dashboards live in [`grafana/`](grafana/) and are auto-provisioned by the Compose quickstart against the bundled Prometheus datasource:

| Dashboard | File | Focus |
| --------- | ---- | ----- |
| BMC | `grafana/idrac.json` | Per-machine detail |
| BMC overview | `grafana/idrac_overview.json` | Fleet overview + health |
| BMC Status | `grafana/status-alternative.json` | Alternative per-machine status |
| PDU | `grafana/pdu.json` | Rack PDU power, energy, health |

All dashboards select hosts via a single `system` template variable. The overview/detail dashboards were contributed by [@7840vz](https://github.com/7840vz). See [Dashboards](https://fjacquet.github.io/idrac_exporter/dashboards/).

## Contributing

Issues and pull requests are welcome. Prefer metrics that work **across vendors** (see [CONTRIBUTING.md](CONTRIBUTING.md)). The CI gate is `make ci`.

## License

[MIT](LICENSE)
