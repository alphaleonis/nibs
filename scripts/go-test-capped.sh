#!/usr/bin/env bash
# Run `go test` inside a memory-capped systemd --user scope so a runaway test
# (unbounded allocation) is OOM-killed inside its own cgroup instead of taking
# down the machine.
#
# The cap matters on WSL and, as far as we have seen, only there. On WSL a
# runaway test has twice escalated from "the test dies" to a teardown of every
# terminal in the VM (2026-07-06 nibs-mv0i, 2026-07-29 nibs-mlss). On native
# Linux, macOS and Windows the same runaway is an ordinary process-level OOM:
# the test binary is killed, the run fails, and nothing else is disturbed. So
# off WSL this script is a plain `go test` with no ceremony, and no attempt is
# made to cap or guard a platform where the problem has never appeared.
#
# On WSL, an unavailable cap is a refusal rather than a silently uncapped run.
# Warning and running anyway put the protection at its weakest exactly when the
# environment was unusual enough to need it, and the warning scrolls past in
# `task test` output nobody reads (nibs-oz2e).
#
# Override the cap size with GO_TEST_MEM_MAX (default 4G). To run uncapped on
# WSL anyway, accepting the risk, set GO_TEST_UNCAPPED=1. All arguments are
# forwarded to `go test`, e.g.  scripts/go-test-capped.sh ./...  or
# ... -run TestFoo ./pkg
set -euo pipefail

LIMIT="${GO_TEST_MEM_MAX:-4G}"

if systemd-run --user --scope --quiet true >/dev/null 2>&1; then
	exec systemd-run --user --scope --quiet \
		-p MemoryMax="$LIMIT" -p MemorySwapMax=0 -p MemoryAccounting=yes \
		go test "$@"
fi

# A WSL kernel names itself in osrelease, e.g. 6.18.33.2-microsoft-standard-WSL2.
# Read with bash's own builtins rather than grep: bash is already required, and
# the check must not turn into a no-op on a machine missing a coreutil. The path
# is overridable so the tests can exercise both branches on one machine.
osrelease=""
osrelease_file="${NIBS_OSRELEASE_FILE:-/proc/sys/kernel/osrelease}"
if [[ -r $osrelease_file ]]; then
	read -r osrelease <"$osrelease_file" || true
fi

if [[ ${osrelease,,} == *microsoft* && ${GO_TEST_UNCAPPED:-} != 1 ]]; then
	# printf, not a `cat` heredoc, for the same reason the check above avoids
	# grep: the refusal must not depend on a binary that might not be there.
	printf '%s\n' \
		"error: refusing to run 'go test' uncapped on WSL." \
		"" \
		"No memory cap is available: 'systemd-run --user --scope' does not work" \
		"here, so a runaway test could not be confined to its own cgroup. On WSL" \
		"that has twice taken down the whole VM and every terminal in it" \
		"(nibs-mv0i, nibs-mlss) — a bounded test failure elsewhere, but not here." \
		"" \
		"Fix the cap (a user systemd manager, i.e. systemd=true in /etc/wsl.conf)," \
		"or accept the risk explicitly:" \
		"" \
		"    GO_TEST_UNCAPPED=1 $0 $*" >&2
	exit 1
fi

exec go test "$@"
