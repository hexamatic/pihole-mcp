set dotenv-load

# `mise bin-paths` emits one path per line, so we collapse the newlines into
# the colon separator PATH expects before prepending to the inherited PATH.
export PATH := `mise bin-paths | tr '\n' ':'` + env("PATH")

# Default recipe — show help
[private]
default:
    @just --list --unsorted

# ─── Setup ───────────────────────────────────────────────────────────────────

# Bootstrap: install tools, dependencies, and git hooks
[group('setup')]
setup:
    mise install
    go mod download
    lefthook install
    @echo "\033[32m✓ Setup complete. Run 'just dev-up' to start Pi-hole.\033[0m"

# Download Go module dependencies
[group('setup')]
deps:
    go mod download
    go mod tidy

# ─── Build ───────────────────────────────────────────────────────────────────

# Build the pihole-mcp binary with version injection
[group('build')]
build:
    go build -ldflags="-s -w -X github.com/hexamatic/pihole-mcp/internal/server.Version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bin/pihole-mcp ./cmd/pihole-mcp

# Build the slim binary (no OpenTelemetry; ~45% smaller)
[group('build')]
build-slim:
    go build -tags slim -ldflags="-s -w -X github.com/hexamatic/pihole-mcp/internal/server.Version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bin/pihole-mcp-slim ./cmd/pihole-mcp

# Install to $GOPATH/bin
[group('build')]
install:
    go install ./cmd/pihole-mcp

# ─── Quality ─────────────────────────────────────────────────────────────────

# Run all tests with race detection
[group('quality')]
test:
    go test -race -count=1 ./...

# Run golangci-lint
[group('quality')]
lint:
    golangci-lint run ./...

# Format Go code
[group('quality')]
fmt:
    gofmt -w .
    goimports -w .

# Check formatting without modifying (CI gate)
[group('quality')]
fmt-check:
    #!/usr/bin/env sh
    set -eu
    unformatted="$(gofmt -l .)"
    if [ -n "$unformatted" ]; then
        echo "Files need formatting:"
        echo "$unformatted"
        exit 1
    fi
    echo "Formatting OK"

# Fuzz the tool-parameter validators (30s; raise -fuzztime for deeper runs)
[group('quality')]
fuzz:
    go test -run=^$ -fuzz=FuzzValidateDomainName -fuzztime=30s ./internal/tools/

# Regenerate docs/TOOLS.md from the registered tool definitions
[group('quality')]
docs-gen:
    go run ./cmd/toolsdoc

# Run all quality checks (format + lint + test)
[group('quality')]
check: fmt lint test

# ─── Development ─────────────────────────────────────────────────────────────

# Start local Pi-hole dev instance
[group('dev')]
dev-up:
    docker compose -f docker-compose.dev.yml up -d --wait
    @echo "\033[32m✓ Pi-hole running at http://localhost:8081/admin (password: test)\033[0m"

# Start both Pi-hole instances (multi-instance dev: primary 8081 + secondary 8082)
[group('dev')]
dev-up-multi:
    docker compose --profile multi -f docker-compose.dev.yml up -d --wait
    @echo "\033[32m✓ Pi-hole primary at :8081, secondary at :8082 (password: test)\033[0m"

# Stop local Pi-hole
[group('dev')]
dev-down:
    docker compose -f docker-compose.dev.yml down

# Stop both Pi-hole instances (multi-instance dev)
[group('dev')]
dev-down-multi:
    docker compose --profile multi -f docker-compose.dev.yml down

# Drive DNS queries through the dev Pi-hole so stats endpoints have data
[group('dev')]
seed:
    sh scripts/seed-dev.sh

# Reset Pi-hole (clean volumes)
[group('dev')]
dev-reset:
    docker compose -f docker-compose.dev.yml down -v
    docker compose -f docker-compose.dev.yml up -d --wait
    @echo "\033[32m✓ Pi-hole reset with clean volumes\033[0m"

# Tail Pi-hole container logs
[group('dev')]
dev-logs:
    docker compose -f docker-compose.dev.yml logs -f

# Run integration tests against local Pi-hole
[group('dev')]
integration:
    PIHOLE_URL=http://localhost:8081 PIHOLE_PASSWORD=test go test -tags=integration -race -count=1 ./...

# Run multi-instance integration tests against both local Pi-holes (8081 + 8082)
[group('dev')]
integration-multi:
    PIHOLE_1_URL=http://localhost:8081 PIHOLE_1_PASSWORD=test \
    PIHOLE_2_URL=http://localhost:8082 PIHOLE_2_PASSWORD=test \
    go test -tags=integration -race -count=1 ./internal/pihole/

# Run E2E test of all tools against local Pi-hole
[group('dev')]
e2e: build
    PIHOLE_URL=http://localhost:8081 PIHOLE_PASSWORD=test scripts/e2e-test.sh ./bin/pihole-mcp

# Run the Docker-free multi-instance simulation (routing, aggregation, diff, sync)
[group('dev')]
sim:
    go test -tags=sim -race -count=1 -v -run TestSimulation ./internal/tools/

# Refresh testdata/fixtures/ from the live dev Pi-hole.
# Seeds first: a Pi-hole with no query history answers the stats endpoints with
# empty results, and fixtures captured from it would assert nothing.
[group('dev')]
refresh-fixtures: seed
    PIHOLE_URL=http://localhost:8081 PIHOLE_PASSWORD=test scripts/refresh-fixtures.sh

# ─── CI ──────────────────────────────────────────────────────────────────────

# Run full CI pipeline (mirrors GitHub Actions)
[group('ci')]
ci: fmt-check lint test
    go build -o /dev/null ./cmd/pihole-mcp
    @echo "\033[32m✓ CI passed\033[0m"

# ─── Release ─────────────────────────────────────────────────────────────────

# Dry-run release build (local snapshot)
[group('release')]
release-dry:
    goreleaser release --snapshot --clean

# Preview the release body that will be published for VERSION
[group('release')]
release-notes VERSION:
    @scripts/release-notes.sh {{VERSION}}

# Scaffold a CHANGELOG.md draft entry for VERSION from git log
[group('release')]
changelog-draft VERSION:
    @scripts/changelog-draft.sh {{VERSION}}

# Run the server with HTTP transport (for testing)
[group('dev')]
run-http: build
    PIHOLE_URL=http://localhost:8081 PIHOLE_PASSWORD=test bin/pihole-mcp -transport http -address localhost:9090

# ─── Cleanup ─────────────────────────────────────────────────────────────────

# Remove build artefacts
[group('cleanup')]
clean:
    rm -rf bin/ dist/
    go clean -cache
