# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build
WORKDIR /src
ENV GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/flagd/ ./cmd/flagd/
COPY internal/ ./internal/
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -mod=readonly -trimpath -ldflags='-s -w' -o /out/flagd ./cmd/flagd
# Match private bind-mounted credential files when building for local Compose.
ARG RUNTIME_UID=65532
ARG RUNTIME_GID=65532
RUN test "$RUNTIME_UID" -gt 0 && test "$RUNTIME_GID" -gt 0 \
    && mkdir -p /out/data && chmod 0700 /out/data

FROM scratch
ARG RUNTIME_UID=65532
ARG RUNTIME_GID=65532
LABEL org.opencontainers.image.title="flagd" \
      org.opencontainers.image.description="Environment-scoped feature flag service" \
      org.opencontainers.image.source="https://github.com/ahmedr1zwan/flagctl"
COPY --from=build --chmod=0555 /out/flagd /flagd
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=${RUNTIME_UID}:${RUNTIME_GID} /out/data /var/lib/flagctl
USER ${RUNTIME_UID}:${RUNTIME_GID}
WORKDIR /var/lib/flagctl
EXPOSE 8443
ENTRYPOINT ["/flagd"]
# The image alone retains the safe loopback default. Compose enables TLS/network access.
CMD ["--data-dir", "/var/lib/flagctl/data"]
