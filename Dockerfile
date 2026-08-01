# Local / development image. The release image is built from Dockerfile.goreleaser.
# A Debian-based builder ships the CA bundle, so the runtime can COPY it instead
# of `apk add ca-certificates` (which fetches over TLS from the Alpine CDN and
# fails behind a corporate MITM proxy — the bare alpine image has no CA bundle
# yet to validate the proxy certificate).
FROM golang:1.26 AS builder

WORKDIR /app/src
COPY . .
RUN make cli

FROM alpine:latest AS container

WORKDIR /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /app/src/bin/idrac_exporter /app/bin/idrac_exporter
COPY default-config.yml /etc/prometheus/idrac.yml
COPY entrypoint.sh /app/entrypoint.sh

RUN adduser -D -u 10001 idrac && chown -R idrac /app
USER idrac

# 127.0.0.1, not localhost: busybox wget tries ::1 first and the exporter binds
# IPv4 only, so a localhost check fails at runtime while passing every static
# check. Timeout matches the compose healthcheck (5s) exactly.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --quiet --tries=1 --spider http://127.0.0.1:9348/livez || exit 1

ENTRYPOINT ["/app/entrypoint.sh"]
