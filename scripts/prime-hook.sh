#!/usr/bin/env bash
# SessionStart / PreCompact hook: emit the agent onboarding prompt, then say which
# of the two things called `nibs` it actually describes.
#
# `nibs prime` deliberately runs the RELEASED binary on PATH. That binary is what
# this repo's own .nibs store requires, and keeping the tracker off the code under
# development is what stops a half-finished refactor from taking away the ability to
# record work. The cost is that its instructions describe a CLI that can be far
# behind this worktree — and an agent editing nibs source reads them as a
# description of the product it is changing. Hence the trailer.
#
# The drift is measured rather than asserted: `nibs version` prints the commit it
# was built from, so the count is exact and grows on its own as the branch moves.
set -uo pipefail

repo="${CLAUDE_PROJECT_DIR:-$(dirname "$(dirname "$(readlink -f "$0")")")}"

# Outside a nibs project `nibs prime` prints nothing; match that exactly rather than
# announcing a distinction that does not apply here. A missing binary is the same
# case: no prompt to qualify.
prompt="$(nibs prime 2>/dev/null)" || exit 0
[ -n "$prompt" ] || exit 0
printf '%s\n' "$prompt"

drift=""
released="$(nibs version 2>/dev/null | sed -n 's/.*(\([0-9a-f]\{7,\}\)).*/\1/p')"
if [ -n "$released" ] && git -C "$repo" cat-file -e "${released}^{commit}" 2>/dev/null; then
	ahead="$(git -C "$repo" rev-list --count "${released}..HEAD" 2>/dev/null || true)"
	if [ -n "$ahead" ] && [ "$ahead" != "0" ]; then
		drift=" — $ahead commits behind this worktree"
	fi
fi

cat <<EOF

<EXTREMELY_IMPORTANT>
## Which nibs is which — read before making any claim about CLI behavior

The instructions above come from \`nibs prime\` on PATH: the **released** build${drift}.
Two different things share the name \`nibs\`, and they are not the same CLI:

- **The tool you use** to track your work — the PATH binary. Everything above
  describes it. This repo's \`.nibs\` store requires it, and it keeps working when the
  checkout does not. That separation is deliberate; do not "fix" it.
- **The product you are editing** — this worktree, which may add, rename or remove
  commands and flags the PATH binary knows nothing about.

So \`nibs cheat\`, \`nibs catalog\` and \`nibs <cmd> --help\` **run from PATH describe the
released CLI, not this branch**. Never cite them as evidence about code you are
changing, and never copy a flag grammar out of them into new documentation — an
already-wrong line is how the last such defect propagated.

To find out what this branch actually does: \`task build\`, then run \`./nibs\` against a
throwaway copy of \`testdata/fixtures/sample-project\` — never against this repo's own
\`.nibs\`, which the branch binary refuses by design.

**Execute every factual claim you write about the CLI before you commit it.** Prose is
the only artifact here with no compiler and no test behind it.
</EXTREMELY_IMPORTANT>
EOF
