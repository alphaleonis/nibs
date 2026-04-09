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
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("nibs %s (%s) built %s\n", version, commit, date)
	},
}
