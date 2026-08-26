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
	"github.com/alphaleonis/nibs/internal/nibcore"
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

	// Derives the rename plan from whatever the loaded store holds RIGHT NOW, so
	// it can be run a second time once the store lock is held.
	//
	// It hands its refusal back unwrapped, with the exit code that belongs to
	// it, because the two calls have DIFFERENT things to say about the same two
	// failures: at the first, a store the plan does not fit is one the caller
	// typed the wrong prefix for; at the second, it is a store that moved while
	// this command waited. cmdError cannot be applied twice — in JSON mode it
	// reports the envelope on stdout as it builds it — so the sentence has to be
	// chosen before the error is made, not wrapped afterwards.
	buildPlan := func() (*reprefix.RenamePlan, string, error) {
		plan, err := reprefix.BuildPlan(buildSnapshot(app.Core.All()), oldPrefix, newPrefix, targetExists)
		if err != nil {
			return nil, output.ErrValidation, err
		}
		if len(plan.Collisions) > 0 {
			return nil, output.ErrConflict, fmt.Errorf("cannot proceed: %d target path(s) would collide: %v",
				len(plan.Collisions), plan.Collisions)
		}
		return plan, "", nil
	}

	plan, code, err := buildPlan()
	if err != nil {
		return cmdError(setPrefixJSON, code, "%v", err)
	}

	if setPrefixDryRun {
		return printPlan(plan, setPrefixJSON)
	}

	// The renames and the config rewrite are two halves of one change, and the
	// config half is a read-modify-write of the whole file, so they are one
	// critical section. Without the lock, a concurrent `nibs area rename` — which
	// holds this same lock from the moment it plans its own config edit until it
	// writes it — reads the pre-set-prefix config inside its window and writes it
	// back over this one. Both processes exit 0, and the store is left with every
	// nib file renamed and a config still declaring the old prefix.
	//
	// The dry run above stays outside: it is read-only, and holding a write lock
	// to print a plan would block writers for nothing. The plan it printed is
	// discarded for a real run — see the re-derivation below, which is what makes
	// the lock reach the rename at all.
	//
	// It blocks rather than refusing, matching every other store mutation: the
	// other holder is another nibs process finishing one operation.
	lock, err := nibcore.AcquireStoreLock(root)
	if err != nil {
		return cmdError(setPrefixJSON, output.ErrFileError,
			"this store's write lock could not be taken, and set-prefix renames every nib file and rewrites the config so it must hold one: %v", err)
	}
	defer func() { _ = lock.Release() }()

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

	// THE PLAN IS DERIVED HERE, at the last read before the first rename, and
	// that placement is the whole point of the lock above: a `nibs new` waits on
	// it (Core.Create takes the same lock), so a plan built now cannot be missing
	// a file that process is about to write. Built from the snapshot loaded at
	// startup instead — before the lock — it misses whatever landed in that
	// window, and the run leaves that nib under the OLD prefix while every other
	// file and the config carry the new one: an id no vocabulary in the store
	// declares. A concurrent create is an ordinary event, so it is included
	// rather than refused over.
	//
	// oldPrefix is still the one this process read at startup, and that is a
	// refusal rather than a hole: another set-prefix completing in the window
	// leaves every id under ITS new prefix, which BuildPlan rejects by name —
	// over an untouched store, since nothing below has run yet.
	if err := app.Core.Load(); err != nil {
		return cmdError(setPrefixJSON, output.ErrFileError,
			"nothing was renamed: re-reading the store under its write lock failed: %v", err)
	}
	plan, code, err = buildPlan()
	if err != nil {
		// A plan that fitted the store before the lock and does not fit it after
		// can only mean the store moved in between, so this is the one refusal
		// here that is nobody's mistake — and a rerun is the whole of the
		// repair: a fresh process reads the prefix the winner left and plans
		// from that. Naming it matters more than usual, because BuildPlan's own
		// wording ("snapshot contains nib …") describes this command's
		// bookkeeping rather than anything the caller did.
		//
		// newPrefix is safe to interpolate into a command: reaching here means
		// the FIRST buildPlan succeeded, and BuildPlan runs it through
		// reprefix.ValidatePrefix.
		//
		// --force is carried over rather than dropped, because the winner just
		// renamed every file in the store: in a git-tracked store the rerun
		// meets the dirtiness guard the caller already answered, and prescribing
		// the bare form would be prescribing a refusal.
		rerun := "nibs config set-prefix " + newPrefix
		if setPrefixForce {
			rerun += " --force"
		}
		return cmdError(setPrefixJSON, code,
			"nothing was renamed: another nibs process changed this store while this command waited for its write lock (%v), so the plan rebuilt under the lock no longer fits it — rerun `%s` to plan against the store as it now stands",
			err, rerun)
	}

	// The config edit is resolved BEFORE the first file is renamed: a rename is
	// durable the moment it lands, so a content refusal discovered afterwards
	// would leave every file carrying a prefix the config does not declare, with
	// no rerun that repairs it. Resolved here it is a refusal over an untouched
	// store. See config.StoredPrefixEdit.
	edit, err := config.PlanSetStoredPrefix(cfg.StoreDir(), newPrefix)
	if err != nil {
		// Same split in exit codes the areas verbs make: a PrefixEditRefusal is
		// about the file's CONTENT, which is the caller's to fix; anything else
		// is the filesystem's.
		var refusal *config.PrefixEditRefusal
		if errors.As(err, &refusal) {
			return cmdError(setPrefixJSON, output.ErrValidation,
				"nothing was renamed: %v", err)
		}
		return cmdError(setPrefixJSON, output.ErrFileError, "nothing was renamed: %v", err)
	}

	if err := reprefix.Execute(plan, root); err != nil {
		return cmdError(setPrefixJSON, output.ErrFileError, "%v", err)
	}

	// Only the prefix key, edited in place — see config.PlanSetStoredPrefix for
	// why the merged read model must not be marshaled back over the file.
	cfg.Nibs.Prefix = newPrefix
	staleLink, err := edit.Write()
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
	// A holder that loaded this store before the renames is now working under a
	// vocabulary no file in it carries, and nothing it can do clears that: its
	// creates refuse (nibcore.StoreRePrefixedError) and every short id it is
	// asked to resolve is prepended the retired prefix. The operator who caused
	// it is the only one positioned to act, and this is the only moment they are
	// looking.
	msg += liveServeNote(app, "\nNote: another nibs process is holding this store. A running `nibs serve` reads the id prefix once at startup, so restart it — until then it refuses every nib it is asked to create and resolves no short id against the renamed files.")
	if setPrefixJSON {
		return output.SuccessMessage(msg)
	}
	fmt.Println(msg)
	return nil
}

