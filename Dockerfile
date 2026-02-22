# ============================================================
# Stage 1: Build the sciclaw binary
# ============================================================
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS builder

ARG TARGETOS
ARG TARGETARCH

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates git make && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -ldflags "-s -w" \
    -o /out/sciclaw \
    ./cmd/picoclaw

# ============================================================
# Stage 2: Full runtime image
# ============================================================
FROM debian:bookworm-slim

ARG TARGETARCH

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      ca-certificates \
      curl \
      imagemagick \
      pandoc \
      python3 \
      python3-pip \
      python3-venv \
      ripgrep \
      tar \
      tzdata && \
    rm -rf /var/lib/apt/lists/*

# Debian's ImageMagick 6 provides `convert` but not the `magick` frontend.
RUN if [ ! -x /usr/bin/magick ] && [ -x /usr/bin/convert ]; then \
      ln -sf /usr/bin/convert /usr/local/bin/magick; \
    fi

# Shared runtime tool versions/checksums used by both Docker and Multipass cloud-init.
COPY deploy/toolchain.env /tmp/sciclaw-toolchain.env

RUN set -eux; \
    . /tmp/sciclaw-toolchain.env; \
    case "${TARGETARCH}" in \
      amd64) \
        ARCH=amd64; \
        UV_ARCH=x86_64-unknown-linux-gnu; \
        QUARTO_ARCH=amd64; \
        UV_SHA256="${UV_SHA256_AMD64}"; \
        QUARTO_SHA256="${QUARTO_SHA256_AMD64}"; \
        IRL_SHA256="${IRL_SHA256_AMD64}"; \
        DOCX_REVIEW_SHA256="${DOCX_REVIEW_SHA256_AMD64}"; \
        PUBMED_CLI_SHA256="${PUBMED_CLI_SHA256_AMD64}"; \
        ;; \
      arm64) \
        ARCH=arm64; \
        UV_ARCH=aarch64-unknown-linux-gnu; \
        QUARTO_ARCH=arm64; \
        UV_SHA256="${UV_SHA256_ARM64}"; \
        QUARTO_SHA256="${QUARTO_SHA256_ARM64}"; \
        IRL_SHA256="${IRL_SHA256_ARM64}"; \
        DOCX_REVIEW_SHA256="${DOCX_REVIEW_SHA256_ARM64}"; \
        PUBMED_CLI_SHA256="${PUBMED_CLI_SHA256_ARM64}"; \
        ;; \
      *) echo "Unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/uv.tgz \
      "https://github.com/astral-sh/uv/releases/download/${UV_VERSION}/uv-${UV_ARCH}.tar.gz"; \
    printf '%s  %s\n' "${UV_SHA256}" "/tmp/uv.tgz" | sha256sum -c -; \
    tar -xzf /tmp/uv.tgz -C /tmp; \
    install -m 0755 "/tmp/uv-${UV_ARCH}/uv" /usr/local/bin/uv; \
    install -m 0755 "/tmp/uv-${UV_ARCH}/uvx" /usr/local/bin/uvx; \
    curl -fsSL -o /tmp/quarto.tgz \
      "https://github.com/quarto-dev/quarto-cli/releases/download/v${QUARTO_VERSION}/quarto-${QUARTO_VERSION}-linux-${QUARTO_ARCH}.tar.gz"; \
    printf '%s  %s\n' "${QUARTO_SHA256}" "/tmp/quarto.tgz" | sha256sum -c -; \
    tar -xzf /tmp/quarto.tgz -C /opt; \
    ln -sf "/opt/quarto-${QUARTO_VERSION}/bin/quarto" /usr/local/bin/quarto; \
    curl -fsSL -o /tmp/irl \
      "https://github.com/drpedapati/irl-template/releases/download/v${IRL_VERSION}/irl-linux-${ARCH}"; \
    printf '%s  %s\n' "${IRL_SHA256}" "/tmp/irl" | sha256sum -c -; \
    install -m 0755 /tmp/irl /usr/local/bin/irl; \
    curl -fsSL -o /tmp/docx-review \
      "https://github.com/drpedapati/docx-review/releases/download/v${DOCX_REVIEW_VERSION}/docx-review-linux-${ARCH}"; \
    printf '%s  %s\n' "${DOCX_REVIEW_SHA256}" "/tmp/docx-review" | sha256sum -c -; \
    install -m 0755 /tmp/docx-review /usr/local/bin/docx-review; \
    curl -fsSL -o /tmp/pubmed \
      "https://github.com/drpedapati/pubmed-cli/releases/download/v${PUBMED_CLI_VERSION}/pubmed-linux-${ARCH}"; \
    printf '%s  %s\n' "${PUBMED_CLI_SHA256}" "/tmp/pubmed" | sha256sum -c -; \
    install -m 0755 /tmp/pubmed /usr/local/bin/pubmed; \
    ln -sf /usr/local/bin/pubmed /usr/local/bin/pubmed-cli; \
    rm -rf /tmp/uv* /tmp/quarto.tgz /tmp/irl /tmp/docx-review /tmp/pubmed /tmp/sciclaw-toolchain.env

# Copy binary
COPY --from=builder /out/sciclaw /usr/local/bin/sciclaw
RUN ln -sf sciclaw /usr/local/bin/picoclaw

# Copy builtin skills
COPY --from=builder /src/skills /opt/sciclaw/skills

# Create default workspace with baseline skills
RUN mkdir -p /root/sciclaw/skills /root/.picoclaw && \
    cp -r /opt/sciclaw/skills/* /root/sciclaw/skills/ 2>/dev/null || true

ENTRYPOINT ["sciclaw"]
CMD ["gateway"]
