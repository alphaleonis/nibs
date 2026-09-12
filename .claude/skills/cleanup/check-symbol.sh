#!/usr/bin/env bash
# Report whether a Go symbol is DECLARED anywhere under the given roots.
#
# Use this instead of a hand-written grep when a comment names a symbol. Three
# separate hand-rolled patterns produced FALSE PHANTOMS while this skill was
# being written, each a false negative that read as a real finding:
#
#   grep "DefaultType ="      missed  DefaultType     = "task"   (gofmt-aligned)
#   grep ... | grep -v '//'   missed  const MaxConfigBytes = 1 << 20 // 1 MiB
#   grep "func Foo"           misses  a const, var, field or interface method
#
# Usage:  check-symbol.sh <Symbol> [root ...]     (roots default to . )
# Exit:   0 = declaration found, 1 = none found (report it as a phantom), 2 = usage
#
# Always read the printed lines. A declaration inside a _test.go file, or one
# that is a different type's method, is not the symbol the comment meant.

set -uo pipefail

if [ $# -lt 1 ]; then
	echo "usage: check-symbol.sh <Symbol> [root ...]" >&2
	exit 2
fi

sym=$1
shift
roots=("$@")
[ ${#roots[@]} -eq 0 ] && roots=(.)

# Declaration shapes: top-level func/type/const/var, a method with any receiver,
# a grouped const/var entry (any alignment), a struct field, an interface method.
pattern="(^|[^[:alnum:]_])(func|type|const|var)([[:space:]]|\()[^/]*\b${sym}\b"
pattern="${pattern}|^[[:space:]]*${sym}[[:space:]]*="
pattern="${pattern}|^[[:space:]]*${sym}[[:space:]]+[A-Za-z_[*(]"
pattern="${pattern}|^[[:space:]]*${sym}\("

# Drop lines that ARE comments. Do NOT drop lines that merely contain one —
# a trailing `// note` on a real declaration is exactly what fooled attempt #2.
hits=$(grep -rnE --include='*.go' "$pattern" "${roots[@]}" 2>/dev/null \
	| grep -vE '^[^:]+:[0-9]+:[[:space:]]*//' \
	| grep -v '/third_party/')

if [ -n "$hits" ]; then
	echo "$hits"
	exit 0
fi

echo "no declaration of '${sym}' found under: ${roots[*]}"
echo "before reporting a phantom, prove the pattern works:"
echo "  check-symbol.sh <a symbol you know exists> ${roots[*]}"
exit 1
