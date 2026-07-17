package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/reprefix"
	"github.com/spf13/cobra"
)

var (
	setPrefixDryRun bool
	setPrefixForce  bool
	setPrefixJSON   bool
)

// gitIsDirtyFn reports whether any of the given pathspecs have uncommitted
// changes under git, running from workTree as the current directory. It must
// return (false, nil) when workTree is not inside a git repo — the caller
// treats "not a repo" as "nothing to guard". The caller passes the project
// root as workTree and explicit pathspecs for the nibs data directory and
// the .nibs.yml config file, so the check covers exactly what the command
// mutates and no more. Tests override this to avoid shelling out to real git.
var gitIsDirtyFn = realGitIsDirty

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage project configuration",
	// Cobra auto-shows help when no subcommand is given — no RunE needed.
}

var configSetPrefixCmd = &cobra.Command{
	Use:   "set-prefix <new-prefix>",
	Short: "Change the project nib ID prefix and rename all nibs on disk",
	Long: `Renames every nib (active and archive) under .nibs/ to use a new prefix
and updates .nibs.yml. This is a bulk filesystem operation — run on a
clean git working tree (check covers both .nibs/ and .nibs.yml) unless
--force is given.

Use --dry-run to print the planned renames without mutating anything.
Dry-run is read-only and does NOT consult git, so --force is ignored
when combined with --dry-run.`,
	Args: codedExactArgs(&setPrefixJSON, 1),
	RunE: runSetPrefix,
}

func init() {
	configSetPrefixCmd.Flags().BoolVar(&setPrefixDryRun, "dry-run", false, "Print the plan without mutating anything")
	configSetPrefixCmd.Flags().BoolVar(&setPrefixForce, "force", false, "Proceed even if the nibs directory has uncommitted git changes")
	configSetPrefixCmd.Flags().BoolVar(&setPrefixJSON, "json", false, "Output as JSON")
	configCmd.AddCommand(configSetPrefixCmd)
	rootCmd.AddCommand(configCmd)
}

func runSetPrefix(cmd *cobra.Command, args []string) error {
	app := getApp(cmd)
	newPrefix := args[0]
	// Match nibs init's convention: prefixes end with a separator dash.
	// Accept both "bgt" and "bgt-" as equivalent input so users don't have
	// to remember the trailing dash.
	if !strings.HasSuffix(newPrefix, "-") {
		newPrefix += "-"
	}
	cfg := app.Config()
	oldPrefix := cfg.Nibs.Prefix
	root := app.Core.Root()

	snapshot := buildSnapshot(app.Core.All())

	targetExists := func(relPath string) bool {
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(relPath)))
		if err == nil {
			return true
		}
		if os.IsNotExist(err) {
			return false
		}
		// Fail safe: unknown stat errors treated as "exists" so the plan is
		// rejected rather than silently allowed through.
		return true
	}

	plan, err := reprefix.BuildPlan(snapshot, oldPrefix, newPrefix, targetExists)
	if err != nil {
		return cmdError(setPrefixJSON, output.ErrValidation, "%v", err)
	}

	if len(plan.Collisions) > 0 {
		return cmdError(setPrefixJSON, output.ErrConflict,
			"cannot proceed: %d target path(s) would collide: %v",
			len(plan.Collisions), plan.Collisions)
	}

	if setPrefixDryRun {
		return printPlan(plan, setPrefixJSON)
	}

	if !setPrefixForce {
		// Check git-dirtiness over the two paths this command actually mutates:
		// the nibs data directory and the .nibs.yml config file. Unrelated
		// uncommitted edits elsewhere in the project root are not our concern.
		configDir := cfg.ConfigDir()
		nibsRel, err := filepath.Rel(configDir, root)
		if err != nil {
			return cmdError(setPrefixJSON, output.ErrFileError, "resolving nibs path: %v", err)
		}
		dirty, err := gitIsDirtyFn(configDir, filepath.ToSlash(nibsRel), config.ConfigFileName)
		if err != nil {
			return cmdError(setPrefixJSON, output.ErrFileError, "checking git status: %v", err)
		}
		if dirty {
			return cmdError(setPrefixJSON, output.ErrValidation,
				"nibs data directory or %s has uncommitted git changes — commit or stash them, or pass --force to proceed anyway",
				config.ConfigFileName)
		}
	}

	if err := reprefix.Execute(plan, root); err != nil {
		return cmdError(setPrefixJSON, output.ErrFileError, "%v", err)
	}

	cfg.Nibs.Prefix = newPrefix
	if err := cfg.Save(""); err != nil {
		return cmdError(setPrefixJSON, output.ErrFileError,
			"files renamed to prefix %q but updating %s failed: %v\nto recover, edit %s manually and set nibs.prefix to %q",
			newPrefix, config.ConfigFileName, err, config.ConfigFileName, newPrefix)
	}

	msg := fmt.Sprintf("Changed prefix from %q to %q; renamed %d file(s)", oldPrefix, newPrefix, len(plan.Files))
	if setPrefixJSON {
		return output.SuccessMessage(msg)
	}
	fmt.Println(msg)
	return nil
}

