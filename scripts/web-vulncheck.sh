#!/usr/bin/env bash
# Gate `npm audit` over web/ against an explicit allowlist.
#
# The sibling of scripts/vulncheck.sh, deliberately the same shape: the Go
# module tree and the npm tree are both shipped surface — embed.go embeds
# web/dist into the binary — so both deserve a gate that fails the build rather
# than a banner somebody may notice.
#
# The decision cannot come from npm's exit status. `npm audit --json` exits 1
# both when it reports findings and when it fails to run at all (an unreachable
# registry, a lockfile it cannot resolve), and those must not be conflated: one
# is "we found something", the other is "we did not look". What separates them
# is the payload — a real audit carries `.metadata` and `.vulnerabilities`, a
# failure carries `.error` and neither.
#
# Two failure modes, deliberately symmetric:
#   * an advisory that is not allowlisted -> fail, because it is new
#   * an allowlist entry that matches no advisory -> fail, because the
#     suppression has outlived its reason and now reads as "reviewed" while
#     covering nothing
#
# The second is the one that matters over time. A stale entry is how a gate
# quietly stops being a gate.
#
# The whole tree is audited — no --omit=dev, no severity floor. Not for
# thoroughness's sake: svelte, svelte-sonner, tailwindcss, tailwind-variants and
# @lucide/svelte are declared devDependencies whose *compiled output ships* in
# web/dist, so --omit=dev would hide advisories in code that reaches a user's
# browser. A genuinely tooling-only advisory is still accepted here — but by an
# allowlist entry that says so in prose, not by a flag that hides it silently.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
# Both overridable so scripts/web_vulncheck_test.go can drive every failure mode
# against fixtures instead of the real tree. Not intended for normal use.
allowfile="${WEB_VULNCHECK_ALLOWFILE:-$root/scripts/web-vulncheck-allow.txt}"
webdir="${WEB_VULNCHECK_DIR:-$root/web}"

die() { printf '%s\n' "$*" >&2; exit 1; }

# Fail loudly on a missing tool rather than degrading to a pass. This is a
# control: "could not check" and "nothing to report" must not look alike.
command -v jq >/dev/null 2>&1 || die "web-vulncheck: jq is required but not on PATH."
command -v npm >/dev/null 2>&1 || die "web-vulncheck: npm is required but not on PATH. See CLAUDE.md for setup."
[ -f "$allowfile" ] || die "web-vulncheck: allowlist not found at $allowfile"
[ -d "$webdir" ] || die "web-vulncheck: web directory not found at $webdir"

raw=$(mktemp)
err=$(mktemp)
trap 'rm -f "$raw" "$err"' EXIT

# npm audit resolves the tree from package-lock.json and queries the registry —
# it needs neither node_modules nor a build, so this stays a cheap standalone
# check with no web:install or web:build precondition.
#
# The status is discarded on purpose (see the header); the payload decides.
(cd "$webdir" && npm audit --json) >"$raw" 2>"$err" || true

if ! jq -e 'has("metadata") and has("vulnerabilities")' "$raw" >/dev/null 2>&1; then
	printf '\nweb-vulncheck: npm audit did not produce a report — treating as a failure,\n' >&2
	printf 'because "could not check" must not look like "nothing to report".\n\n' >&2
	printf 'Most likely an unreachable registry or an unresolvable lockfile. npm said:\n\n' >&2
	jq -r '.message // .error.summary // empty' "$raw" 2>/dev/null | sed 's/^/  /' >&2 || true
	sed 's/^/  /' "$err" >&2 || true
	printf '\nReproduce with:\n  cd %s && npm audit\n\n' "$webdir" >&2
	exit 1
fi

# npm reports one entry per affected package, and `via` mixes two kinds: advisory
# objects on the package that actually carries the defect, and bare strings
# naming a parent on every package that merely depends on it. Only the objects
# are advisories, so the string chain is filtered out rather than parsed.
#
# The GHSA id is the allowlist token. `.url` is dropped down to its id for
# readability; a finding without one falls back to npm's numeric source id so
# that nothing can be silently omitted from the comparison.
#
# This deliberately counts a different thing from `npm audit`'s own summary,
# which counts affected *packages* — one lockfile measured 9 there against 41
# distinct advisories here. The advisory is the reviewable unit: it is what has
# a severity, a fixed range and a page to read, and what an allowlist entry has
# to give a reason for. A package count cannot be allowlisted at all.
found=$(jq -r '
	.vulnerabilities
	| to_entries[]
	| .value.via[]?
	| select(type == "object")
	| (.url // ("npm-source-" + (.source | tostring)))
' "$raw" | sed -e 's#^https://github.com/advisories/##' | sort -u)

allowed=$(sed -e 's/#.*//' -e 's/[[:space:]]//g' "$allowfile" | grep -v '^$' | sort -u || true)

unexpected=$(comm -23 <(printf '%s\n' "$found" | grep -v '^$' || true) <(printf '%s\n' "$allowed") || true)
stale=$(comm -13 <(printf '%s\n' "$found" | grep -v '^$' || true) <(printf '%s\n' "$allowed") || true)

status=0

if [ -n "$unexpected" ]; then
	status=1
	printf '\nweb-vulncheck: NEW advisories reported by npm audit:\n\n' >&2
	while IFS= read -r id; do
		[ -z "$id" ] && continue
		printf '  %s  https://github.com/advisories/%s\n' "$id" "$id" >&2
	done <<<"$unexpected"
	printf '\nUpdate the dependency if a fixed version exists. If it genuinely cannot be\n' >&2
	printf 'fixed, or is unreachable in what we ship, add the id to\n' >&2
	printf 'scripts/web-vulncheck-allow.txt with a rationale and what would retire it.\n' >&2
	printf 'Do not add an entry without one.\n\n' >&2
	printf 'Full detail:\n  cd web && npm audit\n\n' >&2
fi

if [ -n "$stale" ]; then
	status=1
	printf '\nweb-vulncheck: STALE allowlist entries — no longer reported by npm audit:\n\n' >&2
	while IFS= read -r id; do
		[ -z "$id" ] && continue
		printf '  %s\n' "$id" >&2
	done <<<"$stale"
	printf '\nThe reason for suppressing these is gone. Delete them from\n' >&2
	printf 'scripts/web-vulncheck-allow.txt along with their comment block.\n\n' >&2
fi

if [ "$status" -eq 0 ]; then
	n=$(printf '%s\n' "$allowed" | grep -c . || true)
	printf 'web-vulncheck: no new advisories (%s allowlisted, all still present).\n' "$n"
fi

exit "$status"
