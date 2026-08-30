#!/usr/bin/env bash
# End-to-end test script for pihole-mcp.
# Sends tool calls sequentially (one at a time) to avoid overwhelming Pi-hole.
# Usage: PIHOLE_URL=http://localhost:8081 PIHOLE_PASSWORD=test ./scripts/e2e-test.sh
#
# Read cases assert only that the call succeeded. Write cases must not: FTL
# answers 200 for a body it silently ignored, so a write that applied nothing
# looks identical to one that worked. Every write below is therefore followed by
# a read that asserts the new value, via call_tool_expect.
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

# run_tool <mode> <name> <args>
# Spawns one server, sends initialize + tools/call, and leaves the outcome in
# RESULT_IS_ERROR ("True"/"False") and RESULT_TEXT. <mode> selects which
# credentials the child sees: "single" for PIHOLE_URL, "multi" for the
# PIHOLE_1_*/PIHOLE_2_* pair.
#
# Every assertion helper goes through this one copy on purpose. The retry policy
# is subtle enough (transient classification, six attempts, exponential backoff)
# that a second copy would drift, and the copy that drifted would be the one
# quietly swallowing failures.
RESULT_IS_ERROR=""
RESULT_TEXT=""

run_tool() {
    local mode="$1"
    local name="$2"
    local args="$3"

    local request
    request=$(printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}\n{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"%s","arguments":%s}}\n' "$name" "$args")

    local result attempt=0
    while :; do
        if [ "$mode" = "multi" ]; then
            result=$(printf '%s\n' "$request" \
                | env -u PIHOLE_URL -u PIHOLE_PASSWORD \
                    PIHOLE_1_URL="${PIHOLE_1_URL}" PIHOLE_1_PASSWORD="${PIHOLE_1_PASSWORD}" PIHOLE_2_URL="${PIHOLE_2_URL}" PIHOLE_2_PASSWORD="${PIHOLE_2_PASSWORD}" timeout 30 "$BINARY" 2>/dev/null \
                | tail -1)
        else
            result=$(printf '%s\n' "$request" \
                | env -u PIHOLE_1_URL -u PIHOLE_1_PASSWORD -u PIHOLE_2_URL -u PIHOLE_2_PASSWORD \
                    PIHOLE_URL="${PIHOLE_URL}" PIHOLE_PASSWORD="${PIHOLE_PASSWORD}" timeout 30 "$BINARY" 2>/dev/null \
                | tail -1)
        fi
        if is_transient "$result" && [ "$attempt" -lt "$MAX_ATTEMPTS" ]; then
            attempt=$((attempt+1)); backoff "$attempt"; continue
        fi
        break
    done

    RESULT_IS_ERROR=$(echo "$result" | python3 -c "import sys,json;d=json.loads(sys.stdin.read());print(d.get('result',{}).get('isError',False))" 2>/dev/null)
    RESULT_TEXT=$(echo "$result" | python3 -c "import sys,json;d=json.loads(sys.stdin.read());[print(c.get('text','')) for c in d.get('result',{}).get('content',[])]" 2>/dev/null)
}

record_pass() {
    echo "  PASS: $1"
    PASS=$((PASS+1))
}

# record_fail <label> [detail]
# The detail is the first line of whatever came back. Without it a red line in
# CI tells you nothing you can act on. Omit it where there is nothing useful to
# quote.
record_fail() {
    local label="$1"
    local detail="${2:-}"
    echo "  FAIL: $label"
    FAIL=$((FAIL+1))
    if [ -n "$detail" ]; then
        detail=${detail%%$'\n'*}
        echo "        ${detail:0:120}"
        ERRORS+=("$label: ${detail:0:100}")
    else
        ERRORS+=("$label")
    fi
}

# call_tool <name> [args] [label] [expect_error]
# A non-empty 4th argument inverts the check: the call is expected to fail.
call_tool() {
    local name="$1"
    local args="${2:-}"
    [ -z "$args" ] && args='{}'
    local label="${3:-$name}"
    local expect_error="${4:-}"

    run_tool "single" "$name" "$args"

    local failed="$RESULT_IS_ERROR"
    if [ -n "$expect_error" ]; then
        # Invert: the call was supposed to fail, so success is the failure.
        [ "$RESULT_IS_ERROR" = "True" ] && failed="False" || failed="True"
    fi

    if [ "$failed" = "True" ]; then
        record_fail "$label" "$RESULT_TEXT"
    else
        record_pass "$label"
    fi
}

