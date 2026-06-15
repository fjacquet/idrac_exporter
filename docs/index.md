# iDRAC Exporter

A Redfish exporter (Dell iDRAC, HPE iLO, Lenovo XClarity, Supermicro, and other BMCs) for
[Prometheus](https://prometheus.io). Metrics are collected on demand via the Redfish API —
Prometheus passes the BMC address as the `target` query parameter:

```text
http://localhost:9348/metrics?target=192.168.1.1
```

## Status

This site is being built out as the exporter is brought into the exporter-standards family.
See the [Architecture Decisions](adr/index.md) for the decisions driving that work. A
metrics catalog, dashboards guide, and deployment docs are added in later phases.

## Endpoints

| Endpoint    | Parameters | Description |
| ----------- | ---------- | ----------- |
| `/metrics`  | `target`   | Metrics for the specified target |
| `/discover` |            | Prometheus HTTP service discovery |
| `/reset`    | `target`   | Reset internal state for a target |
| `/reload`   |            | Reload the configuration file |
| `/health`   |            | Liveness probe (returns 200) |

For the full metric list and configuration reference, see the project `README.md` and
`sample-config.yml`.
