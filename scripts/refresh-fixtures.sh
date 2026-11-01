#!/usr/bin/env sh
# Capture canonical Pi-hole API responses into testdata/fixtures/<ftl version>/.
# Run against a live dev Pi-hole (default: http://localhost:8081, password "test"
# from docker-compose.dev.yml), seeded first so the captures carry data. The
# directory is named after the FTL version the Pi-hole reports, reduced to
# major.minor (v6.8), so captures from each supported version sit side by side
# and the fixture tests run against all of them.
#
# Usage:
#   PIHOLE_URL=http://localhost:8081 PIHOLE_PASSWORD=test ./scripts/refresh-fixtures.sh
#
# Requires: curl, jq.
set -eu

PIHOLE_URL="${PIHOLE_URL:-http://localhost:8081}"
PIHOLE_PASSWORD="${PIHOLE_PASSWORD:-test}"

if ! command -v jq >/dev/null 2>&1; then
    echo "error: jq is required (brew install jq / apt install jq)" >&2
    exit 1
fi

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
WORK="$(mktemp -d)"
SID=""

# Release the session and the scratch directory however the script ends,
# including an interrupt. A leaked session holds one of Pi-hole's API seats
# (16 by default) for half an hour.
cleanup() {
    if [ -n "$SID" ]; then
        curl -fsS -X DELETE "$PIHOLE_URL/api/auth" -H "X-FTL-SID: $SID" >/dev/null 2>&1 || true
    fi
    rm -rf "$WORK"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

echo "Authenticating against $PIHOLE_URL..."
curl -fsS -X POST "$PIHOLE_URL/api/auth" \
    -H 'Content-Type: application/json' \
    -d "{\"password\":\"$PIHOLE_PASSWORD\"}" -o "$WORK/auth.json"
SID="$(jq -r '.session.sid // empty' "$WORK/auth.json")"
if [ -z "$SID" ]; then
    echo "error: failed to obtain session ID" >&2
    exit 1
fi
echo "  session ok"

curl -fsS "$PIHOLE_URL/api/info/version" -H "X-FTL-SID: $SID" -o "$WORK/version.json"
VERSION="$(jq -r '.version.ftl.local.version // empty' "$WORK/version.json" | sed -n 's/^\(v[0-9][0-9]*\.[0-9][0-9]*\).*/\1/p')"
if [ -z "$VERSION" ]; then
    echo "error: could not read a release FTL version from /api/info/version (development builds are not captured)" >&2
    exit 1
fi
FIXTURES_DIR="$SCRIPT_DIR/../testdata/fixtures/$VERSION"
mkdir -p "$FIXTURES_DIR"
echo "  FTL $VERSION -> $FIXTURES_DIR"

FAILED=0

# capture <fixture-name> <api-path>
#
# The response is fetched to a scratch file and pretty-printed to a second one,
# and only a capture that passed both steps replaces the fixture. Redirecting
# straight into the fixture truncated it before the request had even been
# made, and in a pipeline a failed curl was hidden behind jq's exit status, so
# a failure could leave an empty fixture and still report success.
capture() {
    name="$1"
    apipath="$2"
    echo "  -> $name ($apipath)"
    if curl -fsS "$PIHOLE_URL$apipath" -H "X-FTL-SID: $SID" -o "$WORK/raw.json" &&
        jq -S '.' "$WORK/raw.json" >"$WORK/pretty.json" &&
        [ -s "$WORK/pretty.json" ]; then
        mv "$WORK/pretty.json" "$FIXTURES_DIR/$name.json"
    else
        echo "    warning: capture failed for $apipath; the previous fixture is unchanged" >&2
        FAILED=$((FAILED + 1))
    fi
}

# Database history endpoints require from/until. The windows end now, so a
# freshly seeded Pi-hole's queries are inside them once FTL has written them to
# its database (once a minute by default).
NOW="$(date +%s)"
DAY_AGO="$((NOW - 86400))"
WEEK_AGO="$((NOW - 604800))"

echo "Capturing fixtures..."

capture "info_ftl" "/api/info/ftl"
capture "info_metrics" "/api/info/metrics"
capture "info_sensors" "/api/info/sensors"
capture "info_system" "/api/info/system"
capture "info_host" "/api/info/host"
capture "info_database" "/api/info/database"
capture "info_version" "/api/info/version"
capture "info_messages" "/api/info/messages"

capture "padd" "/api/padd"

capture "stats_summary" "/api/stats/summary"
capture "stats_top_domains" "/api/stats/top_domains"
capture "stats_top_clients" "/api/stats/top_clients"
capture "stats_upstreams" "/api/stats/upstreams"
capture "stats_query_types" "/api/stats/query_types"
capture "stats_recent_blocked" "/api/stats/recent_blocked?count=10"

capture "stats_database_top_domains" "/api/stats/database/top_domains?from=$DAY_AGO&until=$NOW"
capture "stats_database_top_clients" "/api/stats/database/top_clients?from=$DAY_AGO&until=$NOW"
capture "stats_database_upstreams" "/api/stats/database/upstreams?from=$DAY_AGO&until=$NOW"
capture "stats_database_query_types" "/api/stats/database/query_types?from=$DAY_AGO&until=$NOW"

capture "config_properties" "/api/config/_properties"

capture "auth_sessions" "/api/auth/sessions"

capture "history_clients" "/api/history/clients"
capture "history_database" "/api/history/database?from=$WEEK_AGO&until=$NOW"
capture "history_database_clients" "/api/history/database/clients?from=$DAY_AGO&until=$NOW"

capture "queries" "/api/queries?length=20"

capture "logs_dnsmasq" "/api/logs/dnsmasq"

capture "network_devices" "/api/network/devices"
capture "network_routes" "/api/network/routes"
capture "network_interfaces" "/api/network/interfaces"

if [ "$FAILED" -gt 0 ]; then
    echo "Done with $FAILED failed capture(s). Fixtures in $FIXTURES_DIR" >&2
    exit 1
fi
echo "Done. Fixtures in $FIXTURES_DIR"