// buildSnapshot projects loaded nibs onto the minimal view the reprefix planner
// consumes. The link fields it copies are exactly nib.LinkSpelling's four, which
// is the completeness bar FOR FRONT MATTER: an id-valued front-matter field left
// out here is one the rename silently leaves naming a nib that no longer exists.
//
// The links come from the FILE's spelling (nib.RawLinks), not from the resolved
// values the loaded store holds. A short-form link needs no rewriting at all —
// it carries no prefix, so it resolves by prefix-prepending under the new prefix
// exactly as it did under the old — but the store canonicalizes `parent: p1` to
// `nibs-p1` in memory, and reprefix.Execute writes the plan's value over
// whatever the file said. Planning from the resolved value therefore expands
// every short link the author wrote, in a command whose contract is that it
// changes the prefix and nothing else.
//
// A nib whose in-memory links have run ahead of its file is safe here: RawLinks
// answers from the live fields when no file spelling has been recorded, and a
// recorded spelling always describes the bytes Execute is about to re-read.
//
// The bar stops at the front matter because the snapshot is a front-matter view.
// Body ids are handled a layer down instead: reprefix.Execute retargets every
// full-form `#<prefix><id>` mention while it re-renders each file, working from
// the plan's prefix pair rather than from anything this snapshot carries. `#` is
// the whole mention grammar — nib.ExtractMentionSpans recognizes no other form,
// and neither does the web renderer, so `[[id]]` in a body is prose that merely
// looks like a link and is left as written.
//
// What still has no coverage anywhere is a body mention naming a nib that does
// not exist: `nibs check` reports no link issue for one, so a mention broken by
// some other means stays silent.
func buildSnapshot(nibs []*nib.Nib) []reprefix.NibSnapshot {
	out := make([]reprefix.NibSnapshot, len(nibs))
	for i, b := range nibs {
		raw := b.RawLinks()
		out[i] = reprefix.NibSnapshot{
			ID:        b.ID,
			Path:      b.Path,
			Parent:    raw.Parent,
			Milestone: raw.Milestone,
			BlockedBy: raw.BlockedBy,
			Blocking:  raw.Blocking,
		}
	}
	return out
}

