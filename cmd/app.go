package cmd

import (
	"context"
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

// Config returns the project configuration from Core.
func (a *App) Config() *config.Config {
	return a.Core.Config()
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
