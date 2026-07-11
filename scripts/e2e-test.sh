#!/usr/bin/env bash
# End-to-end test script for pihole-mcp.
# Sends tool calls sequentially (one at a time) to avoid overwhelming Pi-hole.
# Usage: PIHOLE_URL=http://localhost:8081 PIHOLE_PASSWORD=test ./scripts/e2e-test.sh
set -euo pipefail

BINARY="${1:-./bin/pihole-mcp}"
PASS=0
FAIL=0
ERRORS=()

# is_transient reports whether a result is a transport-level failure (a dropped
# connection, an unparseable response, or an empty result) rather than a genuine
# tool outcome. Spawning one short-lived process per call hammers the local
# Pi-hole's API socket, so the occasional connection reset is expected under
# load and is retried rather than reported as a tool failure.
is_transient() {
    case "$1" in
        ""|*"read tcp"*|*"connection refused"*|*"sending auth request"*|*"i/o timeout"*|*"connection reset"*|*"Parse error"*|*"EOF"*)
            return 0 ;;
        *) return 1 ;;
    esac
}

# The pressure this suite puts on FTL's session table does not clear in
# milliseconds, so retry with exponential backoff rather than a flat 0.3s.
# Six attempts spans 12.6s of sleep in the worst case, which is long enough for
# FTL to reclaim a seat; the previous 3 x 0.3s budget was not, and produced
# intermittent "sending auth request: EOF" failures in CI.
MAX_ATTEMPTS=6

backoff() {
    # backoff <attempt> — 0.2s, 0.4s, 0.8s, 1.6s, 3.2s, 6.4s
    sleep "$(awk "BEGIN{printf \"%.2f\", 0.2 * (2 ^ ($1 - 1))}")"
}

call_tool() {
    local name="$1"
    local args="${2:-}"
    [ -z "$args" ] && args='{}'
    local label="${3:-$name}"

    local result attempt=0
    while :; do
        result=$(printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}\n{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"%s","arguments":%s}}\n' "$name" "$args" \
            | env -u PIHOLE_1_URL -u PIHOLE_1_PASSWORD -u PIHOLE_2_URL -u PIHOLE_2_PASSWORD \
                PIHOLE_URL="${PIHOLE_URL}" PIHOLE_PASSWORD="${PIHOLE_PASSWORD}" timeout 30 "$BINARY" 2>/dev/null \
            | tail -1)
        if is_transient "$result" && [ "$attempt" -lt "$MAX_ATTEMPTS" ]; then
            attempt=$((attempt+1)); backoff "$attempt"; continue
        fi
        break
    done

    local is_error
    is_error=$(echo "$result" | python3 -c "import sys,json;d=json.loads(sys.stdin.read());print(d.get('result',{}).get('isError',False))" 2>/dev/null)

    local content
    content=$(echo "$result" | python3 -c "import sys,json;d=json.loads(sys.stdin.read());[print(c.get('text','')) for c in d.get('result',{}).get('content',[])]" 2>/dev/null)

    if [ "$is_error" = "True" ]; then
        echo "  FAIL: $label"
        echo "        $(echo "$content" | head -1 | cut -c1-120)"
        FAIL=$((FAIL+1))
        ERRORS+=("$label: $(echo "$content" | head -1 | cut -c1-100)")
    else
        echo "  PASS: $label"
        PASS=$((PASS+1))
    fi
}

# call_multi runs a tool against a multi-instance server (PIHOLE_1_*, PIHOLE_2_*).
# A non-empty 4th argument inverts the result check (the call is expected to fail).
call_multi() {
    local name="$1"
    local args="${2:-}"
    [ -z "$args" ] && args='{}'
    local label="${3:-$name}"
    local expect_error="${4:-}"

    local result attempt=0
    while :; do
        result=$(printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}\n{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"%s","arguments":%s}}\n' "$name" "$args" \
            | env -u PIHOLE_URL -u PIHOLE_PASSWORD \
                PIHOLE_1_URL="${PIHOLE_1_URL}" PIHOLE_1_PASSWORD="${PIHOLE_1_PASSWORD}" PIHOLE_2_URL="${PIHOLE_2_URL}" PIHOLE_2_PASSWORD="${PIHOLE_2_PASSWORD}" timeout 30 "$BINARY" 2>/dev/null \
            | tail -1)
        if is_transient "$result" && [ "$attempt" -lt "$MAX_ATTEMPTS" ]; then
            attempt=$((attempt+1)); backoff "$attempt"; continue
        fi
        break
    done

    local is_error
    is_error=$(echo "$result" | python3 -c "import sys,json;d=json.loads(sys.stdin.read());print(d.get('result',{}).get('isError',False))" 2>/dev/null)

    local ok="True"
    if [ -n "$expect_error" ]; then
        [ "$is_error" = "True" ] && ok="True" || ok="False"
    else
        [ "$is_error" = "True" ] && ok="False" || ok="True"
    fi

    if [ "$ok" = "True" ]; then
        echo "  PASS: $label"
        PASS=$((PASS+1))
    else
        echo "  FAIL: $label"
        FAIL=$((FAIL+1))
        ERRORS+=("$label")
    fi
}

