FROM golang:1.26-bookworm AS builder

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        build-essential \
        pkg-config \
        ca-certificates && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG BUILDARCH

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

RUN if [ "${TARGETARCH}" = "${BUILDARCH}" ] || [ -z "${BUILDARCH}" ]; then \
        /out/zen --version && /out/zen-swarm-ctld --version; \
    else \
        echo "skip --version smoke for cross-arch build"; \
    fi

FROM gcr.io/distroless/cc-debian12:latest

LABEL org.opencontainers.image.source="https://github.com/cbip-solutions/hades-system"
LABEL org.opencontainers.image.description="Multi-project agentic development orchestrator"
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.vendor="hades-system"

COPY --from=builder /out/zen /usr/local/bin/zen
COPY --from=builder /out/zen-swarm-ctld /usr/local/bin/zen-swarm-ctld
COPY --from=builder /src/LICENSE /usr/share/doc/zen-swarm/LICENSE
COPY --from=builder /src/README.md /usr/share/doc/zen-swarm/README.md

ENTRYPOINT ["/usr/local/bin/zen-swarm-ctld"]
CMD ["--version"]
USER nonroot:nonroot
