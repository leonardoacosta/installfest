/**
 * Multi-account usage dashboard logic — JS port of
 * `apps/cc-tmux/src/cc_tmux/usage.py`'s pure presentation/dedupe helpers
 * (`color_for`, `pct_for`, `_extract_util`, `_extract_reset_at`,
 * `_account_label`, `dedupe_credentials`, `_freshest_active`).
 *
 * Ported for openspec/changes/add-cmux-sidebar-widgets task [2.2] (beads:if-5oeg).
 * Faithful port: same thresholds, same None/absent-field handling, same
 * fail-open invariant as the Python original (see usage.py's module
 * docstring, "Invariant 5 (fail open)"). Consumed by index.html, which
 * fetches http://localhost:7400/credentials client-side.
 *
 * Countdown/refill-time formatting (formatCountdown/formatRefillTime) has NO
 * Python equivalent in usage.py -- that file only exposes `_extract_reset_at`
 * (ISO-8601 -> epoch seconds), with no display formatting. The two format*
 * functions below are new, derived logic matching the reference screenshot's
 * "Resets in 2h 7m" / "refills Wed at 6:25 PM" style, not a port.
 */

// tmux colour codes -- identical to usage.py's DIM/CYAN/YELLOW/RED constants.
export const DIM = "#454D54";
export const CYAN = "#5BD1B9";
export const YELLOW = "#FAC760";
export const RED = "#E61F44";

/**
 * Colour for a utilization value, mirroring usage.py's `color_for`.
 *
 * null/undefined (absent field) -> DIM; > 0.80 -> RED; >= 0.50 -> YELLOW;
 * otherwise CYAN. A present 0.0 is CYAN, not DIM (only a genuinely absent
 * field dims).
 */
export function colorFor(util) {
  if (util === null || util === undefined) return DIM;
  if (util > 0.8) return RED;
  if (util >= 0.5) return YELLOW;
  return CYAN;
}

/**
 * Percent label for a utilization value, mirroring usage.py's `pct_for`.
 *
 * null/undefined -> "--"; otherwise `util * 100` rounded to a whole percent.
 * Note: uses JS's round-half-away-from-zero (Math.round), not Python's
 * round-half-to-even -- a documented, cosmetic-only divergence that can only
 * differ at an exact .5 percentage-point boundary.
 */
export function pctFor(util) {
  if (util === null || util === undefined) return "--";
  return `${Math.round(util * 100)}%`;
}

/**
 * `<usedKey> / <limitKey>` as a 0..1 number, or null when unusable.
 *
 * Mirrors usage.py's `_extract_util`: a missing/null/non-numeric `used` or
 * `limit`, or a `limit <= 0`, all map to null (the "not polled yet / nothing
 * to show" case).
 */
export function extractUtil(credential, usedKey, limitKey) {
  const used = credential ? credential[usedKey] : undefined;
  const limit = credential ? credential[limitKey] : undefined;
  if (typeof used === "boolean" || typeof limit === "boolean") return null;
  if (used === null || used === undefined || limit === null || limit === undefined) {
    return null;
  }
  const usedF = Number(used);
  const limitF = Number(limit);
  if (Number.isNaN(usedF) || Number.isNaN(limitF)) return null;
  if (limitF <= 0) return null;
  return usedF / limitF;
}

/**
 * Epoch-seconds reset time for `key` (e.g. "usage5hResetAt"), or null.
 *
 * Mirrors usage.py's `_extract_reset_at`: a missing, non-string, or
 * unparseable value -> null (fail-open, same contract as extractUtil).
 * nx-agent serialises reset columns as ISO-8601 strings (Z- or
 * offset-suffixed), which `Date.parse` handles natively.
 */
export function extractResetAt(credential, key) {
  const value = credential ? credential[key] : undefined;
  if (typeof value !== "string" || !value) return null;
  const ms = Date.parse(value);
  if (Number.isNaN(ms)) return null;
  return ms / 1000;
}

/**
 * Account label: full email + first 8 chars of the org id, e.g.
 * "leo@x.dev·bc7da511". Mirrors usage.py's `_account_label`.
 *
 * Falls back to accountName/name (no suffix) when there's no email to
 * anchor the org suffix to.
 */
export function accountLabel(credential) {
  const email = credential ? credential.accountEmail : undefined;
  if (typeof email === "string" && email) {
    const orgUuid = credential.orgUuid;
    if (typeof orgUuid === "string" && orgUuid) {
      return `${email}·${orgUuid.slice(0, 8)}`;
    }
    return email;
  }
  for (const key of ["accountName", "name"]) {
    const value = credential ? credential[key] : undefined;
    if (typeof value === "string" && value) return value;
  }
  return "";
}

function hasUsableTimestamp(value) {
  return typeof value === "string" && !!value;
}

