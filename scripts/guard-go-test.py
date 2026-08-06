#!/usr/bin/env python3
"""PreToolUse guard: reject a bare `go test` in an agent Bash command.

`go test` run outside a memory-capped cgroup has taken down the whole WSL VM
twice (2026-07-06 nibs-mv0i, 2026-07-29 nibs-mlss). A runaway test allocates
faster than any `-timeout` can help: the 2026-07-29 probe hit 14.7 GB in ~19s,
exhausted RAM plus 4 GiB of swap, and the global OOM killer took every terminal
in init.scope with it.

`scripts/go-test-capped.sh` contains that blast radius, but it is opt-in, so a
direct `go test ./pkg -run TestX` walks straight past it. That bypass is what
caused both incidents. This hook makes the cap non-bypassable for agent Bash
calls.

Matches `go test` only at a COMMAND position (start of line, or after a pipe /
`;` / `&&` / `||` / subshell), optionally behind env assignments or a `timeout`
prefix -- so prose mentioning "go test" inside a heredoc or a quoted string does
not trip it, while `timeout 30 go test ...` (the exact 2026-07-29 command shape)
does.

Reached through scripts/guard-go-test.sh, which resolves an interpreter that
actually runs (`python3` and `python` name different things on Windows and on
Linux). The hook entry in .claude/settings.json must keep pointing at that shim
rather than at this file, so the entry point stays valid POSIX sh whichever way
the hook runner invokes it.

Deliberate override, for the rare case that genuinely needs it:
    NIBS_ALLOW_BARE_GO_TEST=1 go test ./...

Exit 0 = allow, exit 2 = block (stderr is fed back to the agent).
"""
import json
import re
import sys

# `go test` at a command position: line start or after a shell separator,
# allowing VAR=val prefixes and a `timeout <dur>` wrapper.
# A backslash-escaped separator is not a shell separator: `\|` is regex
# alternation inside a grep/rg pattern, and blocking on it was an over-block
# found the moment this hook went live.
BARE_GO_TEST = re.compile(
    r"""(?:^|(?<!\\)[\n;&|(])            # command position, not an escaped one
        \s*
        (?:[A-Za-z_][A-Za-z0-9_]*=\S*\s+)*   # optional env assignments
        (?:timeout\s+\S+\s+)?                # optional timeout wrapper
        go\s+test\b
    """,
    re.VERBOSE | re.MULTILINE,
)

MESSAGE = """Blocked: bare `go test` is not allowed in this project.

Run Go tests through the memory-capped harness instead:

    task test                                      # the full gate
    scripts/go-test-capped.sh ./internal/graph/ -run TestFoo -v    # targeted

Why: an uncapped `go test` has killed the WSL VM twice by exhausting RAM and
swap before any `-timeout` could fire (nibs-mv0i, nibs-mlss). The capped runner
confines a runaway to its own cgroup (MemoryMax + MemorySwapMax=0), where an OOM
is a clean exit 137 instead of a machine-wide teardown.

This matters most for mutation probes that break a TERMINATION guard: the mutant
does not fail an assertion, it allocates without bound.

If you genuinely need to bypass it, prefix the command with
NIBS_ALLOW_BARE_GO_TEST=1 and say why."""


def main() -> int:
    try:
        payload = json.load(sys.stdin)
    except (json.JSONDecodeError, ValueError):
        return 0  # never block on a malformed payload

    if payload.get("tool_name") != "Bash":
        return 0

    command = payload.get("tool_input", {}).get("command", "")
    if not command:
        return 0

    if "NIBS_ALLOW_BARE_GO_TEST=1" in command:
        return 0

    if BARE_GO_TEST.search(command):
        print(MESSAGE, file=sys.stderr)
        return 2

    return 0


if __name__ == "__main__":
    sys.exit(main())
