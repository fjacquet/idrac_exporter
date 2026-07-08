# Demo Scrape-All Wiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewire the one-command demo stack so a bare `/metrics` collects **all** configured BMCs (scrape-all), and update the quickstart docs to match — so the Grafana dashboards show every host out of the box.

**Architecture:** Config + docs only, no Go code. `config.yaml` leaves `default_target` empty and lists two env-keyed hosts; `prometheus.yml` scrapes bare `/metrics` with `honor_labels: true` and drops the static `system` label; both compose files gain `IDRAC2_*` env; the README / `docs/deployment/docker.md` / `docs/dashboards.md` describe the scrape-all flow.

**Tech Stack:** YAML (exporter config, Prometheus config, Docker Compose), Markdown (MkDocs). Verification via `docker compose config`, `python3` YAML asserts, `mkdocs build --strict`.

**Spec:** `docs/superpowers/specs/2026-07-08-demo-scrape-all-wiring-design.md`

## Global Constraints

- **Docs + config only.** Do NOT modify any `.go` file or the exporter's behavior. No Grafana dashboard JSON edits (out of scope; dashboard-query bugs are a separate follow-up).
- The demo default is **scrape-all**: `config.yaml` ships `default_target: ""`.
- Injected identity labels are `instance` and `system` (both = the BMC host), supplied by the exporter; `prometheus.yml` must set `honor_labels: true` and must NOT attach a static `system` label.
- `config.yaml` loads via `os.ExpandEnv` over the whole file, so `${VAR}` expands in **both keys and values** — env-keyed host entries are intentional.
- Second host's credentials default to the first host's (`${IDRAC2_USERNAME:-${IDRAC1_USERNAME:-root}}`); Docker Compose supports this nested-default form (verified).
- Keep the per-target `?target=` relabel pattern documented as the alternative (in `prometheus.yml` comments and docs). `default_target` stays deprecated-but-honored.
- Released as `v1.1.1` after merge.
- Tools available in this environment: `docker`, `mkdocs`, `promtool`, `python3`. No `.go`/CI test references these files, so `make ci` is unaffected (run once as a sanity gate).

---

### Task 1: Rewire the runnable demo (config.yaml, both compose files, .env.example)

**Files:**
- Modify: `config.yaml` (full rewrite — currently 20 lines)
- Modify: `docker-compose.yml` (header comment lines 1-6; `idrac_exporter` env block lines 19-22)
- Modify: `docker-compose.ghcr.yml` (header comment lines 1-6; `idrac_exporter` env block lines 15-18)
- Modify: `.env.example`

**Interfaces:**
- Produces: `config.yaml` with `default_target: ""` and two hosts keyed `${IDRAC1_HOST}` / `${IDRAC2_HOST}`; compose services exporting `IDRAC2_HOST`/`IDRAC2_USERNAME`/`IDRAC2_PASSWORD`. Task 2 (`prometheus.yml`) and Task 3 (docs) rely on this being the scrape-all default.

- [ ] **Step 1: Rewrite `config.yaml`**

Replace the entire file with:
```yaml
# Source-of-truth config for the docker-compose demo stack.
# Secrets come from the environment (compose sets IDRAC1_*/IDRAC2_*; .env is gitignored).
# ${VAR} references are expanded at load time — including in the host keys below.
address: 0.0.0.0
port: 9348
timeout: 60 # BMC Redfish calls can be slow

# default_target is left EMPTY on purpose. With it empty, a bare /metrics (no
# ?target=) runs "scrape-all": the exporter collects every host under `hosts:`
# in one response, each series labeled instance="<bmc>" and system="<bmc>", plus
# a per-host idrac_up gauge. Set a single host here only for the legacy
# single-target behavior (it is deprecated but still honored).
default_target: ""

# Demo hosts. Keys are env-expanded, so ${IDRAC1_HOST}/${IDRAC2_HOST} become the
# real BMC addresses at load time. If you only have one BMC, delete the second
# block (otherwise it just reports idrac_up=0 — a harmless "down" series).
hosts:
  ${IDRAC1_HOST}:
    username: ${IDRAC1_USERNAME}
    password: ${IDRAC1_PASSWORD}
  ${IDRAC2_HOST}:
    username: ${IDRAC2_USERNAME}
    password: ${IDRAC2_PASSWORD}

metrics:
  all: true
```

- [ ] **Step 2: Add `IDRAC2_*` env + update header in `docker-compose.yml`**

