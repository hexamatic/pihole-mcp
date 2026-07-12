#!/usr/bin/env sh
# Drive a known set of DNS queries through the local development Pi-hole so that
# statistics, history and top-list endpoints have something to report.
#
# Without this, a freshly created Pi-hole answers every stats endpoint with an
# empty result, and fixtures captured from it assert nothing. Queries are issued
# with the container's own dig, so the only host requirement is Docker — the same
# one `just dev-up` already imposes.
#
# Usage: scripts/seed-dev.sh [container]   (default: pihole-dev)
set -eu

CONTAINER="${1:-pihole-dev}"

if ! docker inspect "$CONTAINER" >/dev/null 2>&1; then
    echo "error: container '$CONTAINER' is not running — start it with 'just dev-up'" >&2
    exit 1
fi

# Resolved normally. Repeated with differing frequency so top-domain rankings
# have a stable, non-tied order to assert on.
ALLOWED="github.com github.com github.com github.com
cloudflare.com cloudflare.com cloudflare.com
google.com google.com
wikipedia.org wikipedia.org
debian.org
archlinux.org"

# In every mainstream blocklist, so these reliably count as blocked. Verified
# against the gravity list the dev Pi-hole downloads on first boot.
BLOCKED="doubleclick.net doubleclick.net doubleclick.net
google-analytics.com google-analytics.com
googleadservices.com
ads.yahoo.com
adservice.google.com"

query() {
    # Failures are expected and fine — a blocked domain returns 0.0.0.0 and an
    # upstream hiccup should not abort seeding. We only care that FTL logged it.
    docker exec "$CONTAINER" dig +tries=1 +time=2 +short @127.0.0.1 "$1" A >/dev/null 2>&1 || true
}

printf 'Seeding %s with DNS queries...\n' "$CONTAINER"

count=0
for domain in $ALLOWED $BLOCKED; do
    query "$domain"
    count=$((count + 1))
done

# A couple of AAAA and a deliberate NXDOMAIN, so query_types and the reply
# breakdown are not uniformly A/IP.
docker exec "$CONTAINER" dig +tries=1 +time=2 +short @127.0.0.1 github.com AAAA >/dev/null 2>&1 || true
docker exec "$CONTAINER" dig +tries=1 +time=2 +short @127.0.0.1 cloudflare.com AAAA >/dev/null 2>&1 || true
docker exec "$CONTAINER" dig +tries=1 +time=2 +short @127.0.0.1 this-domain-does-not-exist.invalid A >/dev/null 2>&1 || true
count=$((count + 3))

# FTL batches writes to its in-memory stats; give it a moment to settle before
# anything reads them back.
sleep 2

printf 'Seeded %d queries.\n' "$count"
