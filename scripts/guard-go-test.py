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

The guard fires on an *invocation*, not on a mention. Deciding which is which
needs the shell's own distinction between code and data, so the command string
is first scanned into the two: text the shell would execute keeps its
characters, and text it would only pass along as an argument or feed to a
program's stdin is blanked out. What survives is then matched for `go test` at a
command position -- start of input, or after a separator -- allowing env
assignments, `sudo`-style wrappers and shell keywords in front of it.

Data, therefore not matched:

  * a heredoc body, which is stdin for the command that owns it, so a commit
    message written as `git commit -F - <<'EOF' ... EOF` may discuss the phrase
    freely -- including at the start of a line, where a command would sit;
  * a quoted argument, so `git commit -m "go test discards stdout"` passes.

Code, therefore matched, because the shell executes it after all:

  * a command substitution -- `$(...)` or backticks -- wherever it appears,
    quoted argument and expanding heredoc body included;
  * a quoted argument of a command that hands what it is given to an
    interpreter (`sh -c`, `bash -c`, `eval`, `xargs`, ssh, ...), and a heredoc
    body whose pipeline contains one, since `bash <<EOF` really does run it.

Ambiguity resolves toward the widest match. An unterminated quote, backtick or
`$(`, or any failure inside the scanner, throws the scan away and matches the
raw command string instead -- never a subset of it. A command word that cannot
be resolved counts as a runner, so its quoted arguments are code. A false
positive costs one rephrased command; a false negative costs the machine.

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

# Words that hand what they are given to an interpreter, so their quoted
# arguments -- and any heredoc body in their pipeline -- are code, not data.
SHELL_RUNNERS = frozenset(
    {
        "sh", "bash", "zsh", "dash", "ksh", "mksh", "ash", "busybox", "fish",
        "eval", "source", ".", "xargs", "ssh", "su", "watch", "parallel",
        "script", "wsl", "wsl.exe", "docker", "podman", "kubectl",
    }
)

# Words that wrap another command without consuming its arguments. The command
# word is whatever follows them, so `sudo -u ci bash -c '...'` resolves to bash.
PREFIX_COMMANDS = frozenset(
    {
        "sudo", "doas", "env", "nohup", "nice", "ionice", "stdbuf", "setsid",
        "chrt", "taskset", "time", "timeout", "command", "exec", "builtin",
    }
)

# Keywords that introduce a command without being one, so `if x; then go test`
# is still an invocation at a command position.
SHELL_KEYWORDS = frozenset({"if", "then", "else", "elif", "while", "until", "do", "!"})

# Runners that take the command as trailing words rather than as one string, so
# `xargs go test` is an invocation in plain sight. They stay in SHELL_RUNNERS as
# well: a quoted argument of theirs is still shell code.
TRAILING_WRAPPERS = frozenset({"xargs", "watch", "parallel"})

_ENV_ASSIGN = re.compile(r"[A-Za-z_][A-Za-z0-9_]*=")
_DURATION = re.compile(r"\d+[smhdSMHD]?\Z")

# The words the match may step over before reaching `go test`: an env
# assignment, a wrapper command or keyword, an option belonging to one of those,
# or a bare duration such as the `30` in `timeout 30`. Longest alternatives
# first so `timeout` is not shadowed by `time`.
_STEP_OVER = "|".join(
    sorted(
        (re.escape(w) for w in PREFIX_COMMANDS | SHELL_KEYWORDS | TRAILING_WRAPPERS),
        key=len,
        reverse=True,
    )
)
_PREFIX_WORD = (
    r"(?:[A-Za-z_][A-Za-z0-9_]*=\S*"  # env assignment
    r"|" + _STEP_OVER + r"|-{1,2}[A-Za-z0-9]\S*"  # option of a wrapper
    r"|\d+[smhdSMHD]?)"  # bare duration
)