Replace the header comment (lines 1-6, from `# One-command demo stack:` through `# config.yaml is the source of truth; the IDRAC1_* env vars feed its ${...} references.`) with:
```yaml
# One-command demo stack: exporter (built locally) + Prometheus + Grafana.
# Runs in scrape-all mode: config.yaml leaves default_target empty, so a bare
# /metrics returns every configured host and the dashboards show them all.
#
#   # one BMC:
#   IDRAC1_HOST=10.0.0.10 IDRAC1_USERNAME=monitor IDRAC1_PASSWORD='...' docker compose up -d
#   # two BMCs (host 2 reuses host 1 creds unless IDRAC2_USERNAME/PASSWORD are set):
#   IDRAC1_HOST=10.0.0.10 IDRAC2_HOST=10.0.0.11 IDRAC1_USERNAME=monitor IDRAC1_PASSWORD='...' docker compose up -d
#
# Then: Grafana http://localhost:3000 (admin/admin), Prometheus http://localhost:9090.
# config.yaml is the source of truth; the IDRAC1_*/IDRAC2_* env vars feed its ${...} references.
```

Replace the `idrac_exporter` service `environment:` block (lines 19-22):
```yaml
    environment:
      - IDRAC1_HOST=${IDRAC1_HOST:-192.168.1.1}
      - IDRAC1_USERNAME=${IDRAC1_USERNAME:-root}
      - IDRAC1_PASSWORD=${IDRAC1_PASSWORD:-}
```
with:
```yaml
    environment:
      - IDRAC1_HOST=${IDRAC1_HOST:-192.168.1.1}
      - IDRAC1_USERNAME=${IDRAC1_USERNAME:-root}
      - IDRAC1_PASSWORD=${IDRAC1_PASSWORD:-}
      # Second demo host. Its credentials default to the first host's unless set.
      - IDRAC2_HOST=${IDRAC2_HOST:-192.168.1.2}
      - IDRAC2_USERNAME=${IDRAC2_USERNAME:-${IDRAC1_USERNAME:-root}}
      - IDRAC2_PASSWORD=${IDRAC2_PASSWORD:-${IDRAC1_PASSWORD:-}}
```

- [ ] **Step 3: Same env + header update in `docker-compose.ghcr.yml`**

Replace the header comment (lines 1-6, from `# Pull-based stack:` through the second `#   IDRAC_TAG=2.6.1 ...` line) with:
```yaml
# Pull-based stack: runs the published GHCR image instead of building locally.
# Runs in scrape-all mode (config.yaml leaves default_target empty), so a bare
# /metrics returns every configured host and the dashboards show them all.
#
#   IDRAC1_PASSWORD='...' docker compose -f docker-compose.ghcr.yml up -d
#   # two BMCs: IDRAC1_HOST=10.0.0.10 IDRAC2_HOST=10.0.0.11 IDRAC1_PASSWORD='...' docker compose -f docker-compose.ghcr.yml up -d
#
# Pin a version with IDRAC_TAG (defaults to :latest):
#   IDRAC_TAG=2.6.1 IDRAC1_PASSWORD='...' docker compose -f docker-compose.ghcr.yml up -d
```

Replace the `idrac_exporter` service `environment:` block (lines 15-18):
```yaml
    environment:
      - IDRAC1_HOST=${IDRAC1_HOST:-192.168.1.1}
      - IDRAC1_USERNAME=${IDRAC1_USERNAME:-root}
      - IDRAC1_PASSWORD=${IDRAC1_PASSWORD:-}
```
with:
```yaml
    environment:
      - IDRAC1_HOST=${IDRAC1_HOST:-192.168.1.1}
      - IDRAC1_USERNAME=${IDRAC1_USERNAME:-root}
      - IDRAC1_PASSWORD=${IDRAC1_PASSWORD:-}
      # Second demo host. Its credentials default to the first host's unless set.
      - IDRAC2_HOST=${IDRAC2_HOST:-192.168.1.2}
      - IDRAC2_USERNAME=${IDRAC2_USERNAME:-${IDRAC1_USERNAME:-root}}
      - IDRAC2_PASSWORD=${IDRAC2_PASSWORD:-${IDRAC1_PASSWORD:-}}
```

- [ ] **Step 4: Update `.env.example`**

