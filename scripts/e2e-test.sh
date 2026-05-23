#!/usr/bin/env bash
# End-to-end test script for pihole-mcp.
# Sends tool calls sequentially (one at a time) to avoid overwhelming Pi-hole.
# Usage: PIHOLE_URL=http://localhost:8081 PIHOLE_PASSWORD=test ./scripts/e2e-test.sh
set -euo pipefail

BINARY="${1:-./bin/pihole-mcp}"
PASS=0
FAIL=0
ERRORS=()

call_tool() {
    local name="$1"
    local args="${2:-{}}"
    local label="${3:-$name}"

    local result
    result=$(printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}\n{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"%s","arguments":%s}}\n' "$name" "$args" \
        | PIHOLE_URL="${PIHOLE_URL}" PIHOLE_PASSWORD="${PIHOLE_PASSWORD}" timeout 30 "$BINARY" 2>/dev/null \
        | tail -1)

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

echo "=== pihole-mcp E2E Test Suite ==="
echo "Binary: $BINARY"
echo "Pi-hole: ${PIHOLE_URL}"
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
