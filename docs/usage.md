# Usage

Run the exporter with a configuration file:

```sh
idrac_exporter --config /etc/prometheus/idrac.yml
```

It then serves `/metrics?target=<bmc>` on the configured port (default `9348`).

## Command-line flags

| Flag             | Description                                                           |
| ---------------- | -------------------------------------------------------------------- |
| `--config`       | Path to the configuration file (default `/etc/prometheus/idrac.yml`) |
| `--config-watch` | Watch the configuration file and hot-reload on change                |
| `--verbose`      | More verbose logging                                                  |
| `--debug`        | Dump raw Redfish JSON responses (implies `--verbose`)                |
| `--trace`        | Log each Redfish request — method, path, status — without credentials |
| `--once`         | Collect every configured host once, print the exposition, and exit   |
| `--version`      | Print version and exit                                                |

## Validating against a BMC

You do not need Prometheus to check that the exporter talks to a BMC correctly.

`--once` collects every configured host a single time and writes the metrics exposition to
stdout, sorted by target so the output is stable and diffable, then exits:

```sh
idrac_exporter --config config.yml --once
```

This is the quickest way to confirm credentials, reachability, and which metric groups a
particular BMC exposes.

## Tracing requests

When a BMC returns unexpected data, trace what the exporter is doing:

```sh
idrac_exporter --config config.yml --once --trace   # request-level: method, path, status
idrac_exporter --config config.yml --once --debug   # full raw JSON of every response
```

`--trace` logs every Redfish request (method, path, status) and is **token-safe** — it never
logs credentials or session tokens. `--debug` additionally dumps the raw JSON body of each
response (and implies `--verbose`), which is useful when a field is parsed as absent and you
need to see the exact shape the BMC returned.

## Endpoints

| Endpoint    | Parameters | Description                                   |
| ----------- | ---------- | --------------------------------------------- |
| `/metrics`  | `target`   | Metrics for the specified target              |
| `/reset`    | `target`   | Reset internal state for the specified target |
| `/reload`   |            | Reload the configuration file                 |
| `/discover` |            | Prometheus HTTP Service Discovery             |
| `/health`   |            | Liveness probe (returns HTTP 200)             |
| `/`         |            | Landing page                                  |

`/reset?target=` drops a target's cached collector, forcing fresh Redfish discovery on the
next scrape. `/reload` re-reads the configuration file and resets only the hosts whose
credentials changed.