Replace the entire file with:
```sh
# Copy to .env and fill in — `.env` is gitignored, never commit real secrets.
# Compose reads .env natively; config.yaml references these as ${IDRAC1_*}/${IDRAC2_*}.

# First BMC (required). A dedicated read-only BMC user is recommended.
IDRAC1_HOST=192.168.1.1
IDRAC1_USERNAME=root
IDRAC1_PASSWORD=changeme

# Second BMC (optional). The demo runs in scrape-all mode and shows every host.
# Credentials default to the IDRAC1_* values unless set here. If you only have one
# BMC, remove IDRAC2_HOST (or leave it — it will just report idrac_up=0).
IDRAC2_HOST=192.168.1.2
#IDRAC2_USERNAME=root
#IDRAC2_PASSWORD=changeme

# Grafana admin password for the local test stack.
GF_SECURITY_ADMIN_PASSWORD=admin
```

- [ ] **Step 5: Verify config.yaml env-expands to two hosts + empty default_target**

Run:
```bash
IDRAC1_HOST=10.0.0.1 IDRAC1_USERNAME=u1 IDRAC1_PASSWORD=p1 \
IDRAC2_HOST=10.0.0.2 IDRAC2_USERNAME=u2 IDRAC2_PASSWORD=p2 \
python3 -c '
import os, yaml
c = yaml.safe_load(os.path.expandvars(open("config.yaml").read()))
assert c["default_target"] == "", repr(c["default_target"])
h = c["hosts"]
assert set(h) == {"10.0.0.1", "10.0.0.2"}, set(h)
assert "default" not in h, "no bare default: block expected"
for k, v in h.items():
    assert v["username"] and v["password"], (k, v)
print("config.yaml OK:", sorted(h))
'
```
Expected: `config.yaml OK: ['10.0.0.1', '10.0.0.2']` (exit 0, no AssertionError).

- [ ] **Step 6: Verify both compose files interpolate cleanly (two hosts, creds resolved)**

