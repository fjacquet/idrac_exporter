# Docker Compose quickstart

A one-command demo stack: the exporter, Prometheus (with starter alert rules), and
Grafana (with the datasource and dashboards auto-provisioned).

## Run it

```sh
# One BMC (build the image locally):
IDRAC1_HOST=10.0.0.10 IDRAC1_USERNAME=monitor IDRAC1_PASSWORD='secret' docker compose up -d

# Two BMCs — the stack runs in scrape-all mode and shows both on the dashboards.
# Host 2 reuses host 1's credentials unless you set IDRAC2_USERNAME/IDRAC2_PASSWORD:
IDRAC1_HOST=10.0.0.10 IDRAC2_HOST=10.0.0.11 \
  IDRAC1_USERNAME=monitor IDRAC1_PASSWORD='secret' docker compose up -d
```

Or copy `.env.example` to `.env`, fill it in, and just `docker compose up -d` (compose reads
`.env` natively; it is gitignored).

| Service | URL | Notes |
|---------|-----|-------|
| Grafana | http://localhost:3000 | `admin` / `admin` (or `GF_SECURITY_ADMIN_PASSWORD`) |
| Prometheus | http://localhost:9090 | scrape + alert rules |
| Exporter | http://localhost:9348/metrics | needs a reachable BMC |

To run the published image instead of building locally:

```sh
IDRAC1_PASSWORD='secret' docker compose -f docker-compose.ghcr.yml up -d
# pin a version: IDRAC_TAG=2.6.1 docker compose -f docker-compose.ghcr.yml up -d
```

## How it is wired

- **`config.yaml`** is the source of truth. It leaves `default_target` **empty** and lists the
  BMCs under `hosts:` with env-expanded keys (`${IDRAC1_HOST}`, `${IDRAC2_HOST}`) and their
  `${IDRAC*_USERNAME}` / `${IDRAC*_PASSWORD}` credentials, expanded at load time. The compose
  file passes those variables in. `.env` is nice; `config.yaml` is the way.
- Because `default_target` is empty, a bare `idrac_exporter:9348/metrics` runs **scrape-all**:
  the exporter collects every host under `hosts:` in one response, each series labeled
  `instance="<bmc>"` and `system="<bmc>"`, plus a per-host `idrac_up` gauge (`0` for an
  unreachable BMC — one bad host never fails the scrape). Prometheus scrapes that single URL
  with `honor_labels: true` so those labels survive.
- Prefer per-target scraping? Set a single host in `default_target`, or use the multi-target
  relabel pattern (commented in `prometheus.yml` and the
  [README](https://github.com/fjacquet/idrac_exporter#prometheus-configuration)), one entry per BMC.
- BMC Redfish scrapes are slow, so `scrape_interval`/`scrape_timeout` default to 60s/55s —
  tune them for your hardware.

## The `system` label

All dashboards use a single **`system`** template variable to identify the target host.
How it is populated depends on the export path:

- **Scrape-all quickstart (default):** the exporter injects `instance="<bmc>"` and
  `system="<bmc>"` on every series of a bare `/metrics`, and `prometheus.yml` scrapes it with
  `honor_labels: true` so those labels are kept as-is:

  ```yaml
  - job_name: idrac
    honor_labels: true
    static_configs:
      - targets: ["idrac_exporter:9348"]
  ```

- **Multi-target fleet (scrape path):** set `system` per BMC via a relabel rule that copies
  the `?target=` parameter:

  ```yaml
  - source_labels: [__param_target]
    target_label: system
  ```

- **OTLP/snapshot push path:** the exporter injects `system` itself — no Prometheus relabel
  needed. The label key is configurable via `otlp.identity_label` (default `system`), so the
  same dashboards work for both paths without modification.

## Notes

- The exporter container runs as a non-root user (uid 10001).
- Grafana provisioning lives in `grafana/provisioning/`; the bundled dashboards (including the
  PDU dashboard) are auto-provisioned and mounted read-only — see [Dashboards](../dashboards.md).
- **A reachable BMC is required** for metrics to appear. Without one, the stack still starts
  and provisions cleanly (datasource green, all dashboards loaded, alert rules parsed), but
  BMC-dependent panels will be empty and the exporter target will show as down in Prometheus.