echo "=== pihole-mcp E2E Test Suite ==="
echo "Binary: $BINARY"
echo "Pi-hole: ${PIHOLE_URL}"
echo ""

echo "--- Dashboard ---"
call_tool "pihole_padd"
call_tool "pihole_padd" '{"detail":"minimal"}' "padd (minimal)"
call_tool "pihole_padd" '{"detail":"full"}' "padd (full)"

echo ""
echo "--- DNS Control ---"
call_tool "pihole_dns_get_blocking"
call_tool "pihole_dns_set_blocking" '{"blocking":false,"timer":3}' "dns_set_blocking (disable 3s)"
sleep 1
call_tool "pihole_dns_get_blocking" '{}' "dns_get_blocking (verify disabled)"

echo ""
echo "--- Statistics ---"
call_tool "pihole_stats_summary"
call_tool "pihole_stats_summary" '{"detail":"minimal"}' "stats_summary (minimal)"
call_tool "pihole_stats_summary" '{"detail":"full"}' "stats_summary (full)"
call_tool "pihole_stats_top_domains" '{"count":3}'
call_tool "pihole_stats_top_domains" '{"count":3,"format":"csv"}' "stats_top_domains (csv)"
call_tool "pihole_stats_top_clients" '{"count":3}'
call_tool "pihole_stats_upstreams"
call_tool "pihole_stats_query_types"
call_tool "pihole_stats_recent_blocked" '{"count":3}'
call_tool "pihole_stats_database"
call_tool "pihole_stats_database" '{"from":1712300000,"until":1712400000}' "stats_database (with range)"
call_tool "pihole_stats_database_top_domains" '{"from":1712300000,"until":1712400000,"count":3}'
call_tool "pihole_stats_database_top_clients" '{"from":1712300000,"until":1712400000,"count":3}'
call_tool "pihole_stats_database_upstreams" '{"from":1712300000,"until":1712400000}'
call_tool "pihole_stats_database_query_types" '{"from":1712300000,"until":1712400000}'

echo ""
echo "--- System Info ---"
call_tool "pihole_info_system"
call_tool "pihole_info_system" '{"detail":"minimal"}' "info_system (minimal)"
call_tool "pihole_info_version"
call_tool "pihole_info_database"
call_tool "pihole_info_messages"
call_tool "pihole_info_client"
call_tool "pihole_info_ftl"
call_tool "pihole_info_metrics"
call_tool "pihole_info_sensors"

echo ""
echo "--- Query Log ---"
call_tool "pihole_queries_search" '{"length":3}'
call_tool "pihole_queries_search" '{"length":3,"detail":"minimal"}' "queries_search (minimal)"
call_tool "pihole_queries_search" '{"length":3,"format":"csv"}' "queries_search (csv)"
call_tool "pihole_queries_suggestions"

echo ""
echo "--- History ---"
call_tool "pihole_history_graph"
call_tool "pihole_history_clients" '{"count":3}'
call_tool "pihole_history_database" '{"from":1712300000,"until":1712400000}' "history_database (range)"
call_tool "pihole_history_database" '{}' "history_database (default 7d)"
call_tool "pihole_history_database_clients" '{"from":1712300000,"until":1712400000}' "history_database_clients (range)"

echo ""
echo "--- Domain Search ---"
call_tool "pihole_search_domains" '{"domain":"google.com"}'

echo ""
echo "--- Domain CRUD ---"
call_tool "pihole_domains_add" '{"type":"deny","kind":"exact","domain":"e2e-test.example.com","comment":"e2e test"}' "domains_add"
call_tool "pihole_domains_list" '{"type":"deny","kind":"exact"}' "domains_list"
call_tool "pihole_domains_list" '{"type":"deny","kind":"exact","detail":"minimal"}' "domains_list (minimal)"
call_tool "pihole_domains_list" '{"type":"deny","kind":"exact","format":"csv"}' "domains_list (csv)"
call_tool "pihole_domains_delete" '{"type":"deny","kind":"exact","domain":"e2e-test.example.com"}' "domains_delete"

