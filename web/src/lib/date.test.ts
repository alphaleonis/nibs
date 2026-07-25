import { describe, it, expect, vi } from "vitest";
import { formatRelative, formatAbsolute } from "./date";

// Fixed reference time so every relative bucket is deterministic regardless of
// when/where the suite runs. `ago(ms)` builds an ISO timestamp `ms` before it.
const NOW = new Date("2026-07-23T12:00:00.000Z");
const MIN = 60_000;
const HR = 60 * MIN;
const DAY = 24 * HR;

function ago(ms: number): string {
  return new Date(NOW.getTime() - ms).toISOString();
}

describe("formatRelative", () => {
  it("returns '' for an empty string", () => {
    expect(formatRelative("", NOW)).toBe("");
  });

  it("returns '' for an unparseable string", () => {
    expect(formatRelative("not a date", NOW)).toBe("");
  });

  it("returns '' for an invalid date value", () => {
    expect(formatRelative("2026-13-45T99:99:99Z", NOW)).toBe("");
  });

  it("treats a future timestamp as 'just now'", () => {
    // NOW is before the timestamp (1 hour in the future).
    expect(formatRelative("2026-07-23T13:00:00.000Z", NOW)).toBe("just now");
  });

  it("returns 'just now' under a minute old", () => {
    expect(formatRelative(ago(30 * 1000), NOW)).toBe("just now");
  });

  it("returns '1m ago' exactly at the one-minute boundary", () => {
    expect(formatRelative(ago(MIN), NOW)).toBe("1m ago");
  });

  it("returns minutes for ages under an hour", () => {
    expect(formatRelative(ago(5 * MIN), NOW)).toBe("5m ago");
    expect(formatRelative(ago(59 * MIN), NOW)).toBe("59m ago");
  });

  it("returns '1h ago' exactly at the one-hour boundary", () => {
    expect(formatRelative(ago(HR), NOW)).toBe("1h ago");
  });

  it("returns hours for ages under a day", () => {
    expect(formatRelative(ago(5 * HR), NOW)).toBe("5h ago");
    expect(formatRelative(ago(23 * HR), NOW)).toBe("23h ago");
  });

  it("returns '1d ago' exactly at the 24-hour boundary", () => {
    expect(formatRelative(ago(DAY), NOW)).toBe("1d ago");
  });

  it("returns days for ages under ~30 days", () => {
    expect(formatRelative(ago(5 * DAY), NOW)).toBe("5d ago");
    expect(formatRelative(ago(29 * DAY), NOW)).toBe("29d ago");
  });

  it("returns '1mo ago' exactly at the ~30-day boundary", () => {
    expect(formatRelative(ago(30 * DAY), NOW)).toBe("1mo ago");
  });

  it("returns months for ages under ~12 months", () => {
    expect(formatRelative(ago(180 * DAY), NOW)).toBe("6mo ago");
    expect(formatRelative(ago(364 * DAY), NOW)).toBe("12mo ago");
  });

  it("falls back to a short absolute date at the ~1-year boundary", () => {
    // 365 days is the first age in the absolute-date bucket. NOW - 365d is
    // 2025-07-23T12:00:00Z, whose UTC month/year is "Jul 2025" — pinned exactly
    // (not just the format) so the 365d transition's content is covered.
    expect(formatRelative(ago(365 * DAY), NOW)).toBe("Jul 2025");
  });

  it("renders the absolute fallback as 'MMM yyyy' for an old timestamp", () => {
    // ~2.3 years before NOW; mid-month + midday UTC keeps the month stable
    // across timezones.
    expect(formatRelative("2024-03-15T12:00:00.000Z", NOW)).toBe("Mar 2024");
  });

  it("uses UTC (not local) calendar fields for the absolute fallback at a boundary", () => {
    // 2024-12-31T23:00:00Z is Dec 2024 in UTC but rolls into Jan 2025 for any
    // positive-offset local zone. The label must match its canonical UTC hover
    // tooltip (formatAbsolute -> toISOString), so it reads "Dec 2024" regardless
    // of the viewer's timezone. Local getMonth()/getFullYear() would yield
    // "Jan 2025" here in a UTC+ zone and contradict the tooltip.
    //
    // Pin a positive-offset TZ so this guard bites host-independently: the CI
    // runners (ubuntu/windows) default to UTC, where local and UTC accessors
    // coincide — so without forcing a non-UTC zone the test could not catch a
    // regression back to local accessors. `vi.stubEnv` mutates process.env.TZ
    // (Node re-reads TZ on the next Date construction); unstub in finally so no
    // sibling test leaks it. (`process` isn't typed for svelte-check here, so we
    // go through vitest's typed env API rather than touching process directly.)
    vi.stubEnv("TZ", "Europe/Stockholm"); // UTC+1/+2: local !== UTC for this instant
    try {
      expect(formatRelative("2024-12-31T23:00:00.000Z", NOW)).toBe("Dec 2024");
    } finally {
      vi.unstubAllEnvs();
    }
  });
});

describe("formatAbsolute", () => {
  it("returns '' for an empty string", () => {
    expect(formatAbsolute("")).toBe("");
  });

  it("returns '' for an unparseable string", () => {
    expect(formatAbsolute("not a date")).toBe("");
  });

  it("returns the normalized ISO timestamp for a valid date", () => {
    expect(formatAbsolute("2026-03-20T10:00:00Z")).toBe("2026-03-20T10:00:00.000Z");
  });
});
