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
ARG UV_VERSION=0.10.4
ARG QUARTO_VERSION=1.8.27
ARG IRL_VERSION=0.5.17
ARG DOCX_REVIEW_VERSION=1.3.0
ARG PUBMED_CLI_VERSION=0.5.4
ARG UV_SHA256_AMD64=6b52a47358deea1c5e173278bf46b2b489747a59ae31f2a4362ed5c6c1c269f7
ARG UV_SHA256_ARM64=c84a6e6405715caa6e2f5ef8e5f29a5d0bc558a954e9f1b5c082b9d4708c222e
ARG QUARTO_SHA256_AMD64=bdf689b5589789a1f21d89c3b83d78ed02a97914dd702e617294f2cc1ea7387d
ARG QUARTO_SHA256_ARM64=1f2082e82e971c5b2b78424cac93a0921c0050455ec5eaa32533b0230682883e
ARG IRL_SHA256_AMD64=3cd4ceda734027d77ba8bde1d8413848f498cf41ca3d59ee4b491eade5dcd98a
ARG IRL_SHA256_ARM64=7706a2262c1de343b4808ca995c463850c3470673f005d7337eef82fea542de9
ARG DOCX_REVIEW_SHA256_AMD64=2f088bcdfb0d152988960a8a08fb513fc7912a522c8c7afa6d8b0c7c2e4d063f
ARG DOCX_REVIEW_SHA256_ARM64=3f49741164a040bcb5b8e09e062a4d7f6088ef7b611d31754f751314e6db3aca
ARG PUBMED_CLI_SHA256_AMD64=e2c539e4c9a268f06aa3bbf1387dff923e1fdc148a0edb8f1449eadc8e63e828
ARG PUBMED_CLI_SHA256_ARM64=8360f26c33a6c5b5e12acdc6f7d55899dff15d5f0fc766b0632b69347b61c638

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

RUN set -eux; \
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
    rm -rf /tmp/uv* /tmp/quarto.tgz /tmp/irl /tmp/docx-review /tmp/pubmed

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
