# syntax=docker/dockerfile:1.7
#
# Multi-arch without QEMU: every stage runs on the BUILD platform
# (--platform=$BUILDPLATFORM) and only produces TARGET-arch output. Go
# cross-compiles natively (pure Go: modernc sqlite, no cgo); the pkl CLI is
# downloaded for the target arch; the web build is arch-neutral.
#
#   docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/bmurray/alexandria:dev .
ARG GO_VERSION=1.26
ARG PKL_VERSION=0.32.1

# ---- web: Vite build with Bun -> web/dist ----------------------------------
FROM --platform=$BUILDPLATFORM oven/bun:1 AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

# ---- go: cross-compile with the web build embedded -------------------------
# internal/gen (Pkl codegen) is committed, so no pkl is needed here. The
# `web` package embeds all:dist; without -tags dev the directory must exist
# before `go build`, hence the COPY from the web stage.
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS build
ARG VERSION=dev
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/alexandria ./cmd/alexandria

# ---- pkl: the CLI for the TARGET arch, fetched on the BUILD platform --------
# CHAD-DEPLOY mounts system.pkl / appconfig.pkl as ConfigMaps evaluated at
# startup (pkl-go spawns `pkl server`), so the image carries the pkl CLI.
# The native pkl binary is glibc-linked and also needs libz.so.1, which
# neither distroless/base nor distroless/cc ships (verified: cc-debian12
# fails with "libz.so.1: cannot open shared object file"). Fetch the TARGET
# arch's zlib1g .deb via apt's foreign-architecture support and unpack it;
# dpkg-deb -x only extracts, nothing target-arch is executed.
FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS pkl
ARG PKL_VERSION
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates \
    && case "${TARGETARCH}" in amd64) a=linux-amd64 ;; arm64) a=linux-aarch64 ;; *) echo "unsupported arch ${TARGETARCH}" && exit 1 ;; esac \
    && curl -fsSL -o /pkl "https://github.com/apple/pkl/releases/download/${PKL_VERSION}/pkl-${a}" && chmod +x /pkl \
    && dpkg --add-architecture "${TARGETARCH}" && apt-get update \
    && mkdir -p /deb /zlib && cd /deb && apt-get download "zlib1g:${TARGETARCH}" \
    && dpkg-deb -x /deb/zlib1g_*.deb /zlib \
    # bookworm's deb installs under /lib/<triplet>; distroless is usr-merged,
    # so present it as /usr/lib for a single COPY below.
    && if [ -d /zlib/lib ]; then mkdir -p /zlib/usr && mv /zlib/lib /zlib/usr/lib; fi \
    && ls /zlib/usr/lib/*/libz.so.1 \
    && rm -rf /var/lib/apt/lists/*

# ---- runtime ---------------------------------------------------------------
FROM gcr.io/distroless/base-debian12:nonroot
COPY --from=pkl /zlib/usr/lib/ /usr/lib/
COPY --from=pkl /pkl /usr/local/bin/pkl
COPY --from=build /out/alexandria /usr/local/bin/alexandria
# CONFIG_FILE is the only environment variable; the Deployment projects
# system.pkl + appconfig.pkl into /config.
ENV CONFIG_FILE=/config/appconfig.pkl
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/alexandria"]