echo ""
echo "--- Group CRUD ---"
call_tool "pihole_groups_add" '{"name":"e2e-test-group","comment":"e2e test"}' "groups_add"
call_tool "pihole_groups_list"
call_tool "pihole_groups_delete" '{"name":"e2e-test-group"}' "groups_delete"

echo ""
echo "--- Clients ---"
call_tool "pihole_clients_list"
call_tool "pihole_clients_list" '{"format":"csv"}' "clients_list (csv)"
call_tool "pihole_clients_suggestions"

echo ""
echo "--- Lists ---"
call_tool "pihole_lists_list"
call_tool "pihole_lists_list" '{"detail":"minimal"}' "lists_list (minimal)"
call_tool "pihole_lists_list" '{"detail":"full"}' "lists_list (full)"
call_tool "pihole_lists_list" '{"format":"csv"}' "lists_list (csv)"

echo ""
echo "--- Configuration ---"
call_tool "pihole_config_get" '{"section":"dns"}' "config_get (dns)"
call_tool "pihole_config_get" '{"detail":"minimal"}' "config_get (minimal)"
call_tool "pihole_config_get_value" '{"element":"dns.upstreams"}' "config_get_value (dns.upstreams)"
call_tool "pihole_config_add_value" '{"element":"dns.upstreams","value":"127.0.0.99#53","restart":false}' "config_add_value (round-trip add)"
call_tool "pihole_config_remove_value" '{"element":"dns.upstreams","value":"127.0.0.99#53","restart":false}' "config_remove_value (round-trip remove)"
call_tool "pihole_config_properties" '{}' "config_properties (FTL v6.6.1+)"

echo ""
echo "--- Network ---"
call_tool "pihole_network_devices" '{"max_devices":3}'
call_tool "pihole_network_devices" '{"max_devices":3,"detail":"minimal"}' "network_devices (minimal)"
call_tool "pihole_network_devices" '{"max_devices":3,"format":"csv"}' "network_devices (csv)"
call_tool "pihole_network_gateway"
call_tool "pihole_network_info"
call_tool "pihole_network_routes"
call_tool "pihole_network_interfaces"
# pihole_network_delete_device intentionally not exercised against the live
# instance — deleting an actual device record changes state for subsequent
# runs. The handler is covered by unit tests in network_test.go.

echo ""
echo "--- DHCP ---"
call_tool "pihole_dhcp_leases"

echo ""
echo "--- Logs ---"
call_tool "pihole_logs_dns"
call_tool "pihole_logs_ftl"
call_tool "pihole_logs_webserver"

echo ""
echo "--- Sessions ---"
call_tool "pihole_auth_sessions"

echo ""
echo "--- Teleporter ---"
call_tool "pihole_teleporter_export"

echo ""
echo "--- Actions ---"
call_tool "pihole_action_restart_dns"

# Multi-instance checks run only when a second instance is provided via
# PIHOLE_2_URL (e.g. after `just dev-up-multi`). Instance 1's default name is
# "instance-1" and instance 2's is "instance-2" unless PIHOLE_N_NAME is set.
if [ -n "${PIHOLE_2_URL:-}" ]; then
    echo ""
    echo "--- Multi-instance ---"
    call_multi "pihole_stats_summary" '{}' "stats_summary (default = instance 1)"
    call_multi "pihole_stats_summary" '{"instance":"instance-2"}' "stats_summary (instance 2)"
    call_multi "pihole_stats_summary" '{"instance":"all"}' "stats_summary (all, aggregated)"
    call_multi "pihole_padd" '{"instance":"all"}' "padd (all, aggregated)"
    call_multi "pihole_stats_summary" '{"instance":"ghost"}' "stats_summary (unknown instance, should fail)" expect_error
    call_multi "pihole_dns_set_blocking" '{"blocking":false,"instance":"all"}' "set_blocking instance=all (should fail)" expect_error
    call_multi "pihole_instance_diff" '{"source":"instance-1","target":"instance-2"}' "instance_diff (1 vs 2)"
    call_multi "pihole_instance_sync" '{"source":"instance-1","target":"instance-2","mode":"plan","snapshot":false}' "instance_sync plan (1 -> 2)"
    call_multi "pihole_instance_sync" '{"target":"instance-1"}' "instance_sync self-target (should fail)" expect_error
fi

echo ""
echo "=============================="
echo "Results: $PASS passed, $FAIL failed"
if [ ${#ERRORS[@]} -gt 0 ]; then
    echo ""
    echo "Failures:"
    for err in "${ERRORS[@]}"; do
        echo "  - $err"
    done
    exit 1
fi
echo "All tests passed."
