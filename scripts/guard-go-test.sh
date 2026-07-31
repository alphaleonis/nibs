#!/bin/sh
# Shim that runs the real PreToolUse guard, scripts/guard-go-test.py.
#
# The guard is Python, but `.claude/settings.json` registers a `.sh` path for it
# and the hook runner may either honor a shebang or hand the file to `bash` as a
# plain argument. A Python file with a `.sh` name survives only the first shape;
# in the second the shebang is just a comment and bash executes the Python source
# as shell commands. Keeping the `.sh` entry point as genuine POSIX sh makes both
# shapes correct, so the settings entry does not have to change. See nibs-zvz3.
#
# The interpreter name cannot be hardcoded either: in Git Bash on this machine
# `python3` is a Windows App Execution Alias stub that exits nonzero without
# running anything, while `python` is real; under WSL it is the other way around
# and `python` does not exist. So each candidate is probed by actually running
# it, and the first one that works wins. Probe stdin comes from /dev/null so the
# hook's JSON payload on stdin is left intact for the guard.
#
# The degraded paths below exit 1 — never 0, never 2. The hook contract reads 2
# as "block", which would stop every Bash call; stderr on exit 0 is discarded, so
# a warning there reaches nobody. Exit 1 is non-blocking and its stderr is
# surfaced, the only combination that both lets the call through and says the
# guard did not run. A shell-side regex fallback is deliberately omitted: a
# second definition of "bare go test" would drift from the Python one.

DIR=$(dirname "$0")
GUARD="$DIR/guard-go-test.py"

if [ ! -r "$GUARD" ]; then
	echo "warning: cannot read $GUARD; the bare 'go test' guard did NOT run for this call" >&2
	exit 1
fi

for PY in python3 python; do
	if command -v "$PY" >/dev/null 2>&1 && "$PY" -c '' </dev/null >/dev/null 2>&1; then
		# exec so the guard's stdin, stderr and exit code (0 allow, 2 block) pass
		# through unchanged.
		exec "$PY" "$GUARD"
	fi
done

echo "warning: no working Python interpreter found (tried python3, python); the bare 'go test' guard did NOT run for this call" >&2
exit 1
