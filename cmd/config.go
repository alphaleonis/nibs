package cmd

import (
	"bytes"
	"errors"
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
// treats "not a repo" as "nothing to guard". The caller passes the store
// directory as workTree, so the check covers exactly what the command mutates
// (its nib files and its config) and no more. Tests override this to avoid
// shelling out to real git.
var gitIsDirtyFn = realGitIsDirty

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage project configuration",
	// Cobra auto-shows help when no subcommand is given — no RunE needed.
}

var configSetPrefixCmd = &cobra.Command{
	Use:   "set-prefix <new-prefix>",
	Short: "Change the project nib ID prefix and rename all nibs on disk",
	Long: `Renames every nib (active and archived) in the store to use a new prefix
and updates the store's config.yml. This is a bulk filesystem operation — run
on a clean git working tree (the check covers the whole .nibs store) unless
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
		// Check git-dirtiness over the ONE path this command mutates: the store
		// directory, which holds the renamed nib files and the config file it
		// rewrites. Unrelated uncommitted edits elsewhere in the project are not
		// our concern.
		dirty, err := gitIsDirtyFn(root, ".")
		if err != nil {
			return cmdError(setPrefixJSON, output.ErrFileError, "checking git status: %v", err)
		}
		if dirty {
			return cmdError(setPrefixJSON, output.ErrValidation,
				"the nibs store at %s has uncommitted git changes — commit or stash them, or pass --force to proceed anyway",
				root)
		}
	}

	if err := reprefix.Execute(plan, root); err != nil {
		return cmdError(setPrefixJSON, output.ErrFileError, "%v", err)
	}

	// Only the prefix key, edited in place. cfg is the MERGED read model — user
	// config and system defaults layered onto the project's own values — so
	// marshaling it back would write advisory settings into the project's
	// committed config and destroy every key this build does not model.
	cfg.Nibs.Prefix = newPrefix
	staleLink, err := config.SetStoredPrefix(cfg.StoreDir(), newPrefix)
	if err != nil {
		configFile := cfg.Layout().ConfigPath()
		return cmdError(setPrefixJSON, output.ErrFileError,
			"files renamed to prefix %q but updating %s failed: %v\nto recover, edit %s manually and set nibs.prefix to %q",
			newPrefix, configFile, err, configFile, newPrefix)
	}

	msg := fmt.Sprintf("Changed prefix from %q to %q; renamed %d file(s)", oldPrefix, newPrefix, len(plan.Files))
	if staleLink != "" {
		// The atomic write replaced a symlink, so whatever manages that target
		// still holds the old prefix and will restore it (see config.Save).
		msg += fmt.Sprintf("\nNote: %s was a symlink to %s and is now a regular file; %s still holds the old prefix, so update or remove it",
			cfg.Layout().ConfigPath(), stripControlChars(staleLink), stripControlChars(staleLink))
	}
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
		fmt.Printf("  %s -> %s\n", stripControlChars(fp.OldPath), stripControlChars(fp.NewPath))
	}
	return nil
}

// storeGitStateFn reports the git protection state of the nibs store for the
// migrate command's safety net. Tests override it to exercise the paths real
// git cannot produce on demand (a genuine git failure). It belongs to the same
// helper family as gitIsDirtyFn above: one seam per git question, backed by
// shared exec conventions.
var storeGitStateFn = realStoreGitState

// realStoreGitState reports whether the store at nibsRoot is protected by a
// git repository (isRepo) and whether that repository has uncommitted changes
// under the store (dirty).
//
// "Inside a repo" is not enough for isRepo: a store directory gitignored by an
// enclosing repository is invisible to it — `git status` lists nothing and
// `git add` refuses outright — so that repository can neither review nor roll
// back a migration. `git check-ignore -q .` discriminates the two layouts:
// exit 0 means the store directory itself is ignored (no safety net; report
// isRepo=false so the caller falls back to the backup suggestion), exit 1
// means the repository genuinely covers the store. A store with its own
// nested .git is never ignored by itself, so it reports isRepo=true.
//
// Error contract: "not a repo at all" (or git missing) is a normal state,
// reported as (false, false, nil). A genuine git failure inside a repo
// (check-ignore or status crashing) is returned as err so the caller can tell
// the user the safety net could not be evaluated instead of silently
// downgrading to "clean".
func realStoreGitState(nibsRoot string) (dirty, isRepo bool, err error) {
	inside := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	inside.Dir = nibsRoot
	inside.Env = append(os.Environ(), "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0")
	if err := inside.Run(); err != nil {
		// Not a git repo, git missing, or otherwise unreachable — nothing to
		// guard; the caller suggests a backup instead.
		return false, false, nil
	}

	ignored := exec.Command("git", "check-ignore", "-q", ".")
	ignored.Dir = nibsRoot
	ignored.Env = append(os.Environ(), "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0")
	if runErr := ignored.Run(); runErr == nil {
		// Exit 0: the store directory is gitignored by the repository around it
		// — untracked, unrestorable, no rollback net.
		return false, false, nil
	} else {
		var ee *exec.ExitError
		if !errors.As(runErr, &ee) || ee.ExitCode() != 1 {
			// Exit 1 means "not ignored" (the covered case, handled below);
			// anything else is a genuine git failure.
			return false, true, fmt.Errorf("git check-ignore: %w", runErr)
		}
	}

	dirty, derr := realGitIsDirty(nibsRoot, ".")
	if derr != nil {
		return false, true, derr
	}
	return dirty, true, nil
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
