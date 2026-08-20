package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Set via ldflags at build time; fallback to VCS info from debug.ReadBuildInfo.
var (
	version = ""
	commit  = ""
	date    = ""
)

func init() {
	if version == "" || commit == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, s := range info.Settings {
				switch s.Key {
				case "vcs.revision":
					if commit == "" && len(s.Value) >= 7 {
						commit = s.Value[:7]
					}
				case "vcs.time":
					if date == "" {
						date = s.Value
					}
				case "vcs.modified":
					if s.Value == "true" && commit != "" {
						commit += "-dirty"
					}
				}
			}
		}
		if version == "" {
			version = "dev"
		}
		if commit == "" {
			commit = "unknown"
		}
		if date == "" {
			date = "unknown"
		}
	}

	rootCmd.AddCommand(versionCmd)

	// `--version` on the root command. Cobra gives the flag for free once
	// Version is non-empty; the template makes it print exactly what the
	// subcommand does.
	//
	// `-v` COMES WITH IT, and is kept: InitDefaultVersionFlag adds the shorthand
	// whenever root's own flagset has no `-v`, which it does not. It is safe here
	// only because this flag is LOCAL to root — `nibs query` binds -v to
	// --variables, and the two never meet. A PERSISTENT `-v` on root would not
	// merely shadow it, it panics at merge time ("unable to redefine 'v'
	// shorthand in \"query\" flagset"), so this is a line to leave local.
	rootCmd.Version = versionLine()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Args:  codedNoArgs(nil), // prints build info; takes no positional args
	Run: func(cmd *cobra.Command, args []string) {
		// Through the command's writer, which is where the --version path writes
		// too: identical in production, but the package's tests do redirect it,
		// and two spellings of one answer should not be able to land in two
		// places.
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), versionLine())
	},
}

// versionLine is the ONE rendering of this build's identity, shared by the
// `version` subcommand and the `--version` flag.
//
// The two spellings differ in exactly one respect, deliberately: `nibs version`
// can be followed by the "an update is available" line and `--version` cannot,
// because cobra returns from the version branch before PersistentPostRun exists
// to run it. That asymmetry is kept rather than evened out — a person asking
// which version they are on is well served by being told a newer one exists,
// while the flag is the spelling scripts and agents reach for, where an extra
// stderr line is noise. Both still print the same thing on stdout.
//
// One caveat on how far the sharing is enforced: the test pins that the two
// spellings RENDER IDENTICALLY, which is the property that matters, and it
// cannot see whether they came from here or from two copies of the same literal.
// Editing either copy is still caught.
//
// Shared rather than written twice because the two spellings are asked by
// different readers — a person types one, a script or an agent reaches for the
// other — and a build-identity string that differs by how it was asked for is one
// somebody parses wrongly. Cobra's default version template would have printed
// `nibs version <v>`, dropping the commit and the build date, so the template is
// overridden to emit this verbatim.
func versionLine() string {
	return fmt.Sprintf("nibs %s (%s) built %s", version, commit, date)
}
