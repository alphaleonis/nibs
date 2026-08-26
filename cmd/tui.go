package cmd

import (
	"errors"
	"fmt"

	"github.com/alphaleonis/nibs/internal/nibcore"
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
		return runTUISession(app, func() error {
			backend := tui.NewRealBackend(app.Core, app.newResolver())
			return tui.Run(backend, app.Config(), version)
		})
	},
}

// runTUISession announces that this store is held, then runs the TUI under that
// announcement for as long as the session lasts.
//
// A TUI is the same hazard to `nibs migrate` as a serve, and the announcement is
// the same one: it reads <store>/config.yml once, keeps nibs in memory across the
// whole session, watches the store and writes back — so a clone parked on the
// per-operation write lock can put a pre-migration render over migrated files,
// with nothing left to detect it afterwards. The shared side, so another TUI and
// a live serve are still admitted beside it; what it excludes is the exclusive
// side migrate holds for its run.
//
// It also makes the session VISIBLE to liveServeNote, which probes this lock:
// a `nibs config set-prefix` or an areas rewrite under a live TUI is exactly as
// broken as under a live serve, so the same warning has to reach it.
//
// run is a parameter rather than an inlined body because tui.Run blocks on a real
// terminal, which would put the acquisition out of reach of every test.
func runTUISession(app *App, run func() error) error {
	holding, err := nibcore.AcquireServeLock(app.Core.Root())
	if err != nil {
		if errors.Is(err, nibcore.ErrStoreServed) {
			return fmt.Errorf("`nibs migrate` is running against this store; wait for it to finish, then start `nibs tui` again")
		}
		return fmt.Errorf("announcing that this store is being served: %w", err)
	}
	// Deferred so a Bubbletea quit and a panic unwind release it alike; a hard
	// kill is covered by the kernel closing the descriptor.
	defer func() { _ = holding.Release() }()
	return run()
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
