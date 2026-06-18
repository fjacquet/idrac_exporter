# Deployment topologies documentation — design

Date: 2026-06-18
Status: Approved

## Problem

Operators ask: *"If I run this as a per-node sidecar, does it scan its local host? If I
run it as an external/central exporter, can I point it at multiple hosts?"*

The capabilities to do both already exist, but they are scattered across `usage.md`,
`deployment/docker.md`, and `otlp.md`, and never framed as the two deployment shapes
operators actually reason about. One subtlety is also undocumented and easy to get wrong:
a sidecar does **not** auto-discover its node's BMC.

## Goal

Clarify and document — no behavioral change. Produce a single page that frames the two
topologies and answers the question directly. The only code change is one `mkdocs.yml` nav
line.

Explicitly **out of scope**: any "mode" flag, and any BMC auto-detection for sidecars
(that would be a separate, code-level feature — noted here only as a non-goal).

## Key facts (verified against the code)

- **`default_target`** (`internal/config/model.go`, `cmd/idrac_exporter/handler.go`): when
  `/metrics` is called with no `?target=`, the exporter scrapes this single host. This is the
  sidecar mechanism.
- **A sidecar has no idea what its node's BMC address is.** The BMC (iDRAC/iLO/…) is a
  separate NIC with its own IP. "Local" means *configured*, not *discovered*. The operator
  supplies the address via `default_target` (env / Kubernetes downward API).
- **External multi-host, pull path**: the `hosts:` map plus Prometheus `?target=` relabeling;
  `/discover` (`internal/config/discover.go`) exposes the host keys as Prometheus HTTP SD.
- **External multi-host, push path**: the optional OTLP loop (`internal/collector/loop.go`)
  polls every `hosts:` entry (except the `default` credential fallback) on
  `collection.interval` and pushes via OTLP.
- It is one binary in all cases; topology is a configuration choice, not a build or flag.

## Design

New page `docs/deployment/topologies.md`, added to the `Deployment` nav section in
`mkdocs.yml` directly after Docker Compose:

```yaml
  - Deployment:
      - Docker Compose: deployment/docker.md
      - Topologies: deployment/topologies.md
```

### Page outline

1. **Intro** — both shapes are the same binary; topology is a config choice, no mode flag.
2. **Sidecar: scan the local host** — set `default_target` to the node's BMC address (env /
   downward API), scrape `/metrics` bare. State plainly that the BMC is a separate NIC, so
   "local" means *configured*, not *auto-detected*; auto-detect is a non-goal of this page.
3. **External: many hosts** — the direct "yes." Two sub-paths, each kept short and linked
   rather than re-explained:
   - *Pull fan-out* — `hosts:` + `?target=` relabeling + `/discover` SD → link to the
     `docker.md` fleet pattern / README Prometheus configuration.
   - *OTLP push* — exporter polls every `hosts:` entry on `collection.interval` → link to
     `otlp.md`.
4. **Choosing** — short "pick this when…" guidance (blast radius, network reach to the BMC
   subnet, who drives the loop, metric cardinality).
5. **See also** — `configuration.md`, `usage.md`, `otlp.md`.

### Comparison table (page centerpiece)

| | Sidecar (per-node) | External (central) |
|---|---|---|
| Shape | One exporter next to each node (DaemonSet) | One exporter (or a few) for the fleet |
| Hosts | Exactly one — the node's own BMC | Many BMCs |
| Mechanism | `default_target` = node's BMC addr; scrape `/metrics` bare | `hosts:` + `?target=` fan-out, **or** the OTLP push loop |

### No-duplication rule

The page *owns* only the sidecar-vs-external framing and the sidecar `default_target` recipe.
Relabeling, OTLP, and `--once` details stay in their existing pages and are linked.

## Testing / verification

Documentation only. Verify the MkDocs build succeeds and nav renders
(`mkdocs build --strict` if the toolchain is available locally; otherwise the `docs.yml`
workflow validates on push). Check internal links resolve.
