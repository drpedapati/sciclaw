# ============================================================
# Stage 1: Build the sciclaw binary
# ============================================================
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN make build

# ============================================================
# Stage 2: Minimal runtime image
# ============================================================
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

# Copy binary
COPY --from=builder /src/build/sciclaw /usr/local/bin/sciclaw
RUN ln -sf sciclaw /usr/local/bin/picoclaw

# Copy builtin skills
COPY --from=builder /src/skills /opt/sciclaw/skills

# Create picoclaw-compatible home directory
RUN mkdir -p /root/sciclaw/skills && \
    cp -r /opt/sciclaw/skills/* /root/sciclaw/skills/ 2>/dev/null || true

ENTRYPOINT ["sciclaw"]
CMD ["gateway"]
