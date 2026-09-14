// Date formatting for the table's Created / Modified columns.

const MINUTE = 60 * 1000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
// Approximate spans, the "~" in formatRelative's buckets.
const MONTH = 30 * DAY;
const YEAR = 365 * DAY;

const MONTH_ABBREVIATIONS = [
  "Jan", "Feb", "Mar", "Apr", "May", "Jun",
  "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
];

/** Parse an ISO string to epoch ms, or null for empty / unparseable input. */
function parseTime(iso: string): number | null {
  if (!iso) return null;
  const ms = new Date(iso).getTime();
  return Number.isNaN(ms) ? null : ms;
}

/**
 * "MMM yyyy" (e.g. "Jul 2024") from UTC fields, so the label agrees with the UTC
 * `formatAbsolute` tooltip near a calendar boundary.
 */
function formatMonthYear(ms: number): string {
  const d = new Date(ms);
  return `${MONTH_ABBREVIATIONS[d.getUTCMonth()]} ${d.getUTCFullYear()}`;
}

/**
 * Relative age of `iso` measured against `now`.
 *
 *   < 1 min   -> "just now"   (including future timestamps)
 *   < 1 h     -> "{m}m ago"
 *   < 24 h    -> "{h}h ago"
 *   < ~30 d   -> "{d}d ago"
 *   < ~12 mo  -> "{mo}mo ago"
 *   >= ~1 y   -> "MMM yyyy"
 *
 * Empty or unparseable input returns "".
 */
export function formatRelative(iso: string, now: Date = new Date()): string {
  const then = parseTime(iso);
  if (then === null) return "";

  const diffMs = now.getTime() - then;

  if (diffMs < MINUTE) return "just now";
  if (diffMs < HOUR) return `${Math.floor(diffMs / MINUTE)}m ago`;
  if (diffMs < DAY) return `${Math.floor(diffMs / HOUR)}h ago`;
  if (diffMs < MONTH) return `${Math.floor(diffMs / DAY)}d ago`;
  if (diffMs < YEAR) return `${Math.floor(diffMs / MONTH)}mo ago`;
  return formatMonthYear(then);
}

/**
 * Full ISO timestamp for the hover `title`, normalized via `toISOString()`.
 * Empty, unparseable, or otherwise invalid input returns "".
 */
export function formatAbsolute(iso: string): string {
  const ms = parseTime(iso);
  if (ms === null) return "";
  return new Date(ms).toISOString();
}
