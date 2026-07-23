// Relative + absolute date formatting for the table's Created / Modified columns.
// Display-only: no locale-sensitive parsing of the input beyond `new Date(iso)`,
// and the relative buckets are coarse ("5m ago", "3d ago") so a row's age reads
// at a glance. Anything a year or older collapses to a short absolute month/year
// (e.g. "Jul 2024") since exact age stops mattering at that range.

const MINUTE = 60 * 1000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
// Approximate calendar spans — the "~" in the bucket boundaries. A month is
// treated as 30 days and a year as 365 days; both are deliberately coarse.
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
 * Short absolute label ("MMM yyyy", e.g. "Jul 2024") for a timestamp.
 *
 * Uses UTC calendar fields so the label matches the canonical UTC hover tooltip
 * (`formatAbsolute` -> `toISOString()`). Local `getMonth()/getFullYear()` could
 * disagree with the tooltip by up to a year near a calendar boundary in a
 * non-UTC timezone. The sub-year relative buckets are pure ms-diff arithmetic
 * and are timezone-safe, so only this absolute fallback needs the alignment.
 */
function formatMonthYear(ms: number): string {
  const d = new Date(ms);
  return `${MONTH_ABBREVIATIONS[d.getUTCMonth()]} ${d.getUTCFullYear()}`;
}

/**
 * Relative age of `iso` measured against `now` (injectable for deterministic
 * tests; defaults to the current time).
 *
 * Buckets, in order:
 *   < 1 min   -> "just now"   (also covers future timestamps: a negative age)
 *   < 1 h     -> "{m}m ago"
 *   < 24 h    -> "{h}h ago"
 *   < ~30 d   -> "{d}d ago"
 *   < ~12 mo  -> "{mo}mo ago"
 *   >= ~1 y   -> short absolute date ("MMM yyyy")
 *
 * Empty, unparseable, or otherwise invalid input returns "".
 */
export function formatRelative(iso: string, now: Date = new Date()): string {
  const then = parseTime(iso);
  if (then === null) return "";

  const diffMs = now.getTime() - then;

  // A future timestamp yields a negative diff, which is < MINUTE, so it reads
  // as "just now" rather than a nonsensical negative age.
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
