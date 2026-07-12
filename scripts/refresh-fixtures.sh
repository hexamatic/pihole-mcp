#!/usr/bin/env sh
# Capture canonical Pi-hole API responses into testdata/fixtures/.
# Run against a live dev Pi-hole (default: http://localhost:8081, password "test"
# from docker-compose.dev.yml). Re-run whenever the API surface changes.
#
# Usage:
#   PIHOLE_URL=http://localhost:8081 PIHOLE_PASSWORD=test ./scripts/refresh-fixtures.sh
#
# Requires: curl, jq.
set -eu

PIHOLE_URL="${PIHOLE_URL:-http://localhost:8081}"
PIHOLE_PASSWORD="${PIHOLE_PASSWORD:-test}"

# Resolve fixtures directory relative to this script.
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
FIXTURES_DIR="$SCRIPT_DIR/../testdata/fixtures"
mkdir -p "$FIXTURES_DIR"

if ! command -v jq >/dev/null 2>&1; then
    echo "error: jq is required (brew install jq / apt install jq)" >&2
    exit 1
fi

echo "Authenticating against $PIHOLE_URL..."
SID="$(curl -fsS -X POST "$PIHOLE_URL/api/auth" \
    -H 'Content-Type: application/json' \
    -d "{\"password\":\"$PIHOLE_PASSWORD\"}" \
    | jq -r '.session.sid')"

if [ -z "$SID" ] || [ "$SID" = "null" ]; then
    echo "error: failed to obtain session ID" >&2
    exit 1
fi
echo "  session ok"

# capture <fixture-name> <api-path>
capture() {
    name="$1"
    apipath="$2"
    out="$FIXTURES_DIR/$name.json"
    echo "  -> $name ($apipath)"
    if ! curl -fsS "$PIHOLE_URL$apipath" -H "X-FTL-SID: $SID" \
        | jq -S '.' > "$out"; then
        echo "    warning: capture failed for $apipath; leaving previous fixture in place" >&2
        # If curl failed and we left an empty/zero-length file, restore by removing it.
        if [ ! -s "$out" ]; then rm -f "$out"; fi
        return 0
    fi
}

# Database history endpoints require from/until — use a 24h window relative to
# now so the fixture is realistic. We round to the start of the current hour
# for reproducibility within the same hour-long window.
NOW="$(date +%s)"
HOUR_START="$(( NOW - (NOW % 3600) ))"
DAY_AGO="$(( HOUR_START - 86400 ))"
WEEK_AGO="$(( HOUR_START - 604800 ))"

echo "Capturing fixtures..."

# Info family — info_ftl is the regression that triggered TODO #6.
capture "info_ftl"      "/api/info/ftl"
capture "info_metrics"  "/api/info/metrics"
capture "info_sensors"  "/api/info/sensors"
capture "info_system"   "/api/info/system"
capture "info_database" "/api/info/database"
capture "info_version"  "/api/info/version"
capture "info_messages" "/api/info/messages"

# Stats family — covers the read paths exercised by stats_test.go fixtures.
capture "stats_summary"        "/api/stats/summary"
capture "stats_top_domains"    "/api/stats/top_domains"
capture "stats_top_clients"    "/api/stats/top_clients"
capture "stats_upstreams"      "/api/stats/upstreams"
capture "stats_query_types"    "/api/stats/query_types"
capture "stats_recent_blocked" "/api/stats/recent_blocked?count=10"

# Stats database family — long-term DB lookups, windowed.
capture "stats_database_top_domains"  "/api/stats/database/top_domains?from=$DAY_AGO&until=$HOUR_START"
capture "stats_database_top_clients"  "/api/stats/database/top_clients?from=$DAY_AGO&until=$HOUR_START"
capture "stats_database_upstreams"    "/api/stats/database/upstreams?from=$DAY_AGO&until=$HOUR_START"
capture "stats_database_query_types"  "/api/stats/database/query_types?from=$DAY_AGO&until=$HOUR_START"

# Config properties — added in Pi-hole v6.6.1. The dev Pi-hole sets several
# settings through FTLCONF_* environment variables, so read_only comes back
# populated and the captured fixture reflects a realistic response.
capture "config_properties" "/api/config/_properties"

# Auth sessions — the security_audit prompt entry point.
capture "auth_sessions" "/api/auth/sessions"

# History database endpoints (Phase 1 new tools). Use a week window so we
# always have data even if the dev instance is fresh.
capture "history_database"         "/api/history/database?from=$WEEK_AGO&until=$HOUR_START"
capture "history_database_clients" "/api/history/database/clients?from=$DAY_AGO&until=$HOUR_START"

# Network family (Phase 1 new tools).
capture "network_devices"    "/api/network/devices"
capture "network_routes"     "/api/network/routes"
capture "network_interfaces" "/api/network/interfaces"

# Cleanup the session so we don't burn through the session quota.
curl -fsS -X DELETE "$PIHOLE_URL/api/auth" -H "X-FTL-SID: $SID" >/dev/null 2>&1 || true

echo "Done. Fixtures in $FIXTURES_DIR"
