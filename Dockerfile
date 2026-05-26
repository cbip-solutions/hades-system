# Dockerfile — Plan 15 Phase D-9
#
# Multi-stage build for the zen-swarm v1.0+ public OSS release.
#
#   Builder stage  golang:1.25-bookworm  + build-essential for CGO
#                  (sqlite-vec + Plan-9 stubs + Caronte tree-sitter per
#                   decisión 6).
#   Runtime stage  gcr.io/distroless/cc-debian12  (minimal glibc +
#                   ca-certificates; no shell; ~50-80 MB target).
#
# Multi-arch: docker buildx build --platform linux/amd64,linux/arm64
# produces a multi-arch manifest. The release.yml docker job invokes
# this Dockerfile after the goreleaser release matrix completes; the
# image is pushed to ghcr.io/hades-system/hades-system:v1.0.0 +
# :v1.0 + :latest with cosign-keyless signature + SLSA L2 attestation
# attached to the OCI manifest digest. inv-zen-298 (Docker image multi-
# arch published with sigstore signature).
#
# Build-args:
#   VERSION  — semver tag (e.g., v1.0.0); embedded via ldflags into
#              internal/buildinfo.version (Phase D-6).
#   COMMIT   — git short SHA; embedded into internal/buildinfo.commit.
#   DATE     — commit timestamp (RFC 3339); embedded into
#              internal/buildinfo.date.
#   TARGETOS + TARGETARCH — automatically set by buildx; passed through
#              to go build so the cross-arch matrix produces the right
#              binary per platform.
#
# OCI labels (consumed by `docker inspect` + GHCR UI):
#   org.opencontainers.image.source        = repo URL
#   org.opencontainers.image.description   = product description
#   org.opencontainers.image.licenses      = MIT (decisión 15)
#   org.opencontainers.image.vendor        = hades-system
#
# Non-root runtime: distroless cc-debian12 ships with a nonroot user
# (UID 65532); we run as nonroot:nonroot per CIS Docker Benchmark §4.1.

# ---------------------------------------------------------------------------
# Builder stage — Go 1.25 + build-essential for CGO
# ---------------------------------------------------------------------------
FROM golang:1.25-bookworm AS builder

# Build-essential is required because CGO is enabled for THREE in-tree
# native sources:
#   1) sqlite-vec extension (Plan 8 vector storage substrate; native C extension)
#   2) Plan-9 darwin Keychain credential stubs (resolved via _other.go
#      build constraints to a noop on linux; CGO still required for the
#      build to link)
#   3) Caronte smacker/go-tree-sitter (Plan 19 SHIPPED 2026-05-25;
#      sovereign in-house Go code-graph engine per decisión 6; native
#      tree-sitter C library binding + language grammars)
# pkg-config is required by sqlite-vec for resolving system headers.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        build-essential \
        pkg-config \
        ca-certificates && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Copy go.mod + go.sum first for layer caching: when only source
# changes, the dep-download layer stays cached.
COPY go.mod go.sum ./
RUN go mod download

# Now copy the full source tree.
COPY . .

# Build-args for ldflags injection (with safe defaults).
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

# Multi-arch awareness: TARGETOS + TARGETARCH are set by buildx and
# expose the target platform to `go build`. Defaults below keep the
# Dockerfile usable for `docker build` (single arch).
ARG TARGETOS=linux
ARG TARGETARCH=amd64

# Compile both binaries with CGO + reproducibility ldflags + buildinfo
# embedding. Pinned-toolchain reproducibility (spec §4.6):
#   -trimpath        strip absolute paths from binary metadata.
#   -s -w -buildid=  strip symbol table + DWARF + zero build-id.
#   -tags sqlite_fts5  Plan 8 Phase L FTS5 module support.
#   -X main.{version,commit,date}=…       (inv-zen-294 ldflag shape)
#   -X internal/buildinfo.{version,commit,date}=…  (Phase D-6 runtime
#                                                    surface for
#                                                    --version + audit
#                                                    chain provenance).
RUN CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -tags="sqlite_fts5" \
    -ldflags="-s -w -buildid= \
        -X main.version=${VERSION} \
        -X main.commit=${COMMIT} \
        -X main.date=${DATE} \
        -X github.com/zen-swarm/zen-swarm/internal/buildinfo.version=${VERSION} \
        -X github.com/zen-swarm/zen-swarm/internal/buildinfo.commit=${COMMIT} \
        -X github.com/zen-swarm/zen-swarm/internal/buildinfo.date=${DATE}" \
    -o /out/zen ./cmd/zen

RUN CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -tags="sqlite_fts5" \
    -ldflags="-s -w -buildid= \
        -X main.version=${VERSION} \
        -X main.commit=${COMMIT} \
        -X main.date=${DATE} \
        -X github.com/zen-swarm/zen-swarm/internal/buildinfo.version=${VERSION} \
        -X github.com/zen-swarm/zen-swarm/internal/buildinfo.commit=${COMMIT} \
        -X github.com/zen-swarm/zen-swarm/internal/buildinfo.date=${DATE}" \
    -o /out/zen-swarm-ctld ./cmd/zen-swarm-ctld

# Sanity-check the binaries execute (catches CGO link failures early
# inside the builder stage before they leak to the runtime image).
# Skipped under cross-arch buildx (TARGETARCH != BUILDARCH) because the
# emulator overhead is wasted on a smoke check.
ARG BUILDARCH
RUN if [ "${TARGETARCH}" = "${BUILDARCH}" ] || [ -z "${BUILDARCH}" ]; then \
        /out/zen --version && /out/zen-swarm-ctld --version; \
    else \
        echo "skip --version smoke (TARGETARCH=${TARGETARCH} != BUILDARCH=${BUILDARCH}; cross-arch build)"; \
    fi

# ---------------------------------------------------------------------------
# Runtime stage — distroless cc-debian12 (minimal glibc + ca-certificates)
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/cc-debian12:latest

# OCI standard labels (consumed by `docker inspect` + GHCR UI).
LABEL org.opencontainers.image.source="https://github.com/hades-system/hades-system"
LABEL org.opencontainers.image.description="Multi-project agentic development orchestrator"
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.vendor="hades-system"

# Copy binaries from builder stage.
COPY --from=builder /out/zen /usr/local/bin/zen
COPY --from=builder /out/zen-swarm-ctld /usr/local/bin/zen-swarm-ctld

# Copy LICENSE + README for compliance audits. distroless has no shell,
# but `docker cp` + `docker save` can still extract these for license
# disclosure inspection.
COPY --from=builder /src/LICENSE /usr/share/doc/zen-swarm/LICENSE
COPY --from=builder /src/README.md /usr/share/doc/zen-swarm/README.md

# Default entry point: zen-swarm-ctld (the daemon). The default CMD is
# --version so `docker run ghcr.io/hades-system/hades-system` prints
# version info instead of hanging in daemon-boot mode without config.
# Operators override CMD to run the daemon for real, e.g.
#   docker run --rm ghcr.io/.../hades-system daemon start
ENTRYPOINT ["/usr/local/bin/zen-swarm-ctld"]
CMD ["--version"]

# Non-root runtime user (distroless cc-debian12 ships with nonroot
# UID 65532). CIS Docker Benchmark §4.1.
USER nonroot:nonroot
