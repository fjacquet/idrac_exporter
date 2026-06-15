# Docker Compose quickstart

A one-command demo stack: the exporter, Prometheus (with starter alert rules), and
Grafana (with the datasource and dashboards auto-provisioned).

## Run it

```sh
# Point at a BMC and start the stack (build the image locally):
IDRAC1_HOST=10.0.0.10 IDRAC1_USERNAME=monitor IDRAC1_PASSWORD='secret' docker compose up -d
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

- **`config.yaml`** is the source of truth. It sets `default_target: ${IDRAC1_HOST}` and the
  default credentials from `${IDRAC1_USERNAME}` / `${IDRAC1_PASSWORD}`, expanded from the
  environment at load time. The compose file passes those variables in (with literal
  defaults for the single-target quickstart). `.env` is nice; `config.yaml` is the way.
- Because `default_target` is set, Prometheus scrapes `idrac_exporter:9348/metrics` directly
  — no `?target=` needed. For a **fleet**, drop `default_target` and use the multi-target
  relabel pattern from the [README](https://github.com/fjacquet/idrac_exporter#prometheus-configuration),
  one entry per BMC.
- BMC Redfish scrapes are slow, so `scrape_interval`/`scrape_timeout` default to 60s/55s —
  tune them for your hardware.

## Notes

- The exporter container runs as a non-root user (uid 10001).
- Grafana provisioning lives in `grafana/provisioning/`; the bundled dashboards are mounted
  read-only — see [Dashboards](../dashboards.md).
