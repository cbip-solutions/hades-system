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
        -X github.com/cbip-solutions/hades-system/internal/buildinfo.version=${VERSION} \
        -X github.com/cbip-solutions/hades-system/internal/buildinfo.commit=${COMMIT} \
        -X github.com/cbip-solutions/hades-system/internal/buildinfo.date=${DATE}" \
    -o /out/hades ./cmd/hades

RUN CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -tags="sqlite_fts5" \
    -ldflags="-s -w -buildid= \
        -X main.version=${VERSION} \
        -X main.commit=${COMMIT} \
        -X main.date=${DATE} \
        -X github.com/cbip-solutions/hades-system/internal/buildinfo.version=${VERSION} \
        -X github.com/cbip-solutions/hades-system/internal/buildinfo.commit=${COMMIT} \
        -X github.com/cbip-solutions/hades-system/internal/buildinfo.date=${DATE}" \
    -o /out/hades-ctld ./cmd/hades-ctld

RUN if [ "${TARGETARCH}" = "${BUILDARCH}" ] || [ -z "${BUILDARCH}" ]; then \
        /out/hades --version && /out/hades-ctld --version; \
    else \
        echo "skip --version smoke for cross-arch build"; \
    fi

FROM gcr.io/distroless/cc-debian12:latest

LABEL org.opencontainers.image.source="https://github.com/cbip-solutions/hades-system"
LABEL org.opencontainers.image.description="Multi-project agentic development orchestrator"
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.vendor="hades-system"

COPY --from=builder /out/hades /usr/local/bin/hades
COPY --from=builder /out/hades-ctld /usr/local/bin/hades-ctld
COPY --from=builder /src/LICENSE /usr/share/doc/hades-system/LICENSE
COPY --from=builder /src/README.md /usr/share/doc/hades-system/README.md

ENTRYPOINT ["/usr/local/bin/hades-ctld"]
CMD ["--version"]
USER nonroot:nonroot
