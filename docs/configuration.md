# Configuration

By default the exporter reads its configuration from `/etc/prometheus/idrac.yml`; override the
path with `--config`. A minimal configuration:

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

## Credentials

You can define credentials per host and per group:

- **`hosts:`** — keyed by IP address or hostname. When no matching host is found, the
  exporter falls back to the credentials under `default`.
- **`auths:`** — named credential groups, selected per request with the `auth` query
  parameter:

  ```text
  http://localhost:9348/metrics?target=192.168.10.10&auth=mygroup
  ```

`hosts` and `auths` can be combined freely. The login user only needs read-only permissions —
create a dedicated, unprivileged account for the exporter.

## Environment overrides and variable expansion

File values are overridden by `CONFIG_*` environment variables, and `${VAR}` references inside
the YAML are expanded from the environment at load time. This keeps secrets out of the file:

```yaml
hosts:
  default:
    username: ${IDRAC_USERNAME}
    password: ${IDRAC_PASSWORD}
```

## Metric groups, collection and OTLP

Metric groups are toggled under `metrics:` (`all: true` enables everything). The optional
background snapshot loop and OTLP push are configured under the `collection:` and `otlp:`
blocks (off by default) — see the [OTLP guide](otlp.md).

The full reference — every option, its environment variable, and the `collection:` / `otlp:`
blocks — lives in
[`sample-config.yml`](https://github.com/fjacquet/idrac_exporter/blob/main/sample-config.yml).

!!! note
    Because metrics are collected on demand, a single scrape can take a while depending on how
    many metric groups are enabled. Give Prometheus a generous `scrape_timeout`.

## Prometheus configuration

For a single exporter fronting many BMCs, rewrite the `target` parameter per host with
relabeling. Here `exporter:9348` is where the exporter runs:

```yaml
scrape_configs:
  - job_name: idrac
    static_configs:
      - targets: ['192.168.1.1', '192.168.1.2']
    relabel_configs:
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

Alternatively, use the exporter's service-discovery endpoint instead of a static target list:

```yaml
scrape_configs:
  - job_name: idrac
    http_sd_configs:
      - url: http://exporter:9348/discover
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - source_labels: [__param_target]
        target_label: system
      - source_labels: [__meta_url]
        target_label: __address__
        regex: (https?.{3})([^\/]+)(.+)
        replacement: $2
```