# call_tool_expect <name> <args> <label> <substring>
# Runs the tool exactly as call_tool does, then FAILS unless <substring> appears
# in the returned text.
#
# This is the difference between proving a write landed and proving only that
# the server answered. The config_set double-wrap no-op shipped past a fully
# green suite because every case here asked "did it error?" and none asked "did
# anything change?". Use this for the read-back after every write.
call_tool_expect() {
    local name="$1"
    local args="${2:-}"
    [ -z "$args" ] && args='{}'
    local label="${3:-$name}"
    local want="$4"

    run_tool "single" "$name" "$args"

    if [ "$RESULT_IS_ERROR" = "True" ]; then
        record_fail "$label" "$RESULT_TEXT"
        return 0
    fi

    case "$RESULT_TEXT" in
        *"$want"*) record_pass "$label" ;;
        *) record_fail "$label" "expected \"$want\" in: $RESULT_TEXT" ;;
    esac
}

# call_tool_expect_absent <name> <args> <label> <substring>
# The mirror of call_tool_expect: FAILS if <substring> is still there. A delete
# that deleted nothing returns the same 200 as one that worked, so the only
# honest check after a removal is that the value has stopped coming back.
call_tool_expect_absent() {
    local name="$1"
    local args="${2:-}"
    [ -z "$args" ] && args='{}'
    local label="${3:-$name}"
    local unwanted="$4"

    run_tool "single" "$name" "$args"

    if [ "$RESULT_IS_ERROR" = "True" ]; then
        record_fail "$label" "$RESULT_TEXT"
        return 0
    fi

    case "$RESULT_TEXT" in
        *"$unwanted"*) record_fail "$label" "expected \"$unwanted\" to be gone, found: $RESULT_TEXT" ;;
        *) record_pass "$label" ;;
    esac
}

