package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/cobra"
)

// nibsPath and configPath are package-level vars because Cobra's flag binding
// (StringVar) requires a pointer target at init() time, before App is created.
// They are only read during PersistentPreRunE initialization.
var nibsPath string
var configPath string

var rootCmd = &cobra.Command{
	Use:   "nibs",
	Short: "A file-based issue tracker for AI-first workflows",
	Long: `Nibs is a lightweight issue tracker that stores issues as markdown files.
Track your work alongside your code and supercharge your coding agent with
a full view of your project.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Skip core initialization for commands that don't need App state.
		// These commands must NOT call getApp(). If adding a command here,
		// ensure it never accesses the App.
		if cmd.Name() == "init" || cmd.Name() == "prime" || cmd.Name() == "version" ||
			cmd.Name() == "catalog" || cmd.Name() == "cheat" ||
			(cmd.Name() == "graphql" && querySchemaOnly) {
			return nil
		}

		var cfg *config.Config
		var err error

		// Load configuration (user config provides defaults in both paths)
		if configPath != "" {
			// Use explicit config path, with user config layered underneath
			cfg, err = config.LoadFromExplicitPathWithUserConfig(configPath)
			if err != nil {
				return fmt.Errorf("loading config from %s: %w", configPath, err)
			}
		} else {
			// Search upward for .nibs.yml, with user config providing defaults
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting current directory: %w", err)
			}
			cfg, err = config.LoadWithUserConfig(cwd)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
		}

		// Determine nibs directory
		root, err := resolveNibsPath(nibsPath, cfg)
		if err != nil {
			return err
		}

		core := nibcore.New(root, cfg)
		if err := core.Load(); err != nil {
			return fmt.Errorf("loading nibs: %w", err)
		}

		cmd.SetContext(withApp(cmd.Context(), &App{Core: core}))
		return nil
	},
}

func init() {
	// Cobra defaults print the command's usage block AND a duplicate
	// "Error: <msg>" line whenever a RunE returns an error. That mixes
	// human-readable text into --json's JSON-only stdout/stderr contract,
	// and adds noise even in text mode where the usage belongs to --help.
	// Silence both at the root and own all error reporting in the boundary
	// (see reportExitError). The flag propagates to every subcommand.
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true

	rootCmd.PersistentFlags().StringVar(&nibsPath, "nibs-path", "", "Path to data directory (overrides config and NIBS_PATH env var)")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to config file (default: searches upward for .nibs.yml)")
	installFlagSuggestions(rootCmd)
}

// resolveNibsPath determines the nibs data directory path.
// Precedence: --nibs-path flag > NIBS_PATH env var > config.
func resolveNibsPath(flagPath string, c *config.Config) (string, error) {
	var root string
	if flagPath != "" {
		// Use explicit nibs path flag (highest priority)
		root = flagPath
	} else if envPath := os.Getenv("NIBS_PATH"); envPath != "" {
		// Use environment variable (medium priority)
		root = envPath
	} else {
		// Use path from config (lowest priority)
		root = c.ResolveNibsPath()
	}

	// Verify it exists
	if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
		if flagPath != "" || os.Getenv("NIBS_PATH") != "" {
			return "", fmt.Errorf("nibs path does not exist or is not a directory: %s", root)
		}
		return "", fmt.Errorf("no .nibs directory found at %s (run 'nibs init' to create one)", root)
	}

	return root, nil
}

// reportExitError is the single, testable error boundary for the CLI. It is
// the ONE place that decides the process exit status, mapping each error's
// structured CODE to a stable code via output.ExitCode (NOT_FOUND→3,
// VALIDATION→2, CONFLICT→4, IO/file→5, anything else→1, success→0).
//
//   - nil err: returns 0 (ExitOK), writes nothing.
//   - *output.CodedError (recovered via errors.As, so wrapping is fine):
//     exit status comes from its code. If it is Reported the command already
//     wrote the user-visible report to stdout (a --json envelope or get's
//     text line) — printing "Error: ..." on stderr would duplicate it and
//     corrupt callers piping `2>&1 | jq`, so suppress the stderr print.
//     A non-reported coded error (the shared text path) still prints to
//     stderr; only its exit status is now code-driven.
//   - any other (uncoded) err: print "Error: <err>\n" to stderr and map
//     filesystem/IO failures to ExitIO, everything else to ExitError. This
//     replaces Cobra's auto-print, silenced via rootCmd.SilenceErrors so the
//     boundary owns error reporting in one place.
func reportExitError(stderr io.Writer, err error) int {
	if err == nil {
		return output.ExitOK
	}
	var ce *output.CodedError
	if errors.As(err, &ce) {
		if !ce.Reported {
			_, _ = fmt.Fprintf(stderr, "Error: %s\n", ce.Msg)
		}
		return output.ExitCode(ce.Code)
	}
	// Best-effort write to stderr; if the writer is broken we still want to
	// exit non-zero so the shell sees the failure.
	_, _ = fmt.Fprintf(stderr, "Error: %s\n", err.Error())
	if isIOError(err) {
		return output.ExitIO
	}
	return output.ExitError
}

// isIOError reports whether an uncoded error is a filesystem/IO failure, so
// the boundary can map it to output.ExitIO. Coded errors already carry
// output.ErrFileError; this covers plain errors bubbling up from os/fs calls
// (e.g. config or nib loading in PersistentPreRunE).
func isIOError(err error) bool {
	var pe *fs.PathError
	return errors.As(err, &pe) ||
		errors.Is(err, fs.ErrNotExist) ||
		errors.Is(err, fs.ErrPermission)
}

func Execute() {
	if code := reportExitError(os.Stderr, rootCmd.Execute()); code != 0 {
		os.Exit(code)
	}
}

// filterResolvedBlockers returns shallow copies of the given nibs with
// completed/scrapped IDs removed from BlockedBy for display purposes.
// The original in-memory nibs from Core are not mutated. It applies the same
// resolved-status convention (nib.IsResolvedStatus) used across the mutation
// commands' JSON responses.
func filterResolvedBlockers(nibs []*nib.Nib, reader graph.NibReader) []*nib.Nib {
	result := make([]*nib.Nib, len(nibs))
	for i, b := range nibs {
		result[i] = filterResolvedBlockersOne(b, reader)
	}
	return result
}

// filterResolvedBlockersOne returns a shallow copy of the nib with resolved
// blockers removed from BlockedBy. The original nib is not mutated.
func filterResolvedBlockersOne(b *nib.Nib, reader graph.NibReader) *nib.Nib {
	if len(b.BlockedBy) == 0 {
		clone := *b
		return &clone
	}
	active := make([]string, 0, len(b.BlockedBy))
	for _, blockerID := range b.BlockedBy {
		if blocker, err := reader.Get(blockerID); err == nil {
			if !nib.IsResolvedStatus(blocker.Status) {
				active = append(active, blockerID)
			}
		} else {
			// Broken link (deleted nib) — preserve to surface to user
			active = append(active, blockerID)
		}
	}
	clone := *b
	clone.BlockedBy = active
	return &clone
}