/**
 * Data-presence-first, then-recency, then-last-wins tie-break shared by
 * `dedupeCredentials` (within a group) and `freshestActive` (across
 * groups) -- mirrors usage.py's identical duplicated logic in
 * `dedupe_credentials` and `_freshest_active`.
 *
 * Returns true when `candidate` should replace `existing`.
 */
function candidateWinsOnRecency(candidate, existing) {
  const newPolled = candidate.usagePolledAt;
  const oldPolled = existing.usagePolledAt;
  const newHasTs = hasUsableTimestamp(newPolled);
  const oldHasTs = hasUsableTimestamp(oldPolled);

  if (newHasTs && oldHasTs) return newPolled >= oldPolled;
  if (newHasTs && !oldHasTs) return true;
  if (oldHasTs && !newHasTs) return false;
  // Neither side has a usable timestamp -> no basis to prefer either;
  // last one wins (list/iteration order presumed oldest-to-newest).
  return true;
}

/**
 * Collapse duplicate (accountEmail, orgUuid) rows, most-recent/most-active
 * kept. Mirrors usage.py's `dedupe_credentials` faithfully:
 *
 * - Groups by (accountEmail, orgUuid) when accountEmail is present; falls
 *   back to the label accountLabel() would render when absent.
 * - Drops orphaned junk rows outright: no accountEmail AND
 *   status === "refresh_failed".
 * - Within a group, isActive: true ALWAYS wins over isActive: false,
 *   regardless of timestamps.
 * - Only when both candidates share the same isActive value does the
 *   recency tie-break apply (data-presence-first, not list-position-first).
 *
 * Pure function, no fetch. Non-array input -> []; non-object entries are
 * skipped (fail-open). Return order follows each group's first appearance.
 */
export function dedupeCredentials(credentials) {
  if (!Array.isArray(credentials)) return [];

  const order = [];
  const groups = new Map();

  for (const candidate of credentials) {
    if (typeof candidate !== "object" || candidate === null) continue;

    const email = candidate.accountEmail;
    const hasEmail = typeof email === "string" && !!email;
    if (!hasEmail && candidate.status === "refresh_failed") {
      continue; // orphaned junk row, never linked to a real account
    }

    const org = candidate.orgUuid;
    const orgKey = typeof org === "string" ? org : null;
    const key = JSON.stringify(hasEmail ? [email, orgKey] : [accountLabel(candidate), orgKey]);

    const existing = groups.get(key);
    if (existing === undefined) {
      order.push(key);
      groups.set(key, candidate);
      continue;
    }

    const existingActive = existing.isActive === true;
    const candidateActive = candidate.isActive === true;
    if (candidateActive && !existingActive) {
      groups.set(key, candidate);
      continue;
    }
    if (existingActive && !candidateActive) {
      continue; // never let a stale/inactive duplicate evict the live row
    }

    if (candidateWinsOnRecency(candidate, existing)) {
      groups.set(key, candidate);
    }
  }

  return order.map((key) => groups.get(key));
}

/**
 * Freshest isActive: true row in `deduped`, or null if there is none.
 * Mirrors usage.py's `_freshest_active` -- resolves the case where the same
 * identity is split across orgUuids and dedupeCredentials() legitimately
 * leaves more than one isActive: true row.
 */
export function freshestActive(deduped) {
  let best = null;
  for (const candidate of deduped) {
    if (candidate.isActive !== true) continue;
    if (best === null) {
      best = candidate;
      continue;
    }
    if (candidateWinsOnRecency(candidate, best)) {
      best = candidate;
    }
  }
  return best;
}

/**
 * "Resets in 2h 7m" / "Resets in 45m" / "Resets in <1m" / "Resetting…"
 * countdown string for a 5H-style reset. New logic (no Python equivalent) --
 * derived from the reference screenshot's countdown style. null/undefined
 * epoch -> "" (fail-open, same convention as the rest of this module).
 */
export function formatCountdown(resetEpochSeconds, nowEpochSeconds) {
  if (resetEpochSeconds === null || resetEpochSeconds === undefined) return "";
  const now = nowEpochSeconds !== undefined ? nowEpochSeconds : Date.now() / 1000;
  const deltaSeconds = resetEpochSeconds - now;
  if (deltaSeconds <= 0) return "Resetting…";
  const totalMinutes = Math.floor(deltaSeconds / 60);
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  if (hours > 0) return `Resets in ${hours}h ${minutes}m`;
  if (minutes > 0) return `Resets in ${minutes}m`;
  return "Resets in <1m";
}

/**
 * "refills Wed at 6:25 PM" style string for a 7D-style reset. New logic (no
 * Python equivalent). Uses the browser's local timezone/locale.
 * null/undefined epoch -> "" (fail-open).
 */
export function formatRefillTime(resetEpochSeconds) {
  if (resetEpochSeconds === null || resetEpochSeconds === undefined) return "";
  const date = new Date(resetEpochSeconds * 1000);
  const weekday = date.toLocaleDateString(undefined, { weekday: "short" });
  const time = date.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
  return `refills ${weekday} at ${time}`;
}