Run:
```bash
IDRAC1_HOST=10.0.0.1 IDRAC1_USERNAME=alice IDRAC1_PASSWORD=secret IDRAC2_HOST=10.0.0.2 \
  docker compose config 2>/dev/null | grep -E 'IDRAC[12]_(HOST|USERNAME|PASSWORD)='
echo "--- ghcr ---"
IDRAC1_HOST=10.0.0.1 IDRAC1_USERNAME=alice IDRAC1_PASSWORD=secret IDRAC2_HOST=10.0.0.2 \
  docker compose -f docker-compose.ghcr.yml config >/dev/null 2>&1 && echo "ghcr compose OK"
```
Expected: the grep prints `IDRAC1_HOST=10.0.0.1`, `IDRAC2_HOST=10.0.0.2`, and `IDRAC2_USERNAME=alice` / `IDRAC2_PASSWORD=secret` (host 2 inherited host 1's creds via nested default); `ghcr compose OK` prints (exit 0). If either `docker compose config` errors, the nested defaults are unsupported — fall back to flat `IDRAC2_USERNAME=${IDRAC2_USERNAME:-root}` / `IDRAC2_PASSWORD=${IDRAC2_PASSWORD:-}` in both compose files and re-run.

- [ ] **Step 7: Commit**

```bash
git add config.yaml docker-compose.yml docker-compose.ghcr.yml .env.example
git commit -m "feat(demo): wire compose stack for scrape-all (two hosts, empty default_target)

config.yaml leaves default_target empty and lists two env-keyed hosts so a bare
/metrics collects all BMCs. Both compose files gain IDRAC2_* env (creds default
to IDRAC1_*). .env.example documents the optional second host.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Prometheus scrape-all config

**Files:**
- Modify: `prometheus.yml` (full rewrite — currently 22 lines)

**Interfaces:**
- Consumes: the scrape-all default from Task 1 (`config.yaml` with empty `default_target`).
- Produces: an `idrac` scrape job with `honor_labels: true`, bare `/metrics`, and NO static `system` label.

- [ ] **Step 1: Rewrite `prometheus.yml`**

Replace the entire file with:
```yaml
global:
  # BMC Redfish scrapes are slow; give them headroom. Tune per hardware.
  scrape_interval: 60s
  scrape_timeout: 55s

rule_files:
  - /etc/prometheus/idrac.rules.yml

scrape_configs:
  # Scrape-all quickstart: config.yaml leaves `default_target` empty, so a bare
  # /metrics collects EVERY configured host in one scrape, each series labeled
  # instance="<bmc>" and system="<bmc>". honor_labels keeps those exporter-set
  # labels (without it, Prometheus would overwrite `instance` with the exporter
  # address). The dashboards' `system` variable then lists every host.
  - job_name: idrac
    honor_labels: true
    metrics_path: /metrics
    static_configs:
      - targets: ["idrac_exporter:9348"]
    # Prefer per-target scraping (one entry per BMC via /metrics?target=<bmc>)?
    # Set a single host in config.yaml's default_target, OR replace the
    # static_configs above with the multi-target relabel pattern:
    #   static_configs:
    #     - targets: ["10.0.0.10", "10.0.0.11"]
    #   relabel_configs:
    #     - source_labels: [__address__]
    #       target_label: __param_target
    #     - source_labels: [__param_target]
    #       target_label: system
    #     - source_labels: [__param_target]
    #       target_label: instance
    #     - target_label: __address__
    #       replacement: idrac_exporter:9348
```

- [ ] **Step 2: Verify the scrape job is scrape-all shaped**

Run:
```bash
python3 -c '
import yaml
c = yaml.safe_load(open("prometheus.yml").read())
job = next(j for j in c["scrape_configs"] if j["job_name"] == "idrac")
assert job.get("honor_labels") is True, job
assert job["metrics_path"] == "/metrics", job
for sc in job.get("static_configs", []):
    assert "system" not in sc.get("labels", {}), ("static system label still present", sc)
assert "demo-bmc" not in yaml.dump(c), "demo-bmc must not appear in the active config"
print("prometheus.yml OK: honor_labels set, no static system label")
'
```
Expected: `prometheus.yml OK: honor_labels set, no static system label` (exit 0). (The `yaml.dump` check confirms `demo-bmc` is gone from parsed config — comments are ignored by the parser, so the relabel example in comments does not trip it.)

- [ ] **Step 3: Commit**

```bash
git add prometheus.yml
git commit -m "feat(demo): scrape bare /metrics with honor_labels, drop static system label

The exporter now supplies per-host instance/system labels on scrape-all; drop
the static system=demo-bmc so those survive (honor_labels: true). Keep the
per-target relabel pattern documented in comments.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Update the quickstart docs

**Files:**
- Modify: `README.md` (Docker Compose quickstart, lines 62-70)
- Modify: `docs/deployment/docker.md` (Run it / How it is wired / The `system` label sections)
- Modify: `docs/dashboards.md` (the on-demand scrape-path bullet, lines 37-39)

**Interfaces:**
- Consumes: the scrape-all wiring from Tasks 1-2 (empty `default_target`, `IDRAC2_*`, `honor_labels`).

- [ ] **Step 1: Update the README quickstart**

In `README.md`, replace the block from `### Docker Compose quickstart` through the `Full walkthrough:` line (lines 62-70) with:
````markdown
### Docker Compose quickstart

A one-command demo stack — exporter, Prometheus (with alert rules) and Grafana (datasource + dashboards auto-provisioned). It runs in **scrape-all** mode: `config.yaml` leaves `default_target` empty, so a bare `/metrics` returns every configured host and Prometheus scrapes it with `honor_labels: true`. Each series is labeled `system="<bmc>"`, so the dashboards' `System` selector lists all hosts.

```sh
# One BMC:
IDRAC1_HOST=10.0.0.10 IDRAC1_USERNAME=monitor IDRAC1_PASSWORD='secret' docker compose up -d

# Two BMCs (host 2 reuses host 1's credentials unless you set IDRAC2_USERNAME/IDRAC2_PASSWORD):
IDRAC1_HOST=10.0.0.10 IDRAC2_HOST=10.0.0.11 \
  IDRAC1_USERNAME=monitor IDRAC1_PASSWORD='secret' docker compose up -d
```

Grafana is then on <http://localhost:3000> (`admin`/`admin`), Prometheus on <http://localhost:9090>. At least one reachable BMC is required. Full walkthrough: [Docker Compose](https://fjacquet.github.io/idrac_exporter/deployment/docker/).
````

- [ ] **Step 2: Update `docs/deployment/docker.md` — "Run it"**

Replace the first code block under `## Run it` (lines 8-11):
````markdown
```sh
# Point at a BMC and start the stack (build the image locally):
IDRAC1_HOST=10.0.0.10 IDRAC1_USERNAME=monitor IDRAC1_PASSWORD='secret' docker compose up -d
```
````
with:
````markdown
```sh
# One BMC (build the image locally):
IDRAC1_HOST=10.0.0.10 IDRAC1_USERNAME=monitor IDRAC1_PASSWORD='secret' docker compose up -d

# Two BMCs — the stack runs in scrape-all mode and shows both on the dashboards.
# Host 2 reuses host 1's credentials unless you set IDRAC2_USERNAME/IDRAC2_PASSWORD:
IDRAC1_HOST=10.0.0.10 IDRAC2_HOST=10.0.0.11 \
  IDRAC1_USERNAME=monitor IDRAC1_PASSWORD='secret' docker compose up -d
```
````

- [ ] **Step 3: Update `docs/deployment/docker.md` — "How it is wired"**

Replace the three bullets under `## How it is wired` (the `- **`config.yaml`** ...`, `- Because `default_target` is set ...`, and `- BMC Redfish scrapes are slow ...` bullets, lines 31-40) with:
```markdown
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
```

- [ ] **Step 4: Update `docs/deployment/docker.md` — "The `system` label"**

Replace the first bullet under `## The `system` label` (the `- **Single-target quickstart (scrape path):** ...` bullet and its YAML block, lines 47-54) with:
````markdown
- **Scrape-all quickstart (default):** the exporter injects `instance="<bmc>"` and
  `system="<bmc>"` on every series of a bare `/metrics`, and `prometheus.yml` scrapes it with
  `honor_labels: true` so those labels are kept as-is:

  ```yaml
  - job_name: idrac
    honor_labels: true
    static_configs:
      - targets: ["idrac_exporter:9348"]
  ```
````

- [ ] **Step 5: Update `docs/dashboards.md` — on-demand scrape bullet**

Replace the on-demand scrape-path bullet (lines 37-39):
```markdown
  - **On-demand scrape path:** attach a `system` label to each Prometheus target via a static
    label or relabel rule — see the [Docker Compose quickstart](deployment/docker.md) for
    details.
```
with:
```markdown
  - **On-demand scrape-all path:** the exporter injects `system` (and `instance`) per host on
    a bare `/metrics`; Prometheus keeps them with `honor_labels: true`. For per-target
    (`?target=`) scraping, set `system` via a relabel rule instead — see the
    [Docker Compose quickstart](deployment/docker.md).
```

- [ ] **Step 6: Verify docs build and contain the new content**

Run:
```bash
mkdocs build --strict 2>&1 | tail -5
grep -c "scrape-all" README.md docs/deployment/docker.md docs/dashboards.md
grep -q "IDRAC2_HOST" README.md docs/deployment/docker.md && echo "IDRAC2_HOST documented"
! grep -q "demo-bmc" docs/deployment/docker.md && echo "demo-bmc removed from docker.md"
```
Expected: `mkdocs build --strict` finishes with `INFO` output and no `WARNING`/`ERROR` (exit 0); each grepped file reports ≥1 `scrape-all`; `IDRAC2_HOST documented` and `demo-bmc removed from docker.md` both print.

- [ ] **Step 7: Commit**

```bash
git add README.md docs/deployment/docker.md docs/dashboards.md
git commit -m "docs: document the scrape-all demo (two hosts, honor_labels)

Update the README quickstart, Docker Compose deployment page, and dashboards
conventions to describe scrape-all: empty default_target, IDRAC2_* second host,
honor_labels keeping per-host instance/system. Keep per-target relabel as the alternative.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Final gate

- [ ] **Step 1: Sanity — the Go build/test suite is unaffected**

Run: `make ci`
Expected: PASS (no Go changed; confirms nothing else broke). `fmt-check`, `vet`, `golangci-lint`, `go test -race`, `govulncheck` all succeed.

- [ ] **Step 2: Re-verify the whole demo renders end-to-end (no live BMC needed)**

Run:
```bash
IDRAC1_HOST=10.0.0.1 IDRAC1_USERNAME=alice IDRAC1_PASSWORD=secret IDRAC2_HOST=10.0.0.2 \
  docker compose config >/dev/null 2>&1 && echo "compose OK"
python3 -c '
import os, yaml
c = yaml.safe_load(os.path.expandvars(open("config.yaml").read().replace("${IDRAC1_HOST}","10.0.0.1").replace("${IDRAC2_HOST}","10.0.0.2")))
assert c["default_target"] == ""
assert len(c["hosts"]) == 2
p = yaml.safe_load(open("prometheus.yml").read())
assert next(j for j in p["scrape_configs"] if j["job_name"]=="idrac")["honor_labels"] is True
print("end-to-end config OK")
'
```
Expected: `compose OK` and `end-to-end config OK` (exit 0).

- [ ] **Step 3: Manual note (post-merge, on a live stack)**

`docker compose up`, open Grafana → confirm the `System` variable lists both BMCs and panels populate per host. If the Memory panel still shows "No data" with a correctly-labeled multi-host scrape, open a **separate** dashboard-query follow-up (out of scope here per the spec).