# `go test` at a command position: start of a line or after a shell separator,
# behind any number of the step-over words above.
# A backslash-escaped separator is not a shell separator: `\|` is regex
# alternation inside a grep/rg pattern, and blocking on it was an over-block
# found the moment this hook went live.
BARE_GO_TEST = re.compile(
    r"""(?:^|(?<!\\)[\n;&|({])            # command position, not an escaped one
        \s*
        (?:""" + _PREFIX_WORD + r"""\s+)*
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

If you are only *writing about* the command -- a commit message, a nib body, a
doc comment -- quote it or put it in a heredoc and this guard will let it
through.

If you genuinely need to bypass it, prefix the command with
NIBS_ALLOW_BARE_GO_TEST=1 and say why."""


class _Frame:
    """One nesting level of the scan: a command context or a quoted span.

    `code` says whether the text here is executed. `closer` is the character
    that ends the frame, `end` an index at which it ends instead (a heredoc
    body). `expand` marks a data span in which `$(...)` and backticks are still
    substituted, so those nest back into code.
    """

    __slots__ = (
        "code", "expand", "closer", "end", "cmd_ctx",
        "cmd_word", "pipeline_runner", "heredocs", "depth",
    )

    def __init__(self, code, expand=False, closer=None, end=None, cmd_ctx=False):
        self.code = code
        self.expand = expand
        self.closer = closer
        self.end = end
        self.cmd_ctx = cmd_ctx
        self.cmd_word = None
        self.pipeline_runner = False
        self.heredocs = []
        self.depth = 0


_WORD = re.compile(r"[ \t]*([^\s;&|<>()`]+)")

# A command prefixed by more wrapper words than this is not a shape worth
# scanning for; the bound keeps the per-separator lookahead from turning the
# whole scan quadratic on adversarial input.
_MAX_PREFIX_WORDS = 64


def _resolve_command_word(text, pos):
    """Return the effective command word starting at pos, or None.

    Steps over env assignments, wrapper commands, keywords, their options and a
    bare duration, so the word returned is the program that will actually run.
    """
    for _ in range(_MAX_PREFIX_WORDS):
        match = _WORD.match(text, pos)
        if not match:
            return None
        word = match.group(1)
        pos = match.end()
        bare = word.strip("'\"")
        if not bare:
            return None
        if (
            _ENV_ASSIGN.match(bare)
            or bare in PREFIX_COMMANDS
            or bare in SHELL_KEYWORDS
            or bare.startswith("-")
            or _DURATION.match(bare)
        ):
            continue
        return bare
    return None


_HEREDOC = re.compile(
    r"""<<(-?)[ \t]*
        (?:'([^']*)'          # <<'EOF' -- no expansion
          |"([^"]*)"          # <<"EOF" -- no expansion
          |\\([A-Za-z_][\w.-]*)   # <<\EOF -- no expansion
          |([A-Za-z_][\w.-]*)     # <<EOF  -- expansion
        )""",
    re.VERBOSE,
)


def _heredoc_body(text, start, delimiter, strip_tabs):
    """Locate a heredoc body beginning at start.

    Returns (body_end, resume) or (None, None) when the delimiter never
    arrives -- an unterminated body, which the caller treats as code.
    """
    pos = start
    length = len(text)
    while pos <= length:
        newline = text.find("\n", pos)
        line_end = length if newline == -1 else newline
        line = text[pos:line_end]
        if (line.lstrip("\t") if strip_tabs else line).rstrip("\r") == delimiter:
            return pos, length if newline == -1 else newline + 1
        if newline == -1:
            return None, None
        pos = newline + 1
    return None, None


def _quoted_is_code(frame):
    """Whether a quoted span opening in this command context is executed.

    True while the command word is still unresolved (the quote is part of the
    command word itself) and for commands that run what they are handed.
    """
    return frame.cmd_word is None or frame.cmd_word in SHELL_RUNNERS


def _blank_data(text, initial):
    """Return text with every span the shell would not execute blanked out.

    Blanking rather than deleting keeps offsets, so a quoted string cannot fuse
    the text around it into a command position that was never there. A quote
    around executed text becomes `;` instead: the start of `sh -c "go test"` is
    a command position, and the mark makes the matcher see it as one.
    """
    buf = list(text)
    length = len(text)
    stack = [initial]
    index = 0

    def command_position(frame, at):
        frame.cmd_word = _resolve_command_word(text, at)
        if frame.cmd_word in SHELL_RUNNERS:
            frame.pipeline_runner = True

    def enclosing_command(top):
        for frame in reversed(stack[:top]):
            if frame.cmd_ctx:
                return frame
        return None

    if initial.cmd_ctx:
        command_position(initial, 0)

    while index < length:
        frame = stack[-1]
        if frame.end is not None and index >= frame.end:
            stack.pop()
            continue
        char = text[index]

        if not frame.code:
            buf[index] = " "

        # A backslash escapes the next character everywhere but inside single
        # quotes, so neither character may be read as syntax.
        if char == "\\" and frame.closer != "'":
            if index + 1 < length and not frame.code:
                buf[index + 1] = " "
            index += 2
            continue

        if frame.closer is not None and char == frame.closer:
            if char == ")" and frame.depth > 0:
                frame.depth -= 1
                index += 1
                continue
            if char in "'\"":
                buf[index] = ";" if frame.code else " "
            elif char == "`":
                buf[index] = ";"
            stack.pop()
            if char == ")":
                outer = enclosing_command(len(stack))
                if outer is not None:
                    command_position(outer, index + 1)
            index += 1
            continue

        if not frame.code:
            # Substitutions still run inside an expanding data span.
            if frame.expand and char == "`":
                buf[index] = ";"
                stack.append(_Frame(code=True, closer="`", cmd_ctx=True))
                command_position(stack[-1], index + 1)
            elif frame.expand and char == "$" and text.startswith("$(", index):
                buf[index + 1] = "("
                stack.append(_Frame(code=True, closer=")", cmd_ctx=True))
                command_position(stack[-1], index + 2)
                index += 1
            index += 1
            continue

        if char in "'\"":
            executed = _quoted_is_code(enclosing_command(len(stack)) or frame)
            buf[index] = ";" if executed else " "
            stack.append(_Frame(code=executed, expand=char == '"', closer=char, cmd_ctx=executed))
            if executed:
                # Executed quoted text is a script in its own right, and the
                # words inside it name their own commands. That is what keeps
                # `sh -c "cd x; git commit -m '...'"` from reading the inner
                # message as more shell code.
                command_position(stack[-1], index + 1)
            index += 1
            continue

        if char == "`":
            buf[index] = ";"
            stack.append(_Frame(code=True, closer="`", cmd_ctx=True))
            command_position(stack[-1], index + 1)
            index += 1
            continue

        if char == "$" and text.startswith("$(", index):
            stack.append(_Frame(code=True, closer=")", cmd_ctx=True))
            command_position(stack[-1], index + 2)
            index += 2
            continue

        if char == "<" and text.startswith("<<", index) and not text.startswith("<<<", index):
            match = _HEREDOC.match(text, index)
            if match:
                delimiter = next(g for g in match.groups()[1:] if g is not None)
                expand = match.group(5) is not None
                owner = enclosing_command(len(stack)) or frame
                owner.heredocs.append((delimiter, bool(match.group(1)), expand))
                index = match.end()
                continue
            index += 1
            continue

        if char == "\n":
            owner = enclosing_command(len(stack) - 1) if not frame.cmd_ctx else frame
            owner = owner or frame
            index += 1
            index = _consume_heredocs(text, buf, owner, index)
            owner.pipeline_runner = False
            command_position(owner, index)
            continue

        if char in ";&|(){}":
            if char == "(" and frame.closer == ")":
                frame.depth += 1
            if char in ";&":
                frame.pipeline_runner = False
            command_position(frame, index + 1)
            index += 1
            continue

        index += 1

    if len(stack) > 1:
        # A quote, backtick or `$(` that never closed. The scan past it rested
        # on a guess about where the span ends, and bash's own answer can differ
        # (`$'it\'s'` ends a single-quoted span where this scanner does not), so
        # the result is not trustworthy enough to blank anything on.
        return text

    return "".join(buf)


def _consume_heredocs(text, buf, frame, index):
    """Blank out the heredoc bodies queued on frame, returning the resume index.

    A body is stdin for its command, so it is data -- unless something in the
    pipeline runs what it reads, which is what `bash <<EOF` does. An
    unterminated body is left as code: the delimiter is missing, so what the
    shell would do with it is anyone's guess.
    """
    queued, frame.heredocs = frame.heredocs, []
    for delimiter, strip_tabs, expand in queued:
        body_end, resume = _heredoc_body(text, index, delimiter, strip_tabs)
        if body_end is None:
            return index
        body = text[index:body_end]
        if frame.pipeline_runner:
            masked = _blank_data(body, _Frame(code=True, cmd_ctx=True))
        else:
            masked = _blank_data(body, _Frame(code=False, expand=expand))
        buf[index:body_end] = list(masked)
        index = resume
    return index


def executable_text(command):
    """Return command with its non-executed spans blanked out.

    Any scanner failure yields the command unchanged, which is the blocking
    direction: unscanned text is matched in full.
    """
    try:
        return _blank_data(command, _Frame(code=True, cmd_ctx=True))
    except Exception:  # an unscannable command must still be matched, in full
        return command


def is_bare_go_test(command):
    """Whether command runs `go test` outside the capped harness."""
    if "NIBS_ALLOW_BARE_GO_TEST=1" in command:
        return False
    return BARE_GO_TEST.search(executable_text(command)) is not None


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

    if is_bare_go_test(command):
        print(MESSAGE, file=sys.stderr)
        return 2

    return 0


if __name__ == "__main__":
    sys.exit(main())
