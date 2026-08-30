# Base images are pinned by digest as well as tag (OpenSSF Scorecard
# Pinned-Dependencies). The tag stays for readability — it is the digest that
# is authoritative. Dependabot's `docker` ecosystem maintains both; when
# bumping Go by hand, re-resolve with:
#   docker buildx imagetools inspect golang:1.XX-alpine --format '{{.Manifest.Digest}}'
FROM golang:1.26-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS builder

ARG VERSION=dev

WORKDIR /build

RUN --mount=type=cache,target=/var/cache/apk apk add --no-cache git

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
    -ldflags="-s -w -X github.com/hexamatic/pihole-mcp/internal/server.Version=${VERSION}" \
    -o /bin/pihole-mcp ./cmd/pihole-mcp

FROM gcr.io/distroless/static-debian13@sha256:f2ea2709ac8db56323cbd7d014277f32cb572d9ea124b0076f7aafe5980678fe

LABEL org.opencontainers.image.title="pihole-mcp"
LABEL org.opencontainers.image.description="MCP server for Pi-hole v6"
LABEL org.opencontainers.image.source="https://github.com/hexamatic/pihole-mcp"
LABEL org.opencontainers.image.licenses="MIT"

# Kept in step with Dockerfile.goreleaser, where it is load-bearing for MCP
# Registry ownership verification. See the note there.
LABEL io.modelcontextprotocol.server.name="io.github.hexamatic/pihole-mcp"

COPY --from=builder /bin/pihole-mcp /pihole-mcp

USER nonroot:nonroot

ENTRYPOINT ["/pihole-mcp"]
