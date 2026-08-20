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
	// NO `-v` SHORTHAND: `nibs query` already binds -v to --variables
	// (cmd/graphql.go), and a persistent short flag on root would collide with it
	// there. The long spelling is the one tooling reaches for anyway.
	rootCmd.Version = versionLine()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Args:  codedNoArgs(nil), // prints build info; takes no positional args
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(versionLine())
	},
}

// versionLine is the ONE rendering of this build's identity, shared by the
// `version` subcommand and the `--version` flag.
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