# call_multi runs a tool against a multi-instance server (PIHOLE_1_*, PIHOLE_2_*).
# A non-empty 4th argument inverts the result check (the call is expected to fail).
call_multi() {
    local name="$1"
    local args="${2:-}"
    [ -z "$args" ] && args='{}'
    local label="${3:-$name}"
    local expect_error="${4:-}"

    run_tool "multi" "$name" "$args"

    local ok="True"
    if [ -n "$expect_error" ]; then
        [ "$RESULT_IS_ERROR" = "True" ] && ok="True" || ok="False"
    else
        [ "$RESULT_IS_ERROR" = "True" ] && ok="False" || ok="True"
    fi

    if [ "$ok" = "True" ]; then
        record_pass "$label"
    else
        record_fail "$label"
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
# The timer is 10s rather than 3s because the verify below now reads the value
# back instead of merely running: the window has to outlive a process spawn plus
# a possible transient retry, or the case fails on timing rather than on
# behaviour. FTL would revert on its own, but the explicit re-enable afterwards
# means the rest of the suite does not run against a half-disabled instance.
call_tool "pihole_dns_set_blocking" '{"blocking":false,"timer":10}' "dns_set_blocking (disable 10s)"
sleep 1
# dns.go:58 emits fmt.Sprintf("**Blocking:** %s", status.Blocking), where
# status.Blocking is FTL's own state string ("enabled"/"disabled").
call_tool_expect "pihole_dns_get_blocking" '{}' "dns_get_blocking (verify disabled)" "**Blocking:** disabled"
call_tool "pihole_dns_set_blocking" '{"blocking":true}' "dns_set_blocking (re-enable)"
call_tool_expect "pihole_dns_get_blocking" '{}' "dns_get_blocking (verify re-enabled)" "**Blocking:** enabled"

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
# A healthy Pi-hole carries no diagnostic messages, so there is nothing to
# dismiss for real here; dismissing an ID that cannot exist still exercises the
# route, the auth path and the error surfacing, and must fail cleanly rather
# than report success.
call_tool "pihole_info_dismiss_message" '{"id":999999}' "info_dismiss_message (unknown id, should fail)" expect_error
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
# domains.go:128 emits fmt.Fprintf(&b, "- %s (%s/%s, %s)", d.Domain, d.Type,
# d.Kind, status) for every row, so an entry that really landed comes back
# verbatim in the list body. "**Domain added.**" from the add call proves only
# that FTL accepted the request.
call_tool_expect "pihole_domains_list" '{"type":"deny","kind":"exact"}' "domains_list (added domain present)" "e2e-test.example.com"
call_tool "pihole_domains_list" '{"type":"deny","kind":"exact","detail":"minimal"}' "domains_list (minimal)"
# domains.go:116 builds the CSV rows from that same slice, so the domain has to
# survive the formatter as well as the API.
call_tool_expect "pihole_domains_list" '{"type":"deny","kind":"exact","format":"csv"}' "domains_list (csv)" "e2e-test.example.com"
# An update edits the comment only. The comment is what domains.go:130 appends
# after the status when detail is normal, so a comment that did not reach FTL is
# invisible in the "**Updated**" reply but plain in the list body.
call_tool "pihole_domains_update" '{"type":"deny","kind":"exact","domain":"e2e-test.example.com","comment":"e2e amended"}' "domains_update"
call_tool_expect "pihole_domains_list" '{"type":"deny","kind":"exact"}' "domains_list (update reached FTL)" "e2e amended"
call_tool "pihole_domains_delete" '{"type":"deny","kind":"exact","domain":"e2e-test.example.com"}' "domains_delete"
# An empty list renders as "No domains found." (domains.go:87), which satisfies
# the absence check just as a populated list without this entry would.
call_tool_expect_absent "pihole_domains_list" '{"type":"deny","kind":"exact"}' "domains_list (deleted domain gone)" "e2e-test.example.com"

echo ""
echo "--- Group CRUD ---"
call_tool "pihole_groups_add" '{"name":"e2e-test-group","comment":"e2e test"}' "groups_add"
# groups.go:84 emits fmt.Fprintf(&b, "- %s (id=%d, %s)", g.Name, g.ID, status).
call_tool_expect "pihole_groups_list" '{}' "groups_list (added group present)" "e2e-test-group"
call_tool "pihole_groups_update" '{"name":"e2e-test-group","comment":"e2e amended"}' "groups_update"
call_tool_expect "pihole_groups_list" '{}' "groups_list (update reached FTL)" "e2e amended"
call_tool "pihole_groups_delete" '{"name":"e2e-test-group"}' "groups_delete"
call_tool_expect_absent "pihole_groups_list" '{}' "groups_list (deleted group gone)" "e2e-test-group"

echo ""
echo "--- Clients ---"
call_tool "pihole_clients_list"
call_tool "pihole_clients_list" '{"format":"csv"}' "clients_list (csv)"
call_tool "pihole_clients_suggestions"
# Full CRUD against a documentation-range address (RFC 5737 TEST-NET-3), so the
# row can never collide with a real device on the machine running this.
call_tool "pihole_clients_add" '{"client":"203.0.113.7","comment":"e2e test"}' "clients_add"
# clients.go:96 emits fmt.Fprintf(&b, "- %s", c.Client) and appends the comment
# after an em-space separator, so both fields come back in the list body.
call_tool_expect "pihole_clients_list" '{}' "clients_list (added client present)" "203.0.113.7"
call_tool "pihole_clients_update" '{"client":"203.0.113.7","comment":"e2e amended"}' "clients_update"
call_tool_expect "pihole_clients_list" '{}' "clients_list (update reached FTL)" "e2e amended"
call_tool "pihole_clients_delete" '{"client":"203.0.113.7"}' "clients_delete"
call_tool_expect_absent "pihole_clients_list" '{}' "clients_list (deleted client gone)" "203.0.113.7"

echo ""
echo "--- Lists ---"
call_tool "pihole_lists_list"
call_tool "pihole_lists_list" '{"detail":"minimal"}' "lists_list (minimal)"
call_tool "pihole_lists_list" '{"detail":"full"}' "lists_list (full)"
call_tool "pihole_lists_list" '{"format":"csv"}' "lists_list (csv)"
# Full CRUD against an example.com address, which is reserved by RFC 2606 and
# will never resolve, so gravity never fetches anything from it. The address
# carries slashes on purpose: FTL takes the whole remainder after /api/lists as
# one item, so a handler that split the path would address the wrong row.
call_tool "pihole_lists_add" '{"address":"https://e2e.example.com/blocklist.txt","type":"block","comment":"e2e test"}' "lists_add"
# lists.go:105 emits fmt.Fprintf(&b, "- %s (%s, %d domains, %s)", ...) followed
# by the comment, so the address and the comment both appear in the list body.
call_tool_expect "pihole_lists_list" '{}' "lists_list (added list present)" "https://e2e.example.com/blocklist.txt"
call_tool "pihole_lists_update" '{"address":"https://e2e.example.com/blocklist.txt","type":"block","comment":"e2e amended"}' "lists_update"
call_tool_expect "pihole_lists_list" '{}' "lists_list (update reached FTL)" "e2e amended"
call_tool "pihole_lists_delete" '{"address":"https://e2e.example.com/blocklist.txt","type":"block"}' "lists_delete"
call_tool_expect_absent "pihole_lists_list" '{}' "lists_list (deleted list gone)" "e2e.example.com/blocklist.txt"

echo ""
echo "--- Configuration ---"
call_tool "pihole_config_get" '{"section":"dns"}' "config_get (dns)"
call_tool "pihole_config_get" '{"detail":"minimal"}' "config_get (minimal)"
call_tool "pihole_config_get_value" '{"element":"dns.upstreams"}' "config_get_value (dns.upstreams)"
# The probe value deliberately carries NO '#'. config.go:245 splices the value
# into the path unescaped, so http.NewRequestWithContext reads everything from a
# '#' onwards as a URL fragment and discards it: "127.0.0.99#53" reaches FTL as
# "127.0.0.99", and restart=false is swallowed with it. A read-back anchored on
# "127.0.0.99#53" could therefore never pass, and its absence check would be a
# tautology that passes just as happily against a remove that did nothing.
# Anchoring on a value the add genuinely lands and the remove genuinely clears
# is what lets both assertions fail when the round trip regresses.
#
# A host#port upstream cannot be exercised end to end until config.go:245 and
# :281 escape the value. The unit-level acceptance test for that fix is already
# written: TestConfigAddRemoveValue_ReservedCharactersAreEscaped in
# internal/tools/config_test.go, currently skipped. Add a '#'-bearing case here
# once it lands.
call_tool "pihole_config_add_value" '{"element":"dns.upstreams","value":"127.0.0.99","restart":false}' "config_add_value (round-trip add)"
# config.go:219 emits fmt.Sprintf("**%s:** %s", element, formatted). Whether
# `formatted` is the bare array or the enclosing object depends on how FTL
# frames a reply for a path element, so anchor on the value itself rather than
# on prose that could legitimately change shape.
call_tool_expect "pihole_config_get_value" '{"element":"dns.upstreams"}' "config_get_value (upstream present after add)" "127.0.0.99"
call_tool "pihole_config_remove_value" '{"element":"dns.upstreams","value":"127.0.0.99","restart":false}' "config_remove_value (round-trip remove)"
call_tool_expect_absent "pihole_config_get_value" '{"element":"dns.upstreams"}' "config_get_value (upstream gone after remove)" "127.0.0.99"
call_tool "pihole_config_set" '{"config":"{\"dns\":{\"cache\":{\"size\":10001}}}"}' "config_set (round-trip write)"
# The echo in the config_set reply (config.go:172) comes from FTL's own
# response, and FTL answers 200 for a body whose keys it ignored, so the echo is
# not evidence. Asserting the value on a separate read is. 10001 and 10000 are
# not substrings of one another, so each assertion below can only pass on its
# own write.
call_tool_expect "pihole_config_get_value" '{"element":"dns.cache.size"}' "config_get_value (cache size now 10001)" "10001"
call_tool "pihole_config_set" '{"config":"{\"config\":{\"dns\":{\"cache\":{\"size\":10000}}}}"}' "config_set (already-wrapped payload, restores default)"
# This is the case the double-wrap bug hid behind. config.go:156 unwraps a lone
# "config" key before sending; if that unwrap regresses, FTL is handed
# {"config":{"config":{...}}}, discards it, keeps 10001 and still returns 200.
# Without this read-back the silent no-op passes.
call_tool_expect "pihole_config_get_value" '{"element":"dns.cache.size"}' "config_get_value (cache size restored to 10000)" "10000"
call_tool "pihole_config_set" '{"config":"{not json"}' "config_set (rejects malformed JSON)" "expect_error"
call_tool "pihole_config_set" '{"config":"[1,2,3]"}' "config_set (rejects non-object)" "expect_error"
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
# The dev Pi-hole runs with DHCP disabled, so there is no real lease to remove
# and removing one would change state later runs depend on. Deleting an address
# that cannot hold a lease still exercises the route, the auth path and the
# error surfacing, and must fail cleanly rather than report success. Verified
# against Pi-hole v6 2026.07.2: "Failed to delete DHCP lease (DHCP is not
# enabled)".
call_tool "pihole_dhcp_delete_lease" '{"ip":"203.0.113.250"}' "dhcp_delete_lease (no such lease, should fail)" expect_error

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
# call_tool leaves the reply in RESULT_TEXT, and teleporter.go:61 renders the
# path as "File: <path> (<size> bytes, <when>)", so the archive this run just
# wrote can be fed straight back in. Importing an instance's own backup is a
# round trip rather than a state change, and gravity-only keeps it away from the
# config the Configuration section above asserts on.
EXPORTED_BACKUP=$(printf '%s' "$RESULT_TEXT" | grep -oE '/[^ ]+\.zip' | head -1)
if [ -n "$EXPORTED_BACKUP" ]; then
    call_tool_expect "pihole_teleporter_import" "{\"file_path\":\"${EXPORTED_BACKUP}\",\"config\":false,\"gravity\":true,\"dhcp_leases\":false}" "teleporter_import (round trip of this run's export)" "Import complete"
    rm -f "$EXPORTED_BACKUP"
else
    record_fail "teleporter_import (round trip)" "could not read the archive path out of the export reply"
fi

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
