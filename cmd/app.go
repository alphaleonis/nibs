package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/spf13/cobra"
)

// App holds the runtime dependencies for all commands.
// Config is accessed via App.Config(), which delegates to Core.Config().
type App struct {
	Core *nibcore.Core

	// MigrationGatePassed records that PersistentPreRunE's migration gate ran
	// against this store and let the command through — which is only possible
	// when nothing was pending and the probe raised nothing. It exists so a
	// command that wants the same answer does not repeat the full store scan
	// that produced it: `check --fix` asked twice, moments apart, and the second
	// answer was known to be "nothing pending" before it was computed.
	//
	// False means "not established here", never "something is pending": plain
	// `nibs check` is exempt from the gate, so it must still probe.
	MigrationGatePassed bool

	// startupAreas is the vocabulary Core held when this App was built, kept
	// because Core's own copy is REPLACED by a later reload — the areas edits
	// re-read the store under its write lock, and the refusal wording needs to
	// tell an argument that was true when it was typed from one that never was.
	startupAreas *config.Areas
}

type appContextKey struct{}

// withApp stores an App in a context.
func withApp(ctx context.Context, app *App) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, appContextKey{}, app)
}

// getApp retrieves the App from a cobra command's context.
// Panics with an actionable message if the App is missing (e.g., the command
// is in PersistentPreRunE's skip list but calls getApp by mistake).
func getApp(cmd *cobra.Command) *App {
	ctx := cmd.Context()
	if ctx == nil {
		panic(fmt.Sprintf("getApp: no App in context for command %q — is it in PersistentPreRunE's skip list?", cmd.Name()))
	}
	v, ok := ctx.Value(appContextKey{}).(*App)
	if !ok || v == nil {
		panic(fmt.Sprintf("getApp: no App in context for command %q — is it in PersistentPreRunE's skip list?", cmd.Name()))
	}
	return v
}

// newApp wraps a loaded Core, capturing the vocabulary it opened the store with
// (see startupAreas). Every route that builds an App for a real command goes
// through here so that capture cannot be forgotten.
func newApp(core *nibcore.Core, migrationGatePassed bool) *App {
	return &App{
		Core:                core,
		MigrationGatePassed: migrationGatePassed,
		startupAreas:        core.Areas(),
	}
}

// Config returns the project configuration from Core.
func (a *App) Config() *config.Config {
	return a.Core.Config()
}

// Areas returns the store's declared area vocabulary from Core. It is separate
// from Config because it is reloaded while the process runs; for a CLI verb that
// prints and exits, one snapshot is the whole story.
func (a *App) Areas() *config.Areas {
	return a.Core.Areas()
}

// StartupAreas returns the vocabulary this process read when it OPENED the
// store, which an areas edit re-reads past under the store's write lock. It is
// consulted for one purpose — wording a refusal about what moved while this
// command waited — and never as grounds for a write: a snapshot cannot say what
// the store declares now.
func (a *App) StartupAreas() *config.Areas {
	return a.startupAreas
}

// liveServeNote returns note when another nibs process is holding this store,
// and "" otherwise. It is what a command appends after an edit that a
// long-running holder cannot see: everything a nibs process reads out of
// <store>/config.yml it reads ONCE, at construction, and no watcher reloads that
// file — so an edit to it is finished for the CLI verb that made it and not for
// the server that is still running.
//
// The probe is the serve interlock's exclusive side, taken and dropped — the
// same question `nibs migrate` asks, and non-blocking, so it costs nothing when
// no serve is running. It is worded for any nibs process holding the store
// because that is literally what the lock answers: `nibs migrate` holds the same
// side for its run, and this must not tell a caller a serve is running when one
// is not. It takes a different lock file from AcquireStoreLock, so a caller
// holding the store's write lock may ask.
//
// The note is the caller's because what goes stale differs per edit: an areas
// rewrite breaks the next write's validation, a prefix change breaks every
// create and every short-id lookup.
func liveServeNote(app *App, note string) string {
	fence, err := nibcore.AcquireServeExclusion(app.Core.Root())
	if err == nil {
		_ = fence.Release()
		return ""
	}
	if !errors.Is(err, nibcore.ErrStoreServed) {
		// The probe failed for some other reason, which says nothing about
		// whether a serve is live — so say nothing.
		return ""
	}
	return note
}

// newResolver creates a graph.Resolver wired to this App's Core.
func (a *App) newResolver() *graph.Resolver {
	return &graph.Resolver{
		Reader:     a.Core,
		Writer:     a.Core,
		Validator:  a.Core,
		Blocking:   a.Core,
		Subscriber: a.Core,
		Orderer:    graph.NewOrderer(a.Core, a.Core),
		Version:    version,
	}
}
