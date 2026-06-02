#!/usr/bin/env node
/*
 * Loopback-bind wrapper for @upstash/mcp-server.
 *
 * Why: the upstream binary calls `httpServer.listen(port, cb)` with no host
 * argument, so Node binds 0.0.0.0/:: (all interfaces). The server carries
 * Upstash account credentials and MUST NOT be reachable from the LAN. There
 * is no --host/--bind CLI flag (v0.2.3). We monkey-patch net.Server.prototype
 * .listen so any call passing only (port, ...) is rewritten to bind 127.0.0.1.
 *
 * Egress to api.upstash.com is unaffected (this only constrains the listening
 * socket), so the server's startup connection check still works.
 */
const net = require("net");
const origListen = net.Server.prototype.listen;
net.Server.prototype.listen = function (...args) {
  if (typeof args[0] === "number") {
    // listen(port[, host][, backlog][, cb]) -> force host = 127.0.0.1
    const port = args[0];
    const rest = args.slice(1);
    // If a host string is already present as the 2nd arg, leave it; else inject.
    if (typeof rest[0] === "string") {
      rest[0] = "127.0.0.1";
      return origListen.call(this, port, ...rest);
    }
    return origListen.call(this, port, "127.0.0.1", ...rest);
  }
  if (args[0] && typeof args[0] === "object" && typeof args[0].port === "number") {
    args[0] = { ...args[0], host: "127.0.0.1" };
  }
  return origListen.apply(this, args);
};

// Resolve the real entrypoint portably. pnpm's global store path embeds a
// store-version dir (".../pnpm/global/<N>/node_modules") that changes across
// pnpm majors and differs per user, so we resolve it at runtime instead of
// hardcoding. Strategy: require.resolve via NODE_PATH-style probing of the
// known pnpm global locations, then fall back to a plain require (works if the
// package is on the default resolution path).
const path = require("node:path");
const fs = require("node:fs");

function resolveEntrypoint() {
  const home = process.env.HOME || require("node:os").homedir();
  const candidates = [];
  // pnpm global store: ~/.local/share/pnpm/global/<N>/node_modules/...
  const pnpmGlobal = path.join(home, ".local/share/pnpm/global");
  try {
    for (const v of fs.readdirSync(pnpmGlobal)) {
      candidates.push(
        path.join(pnpmGlobal, v, "node_modules/@upstash/mcp-server/dist/index.js"),
      );
    }
  } catch {
    /* dir absent on this machine — fall through to other candidates */
  }
  for (const p of candidates) {
    if (fs.existsSync(p)) return p;
  }
  // Last resort: default Node resolution (global node_modules on NODE_PATH).
  return "@upstash/mcp-server/dist/index.js";
}

// Hand off to the real entrypoint with argv intact.
require(resolveEntrypoint());
