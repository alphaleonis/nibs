//go:build !windows

package cmd

import "strings"

// shellArgQuoteTriggers is the set of characters a POSIX shell would split on or
// interpret, so a path carrying any of them has to be quoted to survive as one
// argument. The backslash is in the set because sh reads it as an escape.
const shellArgQuoteTriggers = " \t\"'$&|;<>()*?[]{}#!~`\\"

// quoteShellArg wraps s in single quotes, writing an embedded quote as the
// close-escape-reopen sequence sh requires. Single quotes suppress every
// expansion sh performs, so nothing inside needs further escaping.
func quoteShellArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
