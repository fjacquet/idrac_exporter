# Installation

## Download a release

Pre-built binaries are attached to every
[GitHub release](https://github.com/fjacquet/idrac_exporter/releases) for **Linux, macOS and
Windows** (both `amd64` and `arm64`), packaged as `.tar.gz` archives (`.zip` on Windows)
alongside `checksums.txt` and a CycloneDX SBOM.

## Homebrew (macOS)

```sh
brew install fjacquet/tap/idrac_exporter
```

The macOS cask is published to the
[`fjacquet/homebrew-tap`](https://github.com/fjacquet/homebrew-tap) tap on each release.

## Build from source

The exporter is written in [Go](https://golang.org):

```sh
git clone https://github.com/fjacquet/idrac_exporter.git
cd idrac_exporter
make cli            # builds bin/idrac_exporter (CGO off, with version ldflags)
```

## Docker

Pre-built, multi-arch images are published on the GitHub Container Registry:

```sh
docker pull ghcr.io/fjacquet/idrac_exporter
```

To build the image locally instead:

```sh
docker build -t idrac_exporter .
```

The image runs as a non-root user. Set the listen address to `0.0.0.0` in the configuration
when running inside a container, otherwise the port is not reachable from outside.

For a complete demo environment (exporter + Prometheus + Grafana), see the
[Docker Compose quickstart](deployment/docker.md).

## Helm

A [Helm](https://helm.sh/docs/) chart is published as an OCI artifact on GHCR:

```sh
helm install idrac-exporter oci://ghcr.io/fjacquet/charts/idrac-exporter
```

The chart pulls the container image from GHCR automatically.
