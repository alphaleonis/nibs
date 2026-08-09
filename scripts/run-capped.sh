#!/usr/bin/env bash
# Run a command inside a memory-capped systemd --user scope, so its memory is
# charged to a cgroup of its own instead of to whatever cgroup the caller sits
# in.
#
# Two different hazards need that, and only one of them is a runaway:
#
#   1. A runaway allocation. On WSL an uncapped `go test` has twice escalated
#      from "the test dies" to a teardown of every terminal in the VM
#      (2026-07-06 nibs-mv0i, 2026-07-29 nibs-mlss). On native Linux, macOS and
#      Windows the same runaway is an ordinary process-level OOM: the test
#      binary is killed, the run fails, and nothing else is disturbed.
#
#   2. An honest but large worker fleet. Test lanes fan out across cores, and
#      whatever cgroup the lane is charged to is shared with whatever launched
#      it. When that launcher is a long-lived supervisor — an agent session, an
#      editor — the kernel's memcg OOM killer picks the fattest process in the
#      cgroup, and a supervisor that has been alive for days outweighs any one
#      short-lived worker: it has accumulated swap entries and page tables that
#      count toward oom_badness. On 2026-08-07 two concurrent vitest fleets
#      (~9.4 GB across ~34 jsdom workers) filled a 10 GiB session scope and the
#      kernel killed the supervising agent session, which was 4% of the usage
#      (nibs-0kip). Nothing had run away; the lane simply was not charged to a
#      cgroup of its own.
#
# So a cap is worth having even where no runaway is possible. What differs by
# lane is what to do when no cap is *available*:
#
#   --refuse-on-wsl   Refuse rather than run uncapped on WSL. For lanes with a
#                     runaway history, where warning and running anyway put the
#                     protection at its weakest exactly when the environment was
#                     unusual enough to need it, and the warning scrolls past in
#                     `task test` output nobody reads (nibs-oz2e).
#   (default)         Run uncapped. For lanes whose hazard is fleet size rather
#                     than runaway allocation: losing the cap costs isolation,
#                     not the machine, and refusing would break `task test` on
#                     an ordinary WSL box with no user systemd manager.
#
# Usage: run-capped.sh [--refuse-on-wsl] <command> [args...]
#
# Env: NIBS_CAP_MEM_MAX  scope ceiling, default 4G
#      NIBS_UNCAPPED=1   run uncapped even where the policy would refuse
set -euo pipefail

refuse_on_wsl=0
if [[ ${1:-} == --refuse-on-wsl ]]; then
	refuse_on_wsl=1
	shift
fi

if [[ $# -eq 0 ]]; then
	printf 'usage: %s [--refuse-on-wsl] <command> [args...]\n' "${BASH_SOURCE[0]}" >&2
	exit 2
fi

LIMIT="${NIBS_CAP_MEM_MAX:-4G}"

# MemorySwapMax=0 is what makes the ceiling bound the workload rather than just
# slow the machine down: with swap reachable, a fleet over its ceiling thrashes
# instead of failing.
if systemd-run --user --scope --quiet true >/dev/null 2>&1; then
	exec systemd-run --user --scope --quiet \
		-p MemoryMax="$LIMIT" -p MemorySwapMax=0 -p MemoryAccounting=yes \
		"$@"
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

if [[ $refuse_on_wsl == 1 && ${osrelease,,} == *microsoft* && ${NIBS_UNCAPPED:-} != 1 ]]; then
	# printf, not a `cat` heredoc, for the same reason the check above avoids
	# grep: the refusal must not depend on a binary that might not be there.
	printf '%s\n' \
		"error: refusing to run '$1' uncapped on WSL." \
		"" \
		"No memory cap is available: 'systemd-run --user --scope' does not work" \
		"here, so a runaway could not be confined to its own cgroup. On WSL that" \
		"has twice taken down the whole VM and every terminal in it (nibs-mv0i," \
		"nibs-mlss) — a bounded failure elsewhere, but not here." \
		"" \
		"Fix the cap (a user systemd manager, i.e. systemd=true in /etc/wsl.conf)," \
		"or accept the risk explicitly by setting NIBS_UNCAPPED=1." >&2
	exit 1
fi

exec "$@"
