#!/usr/bin/env python3
"""cmux-bridge — talk to cmux via forwarded Unix socket.

Since cmux CLI isn't installed on the homelab, this script speaks
the cmux socket protocol directly (newline-terminated JSON).

Usage:
    cmux-bridge browser-open <url>           Open URL in embedded browser
    cmux-bridge set-status <key> <value>     Set sidebar status
    cmux-bridge clear-status <key>           Clear sidebar status
    cmux-bridge notify <title> <body>        Send notification
    cmux-bridge raw <method> [json-params]   Send raw socket command
"""

import json
import os
import socket
import sys
import uuid

SOCKET_PATH = os.environ.get("CMUX_SOCKET_PATH", "/tmp/cmux-remote.sock")
WORKSPACE_ID = os.environ.get("CMUX_WORKSPACE_ID", "")


def send_command(method: str, params: dict | None = None) -> dict | None:
    """Send a JSON command to cmux socket, return response."""
    if not os.path.exists(SOCKET_PATH):
        print(f"Socket not found: {SOCKET_PATH}", file=sys.stderr)
        sys.exit(1)

    request = {
        "id": str(uuid.uuid4())[:8],
        "method": method,
        "params": params or {},
    }

    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    try:
        sock.connect(SOCKET_PATH)
        sock.settimeout(5.0)
        msg = json.dumps(request) + "\n"
        sock.sendall(msg.encode())

        # Read response (newline-terminated JSON)
        buf = b""
        while b"\n" not in buf:
            chunk = sock.recv(4096)
            if not chunk:
                break
            buf += chunk

        if buf:
            return json.loads(buf.decode().strip())
        return None
    except Exception as e:
        print(f"Socket error: {e}", file=sys.stderr)
        sys.exit(1)
    finally:
        sock.close()


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(1)

    if sys.argv[1] in ("-h", "--help"):
        print(__doc__)
        sys.exit(0)

    cmd = sys.argv[1]

    if cmd == "browser-open":
        url = sys.argv[2] if len(sys.argv) > 2 else None
        if not url:
            print("Usage: cmux-bridge browser-open <url>", file=sys.stderr)
            sys.exit(1)
        params = {"url": url}
        if WORKSPACE_ID:
            params["workspace_id"] = WORKSPACE_ID
        resp = send_command("browser.open_split", params)
        if resp:
            print(json.dumps(resp, indent=2))

    elif cmd == "set-status":
        if len(sys.argv) < 4:
            print("Usage: cmux-bridge set-status <key> <value> [--color hex] [--icon name] [--priority n]", file=sys.stderr)
            sys.exit(1)
        key, value = sys.argv[2], sys.argv[3]
        params = {"key": key, "value": value}
        if WORKSPACE_ID:
            params["tab"] = WORKSPACE_ID
        # Parse optional flags
        args = sys.argv[4:]
        for i, arg in enumerate(args):
            if arg == "--color" and i + 1 < len(args):
                params["color"] = args[i + 1]
            elif arg == "--icon" and i + 1 < len(args):
                params["icon"] = args[i + 1]
            elif arg == "--priority" and i + 1 < len(args):
                # cmux v0.64.7 added --priority to the native `cmux set-status` CLI
                # (sort priority in the sidebar, default 0); mirrored here since this
                # script speaks the same set_status socket method directly.
                params["priority"] = int(args[i + 1])
        resp = send_command("set_status", params)
        if resp:
            print(json.dumps(resp, indent=2))

    elif cmd == "clear-status":
        key = sys.argv[2] if len(sys.argv) > 2 else None
        if not key:
            print("Usage: cmux-bridge clear-status <key>", file=sys.stderr)
            sys.exit(1)
        params = {"key": key}
        if WORKSPACE_ID:
            params["tab"] = WORKSPACE_ID
        resp = send_command("clear_status", params)
        if resp:
            print(json.dumps(resp, indent=2))

    elif cmd == "notify":
        if len(sys.argv) < 4:
            print("Usage: cmux-bridge notify <title> <body>", file=sys.stderr)
            sys.exit(1)
        resp = send_command("notification.create", {
            "title": sys.argv[2],
            "body": sys.argv[3],
        })
        if resp:
            print(json.dumps(resp, indent=2))

    elif cmd == "raw":
        method = sys.argv[2] if len(sys.argv) > 2 else None
        if not method:
            print("Usage: cmux-bridge raw <method> [json-params]", file=sys.stderr)
            sys.exit(1)
        params = json.loads(sys.argv[3]) if len(sys.argv) > 3 else {}
        resp = send_command(method, params)
        if resp:
            print(json.dumps(resp, indent=2))

    else:
        print(f"Unknown command: {cmd}", file=sys.stderr)
        print(__doc__)
        sys.exit(1)


if __name__ == "__main__":
    main()
