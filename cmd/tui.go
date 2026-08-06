package cmd

import (
	"github.com/alphaleonis/nibs/internal/tui"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Open the interactive TUI",
	Long:  `Opens an interactive terminal user interface for browsing and managing nibs.`,
	Args:  codedNoArgs(nil), // opens an interactive UI; takes no positional args
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		resolver := app.newResolver()
		backend := tui.NewRealBackend(app.Core, resolver)
		return tui.Run(backend, app.Config(), version)
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