func buildSnapshot(nibs []*nib.Nib) []reprefix.NibSnapshot {
	out := make([]reprefix.NibSnapshot, len(nibs))
	for i, b := range nibs {
		out[i] = reprefix.NibSnapshot{
			ID:        b.ID,
			Path:      b.Path,
			Parent:    b.Parent,
			BlockedBy: b.BlockedBy,
		}
	}
	return out
}

// dryRunResponse is the JSON envelope returned by `config set-prefix --dry-run --json`.
// It mirrors the shape of output.Response (success + message) but adds the
// rendered plan so scripts can branch on `.success` consistently across the
// success, error, and dry-run exit paths.
type dryRunResponse struct {
	Success bool                 `json:"success"`
	Message string               `json:"message"`
	Plan    *reprefix.RenamePlan `json:"plan,omitempty"`
}

func printPlan(plan *reprefix.RenamePlan, jsonMode bool) error {
	if jsonMode {
		return output.JSONRaw(dryRunResponse{
			Success: true,
			Message: fmt.Sprintf("Would change prefix from %q to %q (%d files)", plan.OldPrefix, plan.NewPrefix, len(plan.Files)),
			Plan:    plan,
		})
	}
	fmt.Printf("Would change prefix from %q to %q\n", plan.OldPrefix, plan.NewPrefix)
	fmt.Printf("Plan: %d file(s) to rename\n", len(plan.Files))
	for _, fp := range plan.Files {
		fmt.Printf("  %s -> %s\n", fp.OldPath, fp.NewPath)
	}
	return nil
}

// realGitIsDirty shells out to git to check for uncommitted changes, running
// from workTree as the current directory and restricting the status query to
// the given pathspecs (interpreted relative to workTree). With no pathspecs,
// checks the whole worktree. Returns (false, nil) when workTree is not inside
// a git repo (nothing to guard), (true, nil) when git reports any uncommitted
// changes within the scope, or (false, err) when git is available but the
// status call itself fails unexpectedly.
//
// LC_ALL=C forces English output so any future error-message parsing remains
// stable across locales. GIT_OPTIONAL_LOCKS=0 avoids grabbing the index lock
// for a read-only query.
func realGitIsDirty(workTree string, pathspecs ...string) (bool, error) {
	// Cheap precheck: is this directory inside a git repo at all?
	check := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	check.Dir = workTree
	check.Env = append(os.Environ(), "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0")
	if err := check.Run(); err != nil {
		// Not a git repo, git missing, or otherwise unreachable — nothing to guard.
		return false, nil
	}

	args := []string{"status", "--porcelain"}
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = workTree
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}