// dryRunResponse is the JSON envelope returned by `config set-prefix --dry-run --json`.
// It mirrors the shape of output.Response (success + message) but adds the
// rendered plan so scripts can branch on `.success` consistently across the
// success, error, and dry-run exit paths.
//
// BodyMentions is not part of the plan and is not omitempty: it reports the one
// class of write no FilePlan describes, and that write happens on every run.
type dryRunResponse struct {
	Success      bool                 `json:"success"`
	Message      string               `json:"message"`
	Plan         *reprefix.RenamePlan `json:"plan,omitempty"`
	BodyMentions bodyMentionPreview   `json:"bodyMentions"`
}

// bodyMentionPreview is the dry run's account of the prose the real run
// rewrites. reprefix.Execute retargets every full-form `#<oldPrefix><id>` body
// mention, driven by the plan's prefix pair rather than by anything a FilePlan
// carries, so the file list previews none of it.
//
// It reports the mention SHAPES rather than a count, and that is a design
// constraint rather than a shortcut: reprefix.BuildPlan does no disk I/O, so
// nothing on this path has read a body, and a number here would be a number
// nobody measured. What an operator needs before an irreversible store-wide run
// is that prose is in scope at all.
type bodyMentionPreview struct {
	From string `json:"from"`
	To   string `json:"to"`
	Note string `json:"note"`
}

// bodyMentionNote is shared by the JSON envelope and the human preview so the
// two cannot drift into describing the same write differently.
const bodyMentionNote = "nib bodies are rewritten too; those edits are not itemized, because the plan covers filenames and front matter only"

// previewBodyMentions renders the mention shapes the rewrite moves between.
// `<id>` is a placeholder, not a literal: the real run retargets whatever id
// follows the prefix.
func previewBodyMentions(plan *reprefix.RenamePlan) bodyMentionPreview {
	return bodyMentionPreview{
		From: "#" + plan.OldPrefix + "<id>",
		To:   "#" + plan.NewPrefix + "<id>",
		Note: bodyMentionNote,
	}
}

func printPlan(plan *reprefix.RenamePlan, jsonMode bool) error {
	bodies := previewBodyMentions(plan)
	if jsonMode {
		return output.JSONRaw(dryRunResponse{
			Success:      true,
			Message:      fmt.Sprintf("Would change prefix from %q to %q (%d files)", plan.OldPrefix, plan.NewPrefix, len(plan.Files)),
			Plan:         plan,
			BodyMentions: bodies,
		})
	}
	fmt.Printf("Would change prefix from %q to %q\n", plan.OldPrefix, plan.NewPrefix)
	fmt.Printf("Plan: %d file(s) to rename\n", len(plan.Files))
	for _, fp := range plan.Files {
		fmt.Printf("  %s -> %s\n", stripControlChars(fp.OldPath), stripControlChars(fp.NewPath))
	}
	// This list is the operator's only look at a store-wide run with no
	// rollback, and it stops at filenames. The write class it cannot show is
	// stated here rather than left to FilePlan's doc comment, which is read by
	// maintainers and not by the person standing in front of the run. It is a
	// statement of scope, not a count — see previewBodyMentions.
	fmt.Printf("Note: %s. Every full-form %q mention in prose becomes %q; mentions inside code spans, code fences, link URLs and HTML blocks are left as written.\n",
		bodyMentionNote, bodies.From, bodies.To)
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
