import {
  McpKind,
  McpScope,
  PullRequestState,
} from "../gen/dashboard/v1/dashboard_pb";

/**
 * relativeTime renders a compact, human relative label ("5s", "3m", "2d",
 * "in 4h", "just now") for `from` measured against `now` (default: the current
 * time). Undefined/invalid dates yield an empty string. Mirrors the vanilla
 * dashboard's `fmtRel`/`relOf` bucketing exactly.
 */
export function relativeTime(from?: Date | null, now: Date = new Date()): string {
  if (!from || Number.isNaN(from.getTime())) {
    return "";
  }
  const ms = now.getTime() - from.getTime();
  const future = ms < 0;
  const s = Math.round(Math.abs(ms) / 1000);

  let o: string;
  if (s < 45) {
    o = s <= 1 ? "just now" : `${s}s`;
  } else {
    const m = Math.round(s / 60);
    if (m < 45) {
      o = `${m}m`;
    } else {
      const h = Math.round(m / 60);
      if (h < 24) {
        o = `${h}h`;
      } else {
        const d = Math.round(h / 24);
        if (d < 30) {
          o = `${d}d`;
        } else {
          const mo = Math.round(d / 30);
          o = mo < 12 ? `${mo}mo` : `${Math.round(mo / 12)}y`;
        }
      }
    }
  }

  if (o === "just now") {
    return o;
  }
  return future ? `in ${o}` : `${o} ago`;
}

/**
 * fullTimestamp renders a locale-formatted absolute timestamp for tooltips.
 * Undefined/invalid dates yield an empty string.
 */
export function fullTimestamp(d?: Date | null): string {
  if (!d || Number.isNaN(d.getTime())) {
    return "";
  }
  return d.toLocaleString();
}

/**
 * isSafeHref rejects anything that is not an http(s) URL (e.g. `javascript:`,
 * `data:`) before it is used as an href.
 */
export const isSafeHref = (u: unknown): u is string =>
  typeof u === "string" && /^https?:/i.test(u);

/** shortId returns the first 8 characters of an id (a Claude UUID prefix). */
export const shortId = (id: string): string => id.slice(0, 8);

/** prStateClass maps a pull-request state to its badge CSS class. */
export function prStateClass(state: PullRequestState): string {
  switch (state) {
    case PullRequestState.MERGED:
      return "pr-merged";
    case PullRequestState.CLOSED:
      return "pr-closed";
    default:
      return "pr-open";
  }
}

/** prStateLabel maps a pull-request state to its human label. */
export function prStateLabel(state: PullRequestState): string {
  switch (state) {
    case PullRequestState.MERGED:
      return "merged";
    case PullRequestState.CLOSED:
      return "closed";
    case PullRequestState.OPEN:
      return "open";
    default:
      return "open";
  }
}

/** scopeLabel maps an MCP scope to its human label ("—" when unspecified). */
export function scopeLabel(scope: McpScope): string {
  switch (scope) {
    case McpScope.GLOBAL:
      return "global";
    case McpScope.SESSION:
      return "session";
    case McpScope.BUILTIN:
      return "builtin";
    default:
      return "—";
  }
}

/** scopeClass maps an MCP scope to its badge CSS class. */
export function scopeClass(scope: McpScope): string {
  switch (scope) {
    case McpScope.GLOBAL:
      return "scope-global";
    case McpScope.SESSION:
      return "scope-session";
    case McpScope.BUILTIN:
      return "scope-builtin";
    default:
      return "scope-unknown";
  }
}

/** kindLabel maps an MCP kind to its human label ("—" when unspecified). */
export function kindLabel(kind: McpKind): string {
  switch (kind) {
    case McpKind.CONTAINER:
      return "container";
    case McpKind.STDIO:
      return "stdio";
    case McpKind.REMOTE:
      return "remote";
    default:
      return "—";
  }
}
