# Local / development image. The release image is built from Dockerfile.goreleaser.
# A Debian-based builder ships the CA bundle, so the runtime can COPY it instead
# of `apk add ca-certificates` (which fetches over TLS from the Alpine CDN and
# fails behind a corporate MITM proxy — the bare alpine image has no CA bundle
# yet to validate the proxy certificate).
FROM golang:1.26 AS builder

WORKDIR /app/src
COPY . .
RUN make cli

FROM alpine:3.23 AS container

WORKDIR /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /app/src/bin/idrac_exporter /app/bin/idrac_exporter
COPY default-config.yml /etc/prometheus/idrac.yml
COPY entrypoint.sh /app/entrypoint.sh

RUN adduser -D -u 10001 idrac && chown -R idrac /app
USER idrac

ENTRYPOINT ["/app/entrypoint.sh"]
