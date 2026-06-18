# Deployment topologies

There is **one binary**, and it runs in two shapes depending on where you put it and how you
point it at BMCs. The topology is a *configuration* choice — there is no "mode" flag and no
separate build.

| | **Sidecar** (per-node) | **External** (central) |
|---|---|---|
| Shape | One exporter next to each node (e.g. a DaemonSet) | One exporter (or a few) for the whole fleet |
| Hosts | Exactly one — the node's own BMC | Many BMCs |
| Mechanism | `default_target` set to the node's BMC address; scrape `/metrics` with no `?target=` | `hosts:` map + Prometheus `?target=` fan-out, **or** the OTLP push loop |

## Sidecar: scan the local host

Run one exporter per node and have it scrape only that node's BMC.

The catch: **a sidecar cannot discover its node's BMC on its own.** The BMC (iDRAC, iLO,
XClarity, …) is a *separate* network interface with its own IP address — it is not reachable
as `localhost` and nothing on the host advertises it to a neighbouring container. So "scan my
local host" means "scan the BMC address I was *given*", not "auto-detect the BMC". You supply
that address through `default_target`.

When `/metrics` is called without a `?target=` parameter, the exporter scrapes
`default_target`:

```yaml
# config.yml
default_target: ${NODE_BMC_ADDR}   # injected per-node (see below)
hosts:
  default:
    username: ${BMC_USERNAME}
    password: ${BMC_PASSWORD}
```

Prometheus then scrapes the sidecar with no target parameter:

```yaml
scrape_configs:
  - job_name: idrac
    static_configs:
      - targets: ["localhost:9348"]   # the sidecar on this node
```

In Kubernetes, inject `NODE_BMC_ADDR` per node — for example from a node label or annotation
copied into an environment variable, or from an external mapping of node → BMC. The exporter
treats it as an opaque address; how you resolve it is up to your platform.

!!! note "Auto-detecting the BMC is out of scope"
    This exporter never probes IPMI/`dmidecode`/the local management device to find a BMC.
    "Local" always means "the address you configured." If you want true auto-detection, that
    is a code-level feature, not a configuration of the current binary.

## External: many hosts

Yes — a central exporter handles many BMCs. There are two paths, and you can use either.

### Pull fan-out (primary)

List every BMC under `hosts:` and let **Prometheus** drive the fan-out, one scrape per target
via the `?target=` parameter and relabeling. The exporter keeps a per-target collector cache,
so each BMC gets its own Redfish session.

The exporter also serves `/discover`, a [Prometheus HTTP Service Discovery][http-sd] endpoint
that returns every configured host — point `http_sd_configs` at it instead of maintaining a
static target list.

See the [fleet relabel pattern in the Docker Compose guide](docker.md#how-it-is-wired) and the
[README Prometheus configuration][readme-prom] for the exact `relabel_configs`.

### OTLP push (optional)

Alternatively the **exporter** drives the loop: with the OTLP path enabled, a background loop
polls every entry in `hosts:` on `collection.interval` and pushes the metrics out over OTLP —
no Prometheus `?target=` scraping involved. It injects a per-host identity label and an
`idrac_up` gauge so the same dashboards work for both paths.

See [OTLP push](../otlp.md) for configuration. This path is off by default.

## Choosing

Pick **sidecar** when:

- you want per-node blast radius — one exporter failing affects one node;
- the exporter can reach its own node's BMC but not necessarily the whole BMC subnet;
- you already run per-node agents (DaemonSet) and want BMC health alongside them.

Pick **external** when:

- a central host (or a small set) has network reach to the BMC management subnet;
- you would rather manage one `hosts:` list than per-node injection;
- you want Prometheus (pull) or the exporter itself (OTLP push) to own the collection loop.

The two are not exclusive — a small fleet behind one exporter and a few sidecars for isolated
nodes is a perfectly valid mix, since it is all the same binary and the same metrics.

## See also

- [Configuration](../configuration.md) — `default_target`, `hosts:`, `auths:`
- [Usage](../usage.md) — flags and endpoints (`/metrics`, `/discover`, `/reset`, `/reload`)
- [OTLP push](../otlp.md) — the optional snapshot/push loop

[http-sd]: https://prometheus.io/docs/prometheus/latest/http_sd/
[readme-prom]: https://github.com/fjacquet/idrac_exporter#prometheus-configuration
