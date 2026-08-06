#!/usr/bin/env bash
# Gate `govulncheck` against an explicit allowlist.
#
# govulncheck has no native suppression flag (checked: no -ignore, no allowlist
# input), and `-format json` exits 0 even when it reports findings — the exit
# code only carries meaning in text mode. So the decision has to be made here,
# from the parsed findings, not from govulncheck's status.
#
# Two failure modes, deliberately symmetric:
#   * a finding that is not allowlisted  -> fail, because it is new
#   * an allowlist entry that matches no finding -> fail, because the
#     suppression has outlived its reason and now reads as "reviewed" while
#     covering nothing
#
# The second is the one that matters over time. A stale entry is how a gate
# quietly stops being a gate.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
# Overridable so scripts/vulncheck_test.go can drive both failure modes against
# fixtures instead of the real allowlist. Not intended for normal use.
allowfile="${VULNCHECK_ALLOWFILE:-$root/scripts/vulncheck-allow.txt}"

die() { printf '%s\n' "$*" >&2; exit 1; }

# Fail loudly on a missing tool rather than degrading to a pass. This is a
# control: "could not check" and "nothing to report" must not look alike.
command -v jq >/dev/null 2>&1 || die "vulncheck: jq is required but not on PATH."
command -v mise >/dev/null 2>&1 || die "vulncheck: mise is required but not on PATH. See CLAUDE.md for setup."
[ -f "$allowfile" ] || die "vulncheck: allowlist not found at $allowfile"

raw=$(mktemp) && trap 'rm -f "$raw"' EXIT

# `set -e` would abort on a non-zero status here, but govulncheck's json mode
# returns 0 even with findings, so a non-zero status means it genuinely failed
# to run — a broken checkout, an unbuildable package, no network for the vuln
# database. That must fail the gate, not read as "clean".
if ! mise exec -- govulncheck -format json ./... >"$raw" 2>/dev/null; then
	printf 'vulncheck: govulncheck failed to run. Re-run without -format json to see why:\n' >&2
	printf '  mise exec -- govulncheck ./...\n' >&2
	printf '(embed.go embeds web/dist, so an unbuilt web tree reports a missing embed pattern\n' >&2
	printf ' rather than anything about dependencies — `task web:build` first.)\n' >&2
	exit 1
fi

# govulncheck streams concatenated JSON objects; jq consumes that natively.
found=$(jq -r 'select(has("finding")) | .finding.osv' "$raw" | sort -u)
allowed=$(sed -e 's/#.*//' -e 's/[[:space:]]//g' "$allowfile" | grep -v '^$' | sort -u || true)

unexpected=$(comm -23 <(printf '%s\n' "$found" | grep -v '^$' || true) <(printf '%s\n' "$allowed") || true)
stale=$(comm -13 <(printf '%s\n' "$found" | grep -v '^$' || true) <(printf '%s\n' "$allowed") || true)

status=0

if [ -n "$unexpected" ]; then
	status=1
	printf '\nvulncheck: NEW vulnerabilities reported by govulncheck:\n\n' >&2
	while IFS= read -r id; do
		[ -z "$id" ] && continue
		printf '  %s  https://pkg.go.dev/vuln/%s\n' "$id" "$id" >&2
	done <<<"$unexpected"
	printf '\nFix the dependency if a fixed version exists. If it genuinely cannot be\n' >&2
	printf 'fixed, add the ID to scripts/vulncheck-allow.txt with a rationale and what\n' >&2
	printf 'would retire it. Do not add an entry without one.\n\n' >&2
	printf 'Full detail:\n  mise exec -- govulncheck ./...\n\n' >&2
fi

if [ -n "$stale" ]; then
	status=1
	printf '\nvulncheck: STALE allowlist entries — no longer reported by govulncheck:\n\n' >&2
	while IFS= read -r id; do
		[ -z "$id" ] && continue
		printf '  %s\n' "$id" >&2
	done <<<"$stale"
	printf '\nThe reason for suppressing these is gone. Delete them from\n' >&2
	printf 'scripts/vulncheck-allow.txt along with their comment block.\n\n' >&2
fi

if [ "$status" -eq 0 ]; then
	n=$(printf '%s\n' "$allowed" | grep -c . || true)
	printf 'vulncheck: no new vulnerabilities (%s allowlisted, all still present).\n' "$n"
fi

exit "$status"
