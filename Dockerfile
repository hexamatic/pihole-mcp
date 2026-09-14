# Base images are pinned by digest as well as tag (OpenSSF Scorecard
# Pinned-Dependencies). The tag stays for readability — it is the digest that
# is authoritative. Dependabot's `docker` ecosystem maintains both; when
# bumping Go by hand, re-resolve with:
#   docker buildx imagetools inspect golang:1.XX-alpine --format '{{.Manifest.Digest}}'
FROM golang:1.27-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS builder

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

FROM gcr.io/distroless/static-debian13@sha256:58133991db06659feaabe0f4e97a35cebf15ef4ea08f8a4c6d2ee5f75e4aa6a0

LABEL org.opencontainers.image.title="pihole-mcp"
LABEL org.opencontainers.image.description="MCP server for Pi-hole v6"
LABEL org.opencontainers.image.source="https://github.com/hexamatic/pihole-mcp"
LABEL org.opencontainers.image.licenses="MIT"

# Kept in step with Dockerfile.goreleaser, where it is load-bearing for MCP
# Registry ownership verification. See the note there.
#
# "Kept in step" is enforced, not hoped for: TestServerJSONNameMatchesImageLabel
# in internal/config reads both Dockerfiles and fails if either label diverges
# from `name` in server.json. It read only Dockerfile.goreleaser until v0.9.0,
# so this line was a promise nothing checked.
LABEL io.modelcontextprotocol.server.name="io.github.hexamatic/pihole-mcp"

COPY --from=builder /bin/pihole-mcp /pihole-mcp

USER nonroot:nonroot

ENTRYPOINT ["/pihole-mcp"]
