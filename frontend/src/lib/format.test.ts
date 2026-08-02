import { describe, expect, it } from "vitest";
import {
  McpKind,
  McpScope,
  PullRequestState,
} from "../gen/dashboard/v1/dashboard_pb";
import {
  fullTimestamp,
  isSafeHref,
  kindLabel,
  prStateClass,
  prStateLabel,
  relativeTime,
  scopeClass,
  scopeLabel,
  shortId,
} from "./format";

const NOW = new Date("2026-08-02T12:00:00Z");
const ago = (ms: number) => new Date(NOW.getTime() - ms);
const ahead = (ms: number) => new Date(NOW.getTime() + ms);

const S = 1000;
const M = 60 * S;
const H = 60 * M;
const D = 24 * H;

describe("relativeTime", () => {
  it("returns 'just now' for <=1s (past or future)", () => {
    expect(relativeTime(NOW, NOW)).toBe("just now");
    expect(relativeTime(ago(1 * S), NOW)).toBe("just now");
    expect(relativeTime(ahead(1 * S), NOW)).toBe("just now");
  });

  it("buckets seconds", () => {
    expect(relativeTime(ago(5 * S), NOW)).toBe("5s ago");
    expect(relativeTime(ahead(5 * S), NOW)).toBe("in 5s");
  });

  it("buckets minutes", () => {
    expect(relativeTime(ago(2 * M), NOW)).toBe("2m ago");
    expect(relativeTime(ahead(2 * M), NOW)).toBe("in 2m");
  });

  it("buckets hours", () => {
    expect(relativeTime(ago(2 * H), NOW)).toBe("2h ago");
  });

  it("buckets days", () => {
    expect(relativeTime(ago(2 * D), NOW)).toBe("2d ago");
  });

  it("buckets months", () => {
    expect(relativeTime(ago(60 * D), NOW)).toBe("2mo ago");
  });

  it("buckets years", () => {
    expect(relativeTime(ago(400 * D), NOW)).toBe("1y ago");
  });

  it("uses the current time when now is omitted", () => {
    expect(relativeTime(new Date())).toBe("just now");
  });

  it("returns '' for undefined or invalid dates", () => {
    expect(relativeTime(undefined)).toBe("");
    expect(relativeTime(null)).toBe("");
    expect(relativeTime(new Date("not-a-date"), NOW)).toBe("");
  });
});

describe("fullTimestamp", () => {
  it("renders a locale string for a valid date", () => {
    expect(fullTimestamp(NOW)).toBe(NOW.toLocaleString());
  });
  it("returns '' for undefined or invalid dates", () => {
    expect(fullTimestamp(undefined)).toBe("");
    expect(fullTimestamp(new Date("nope"))).toBe("");
  });
});

describe("isSafeHref", () => {
  it("accepts http and https (case-insensitive)", () => {
    expect(isSafeHref("https://example.com")).toBe(true);
    expect(isSafeHref("http://example.com")).toBe(true);
    expect(isSafeHref("HTTPS://EXAMPLE.COM")).toBe(true);
  });
  it("rejects javascript:, data: and other schemes", () => {
    expect(isSafeHref("javascript:alert(1)")).toBe(false);
    expect(isSafeHref("data:text/html;base64,xxx")).toBe(false);
    expect(isSafeHref("mailto:a@b.c")).toBe(false);
    expect(isSafeHref("")).toBe(false);
  });
  it("rejects non-strings", () => {
    expect(isSafeHref(123)).toBe(false);
    expect(isSafeHref(undefined)).toBe(false);
    expect(isSafeHref(null)).toBe(false);
    expect(isSafeHref({})).toBe(false);
  });
});

describe("shortId", () => {
  it("returns the first 8 characters", () => {
    expect(shortId("abcdefghijklmnop")).toBe("abcdefgh");
    expect(shortId("abc")).toBe("abc");
  });
});

describe("prStateClass / prStateLabel", () => {
  it("maps every state", () => {
    expect(prStateClass(PullRequestState.OPEN)).toBe("pr-open");
    expect(prStateClass(PullRequestState.MERGED)).toBe("pr-merged");
    expect(prStateClass(PullRequestState.CLOSED)).toBe("pr-closed");
    expect(prStateClass(PullRequestState.UNSPECIFIED)).toBe("pr-open");

    expect(prStateLabel(PullRequestState.OPEN)).toBe("open");
    expect(prStateLabel(PullRequestState.MERGED)).toBe("merged");
    expect(prStateLabel(PullRequestState.CLOSED)).toBe("closed");
    expect(prStateLabel(PullRequestState.UNSPECIFIED)).toBe("open");
  });
});

describe("scopeLabel / scopeClass", () => {
  it("maps every scope", () => {
    expect(scopeLabel(McpScope.GLOBAL)).toBe("global");
    expect(scopeLabel(McpScope.SESSION)).toBe("session");
    expect(scopeLabel(McpScope.BUILTIN)).toBe("builtin");
    expect(scopeLabel(McpScope.UNSPECIFIED)).toBe("—");

    expect(scopeClass(McpScope.GLOBAL)).toBe("scope-global");
    expect(scopeClass(McpScope.SESSION)).toBe("scope-session");
    expect(scopeClass(McpScope.BUILTIN)).toBe("scope-builtin");
    expect(scopeClass(McpScope.UNSPECIFIED)).toBe("scope-unknown");
  });
});

describe("kindLabel", () => {
  it("maps every kind", () => {
    expect(kindLabel(McpKind.CONTAINER)).toBe("container");
    expect(kindLabel(McpKind.STDIO)).toBe("stdio");
    expect(kindLabel(McpKind.REMOTE)).toBe("remote");
    expect(kindLabel(McpKind.UNSPECIFIED)).toBe("—");
  });
});
