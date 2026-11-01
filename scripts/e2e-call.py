#!/usr/bin/env python3
"""Drive one MCP tool call against a pihole-mcp binary over stdio.

Usage: e2e-call.py <binary> <tool-name> <arguments-json>

Set E2E_PROTOCOL=2026-07-28 to talk to the server the way a client on that
revision does: no initialize handshake, and the protocol version, client
identity and capabilities carried in _meta on the request itself.

Prints the JSON-RPC response to the tools/call request on stdout, or nothing
if no response arrived in time. scripts/e2e-test.sh treats empty output as a
transient failure and retries, so this script never prints a partial result.

Why not a shell pipe: piping both requests in and closing stdin straight away
races the server. From mcp-go 1.1.0 onwards, end of input cancels requests
still in flight, so a call queued behind initialize can be dropped or answered
with "context canceled". A real client keeps stdin open for the life of the
session; this does the same until the response it is waiting for has arrived,
then closes stdin and waits for the server to exit so it can end its Pi-hole
session and free the seat.
"""

import json
import os
import queue
import subprocess
import sys
import threading
import time

TIMEOUT_SECONDS = 30
PROTOCOL_VERSION = "2025-11-25"
MODERN_PROTOCOL = "2026-07-28"


def main() -> int:
    binary, name, raw_args = sys.argv[1], sys.argv[2], sys.argv[3]
    arguments = json.loads(raw_args)
    deadline = time.monotonic() + TIMEOUT_SECONDS

    proc = subprocess.Popen(
        [binary],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        text=True,
        bufsize=1,
    )

    # A reader thread keeps select() and Python's line buffering from
    # disagreeing about whether a complete line is waiting.
    lines: "queue.Queue[str | None]" = queue.Queue()

    def pump() -> None:
        assert proc.stdout is not None
        for line in proc.stdout:
            lines.put(line)
        lines.put(None)

    threading.Thread(target=pump, daemon=True).start()

    def send(message: dict) -> None:
        assert proc.stdin is not None
        proc.stdin.write(json.dumps(message) + "\n")
        proc.stdin.flush()

    def await_response(request_id: int) -> "str | None":
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                return None
            try:
                line = lines.get(timeout=remaining)
            except queue.Empty:
                return None
            if line is None:
                return None
            try:
                message = json.loads(line)
            except ValueError:
                continue
            # Skip notifications (progress, logging) that may precede or follow
            # the response; only the matching id ends the wait.
            if message.get("id") == request_id:
                return line.strip()

    try:
        if os.environ.get("E2E_PROTOCOL") == MODERN_PROTOCOL:
            send({
                "jsonrpc": "2.0",
                "id": 2,
                "method": "tools/call",
                "params": {
                    "name": name,
                    "arguments": arguments,
                    "_meta": {
                        "io.modelcontextprotocol/protocolVersion": MODERN_PROTOCOL,
                        "io.modelcontextprotocol/clientInfo": {"name": "e2e", "version": "1"},
                        "io.modelcontextprotocol/clientCapabilities": {},
                    },
                },
            })
            response = await_response(2)
            if response is not None:
                print(response)
            return 0

        send({
            "jsonrpc": "2.0",
            "id": 1,
            "method": "initialize",
            "params": {
                "protocolVersion": PROTOCOL_VERSION,
                "capabilities": {},
                "clientInfo": {"name": "e2e", "version": "1"},
            },
        })
        if await_response(1) is None:
            return 0
        send({"jsonrpc": "2.0", "method": "notifications/initialized"})
        send({
            "jsonrpc": "2.0",
            "id": 2,
            "method": "tools/call",
            "params": {"name": name, "arguments": arguments},
        })
        response = await_response(2)
        if response is not None:
            print(response)
    except (BrokenPipeError, OSError):
        # The server exited early. Empty output makes the caller retry.
        pass
    finally:
        try:
            if proc.stdin is not None:
                proc.stdin.close()
        except OSError:
            pass
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
    return 0


if __name__ == "__main__":
    sys.exit(main())
