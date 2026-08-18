package cmd

// The Windows half of shellArg's quoting, which is not the POSIX half with a
// different quote character — the trigger set differs too, and getting either
// wrong produces a prescribed command the reader cannot run.
//
// THE BACKSLASH IS DELIBERATELY ABSENT from the trigger set. It is the path
// separator here, not an escape, so including it (as the shared POSIX set did)
// quoted EVERY Windows path — and the quote character was `'`, which cmd.exe does
// not treat as a delimiter at all. Measured, passing the rendered argument to a
// program that prints its argv:
//
//	cmd.exe  --nibs-path 'C:\proj\nibdata'      -> argv = `'C:\proj\nibdata'`
//	cmd.exe  --nibs-path 'C:\my proj\nibdata'   -> argv = `'C:\my` + `proj\nibdata'`
//
// The first names a directory that cannot exist; the second splits one argument
// into two. Both were certified "runnable" by the refusal invariant, whose field
// splitter modeled sh. PowerShell strips the single quotes correctly, so the defect
// was invisible to anyone testing there.
//
// Double quotes are the spelling both shells agree on, measured the same way —
// `"C:\proj\nibdata"`, `"C:\my proj\nibdata"` and `"\\localhost\C$\proj"` all
// arrive as exactly one argument in cmd.exe AND PowerShell. The UNC case matters
// because `$` is in the trigger set and an admin share carries one: PowerShell
// leaves `C$\` alone inside double quotes, since `$\` begins no variable name.
//
// TWO RESIDUAL HAZARDS have no rendering that satisfies both shells at once, so
// they are documented rather than fixed:
//
//   - A TRAILING BACKSLASH escapes the closing quote for cmd.exe's argv parser:
//     `"C:\proj\"` arrives as `C:\proj"`. Doubling it repairs cmd.exe and breaks
//     PowerShell, which then reports `C:\proj\\`. Unreachable in practice —
//     filepath.Clean strips a trailing separator from everything but a volume
//     root, and a store is never a volume root.
//   - A BACKTICK is PowerShell's escape character inside double quotes, so
//     `"C:\a`b"` reaches the program as `C:\ab`. cmd.exe passes it through intact.
//
// The characters that would otherwise force a choice here — `"`, `<`, `>`, `|`,
// `*`, `?` — are all ILLEGAL in Windows filenames, so no real path carries one and
// no escaping scheme is owed for them. They stay in the trigger set because a
// caller may hand this a path that was never valid, and quoting a doomed argument
// is better than emitting a bare one that reshapes the command line.
const shellArgQuoteTriggers = " \t\"'$&|;<>()*?[]{}#!~`,@^"

// quoteShellArg wraps s in double quotes, the one delimiter cmd.exe and PowerShell
// both honor. Nothing inside is escaped: see the two residual hazards above for
// the only inputs where that is observable, neither of which a real store path
// reaches.
func quoteShellArg(s string) string {
	return `"` + s + `"`
}
