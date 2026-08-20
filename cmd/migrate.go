package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/fsutil"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// migrateEnv carries the filesystem coordinates a migration step operates on.
//
// The configuration is resolved ON DEMAND rather than captured, and that is
// load-bearing: the layout step MOVES the project config into the store, so a
// *config.Config read before the run is stale for every step that follows it.
// Content steps load a scoped throwaway Core with the config as it stands when
// they ask, which is what makes a short-form link id canonicalize under the
// project's real prefix instead of the empty default a pre-move read returns.
//
// The git observations are memoized on a shared pointer, so the narration and the
// gates that act on the same observation consult git ONCE per run. Splitting
// narration from gating had every `nibs migrate` shell out to git twice with
// identical arguments; --dry-run never did, because it only ran the gates.
type migrateEnv struct {
	nibsRoot string
	// serveExcluded records that this run already holds the serve exclusion, so
	// gateNoLiveServe does not probe a lock the same process is holding.
	serveExcluded bool
	loadCfg       func() (*config.Config, error)
	git           *gitObservations
}

// gitObservations caches the answers to the git questions one migrate run asks.
// Each is answered at most once; the zero value means "not asked yet".
type gitObservations struct {
	storeAsked  bool
	storeDirty  bool
	storeIsRepo bool
	storeErr    error

	legacyAsked bool
	legacyDirty bool
	legacyErr   error
}

// newMigrateEnv builds the env for a resolved store root.
func newMigrateEnv(nibsRoot string) migrateEnv {
	env := migrateEnv{git: &gitObservations{}}
	env.setRoot(nibsRoot)
	return env
}

// storeGitState reports whether a git repository genuinely covers the store and
// whether it has uncommitted changes (see realStoreGitState), asking git once.
func (e migrateEnv) storeGitState() (dirty, isRepo bool, err error) {
	if e.git == nil {
		return storeGitStateFn(e.nibsRoot)
	}
	if !e.git.storeAsked {
		e.git.storeDirty, e.git.storeIsRepo, e.git.storeErr = storeGitStateFn(e.nibsRoot)
		e.git.storeAsked = true
	}
	return e.git.storeDirty, e.git.storeIsRepo, e.git.storeErr
}

// legacyConfigDirty reports whether the pre-layout `.nibs.yml` this migration
// deletes has uncommitted changes, asking git once.
func (e migrateEnv) legacyConfigDirty() (bool, error) {
	if e.git == nil {
		return gitIsDirtyFn(e.layout().ProjectDir(), store.LegacyProjectConfigFileName)
	}
	if !e.git.legacyAsked {
		e.git.legacyDirty, e.git.legacyErr = gitIsDirtyFn(e.layout().ProjectDir(), store.LegacyProjectConfigFileName)
		e.git.legacyAsked = true
	}
	return e.git.legacyDirty, e.git.legacyErr
}

// setRoot points the env at a store root, rebuilding the config loader with it.
//
// It is called again mid-run when the layout step RELOCATES the store: every
// path a step derives comes from nibsRoot, so the steps that follow have to
// address the store where it now is, and the config they read is the one that
// moved with it.
func (e *migrateEnv) setRoot(nibsRoot string) {
	e.nibsRoot = nibsRoot
	e.loadCfg = func() (*config.Config, error) { return migrateConfig(nibsRoot) }
}

// layout returns the store's directory structure.
func (e migrateEnv) layout() store.Layout { return store.NewLayout(e.nibsRoot) }

// legacyConfigPath is where the pre-layout project config sat: beside the
// store, not inside it.
func (e migrateEnv) legacyConfigPath() string {
	return filepath.Join(e.layout().ProjectDir(), store.LegacyProjectConfigFileName)
}

// config resolves the project configuration at the moment it is asked for.
func (e migrateEnv) config() (*config.Config, error) { return e.loadCfg() }

// migrateConfig reads the project config from inside the store as it stands right
// now, and DELIBERATELY ignores --config.
//
// --config can only name a store's own config.yml (resolveStoreDir requires the
// basename and refuses the pre-layout `.nibs.yml`), so for an unmoved store the
// flag's file and the store's config.yml are the same file. But the layout step
// RELOCATES the store, and the flag's value is the static string the user typed:
// after the move it names a file under a directory that no longer exists, and an
// absent config reads as "use the defaults" — so the content steps that follow
// loaded the store under the EMPTY prefix and dropped every short-form `blocking:`
// edge while reporting a completed migration. Deriving from nibsRoot is what makes
// migrateEnv's on-demand resolution mean what its comment says.
//
// A store with no config at all resolves to the defaults, which is the normal
// state of a pre-layout store and must not be an error: the refusal gate has to
// work before the config has moved into the store.
func migrateConfig(nibsRoot string) (*config.Config, error) {
	return config.LoadStoreWithUserConfig(nibsRoot)
}

// logf is the migration engine's progress sink, so runMigrations stays
// testable without capturing stdout.
type logf func(format string, a ...any)

// migrationStep is one ordered entry in the migration chain. A step detects
// its own pendingness in exactly one of two ways, and carries exactly one of
// the two detectors:
//
// pred is a CONTENT step's detector: it answers "does this FILE need the
// step?" over its scanned front-matter header — a pure predicate, never a
// Core, never a full YAML parse, and NEVER a write. Detection ("does this
// store need the step?") is one shared filesystem walk (scanStore) that reads
// each file's header ONCE and evaluates every content step's pred plus the
// newer-store check against it, so the per-command pre-run probe stays
// O(files) no matter how many steps the chain grows.
//
// shape is a STORE-SHAPE step's detector: it answers "is the store's directory
// structure still the old one?", which no per-file header can express, and
// returns the number of files the step would move. It is evaluated ONCE per
// scan rather than per file.
//
// Detection of both kinds runs on every command (the PersistentPreRunE
// refusal) and a second time under the store lock, where detect-gates-apply is
// what makes every step idempotent and a crashed run resumable by re-running.
//
// apply performs the migration and is FAIL-LOUD: the first error aborts the
// run. Content steps load a scoped throwaway Core (the existing load pipeline
// does canonicalization) rather than growing a second parse pipeline. The
// *StoreLock is runMigrations' whole-run store lock, threaded through as the
// Core migration methods' proof-of-lock parameter.
// apply takes the env by POINTER because one step moves the store itself: the
// layout step's relocation repoints the env (see migrateEnv.setRoot) so the
// steps after it address the store's new location.
//
// plan and invalidatesLoad are DECLARED CAPABILITIES rather than things a gate
// infers. Two gates used to read "any step with no per-file predicate is pending"
// as "the layout step is pending", in opposite directions — one runs planLayout
// when it holds, the other skips itself when it holds — and only the layout step
// actually has the property both were reaching for. A second shape step would have
// silently disabled the load gate for the whole run with nothing in the type or the
// list to catch it.
type migrationStep struct {
	name, title string
	pred        func(fmHeader) bool
	shape       func(migrateEnv) (int, error)
	// plan, when set, computes everything the step will do WITHOUT touching the
	// filesystem, returning the step's complete set of refusals. It is what lets
	// --dry-run name them: the step computes the same plan when it runs.
	plan func(migrateEnv) error
	// invalidatesLoad marks a step whose apply MOVES the files Core.Load reads.
	// While such a step is pending, anything answered by loading the store is
	// answered against a store that is not there yet.
	invalidatesLoad bool
	apply           func(*migrateEnv, *nibcore.StoreLock, logf) error
}

// isContent reports whether the step detects per file (as opposed to per
// store shape). The distinction decides which files a scan problem can
// endanger — see runMigrations' fail-loud gate.
func (s migrationStep) isContent() bool { return s.pred != nil }

// migrationSteps is the ordered migration chain. A future format bump (v2)
// appends one more entry with no engine change — but note the watcher's
// legacy-shape breadcrumb (handleChanges in internal/nibcore/watcher.go)
// restates this chain's detection for files arriving into a LIVE serve:
// below-current versions are covered there by nib.CurrentVersion, while a new
// step NOT keyed on the version must be mirrored into that condition by hand,
// or its files arrive with no breadcrumb.
//
// Two invariants govern this chain, and only one of them is about order.
//
// The order-free one: ONLY the version-bump step may write the version stamp.
// `version: 1` is v0-blocking's completion record — a step that stamped a
// still-v0 file would mark its `blocking:` edges migrated without transferring
// them, and because v0 detection keys on the version, nothing would ever
// return to finish the job; the edges silently vanish from every view (the
// retired load-time migration carried the same guard, refusing to persist
// exactly that half-migrated shape). Because NormalizeLegacyPriorities honors
// the rule — a still-v0 file it rewrites renders `version: 0`, so isV0Header
// keeps firing — the content steps converge to the same store whichever order
// they run in; v0-blocking precedes priority-deferred simply so one run
// converts a doubly-legacy file in one pass.
//
// The ordering one: `layout` MUST run FIRST. It relocates the project config
// INTO the store, and every content step afterwards loads a Core whose
// canonicalization resolves short-form link ids under the project's prefix —
// which only that config carries. Run a content step first and its Core reads
// the empty default prefix, leaving a short-form `blocking:` target
// unresolvable and its edge dropped. Nothing else in the engine encodes this;
// the position in this slice is the whole mechanism.
var migrationSteps = []migrationStep{
	{
		name:            layoutStepName,
		title:           "put the store, the project config and the nib files where the current layout expects them",
		shape:           layoutPendingCount,
		plan:            func(env migrateEnv) error { _, err := planLayout(env); return err },
		invalidatesLoad: true,
		apply:           applyLayout,
	},
	{
		name:  "v0-blocking",
		title: "invert legacy `blocking:` edges onto targets' blocked_by and stamp version: 1",
		pred:  isV0Header,
		apply: applyV0Blocking,
	},
	{
		name:  "priority-deferred",
		title: "rewrite the legacy `priority: deferred` value to `low`",
		pred:  hasDeferredPriorityHeader,
		apply: applyPriorityDeferred,
	},
}

// isV0Header reports whether a scanned header describes a legacy v0 file: an
// absent `version:` key parses as 0 (see nib.Nib.Version).
func isV0Header(h fmHeader) bool {
	return h.version < 1
}

// hasDeferredPriorityHeader reports whether a scanned header carries the
// legacy `priority: deferred` value. Deliberately independent of the version
// key: a version: 1 file hand-edited back to `deferred` (or restored from a
// backup) is still caught — the per-file scan is what makes the store
// self-healing without any store-level version stamp.
func hasDeferredPriorityHeader(h fmHeader) bool {
	return h.priority == "deferred"
}

// applyPriorityDeferred loads the store through the normal pipeline and
// rewrites legacy deferred priorities (see nibcore.Core.NormalizeLegacyPriorities).
func applyPriorityDeferred(env *migrateEnv, lock *nibcore.StoreLock, log logf) error {
	core, err := loadStoreForMigration(*env)
	if err != nil {
		return err
	}
	n, err := core.NormalizeLegacyPriorities(lock)
	if err != nil {
		return err
	}
	log("priority-deferred: rewrote %d nib(s) to priority low", n)
	return nil
}

// applyV0Blocking loads the store through the normal pipeline and runs the
// relocated v0→v1 conversion (see nibcore.Core.MigrateV0ToV1).
func applyV0Blocking(env *migrateEnv, lock *nibcore.StoreLock, log logf) error {
	core, err := loadStoreForMigration(*env)
	if err != nil {
		return err
	}
	n, err := core.MigrateV0ToV1(lock)
	if err != nil {
		return err
	}
	log("v0-blocking: migrated %d nib(s) to version 1", n)
	return nil
}

// layoutPendingCount reports how many things the layout step would put right:
// every nib file still sitting outside data/ and archive/, the project config if it
// is still beside the store rather than inside it, the store DIRECTORY itself if it
// is still outside `<project>/.nibs`, and a config ALREADY inside the store that
// still carries the retired `nibs.path` key. Zero means the store already has the
// current shape.
func layoutPendingCount(env migrateEnv) (int, error) {
	movable, err := layoutMovableFiles(env)
	if err != nil {
		return 0, err
	}
	n := len(movable)
	if legacyConfigExists(env) {
		n++
	}
	relocating, err := storeRelocationPending(env)
	if err != nil {
		return 0, err
	}
	if relocating {
		n++
	}
	// Only when no legacy config sits beside the store, matching planLayout's
	// precedence: with both present the relocation owns the decision, and a
	// destination that cannot be parsed is a two-config conflict planConfigRelocation
	// reports precisely rather than a detection failure.
	if !legacyConfigExists(env) {
		retired, err := storeConfigRetiredKeyPending(env)
		if err != nil {
			return 0, err
		}
		if retired {
			n++
		}
	}
	return n, nil
}

// storeConfigRetiredKeyPending reports whether the config INSIDE the store still
// carries the retired `nibs.path` key.
//
// It is layout work like any other shape defect, and until it was detected here the
// state was terminal: config.loadRaw refuses such a config outright, naming
// `nibs migrate`, while `nibs migrate` answered "Store is up to date; no migrations
// pending" — no legacy config beside the store, no movable files, no relocation. A
// user reaches it by hand-moving `.nibs.yml` to `.nibs/config.yml`, which is the
// obvious reading of the new layout.
//
// An unreadable or non-YAML config is reported as an error rather than as "no key":
// deciding nothing is pending because the evidence could not be read is what leaves
// a wedged store looking healthy.
func storeConfigRetiredKeyPending(env migrateEnv) (bool, error) {
	declared, err := config.RetiredNibsPath(env.layout().ConfigPath())
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", env.layout().ConfigPath(), err)
	}
	return declared != "", nil
}

// storeRelocationPending reports whether the store still sits somewhere other
// than `<project>/.nibs` while the pre-layout `.nibs.yml` beside it NAMES it
// there through the retired `nibs.path` key.
//
// Where the store lives is part of the shape this step fixes. `store.FindStore`
// keys on the DIRECTORY NAME, `nibs.path` is retired and nothing replaces it as
// a root pointer — so a store converted in place outside `.nibs` is correctly
// shaped and undiscoverable, and the next command answers "run `nibs init`",
// which creates an empty second store beside the real data.
//
// The trigger is deliberately narrow. Only a store a `.nibs.yml` names moves; a
// store the user deliberately put elsewhere (`nibs init --nibs-path /srv/nibs`)
// has no such file naming it and is left exactly where it is.
//
// An occupied destination is NOT an error here: detection runs on every command,
// and a relocation blocked by one is still pending work — planLayout raises that
// refusal when migrate actually runs. An error means the evidence itself could not
// be read (`hasLegacyStoreShape`), which is a different answer from "not pending":
// deciding a store must stay put because a file's permissions denied the read
// would leave it undiscoverable with nothing said about why.
func storeRelocationPending(env migrateEnv) (bool, error) {
	if filepath.Base(env.nibsRoot) == store.DirName {
		return false, nil
	}
	return hasLegacyStoreShape(env.nibsRoot)
}

// legacyConfigExists reports whether the pre-layout project config is still
// beside the store. An unreadable path counts as absent: the apply re-checks
// and fails loudly there, and detection must never abort every command.
func legacyConfigExists(env migrateEnv) bool {
	info, err := os.Stat(env.legacyConfigPath())
	return err == nil && !info.IsDir()
}

// confirmAssumedNibs asks the person at the terminal about the files this step
// could only ASSUME are nibs. It is authorizeAssumedNibs' other half: that one
// answers for a run with nobody to ask, this one for a run with somebody.
//
// It lives in the APPLY path, not in planLayout, for two reasons. planLayout is
// re-run by wouldRefuse's plan gate before the steps execute, so a question
// asked there would be asked twice; and `--dry-run` previews through that same
// gate, where the run must REPORT what it would ask rather than ask it.
// Answering still happens before the first rename — applyLayout plans, asks,
// and only then moves.
//
// Declining ABORTS the whole migration instead of migrating everything else.
// Pending-ness is derived from what the mover would move, so a file declined
// here and left in place would still read as outstanding layout work and keep
// every command refusing; leaving the store exactly as it was is the only
// answer that does not wedge it.
func confirmAssumedNibs(assumed []string) error {
	if len(assumed) == 0 || migrateForce || !isInteractiveTerminal() {
		return nil
	}
	fmt.Printf("%d file(s) carry a title and a known status but not the shape nibs writes, so they could be nibs or ordinary documents:\n  %s\n",
		len(assumed), echoedList(sanitizedList(assumed), ""))
	fmt.Printf("Treat them as nibs and move them into %s/? [y/N] ", store.DataDirName)
	if confirmedYes() {
		return nil
	}
	return errors.New("migration cancelled; nothing has been changed. Move those files out of the store to keep them as documents, then re-run `nibs migrate`")
}

// authorizeAssumedNibs decides whether the run may move the files it could only
// ASSUME are nibs (see nibFileVerdict).
//
// The three answers are one decision seen from three places, and they must stay
// one function or they will drift: --force is a standing yes, a terminal has
// somebody to ask, and a run that is neither has nobody — so it refuses and
// changes nothing rather than choosing for the user. Moving a document is
// recoverable (nothing is deleted, and unknown front-matter keys survive the
// rewrite through frontMatter.Extra) but it is not free, and a script that
// silently converted a readme into a nib would do it on every store it touched.
//
// It runs during PLANNING, which is what makes the refusal safe: `nibs migrate`
// computes its whole plan before the first rename, so this fires before anything
// has moved (see layoutPlan).
func authorizeAssumedNibs(assumed []string) error {
	if len(assumed) == 0 || migrateForce {
		return nil
	}
	if isInteractiveTerminal() {
		return nil
	}
	return fmt.Errorf("cannot tell whether %d file(s) are nibs or ordinary documents:\n  %s\n"+
		"each carries a title and a known status but not the shape nibs writes, and this run has no terminal to ask at. "+
		"Re-run with --force to treat them as nibs and move them into %s/, or move them out of the store first to keep "+
		"them as documents. Nothing has been changed",
		len(assumed), echoedList(sanitizedList(assumed), ""), store.DataDirName)
}

// sanitizedList renders paths read from the filesystem for a message, through
// the control-character boundary every filename crosses here.
func sanitizedList(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = stripControlChars(p)
	}
	return out
}

// layoutMove is one file the layout step will move into data/, carrying the
// verdict that put it there. The verdict travels with the path because what the
// run OWES the user differs by tier: an assumed nib has to be named, and a run
// with nobody to ask has to refuse over it.
type layoutMove struct {
	rel     string
	verdict nibFileVerdict
}

// layoutMovePaths is the store-relative paths of moves, in the order given.
func layoutMovePaths(moves []layoutMove) []string {
	paths := make([]string, len(moves))
	for i, m := range moves {
		paths[i] = m.rel
	}
	return paths
}

// layoutAssumedPaths is the store-relative paths of the moves that were only
// ASSUMED to be nibs, in the order given.
func layoutAssumedPaths(moves []layoutMove) []string {
	var assumed []string
	for _, m := range moves {
		if m.verdict == assumedNib {
			assumed = append(assumed, m.rel)
		}
	}
	return assumed
}

// layoutMovableFiles returns the store-relative paths (forward slashes) of the
// nib files the layout step must move into data/, in walk order.
//
// Everything already under data/ or archive/ is where it belongs. Everything
// else under the store root is pre-layout content — including files nested in
// subdirectories, which keep their relative shape under data/.
//
// What moves is layoutVerdict's answer: the rendered shape wherever it sits, and
// at the store ROOT also a header that fits a nib and ordinary content equally
// well. What that verdict calls notANib is left where it is, so after the
// migration it simply stops being store content — which makes `.nibs/README.md`
// a legal place for a readme rather than a permanent complaint.
//
// A file whose header cannot be READ is moved anyway: the scan cannot prove it
// is not a nib, and leaving a real nib behind would drop it out of every query.
// That leniency is load-bearing elsewhere — blockingScanProblems refuses to
// migrate AROUND such a file precisely because this step moves it into data/.
func layoutMovableFiles(env migrateEnv) ([]layoutMove, error) {
	moves, _, err := layoutClassifyFiles(env)
	return moves, err
}

// layoutClassifyFiles is layoutMovableFiles plus the files it DECLINED to move —
// one walk, both answers, so the report cannot disagree with what happened.
//
// Only front-mattered files are reported as declined. A fence-less one was never
// a candidate (nib.Parse refuses it wherever it sits), so naming it would turn
// every store holding a readme into a store with a complaint.
func layoutClassifyFiles(env migrateEnv) (moves []layoutMove, declined []string, err error) {
	l := env.layout()
	var movable []layoutMove
	err = forEachNibFile(env, func(path string, skipped error) error {
		// An unreadable header is moved anyway (see above) because the scan
		// cannot prove the file is not a nib. For an irregular file it CAN: a
		// FIFO is not a nib whatever it is named, so it stays where it is rather
		// than being relocated into data/ and counted as a moved file.
		if skipped != nil {
			return nil
		}
		rel := storeRelPath(env, path)
		if l.IsDataRel(rel) || l.IsArchivedRel(rel) {
			return nil
		}
		h, hErr := readFrontMatterHeader(path)
		// An unreadable header keeps the leniency above: it is moved, and
		// recorded as isNib because nothing about it is uncertain in the way the
		// middle tier means — the run has a separate, louder answer for it
		// (blockingScanProblems refuses to migrate around such a file).
		verdict := isNib
		if hErr == nil {
			verdict = layoutVerdict(rel, h)
			if verdict == notANib {
				if h.hasFrontMatter {
					declined = append(declined, rel)
				}
				return nil
			}
		}
		movable = append(movable, layoutMove{rel: rel, verdict: verdict})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return movable, declined, nil
}

// layoutPlan is everything the layout step will do, computed BEFORE it does any
// of it.
//
// Planning and performing are separated so that every refusal this step can
// raise is raised before the FIRST rename. The moves used to run unconditionally
// and the config relocation validate afterwards, so a `nibs.path` refusal
// arrived with the store already half-moved — recoverable by re-running, but a
// gate that fires after the movement it guards is not a gate.
type layoutPlan struct {
	// relocateTo is where the WHOLE store directory moves, or "" when it is
	// already at `<project>/.nibs`. See planStoreRelocation.
	relocateTo string
	// finalRoot is the store root once relocateTo has been honored, and it is what
	// the CONFIG destination is derived from. The nib-file moves are derived from
	// the env instead, which agrees with it only because apply repoints the env to
	// relocateTo before deriving them — plan and apply must be given the same env,
	// which applyLayout guarantees by computing the plan from the env it then
	// applies with.
	finalRoot string
	// declined holds the store-relative paths of the front-mattered files this
	// step left where they are. They are not store content afterwards, which is
	// the judgement the report records.
	declined []string
	// assumed holds the store-relative paths of the moves this step could not
	// prove are nibs — a subset of movable. The run names them, and refuses over
	// them when there is nobody to ask.
	assumed []string
	// movable holds the store-relative paths (forward slashes) of the nib files
	// that move into data/, in walk order. They survive the store relocation
	// unchanged: that is a rename of the directory they sit in.
	movable []string
	// config, when non-nil, relocates the pre-layout project config into the
	// store. Nil when the project has none (already migrated, or never had one).
	config *configRelocation
	// retiredKey, when non-nil, strips the retired `nibs.path` key from a config
	// ALREADY inside the store. Mutually exclusive with config: a legacy config
	// beside the store is relocated (and stripped) instead, and two configs are
	// refused by planConfigRelocation before either is touched.
	retiredKey *configRewrite
}

// configRewrite is the planned in-place rewrite of the store's own config.yml: the
// bytes to write, the mode to write them with, and a note for the user.
type configRewrite struct {
	path string
	body []byte
	perm os.FileMode
	note string
}

// apply rewrites the store's config atomically, so a crash cannot leave a torn
// config where the previous one was complete.
func (c *configRewrite) apply(log logf) error {
	if err := fsutil.AtomicWriteFile(c.path, c.body, c.perm); err != nil {
		return fmt.Errorf("rewriting %s: %w", c.path, err)
	}
	log("layout: removed the retired `nibs.path` key from %s", c.path)
	if c.note != "" {
		log("layout: %s", c.note)
	}
	return nil
}

// planStoreConfigRewrite prepares removing the retired `nibs.path` key from the
// config already inside the store. It shares stripRetiredNibsPath with the
// relocation, so the stale-versus-live distinction — and the refusal when the key
// names a store that still exists or cannot be stat'd — is decided in one place.
func planStoreConfigRewrite(env migrateEnv, finalRoot string) (*configRewrite, error) {
	path := store.NewLayout(finalRoot).ConfigPath()
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	data, err := config.ReadConfigFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	rewritten, note, err := stripRetiredNibsPath(data, env, path)
	if err != nil {
		return nil, err
	}
	return &configRewrite{path: path, body: rewritten, perm: info.Mode().Perm(), note: note}, nil
}

// planLayout works out the whole shape change without touching the filesystem.
// Its error IS the layout step's complete set of refusals, which is why
// wouldRefuse can preview them.
func planLayout(env migrateEnv) (*layoutPlan, error) {
	plan := &layoutPlan{finalRoot: env.nibsRoot}

	target, err := planStoreRelocation(env)
	if err != nil {
		return nil, err
	}
	if target != "" {
		plan.relocateTo = target
		plan.finalRoot = target
	}

	moves, declined, err := layoutClassifyFiles(env)
	if err != nil {
		return nil, err
	}
	plan.assumed = layoutAssumedPaths(moves)
	plan.declined = declined
	if err := authorizeAssumedNibs(plan.assumed); err != nil {
		return nil, err
	}
	movable := layoutMovePaths(moves)
	// Collisions are checked against the CURRENT root: a store relocation is a
	// rename of the directory these paths sit in, so a destination that exists
	// now exists at the new root too.
	l := env.layout()
	for _, rel := range movable {
		dst := filepath.Join(l.DataDir(), filepath.FromSlash(rel))
		if _, statErr := os.Stat(dst); statErr == nil {
			return nil, fmt.Errorf("cannot move %s into %s/: %s already exists; resolve the duplicate by hand, then re-run `nibs migrate`",
				rel, store.DataDirName, l.DataRel(rel))
		}
	}
	plan.movable = movable

	if legacyConfigExists(env) {
		cr, err := planConfigRelocation(env, plan.finalRoot)
		if err != nil {
			return nil, err
		}
		plan.config = cr
		return plan, nil
	}
	// No legacy config beside the store, but the store's OWN config may still carry
	// the retired key — the state a hand-moved `.nibs.yml` leaves behind, which
	// every command refuses while naming this command.
	retired, err := storeConfigRetiredKeyPending(env)
	if err != nil {
		return nil, err
	}
	if retired {
		rewrite, err := planStoreConfigRewrite(env, plan.finalRoot)
		if err != nil {
			return nil, err
		}
		plan.retiredKey = rewrite
	}
	return plan, nil
}

// planStoreRelocation returns the directory the store must move to, or "" when
// it is already where discovery can find it (see storeRelocationPending for why
// a store outside `<project>/.nibs` cannot be left there).
//
// An occupied destination is refused rather than merged: two candidate stores
// mean one of them is stale, and choosing silently would either strand the real
// data or bury it under an empty store. Both ways out of the refusal converge —
// removing `<project>/.nibs` lets the relocation run, and removing the declared
// directory makes the retired key a stale record the rewrite then drops (see
// stripRetiredNibsPath).
func planStoreRelocation(env migrateEnv) (string, error) {
	relocating, err := storeRelocationPending(env)
	if err != nil {
		return "", err
	}
	if !relocating {
		return "", nil
	}
	target := filepath.Join(env.layout().ProjectDir(), store.DirName)
	if _, statErr := os.Lstat(target); statErr == nil {
		// ONE DIRECTORY UNDER TWO SPELLINGS, on the same message-only basis as
		// stripRetiredNibsPath's clause: storeRelocationPending compares the
		// store's BASENAME bytewise, so a store reached through a symlink or a
		// differently cased name is read as somewhere else and the Lstat above
		// then finds "another" store at the destination. There is no other, and
		// the remedy below is the one place in this tool where following a
		// refusal DESTROYS the store: "remove the other" removes the only one.
		//
		// os.Stat, not the Lstat above: the existence test must not follow a link
		// (a dangling one still occupies the name), while this comparison must.
		if info, err := os.Stat(target); err == nil && namesTheSameDirectory(info, env.nibsRoot) {
			return "", fmt.Errorf("cannot move the store %s to %s: they are ONE directory under two spellings — a symlink, or a differently cased name — so there is nothing to move and nothing to remove; run `nibs migrate --nibs-path %s`, which names the store the way nibs finds it",
				env.nibsRoot, target, shellArg(target))
		}
		return "", fmt.Errorf("cannot move the store %s to %s: %s already exists, and %s names %s as this project's store — so one of the two is stale; keep the one holding your nibs, remove the other, then re-run `nibs migrate`",
			env.nibsRoot, target, target, env.legacyConfigPath(), env.nibsRoot)
	}
	if linked, err := gitLinkageIsExternal(env.nibsRoot); err != nil {
		return "", err
	} else if linked {
		return "", fmt.Errorf("cannot move the store %s to %s: its %s is a FILE, so the store is a linked git worktree or a submodule and the repository that owns it records the store's CURRENT path — renaming it here leaves that repository pointing at a directory that no longer exists, and `git -C %s status` fails with \"not a repository\" after the next routine prune. Move it with git instead (`git worktree move %s %s` for a worktree, or move it and update the superproject for a submodule), then re-run `nibs migrate`",
			env.nibsRoot, target, gitMarkerName, target, shellArg(env.nibsRoot), shellArg(target))
	}
	return target, nil
}

// gitMarkerName is the entry git puts at the root of a work tree: a DIRECTORY for
// a plain repository, a FILE (`gitdir: …`) for a linked worktree or a submodule.
const gitMarkerName = ".git"

// gitLinkageIsExternal reports whether the git repository covering the store is
// one whose bookkeeping lives OUTSIDE the store directory, so renaming the
// directory would break it.
//
// The layout step's relocation is a single os.Rename, justified by the store's own
// git repository travelling with it intact — which holds for a plain repository,
// whose whole `.git` directory moves along. It does not hold for a linked worktree
// or a submodule: their `.git` is a file pointing into another repository's
// `worktrees/<name>/` (or `modules/<name>/`), and that repository still records the
// pre-rename path. The result is a worktree git reports as `prunable` and, after
// any `git worktree prune` (which `git gc --auto` performs routinely), a store
// directory where git no longer works at all.
//
// That matters more than the inconvenience: gateStoreGitClean verified a
// recoverable pre-migration baseline moments earlier, and postMigrateCommitHint
// then tells the user to review and commit the store's changes with git. Silently
// invalidating the net the run just leaned on is the failure this refuses.
//
// An Lstat failure other than "absent" is reported rather than assumed benign: it
// is the same "cannot determine" the rest of this step takes seriously.
func gitLinkageIsExternal(nibsRoot string) (bool, error) {
	info, err := os.Lstat(filepath.Join(nibsRoot, gitMarkerName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("checking whether the store %s is a linked git worktree: %w", nibsRoot, err)
	}
	return info.Mode().IsRegular(), nil
}

// apply performs the planned layout change, in the one order the derivations
// allow: the store relocation first (every other path hangs off the root), then
// the nib files into data/, then the project config into the store.
//
// env is repointed at the relocated store so the CONTENT steps that run after
// this one address it where it now is, and read the config that travelled with
// it (see migrateEnv.setRoot).
//
// Every move was checked for a free destination during planning, and the plan is
// computed under the run's store lock, so a run that died halfway resumes
// cleanly: the moves it already made are simply not in the next plan.
func (p *layoutPlan) apply(env *migrateEnv, log logf) error {
	if p.relocateTo != "" {
		// One rename, so the store's own git repository (if it is one) travels
		// with it intact rather than being reassembled file by file — see
		// gitLinkageIsExternal for the case where that reasoning does not hold.
		if err := storeRenameFn(env.nibsRoot, p.relocateTo); err != nil {
			// A rename across filesystems fails wholesale, so the store is still
			// exactly where it was and the run is re-runnable — but the raw OS
			// error names no way forward, unlike every other refusal in this step.
			// It takes the store itself being a mount point, since the target is
			// always the store's own parent.
			if errors.Is(err, syscall.EXDEV) {
				return fmt.Errorf("cannot move the store %s to %s: they are on different filesystems, so the store directory is its own mount point (%w). Nothing has been moved. Create %s, copy the store's contents into it, verify them, remove the originals, then re-run `nibs migrate`",
					env.nibsRoot, p.relocateTo, err, shellArg(p.relocateTo))
			}
			return fmt.Errorf("moving the store %s to %s: %w", env.nibsRoot, p.relocateTo, err)
		}
		log("layout: moved the store to %s, where nibs can find it", p.relocateTo)
		env.setRoot(p.relocateTo)
	}

	l := env.layout()
	for _, rel := range p.movable {
		src := filepath.Join(env.nibsRoot, filepath.FromSlash(rel))
		dst := filepath.Join(l.DataDir(), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(dst), err)
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("moving %s into %s/: %w", rel, store.DataDirName, err)
		}
	}
	if len(p.assumed) > 0 {
		// Named individually, and after the move rather than before it: this is
		// the record of a judgement call the run made on the user's behalf, and
		// the only thing standing between an assumed nib and a silent one. The
		// migration report is what keeps it available once the run's output has
		// scrolled away.
		log("layout: assumed %d file(s) to be nibs and moved them into %s/ — they carry a title and a known status but not the shape nibs writes:\n  %s",
			len(p.assumed), store.DataDirName, echoedList(sanitizedList(p.assumed), ""))
	}
	if err := writeMigrationReport(p.finalRoot, p.assumed, p.declined); err != nil {
		// The moves already happened, so a report that cannot be written must not
		// fail the run — but it must not vanish either: the whole reason it exists
		// is that the run's own output does not survive.
		log("warning: could not write %s (%v); the judgement calls above are only in this output",
			migrationReportName, err)
	}
	if len(p.movable) > 0 {
		log("layout: moved %d nib file(s) into %s/", len(p.movable), store.DataDirName)
	}

	if p.config != nil {
		return p.config.apply(log)
	}
	if p.retiredKey != nil {
		return p.retiredKey.apply(log)
	}
	return nil
}

// storeRenameFn is a seam over os.Rename for the STORE relocation, so tests can
// exercise the failures the filesystem will not produce on demand — a cross-device
// rename needs the store to be its own mount point. Production always uses
// os.Rename.
var storeRenameFn = os.Rename

// applyLayout performs the whole shape change in one pass: the store moves to
// `<project>/.nibs` if it is elsewhere, every pre-layout nib file moves into
// data/ under its ORIGINAL basename (ids derive from filenames, so a migration
// moves directories and never renames files), and the project config moves from
// beside the store to inside it.
func applyLayout(env *migrateEnv, _ *nibcore.StoreLock, log logf) error {
	plan, err := planLayout(*env)
	if err != nil {
		return err
	}
	if err := confirmAssumedNibs(plan.assumed); err != nil {
		return err
	}
	return plan.apply(env, log)
}

// configRelocation is the planned move of the pre-layout `.nibs.yml` into the
// store as config.yml: the bytes to write, the mode to write them with, and
// whether an interrupted earlier run already wrote them.
type configRelocation struct {
	legacy string
	dest   string
	// body is what dest must end up holding. Empty when resume is set: the file
	// is already correct and only the legacy copy has to go.
	body []byte
	// perm is the LEGACY file's mode. A move must not widen permissions, and
	// hardcoding 0644 turned a 0600 config into a world-readable one.
	perm os.FileMode
	// resume marks a dest already byte-identical to body — this step's own
	// interrupted write, which only needs the legacy file removed.
	resume bool
	// note is logged after the apply succeeds, when the plan did something the
	// user should know about beyond the move itself.
	note string
}

// planConfigRelocation prepares moving the pre-layout `.nibs.yml` into the store
// as config.yml, dropping the retired `nibs.path` key on the way. The key is not
// cosmetic debris: every config `nibs init` used to write carries it, and a
// config still carrying it is a hard load error — so relocating the file
// verbatim would leave a store that refuses to open.
//
// The rewrite goes through a YAML node tree rather than the Config struct so
// that everything else in the file survives: comments, key order, and any key
// this build does not know about.
//
// The write and the removal are two operations, so a crash between them leaves
// BOTH files on disk. That state is terminal if it is simply refused — every
// command refuses a store with a pending layout step, `nibs migrate` included —
// which would break the engine's crash-recovery contract (see runMigrations).
// So a config.yml BYTE-IDENTICAL to what this step would write is recognized as
// its own interrupted write and finished. Anything else is a genuine second
// config the tool must not choose between, and still refuses: the two differ in
// the load-bearing fields, and picking the wrong one silently re-prefixes the
// project.
//
// The destination is inspected FIRST, and with Lstat:
//
//   - first, so a genuine two-config conflict is reported as one rather than
//     hiding behind an unrelated `nibs.path` refusal from the rewrite;
//   - Lstat, because os.Stat follows symlinks and a config.yml symlinked AT the
//     legacy file read back byte-identical, so the resume branch deleted the one
//     real config and left a broken link behind, reporting success.
func planConfigRelocation(env migrateEnv, finalRoot string) (*configRelocation, error) {
	legacy := env.legacyConfigPath()
	dest := store.NewLayout(finalRoot).ConfigPath()

	info, err := os.Stat(legacy)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", legacy, err)
	}
	data, err := config.ReadConfigFile(legacy)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", legacy, err)
	}
	cr := &configRelocation{legacy: legacy, dest: dest, perm: info.Mode().Perm()}

	destInfo, destErr := os.Lstat(dest)
	if destErr == nil && !destInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("%s exists but is not a regular file (mode %s); refusing to treat it as an interrupted relocation of %s — remove or replace it, then re-run `nibs migrate`",
			dest, destInfo.Mode(), legacy)
	}

	rewritten, note, stripErr := stripRetiredNibsPath(data, env, legacy)
	if destErr == nil {
		existing, readErr := config.ReadConfigFile(dest)
		if readErr == nil && stripErr == nil && bytes.Equal(existing, rewritten) {
			cr.resume = true
			cr.note = note
			return cr, nil
		}
		return nil, twoConfigsError(legacy, dest, existing, readErr, stripErr)
	}
	if stripErr != nil {
		return nil, stripErr
	}
	cr.body = rewritten
	cr.note = note
	return cr, nil
}

// twoConfigsError explains a `<store>/config.yml` that is not this step's own
// interrupted write.
//
// "Keep one and delete the other" is the right advice for two GENUINE configs,
// and the wrong advice when the destination is not a whole config: a user who
// keeps the one in the new canonical location then deletes the only complete
// copy, and the next command fails on a YAML unmarshal error with nothing left
// to recover from. So a destination that cannot be read, or does not parse as
// YAML at all, is named as the broken one instead.
func twoConfigsError(legacy, dest string, existing []byte, readErr, stripErr error) error {
	if readErr != nil {
		return fmt.Errorf("%s exists but cannot be read (%v), so it cannot be compared with %s; repair or remove it — %s is intact — then re-run `nibs migrate`",
			dest, readErr, legacy, legacy)
	}
	if !parseableYAML(existing) {
		return fmt.Errorf("%s exists but is not valid YAML, so it is not a config this project can keep (a truncated write, or a hand edit); repair or remove it — %s is intact — then re-run `nibs migrate`",
			dest, legacy)
	}
	msg := fmt.Sprintf("both %s and %s exist; keep the one you want and delete the other, then re-run `nibs migrate`", legacy, dest)
	if stripErr != nil {
		msg += fmt.Sprintf("\nand note that %s cannot be relocated as it stands: %v", legacy, stripErr)
	}
	return errors.New(msg)
}

// parseableYAML reports whether data is YAML at all — the test that separates a
// second, genuine config from a torn or hand-mangled file.
func parseableYAML(data []byte) bool {
	var probe yaml.Node
	return yaml.Unmarshal(data, &probe) == nil
}

// apply writes the config into the store and removes the legacy copy.
//
// The write is ATOMIC (temp file plus rename), which is what makes the
// byte-identity resume above a two-case decision rather than three: config.yml
// is only ever absent or complete, so an interrupted run cannot leave a partial
// file that reads as "a genuine second config". The rename also REPLACES a
// symlink at the destination instead of following it — os.WriteFile through a
// dangling link created the file wherever the link pointed, outside the store,
// and then deleted the legacy config and reported success.
func (c *configRelocation) apply(log logf) error {
	if !c.resume {
		if err := os.MkdirAll(filepath.Dir(c.dest), 0755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(c.dest), err)
		}
		if err := fsutil.AtomicWriteFile(c.dest, c.body, c.perm); err != nil {
			return fmt.Errorf("writing %s: %w", c.dest, err)
		}
	}
	if err := os.Remove(c.legacy); err != nil {
		return fmt.Errorf("removing %s after relocating it: %w", c.legacy, err)
	}
	if c.resume {
		log("layout: finished an interrupted relocation of the project config to %s", c.dest)
	} else {
		log("layout: relocated the project config to %s", c.dest)
	}
	if c.note != "" {
		log("layout: %s", c.note)
	}
	return nil
}

// stripRetiredNibsPath removes the `nibs.path` key from a config's bytes, leaving
// the rest of the document untouched, and returns a note when dropping the key is
// worth telling the user about.
//
// configFile is the path of the file those bytes came from, and it is a parameter
// rather than derived from env because the key lives in TWO places: the
// pre-layout `.nibs.yml` beside the store, and — for a project whose config was
// hand-moved into the store — `<store>/config.yml`. A refusal has to name the file
// the reader must actually edit, and the in-store caller runs precisely when
// `.nibs.yml` is absent, so citing that file there names nothing.
//
// A `path` naming somewhere OTHER than the store being migrated is refused
// rather than silently dropped — while the directory it names still EXISTS. The
// key is refused because discarding the only record of where a project's nibs
// live would strand them; once that directory is gone there is nothing left to
// strand, and the key is a stale record. Dropping it there is also what makes a
// run interrupted between the store relocation and the config write converge:
// the store is at `.nibs` by then, while the key still names the directory it
// came from.
//
// The `nibs migrate --nibs-path <declared>` remedy is offered only where the
// store-evidence guard ACCEPTS that directory, the same gating noStoreFoundError
// applies: a directory the guard refuses would send the reader to a command the
// tool then rejects, and the remove-the-key remedy converges for every shape
// regardless.
//
// "Gone" means ONLY a definite fs.ErrNotExist. os.Stat fails for far more than
// that — EACCES on any ancestor, an unmounted mount point, an unreachable network
// path, ELOOP — and storing the nibs on another volume is precisely why
// `nibs.path` existed, so reading any of those as "no longer exists" drops the one
// record of where they are. Every other stat failure is "cannot determine" and
// refuses, naming the error, the same posture twoConfigsError takes for an
// unreadable destination.
func stripRetiredNibsPath(data []byte, env migrateEnv, configFile string) (out []byte, note string, err error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		// The yaml error quotes the file's own content, so it crosses the boundary
		// like every other file-sourced reason (see describeScanProblems).
		return nil, "", fmt.Errorf("parsing %s: %s", configFile, flattenReason(err.Error()))
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return data, "", nil // empty document: nothing to strip
	}
	root := doc.Content[0]
	nibs := mappingValue(root, "nibs")
	if nibs == nil {
		return data, "", nil
	}
	pathNode := mappingValue(nibs, "path")
	if pathNode == nil {
		return data, "", nil
	}

	declared := pathNode.Value
	resolved := declared
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(env.layout().ProjectDir(), resolved)
	}
	if !sameDir(resolved, env.nibsRoot) {
		info, statErr := retiredPathStatFn(resolved)
		switch {
		case statErr == nil:
			// ONE DIRECTORY UNDER TWO SPELLINGS, which sameDir cannot see: it
			// compares bytes, so `.NIBS` on a case-folding volume and a symlink
			// beside the store both read as somewhere else. Both remedies below
			// then address it as somewhere else — one prescribes migrating the
			// store already being migrated, the other a move from a directory into
			// itself — and a reader cannot tell without resolving the two paths by
			// hand.
			if namesTheSameDirectory(info, env.nibsRoot) {
				return nil, "", fmt.Errorf("%s sets `nibs.path: %q`, which is the store being migrated (%s) under a different spelling — the comparison is textual, so a symlink or a differently cased name reads as somewhere else; remove the retired `nibs.path` key from %s and re-run",
					configFile, sanitizeFileText(declared), env.nibsRoot, configFile)
			}
			if relocatable, shapeErr := hasLegacyStoreShape(resolved); shapeErr == nil && relocatable {
				return nil, "", fmt.Errorf("%s sets `nibs.path: %q`, which is not the store being migrated (%s) and still exists; migrate that store instead (`nibs migrate --nibs-path %s`), or — if %s is the store you want to keep — remove the retired `nibs.path` key from %s and re-run",
					configFile, sanitizeFileText(declared), env.nibsRoot,
					shellArg(resolved), env.nibsRoot, configFile)
			}
			return nil, "", fmt.Errorf("%s sets `nibs.path: %q`, which is not the store being migrated (%s) and still exists; `nibs migrate` will not relocate that directory for you, so remove the retired `nibs.path` key from %s and re-run, which keeps %s as this project's store — and if the nibs you want are the ones in %s, move them into %s first",
				configFile, sanitizeFileText(declared), env.nibsRoot,
				configFile, env.nibsRoot, sanitizeFilePath(resolved), env.nibsRoot)
		case !errors.Is(statErr, fs.ErrNotExist):
			// flattenReason, not %v: a stat error embeds the path it failed on, and
			// that path is the declared value joined onto the project directory —
			// so interpolating the error raw carries file content into the message
			// around the boundary sanitizeFileText applies one argument earlier.
			return nil, "", fmt.Errorf("%s sets `nibs.path: %q`, and whether that directory still holds this project's nibs cannot be determined (%s); resolve that (mount the volume, fix its permissions), or — if %s is the store you want to keep — remove the retired `nibs.path` key from %s, then re-run `nibs migrate`",
				configFile, sanitizeFileText(declared), flattenReason(statErr.Error()),
				env.nibsRoot, configFile)
		default:
			note = fmt.Sprintf("dropped the retired `nibs.path: %q` from the config — that directory no longer exists, so the key was a stale record",
				sanitizeFileText(declared))
		}
	}

	deleteMappingKey(nibs, "path")
	rewritten, err := yaml.Marshal(root)
	if err != nil {
		return nil, "", fmt.Errorf("rewriting %s without the retired `nibs.path` key: %w", configFile, err)
	}
	return rewritten, note, nil
}

// namesTheSameDirectory reports whether info — the stat of the directory a
// retired `nibs.path` names — and path are ONE directory on disk.
//
// MESSAGE PATH ONLY, and that restriction is what makes it safe rather than a
// second route to a hazard already refused elsewhere. os.Stat FOLLOWS symlinks,
// so os.SameFile calls two genuinely different LOCATIONS equal whenever one leads
// to the other. That is exactly right for choosing a sentence — the reader is
// looking at one directory under two names — and would be fatal for choosing an
// action, because a config could then authorize migrate against a store it did
// not name.
//
// sameDir still governs the branch, and nothing downstream reads this answer: a
// config carrying the retired key is refused either way, and only the remedy
// printed changes. Do not widen it into the condition — teaching the comparison
// itself to fold case or resolve links was declined deliberately, because
// sameDir is also isRealImmediateChild's containment test, the guard that closed
// a data-loss defect. planStoreRelocation uses it on the same terms.
//
// The second stat is a DIRECT os.Stat rather than the retiredPathStatFn seam.
// That seam exists to inject a stat FAILURE for the declared directory; the store
// being migrated exists by definition, and a seam here would let a synthetic
// FileInfo choose a sentence. os.SameFile's type assertion fails on one anyway,
// so an override falls back to the textual wording rather than misfiring.
//
// [Unverified] On Windows os.SameFile compares volume serial plus file index,
// which some filesystems and network redirectors do not guarantee unique. A false
// positive costs the "migrate that store instead" pointer; the remove-the-key
// remedy the message keeps converges regardless.
func namesTheSameDirectory(info fs.FileInfo, path string) bool {
	other, err := os.Stat(path)
	return err == nil && os.SameFile(info, other)
}

// retiredPathStatFn asks whether the directory the retired `nibs.path` key names
// is there. It is a seam because the three answers it drives are not all reachable
// on every platform: "definitely gone" and "definitely there" are trivial, but
// "cannot determine" needs a stat that fails with something other than ENOENT, and
// the portable ways to produce one — a symlink loop, an unreadable ancestor — are
// exactly what a Windows CI leg cannot do. The branch that keeps a project's only
// record of where its nibs live must be verified everywhere it ships.
//
// One seam per git/filesystem question, like storeGitStateFn and gitIsDirtyFn.
// Production always uses os.Stat.
var retiredPathStatFn = os.Stat

// mappingValue returns the value node for key in a YAML mapping, or nil.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// deleteMappingKey removes a key/value pair from a YAML mapping.
func deleteMappingKey(node *yaml.Node, key string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
	}
}

// sameDir reports whether two paths name the same directory, comparing their
// cleaned absolute forms.
//
// The comparison is BYTEWISE, and on a case-insensitive volume that makes it
// stricter than the filesystem. Measured on Windows:
//
//	os.SameFile(<dir>/NibData, <dir>/nibdata) = true
//	sameDir(<dir>/NibData, <dir>/nibdata)     = false
//	sameDir(c:\p\d, C:\p\d)                   = false   (Clean does not case-fold
//	                                                     the drive letter either)
//
// That direction is deliberate and safe: every caller uses this to decide whether
// a declared `nibs.path` names the directory in hand, and the false answer refuses
// — routing the user into the converging manual remedy rather than authorizing
// `nibs migrate` to move and rewrite a tree on a case-variant match. Discovery is
// immune regardless, because dataDir is derived FROM declared, so the two sides
// are the same string.
//
// The cost is a usability one, and it is real: a case-variant or symlinked
// `nibs.path` is refused on a volume that reaches one directory through either
// spelling. What a refusal must not do is turn a false answer here into a claim
// this comparison cannot establish — neither "no `.nibs.yml` beside it names it"
// (false when one names this very directory in another spelling) nor "it names a
// different DIRECTORY" (the same claim wearing a conclusion, and false for the
// same fixtures). A refusal built on this answer reports what was COMPARED: the
// declared value where it has one, and that the match is textual, so another name
// for the same directory lands there too (see resolveStoreDir). Do not close the
// gap by case-folding here: that would make an authorization decision on a
// filesystem-dependent guess.
func sameDir(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(absA) == filepath.Clean(absB)
}

// loadStoreForMigration loads a scoped throwaway Core for a content step's
// apply. Content steps ride the existing load pipeline (canonicalization
// included) rather than growing a second parser.
//
// FAIL-LOUD GATE: a store that did not load cleanly — an unparseable file, a
// duplicate id — is refused before any step writes anything. Migrating around
// a skipped file silently drops edges to it (a v0 `blocking:` transfer to a
// skipped target erases the edge from the source while the target never
// receives it), and a duplicate id means the loaded store is not a faithful
// picture of the directory. The refusal names every offending file and points
// at `nibs check`; the deferral concept the old load-time migration carried is
// deliberately gone.
func loadStoreForMigration(env migrateEnv) (*nibcore.Core, error) {
	cfg, err := env.config()
	if err != nil {
		return nil, fmt.Errorf("loading config for migration: %w", err)
	}
	core := nibcore.New(env.nibsRoot, cfg)
	if err := core.Load(); err != nil {
		return nil, fmt.Errorf("loading store for migration: %w", err)
	}
	unparseable, duplicates := core.LoadDiagnostics()
	if len(unparseable) > 0 || len(duplicates) > 0 {
		var problems []string
		// Filenames and parse errors quoting them, so both halves cross the
		// rendering boundary here as well as at the error boundary that prints
		// them (see stripControlChars).
		for _, uf := range unparseable {
			problems = append(problems, fmt.Sprintf("unparseable nib file %s: %s", stripControlChars(uf.Path), stripControlChars(uf.Reason)))
		}
		for _, d := range duplicates {
			problems = append(problems, fmt.Sprintf("duplicate id %q: %s shadows %s", d.NibID, stripControlChars(d.Loaded), stripControlChars(d.Shadowed)))
		}
		return nil, fmt.Errorf("refusing to migrate a store that does not load cleanly (repair the files below by hand, `nibs check` reports them too, then re-run `nibs migrate`):\n  %s",
			echoedList(problems, echoedListRemedyCheck))
	}
	return core, nil
}

// storeScan is the outcome of one shared pass over the store's files: how many
// files each chain step still has pending (indexed parallel to
// migrationSteps), the files the scan could not classify as nibs, and the
// files written by a newer nibs (collected so the eventual refusal can carry
// everything the walk saw — see scanStore).
type storeScan struct {
	// foreignContent holds files already under data/ or archive/ that
	// layoutVerdict would not have moved there. On a MIGRATED store that is an
	// ordinary diagnostic; on a pre-layout one it is evidence the directory is
	// not ours (see gateContentDirsAreOurs).
	foreignContent []string
	counts         []int
	problems       []scanProblem
	// newer describes each file whose format version exceeds
	// nib.CurrentVersion ("path (format version N)").
	newer []string
}

// scanProblem names a store file the scan skipped instead of classifying: an
// unreadable file (permission denied, a dangling editor-lock symlink) or a
// fence-less .md that is not a nib shape at all. Skipping mirrors Core.Load's
// per-file log-and-skip degradation — one broken file must not hard-fail
// every command — while keeping the file's name for the surfaces that must
// report it (`nibs migrate` warns and refuses to apply around it; both kinds
// additionally land in Load's diagnostics — nib.Parse refuses fence-less
// content — which `nibs check` renders).
type scanProblem struct {
	path   string // store-relative, forward slashes (storeRelPath)
	reason string
	// unreadable distinguishes the two kinds, which the layout step treats
	// differently: a file whose header could not be READ cannot be proven not
	// to be a nib, so it MOVES into data/ with the rest, while a fence-less
	// .md is provably not a nib and stays where it is. That difference decides
	// whether the file can still endanger a content step — see
	// blockingScanProblems.
	unreadable bool
}

// pending returns the steps whose predicate matched at least one file, in
// chain order.
func (s *storeScan) pending() []migrationStep {
	var pending []migrationStep
	for i, step := range migrationSteps {
		if s.counts[i] > 0 {
			pending = append(pending, step)
		}
	}
	return pending
}

// newerStoreError marks the newer-store refusal so migrate's RunE can map it
// to a VALIDATION exit (see migrateCmdError) while the same error surfacing
// through the shared PersistentPreRunE gate stays uncoded, matching that
// gate's other refusals (deliberately out of scope for coding).
//
// It holds the two lists rather than a rendered sentence because the refusal has
// two readers with opposite needs: the pre-run gate prints it on EVERY command,
// where one line per offending file is the whole problem, while `nibs check` —
// the command that bounded rendering points at — has to be able to print the
// list in full. Rendering once at construction would have to pick one of them.
type newerStoreError struct {
	// newer describes each file whose format version exceeds nib.CurrentVersion
	// ("path (format version N)"), and problems the files the same walk could not
	// classify at all. Both are COMPLETE; bounding happens at rendering.
	newer    []string
	problems []scanProblem
}

// Error is the bounded rendering, for the surfaces that print a refusal.
func (e *newerStoreError) Error() string {
	return e.render(func(entries []string) string { return echoedList(entries, echoedListRemedyCheck) })
}

// full is the same refusal with nothing elided, for `nibs check` — the
// full-list channel Error's elision tail sends the reader to.
func (e *newerStoreError) full() string {
	return e.render(func(entries []string) string { return strings.Join(entries, "\n  ") })
}

func (e *newerStoreError) render(list func([]string) string) string {
	msg := fmt.Sprintf("%d file(s) in this store were written by a newer nibs, or carry a format version this build does not support (it supports up to version %d); "+
		"upgrade nibs, or move any file listed here that is not a nib out of the store:\n  %s",
		len(e.newer), nib.CurrentVersion, list(e.newer))
	if len(e.problems) > 0 {
		msg += fmt.Sprintf("\nthe scan also skipped %d file(s) that cannot be read as nibs:\n  %s",
			len(e.problems), list(scanProblemLines(e.problems)))
	}
	return msg
}

// scanStore reads each file's front-matter header ONCE and evaluates the
// newer-store refusal plus every CONTENT step's predicate against it — one
// header walk regardless of how many content steps the chain grows. This probe
// runs on every command, so its cost must stay O(files), not O(files × steps).
//
// SHAPE steps are outside that budget: each answers a directory-structure
// question no per-file header can express, so each performs its own pass — today
// one, layoutPendingCount's. Its cost depends on the store's shape rather than
// being free: on an ALREADY-migrated store the directory-prefix check answers
// every file without opening it, but on a pre-layout store layoutMovableFiles
// reads each out-of-place file's header to tell a nib from a fence-less document.
// That is the O(files) case, once, on the stores the step exists for. A chain that
// grew several shape steps would owe this note a rethink.
//
// A file with a version above nib.CurrentVersion refuses the whole scan (error):
// it was written by a newer nibs and this build must not touch the store. Which
// files that question is ASKED of is newerVersionSpeaksForStore's, and it is
// deliberately not "every fenced file" — `version:` is ordinary front matter
// elsewhere, and one documentation page used to lock a store out of every
// command including the migrate that would fix it. The
// refusal is raised AFTER the walk completes rather than aborting at the first
// such file, so it can name every newer file and every other problem the walk
// collected — aborting mid-walk used to hide a coexisting broken file behind the
// version refusal until the user repaired their way to it.
func scanStore(env migrateEnv) (*storeScan, error) {
	scan := &storeScan{counts: make([]int, len(migrationSteps))}

	// Shape steps ask about the store's directory structure, which no per-file
	// header can answer, so they are evaluated once each — outside the walk.
	for i, step := range migrationSteps {
		if step.shape == nil {
			continue
		}
		n, err := step.shape(env)
		if err != nil {
			return nil, err
		}
		scan.counts[i] = n
	}

	err := forEachNibFile(env, func(path string, skipped error) error {
		if skipped != nil {
			// The same family an unreadable file lands in, and reported the same
			// way: migrate names every file its scan passed over, so an entry it
			// could not even open must not be the one that goes unmentioned.
			// unreadable: FALSE, and the distinction is what keeps this a
			// diagnostic rather than a lockout. `unreadable` means "the scan
			// cannot prove this is not a nib", which is why such a file blocks a
			// content step — the layout step moves it into data/ and the step
			// meets it there. For an irregular file the scan CAN prove it, and
			// layoutMovableFiles leaves it where it is, so it is never migrated
			// around and blocks nothing. Marked unreadable, a FIFO at a pre-layout
			// store's root wedged the project: every command said "run
			// `nibs migrate`" and migrate refused to migrate around it.
			//
			// The bare sentinel, not the wrapped error: the walk prefixes it with
			// the absolute path, and scanProblem already carries the path in the
			// spelling every nibs surface uses.
			reason := skipped.Error()
			if errors.Is(skipped, nibcore.ErrNotRegularFile) {
				reason = nibcore.ErrNotRegularFile.Error()
			}
			scan.problems = append(scan.problems, scanProblem{path: storeRelPath(env, path), reason: reason})
			return nil
		}
		h, err := readFrontMatterHeader(path)
		if err != nil {
			// Per-file degradation, matching Core.Load: skip with the file's
			// name kept, never abort the probe for every command.
			scan.problems = append(scan.problems, scanProblem{path: storeRelPath(env, path), reason: err.Error(), unreadable: true})
			return nil
		}
		if !h.hasFrontMatter {
			scan.problems = append(scan.problems, scanProblem{path: storeRelPath(env, path), reason: "no front matter — not a nib file"})
			return nil
		}
		rel := storeRelPath(env, path)
		l := env.layout()
		if h.version > nib.CurrentVersion && newerVersionSpeaksForStore(l, rel, h) {
			// Not this build's business: no step predicate is evaluated for a
			// newer file, only the refusal below.
			scan.newer = append(scan.newer, fmt.Sprintf("%s (format version %d)", stripControlChars(rel), h.version))
			return nil
		}
		if l.IsDataRel(rel) || l.IsArchivedRel(rel) {
			if layoutVerdict(contentDirRel(l, rel), h) == notANib {
				scan.foreignContent = append(scan.foreignContent, rel)
			}
		}
		if !willBeStoreContent(l, rel, h) {
			return nil
		}
		for i, step := range migrationSteps {
			if step.isContent() && step.pred(h) {
				scan.counts[i]++
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(scan.newer) > 0 {
		return nil, &newerStoreError{newer: scan.newer, problems: scan.problems}
	}
	return scan, nil
}

// pendingMigrations returns the steps whose detection fires for this store, in
// chain order. It is shared VERBATIM by the PersistentPreRunE refusal and the
// migrate command, so the two can never disagree about what is pending. A
// store carrying any file with a version above nib.CurrentVersion is refused
// outright (error) — see scanStore.
//
// Scan problems (unreadable or fence-less files) are deliberately NOT errors
// at this surface: commands proceed, and the files surface where diagnosis
// belongs — Core.Load skips-and-diagnoses an unreadable file for `nibs check`
// to name, and the migrate command itself reports every skipped file before
// deciding anything (see its RunE and runMigrations).
func pendingMigrations(env migrateEnv) ([]migrationStep, error) {
	scan, err := scanStore(env)
	if err != nil {
		return nil, err
	}
	return scan.pending(), nil
}

// runMigrations applies every pending step in chain order, holding the store
// write lock for the WHOLE run so no serve process or concurrent CLI mutates
// the store mid-migration. Pending steps are re-probed under the lock — the
// caller's earlier probe raced other processes — and detect-gates-apply is the
// idempotency contract: a step whose detect no longer fires (already applied,
// possibly by a crashed earlier run) is simply skipped, so re-running after
// any abort resumes exactly where the store's files say it should. The first
// apply error aborts the run.
//
// POST-CONDITION: after the applies, every applied step's detection is re-run
// and must no longer fire. Detection (a streamed header scan) and apply (the
// real YAML parse) are deliberately different grammars; if they disagree on a
// file — a scanner bug, a YAML construct the scan misreads — an unverified
// success here would wedge the CLI permanently: every command refuses on the
// still-firing detection while migrate keeps reporting the store migrated.
// Failing loud with the disagreeing files' names turns that loop into a
// reportable bug, and stays a generic engine guarantee — future steps get it
// without writing anything.
func runMigrations(env migrateEnv, log logf) error {
	// Lock the root the run will FINISH at. The layout step may relocate the
	// store (see planStoreRelocation), and a StoreLock token is bound to the
	// root it was acquired for — a token taken at the old root is refused by the
	// content steps' Core at the new one. Locking the destination is also the
	// right exclusion: nothing may operate on the pre-layout root, because every
	// command refuses a store with a pending layout step.
	// Before the store lock and before every gate: a serve holding nibs in memory
	// must be gone before the shape changes, and refusing early keeps a run that
	// cannot proceed from taking the store's write lock at all.
	fence, err := nibcore.AcquireServeExclusion(env.nibsRoot)
	if err != nil {
		if errors.Is(err, nibcore.ErrStoreServed) {
			return errors.New(liveServeRefusal)
		}
		return fmt.Errorf("checking whether a `nibs serve` is running: %w", err)
	}
	defer func() { _ = fence.Release() }()
	env.serveExcluded = true

	lockRoot := env.nibsRoot
	target, err := planStoreRelocation(env)
	if err != nil {
		return err
	}
	if target != "" {
		lockRoot = target
	}
	lock, err := nibcore.AcquireStoreLock(lockRoot)
	if err != nil {
		return fmt.Errorf("acquiring store lock: %w", err)
	}
	defer func() { _ = lock.Release() }()

	scan, err := scanStore(env)
	if err != nil {
		return err
	}
	pending := scan.pending()

	// Every precondition, re-evaluated under the lock: the caller's earlier
	// evaluation raced other processes. wouldRefuse is the ONE place the set of
	// gates lives, shared with --dry-run's preview, so the preview cannot
	// predict an outcome this run does not produce.
	if refusals, _ := wouldRefuse(env, scan); len(refusals) > 0 {
		return &refusalError{refusals: refusals}
	}

	// Record that this run started before it changes anything, so an interrupted
	// one is recognizable as ours afterwards — the question gateContentDirsAreOurs
	// cannot answer from a directory's contents. A run with nothing pending falls
	// through to the clear below, which is what stops a crash in the instant
	// before removal from leaving the store looking mid-migration forever.
	if len(pending) > 0 {
		if err := writeMigrationMarker(env, pending); err != nil {
			return fmt.Errorf("recording that the migration started: %w", err)
		}
	}

	for _, step := range pending {
		log("applying %s: %s", step.name, step.title)
		if err := step.apply(&env, lock, log); err != nil {
			return fmt.Errorf("migration step %s failed: %w", step.name, err)
		}
	}
	for _, step := range pending {
		stuck, err := stillPending(env, step)
		if err != nil {
			return fmt.Errorf("verifying migration %s: %w", step.name, err)
		}
		if len(stuck) > 0 {
			// No remedy on the elision: this list is the disagreement itself, so
			// nothing else reports it. Twenty names are a bug report, and the store
			// is unchanged — re-running migrate reproduces the whole set.
			return fmt.Errorf("migration %s applied but its detection still fires — the header scan disagrees with the parsed store for:\n  %s\nplease repair these files by hand (or report a nibs bug); until then commands will keep refusing",
				step.name, echoedList(stuck, ""))
		}
	}
	// The store finished at env.nibsRoot's CURRENT value: the layout step
	// repoints env when it relocates the store, so the marker is cleared where it
	// now lives rather than where the run started.
	if err := clearMigrationMarker(env); err != nil {
		return fmt.Errorf("clearing %s after a completed migration: %w", migrationMarkerName, err)
	}
	return nil
}

// anyContentPending reports whether any pending step rewrites file content (as
// opposed to only moving files around).
func anyContentPending(pending []migrationStep) bool {
	for _, step := range pending {
		if step.isContent() {
			return true
		}
	}
	return false
}

// loadPendingInvalidated reports whether any pending step still has to move the
// files Core.Load reads. It asks the DECLARED question (migrationStep.
// invalidatesLoad) rather than inferring it from "the step has no per-file
// predicate", which happened to coincide with it while the layout step was the
// only shape step.
func loadPendingInvalidated(pending []migrationStep) bool {
	for _, step := range pending {
		if step.invalidatesLoad {
			return true
		}
	}
	return false
}

// pendingPlans returns the pending steps that can compute their whole change up
// front, in chain order.
func pendingPlans(pending []migrationStep) []migrationStep {
	var planned []migrationStep
	for _, step := range pending {
		if step.plan != nil {
			planned = append(planned, step)
		}
	}
	return planned
}

// refusal is one precondition `nibs migrate` applies to a store it is about to
// change, and did not meet.
type refusal struct {
	// gate names the precondition it came from — the handle the completeness
	// test uses, and what --dry-run labels each predicted refusal with.
	gate string
	// code is the CLI error code this refusal exits with (see migrateCmdError).
	code   string
	reason string
}

// refusalError carries every precondition that failed, so a run and its preview
// report the same set rather than the first thing each happened to notice.
type refusalError struct{ refusals []refusal }

func (e *refusalError) Error() string {
	reasons := make([]string, len(e.refusals))
	for i, r := range e.refusals {
		reasons[i] = r.reason
	}
	return strings.Join(reasons, "\n")
}

// code is the exit code the FIRST failing gate carries. migrateGates is ordered,
// so this is deterministic rather than a coin toss between two codes.
func (e *refusalError) code() string {
	if len(e.refusals) == 0 {
		return output.ErrFileError
	}
	return e.refusals[0].code
}

// gateResult is one gate's answer, and it has THREE states rather than two. A
// bare *refusal made nil mean both "precondition met" and "cannot be answered
// yet", and the overload is what let --dry-run drop the note explaining the one
// gate it cannot preview as soon as any other gate fired — exactly the case where
// the user acts on the preview and re-runs.
//
// The three states live in the CONSTRUCTORS, not in the type: the two fields are
// independent, so gateResult{refusal: r, undecidable: why} is representable and
// wouldRefuse would silently drop the note (it tests refusal first), and
// gateUndecidable("") is byte-identical to gateMet(). Build one with gateMet,
// gateRefused or gateUndecidable and the states stay disjoint; a struct literal in
// a gate body is the way to break that.
type gateResult struct {
	// refusal is set when the precondition is not met.
	refusal *refusal
	// undecidable is set, to the sentence explaining why, when the gate cannot be
	// answered before the run performs earlier steps. Never set together with
	// refusal.
	undecidable string
}

// gateMet, gateRefused and gateUndecidable are the three answers a gate may give,
// named for the three answers themselves so a gate body never has to encode one.
func gateMet() gateResult { return gateResult{} }

func gateRefused(code, reason string) gateResult {
	return gateResult{refusal: &refusal{code: code, reason: reason}}
}

func gateUndecidable(why string) gateResult { return gateResult{undecidable: why} }

// migrateGate is one precondition, evaluated identically by the real run and by
// --dry-run's preview.
type migrateGate struct {
	name  string
	check func(migrateEnv, *storeScan) gateResult
}

// migrateGates is EVERY precondition `nibs migrate` applies before it changes a
// store, in the order they are reported.
//
// The list is the mechanism, not a convenience. Sharing a predicate PER GATE
// still left the completeness of the SET unenforced, which is how the preview
// came to print step counts with no warning for a store the real run refused
// outright: `blockingScanProblems` was shared, and the load gate and the
// dirty-git refusal simply had no preview at all. A gate added here reaches both
// surfaces at once, and TestMigrateDryRunPreviewsEveryRefusalGate walks this list
// and requires each entry to have a fixture that trips it on both.
var migrateGates = []migrateGate{
	{name: "dirty-store", check: gateStoreGitClean},
	{name: "dirty-legacy-config", check: gateLegacyConfigRecoverable},
	{name: "live-serve", check: gateNoLiveServe},
	{name: "foreign-content-dir", check: gateContentDirsAreOurs},
	{name: "step-plan", check: gatePendingPlans},
	{name: "unclassifiable-content", check: gateContentClassifiable},
	{name: "store-loads-cleanly", check: gateStoreLoadsCleanly},
}

// wouldRefuse runs every gate in migrateGates and returns the ones that failed.
// A store with nothing pending is never refused: the command reports it up to
// date before any gate is consulted.
func wouldRefuse(env migrateEnv, scan *storeScan) (refusals []refusal, undecidable []string) {
	if len(scan.pending()) == 0 {
		return nil, nil
	}
	for _, g := range migrateGates {
		res := g.check(env, scan)
		switch {
		case res.refusal != nil:
			r := *res.refusal
			r.gate = g.name
			refusals = append(refusals, r)
		case res.undecidable != "":
			undecidable = append(undecidable, res.undecidable)
		}
	}
	return refusals, undecidable
}

// gateStoreGitClean refuses a store repository with uncommitted changes: git is
// the plan review and the rollback, so the pre-migration state must be
// recoverable, and an uncommitted change would be entangled with the migration's
// rewrite in the diff. isRepo is "a repository genuinely covers the store", not
// merely "inside a repo": a store gitignored by an enclosing repository has no
// rollback net (see realStoreGitState) and gets the backup suggestion instead. A
// git failure means the net cannot be evaluated, which is narrated rather than
// refused (see reportGitPosture) — blocking migration on git's availability
// would make it unusable without git.
func gateStoreGitClean(env migrateEnv, _ *storeScan) gateResult {
	if migrateAllowDirty {
		return gateMet()
	}
	dirty, isRepo, err := env.storeGitState()
	if err != nil || !isRepo || !dirty {
		return gateMet()
	}
	return gateRefused(output.ErrValidation,
		fmt.Sprintf("the store at %s has uncommitted git changes; commit or stash them so the pre-migration state is recoverable, or re-run with --allow-dirty",
			stripControlChars(env.nibsRoot)))
}

// gateLegacyConfigRecoverable applies the same net to the ONE path the layout
// step touches outside the store: the project's `.nibs.yml`, which it deletes.
// That deletion lands in the PROJECT's repository, frequently a different
// repository from the store's, so the store's clean bill of health says nothing
// about whether it can be undone.
//
// "Not a repo" is not a refusal here, matching the store's own posture: the store
// check has already suggested a backup for an unprotected project, and refusing
// twice for one unprotected tree would make migrate unusable outside git.
func gateLegacyConfigRecoverable(env migrateEnv, _ *storeScan) gateResult {
	if migrateAllowDirty || !legacyConfigExists(env) {
		return gateMet()
	}
	dirty, err := env.legacyConfigDirty()
	if err != nil || !dirty {
		return gateMet()
	}
	return gateRefused(output.ErrValidation,
		fmt.Sprintf("%s has uncommitted git changes and this migration deletes it; commit or stash it so the pre-migration state is recoverable, or re-run with --allow-dirty",
			stripControlChars(env.legacyConfigPath())))
}

// gatePendingPlans surfaces every refusal a pending step's PLANNING raises — for
// the layout step: an occupied relocation destination, a name collision in data/,
// two project configs, a `nibs.path` naming a store that still exists or one that
// cannot be stat'd, a store that is a linked git worktree. Sharing the step's own
// plan is what lets the preview name them; the step computes the same plan when it
// runs.
//
// It asks which pending steps HAVE a plan rather than inferring it from the
// detector kind, so a future step that can pre-compute its change is previewed by
// declaring plan and nothing else.
func gatePendingPlans(env migrateEnv, scan *storeScan) gateResult {
	for _, step := range pendingPlans(scan.pending()) {
		if err := step.plan(env); err != nil {
			return gateRefused(output.ErrFileError, err.Error())
		}
	}
	return gateMet()
}

// gateContentClassifiable is the fail-loud gate over files the scan could not
// classify. Applying a CONTENT step around one is destructive: an unreadable file
// silently loses the edges pointing at it (a v0 `blocking:` transfer to a skipped
// target erases the edge from the source while the target never receives it). A
// fence-less document once LOADED as an empty v0 "nib" the v0 step rewrote into a
// nib render; nib.Parse now refuses it, so this gate and the load gate are two
// layers of the same refusal. Which files endanger anything is
// blockingScanProblems' decision.
func gateContentClassifiable(env migrateEnv, scan *storeScan) gateResult {
	blocking := blockingScanProblems(env, scan)
	if len(blocking) == 0 {
		return gateMet()
	}
	return gateRefused(output.ErrFileError,
		fmt.Sprintf("refusing to migrate around %d file(s) that cannot be read as nibs (move them out of the store or repair them, then re-run `nibs migrate`):\n  %s",
			len(blocking), describeScanProblems(blocking)))
}

// gateNoLiveServe refuses to migrate a store some `nibs serve` is holding.
//
// It replaces guidance that could only ask. AcquireStoreLock excludes cooperating
// writers for ONE mutation, which is not the dangerous window: a web update
// clones a nib, parks on that lock inside Core.Update while holding c.mu — so the
// watcher cannot refresh the stale clone — and writes the pre-migration render
// back after the run releases. The source is v1 by then, so no detection fires
// again: silent, permanent, and not self-healing.
//
// A run that already holds the exclusion answers met without probing. The probe
// takes the same lock, and the flock is per descriptor, so a run asking this
// question about a lock it is itself holding would refuse itself — the shape
// AcquireStoreLock's warning describes.
func gateNoLiveServe(env migrateEnv, _ *storeScan) gateResult {
	if env.serveExcluded {
		return gateMet()
	}
	fence, err := nibcore.AcquireServeExclusion(env.nibsRoot)
	if errors.Is(err, nibcore.ErrStoreServed) {
		return gateRefused(output.ErrConflict, liveServeRefusal)
	}
	if err != nil {
		return gateUndecidable(fmt.Sprintf("whether a `nibs serve` is running could not be determined (%v), so it is not previewed here.", err))
	}
	// A probe, not a hold: the run's own acquisition is the authority, and holding
	// it here would refuse the very run this is previewing for.
	_ = fence.Release()
	return gateMet()
}

// liveServeRefusal is the one wording both the preview and the run use.
const liveServeRefusal = "a `nibs serve` is running against this store; stop it, then re-run `nibs migrate`. " +
	"Migrating under a live serve can silently undo the migration: a web update already in flight writes its pre-migration copy back afterwards, and nothing detects it"

// gateContentDirsAreOurs refuses a PRE-LAYOUT store whose data/ or archive/ holds
// something this tool would not have put there.
//
// On a pre-layout store those directories have two possible histories: a run of
// ours was interrupted partway through moving files in, or they are the user's
// own — a site's data/, a note vault — and migrating would load every page in
// them as a nib. Two tests separate the two, and both are needed:
//
//   - the marker, which says an interrupted run was OURS. It is the only thing
//     that can answer for a directory whose contents look like nibs either way.
//   - the contents, for every store that crashed before markers existed and for
//     the ordinary case where the answer is plain: a resumed run's data/ holds
//     files layoutVerdict would have moved there.
//
// Scoped to a pre-layout store on purpose. Once the layout step has run, data/ IS
// the store's content by definition and a file in it that is not a nib is a
// different complaint — gateContentClassifiable's, or `nibs check`'s.
func gateContentDirsAreOurs(env migrateEnv, scan *storeScan) gateResult {
	if !layoutStepPending(env, scan) || migrationMarkerExists(env) {
		return gateMet()
	}
	l := env.layout()
	var foreign []string
	for _, p := range scan.problems {
		// A file the scan could not read at all is gateContentClassifiable's
		// business, not this gate's: it refuses over exactly those, with a
		// remedy of its own, and two refusals over one file help nobody.
		if p.unreadable || !l.IsDataRel(p.path) && !l.IsArchivedRel(p.path) {
			continue
		}
		foreign = append(foreign, p.path)
	}
	foreign = append(foreign, scan.foreignContent...)
	if len(foreign) == 0 {
		return gateMet()
	}
	slices.Sort(foreign)
	return gateRefused(output.ErrValidation,
		fmt.Sprintf("this store has not been migrated yet, but %s/ already holds %d file(s) nibs would not have written there:\n  %s\n"+
			"that directory is where the migration puts nib files, so migrating would load these as nibs. If they are yours, move them "+
			"aside and re-run `nibs migrate`; if this is a migration of ours that was interrupted, re-run it from the same nibs version, "+
			"which leaves a %s marker behind. Nothing has been changed",
			store.DataDirName, len(foreign), echoedList(sanitizedList(foreign), ""), migrationMarkerName))
}

// layoutStepPending reports whether the store still has the pre-layout shape,
// read off the scan the caller already performed rather than walking again.
func layoutStepPending(env migrateEnv, scan *storeScan) bool {
	for i, step := range migrationSteps {
		if step.name == layoutStepName {
			return scan.counts[i] > 0
		}
	}
	return false
}

// writeMigrationReport records the layout step's judgement calls in the store, or
// removes a previous run's report when this one had none to make.
//
// Removing matters as much as writing: the report is rewritten per run rather
// than accumulated, so a store that was cleaned up between runs must not keep a
// stale list of files that are no longer there.
func writeMigrationReport(root string, assumed, declined []string) error {
	path := filepath.Join(root, migrationReportName)
	if len(assumed) == 0 && len(declined) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}

	var b strings.Builder
	b.WriteString("# nibs migration report\n\n")
	fmt.Fprintf(&b, "Written by `nibs migrate` on %s. Rewritten by each run; delete it once you have acted on it.\n",
		time.Now().Format(time.DateOnly))
	if len(assumed) > 0 {
		fmt.Fprintf(&b, "\n## Moved into %s/ as assumed nibs\n\n"+
			"These carry a title and a known status but not the shape nibs writes, so the migration could not prove they were nibs. "+
			"They are nib files now. If one of them was an ordinary document, move it back out of `%s/` and delete the nib.\n\n",
			store.DataDirName, store.DataDirName)
		writeReportList(&b, assumed)
	}
	if len(declined) > 0 {
		b.WriteString("\n## Left where they are\n\n" +
			"These carry front matter but nothing that identifies them as nibs, so the migration did not touch them. " +
			"They are no longer store content: nibs will not load them and no query will show them. " +
			"If one of them IS a nib, move it into `" + store.DataDirName + "/`.\n\n")
		writeReportList(&b, declined)
	}
	return fsutil.AtomicWriteFile(path, []byte(b.String()), 0644)
}

// writeReportList renders one bullet per path, through the control-character
// boundary every filename read off the filesystem crosses. Unbounded, unlike the
// lists a refusal prints: this is the channel those elisions point AT.
func writeReportList(b *strings.Builder, paths []string) {
	for _, p := range paths {
		fmt.Fprintf(b, "- `%s`\n", stripControlChars(p))
	}
}

// migrationMarkerExists reports whether a previous run of ours was interrupted
// inside this store. A read error counts as absent: the marker only ever WIDENS
// what migrate will proceed over, so failing to read it must not do so silently.
func migrationMarkerExists(env migrateEnv) bool {
	info, err := os.Stat(filepath.Join(env.nibsRoot, migrationMarkerName))
	return err == nil && info.Mode().IsRegular()
}

// writeMigrationMarker records that a run has started working in this store, so
// an interrupted one is recognizable afterwards (see migrationMarkerName). The
// body is diagnostics; presence is the signal.
func writeMigrationMarker(env migrateEnv, steps []migrationStep) error {
	names := make([]string, len(steps))
	for i, step := range steps {
		names[i] = step.name
	}
	body := fmt.Sprintf("# `nibs migrate` is working in this store, or was interrupted while doing so.\n"+
		"# Re-run `nibs migrate` to finish; this file is removed when it completes.\n"+
		"steps: %s\n", strings.Join(names, ", "))
	return fsutil.AtomicWriteFile(filepath.Join(env.nibsRoot, migrationMarkerName), []byte(body), 0644)
}

// clearMigrationMarker removes the marker, tolerating its absence: a run that
// finds nothing pending clears it too, so a crash in the last instant before
// removal does not leave the store looking mid-migration forever.
func clearMigrationMarker(env migrateEnv) error {
	err := os.Remove(filepath.Join(env.nibsRoot, migrationMarkerName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// gateStoreLoadsCleanly previews loadStoreForMigration's own refusal: a store
// that does not load — an unparseable file, a duplicate id — must not have a
// content step run over it, because the loaded store is then not a faithful
// picture of the directory.
//
// It is UNDECIDABLE while a pending step still has to move the files Core.Load
// reads, and that limit is inherent rather than an oversight: Core.Load walks
// data/ and archive/, so on a pre-layout store the nib files are not there yet and
// this gate would answer for an empty store. Evaluating it honestly would mean
// performing the move first. The run still applies it — at the content step's
// apply, after the layout step — and "undecidable" is a state the preview reports
// rather than an absence its silence has to carry.
func gateStoreLoadsCleanly(env migrateEnv, scan *storeScan) gateResult {
	pending := scan.pending()
	if !anyContentPending(pending) {
		return gateMet()
	}
	if loadPendingInvalidated(pending) {
		return gateUndecidable(fmt.Sprintf("the content steps' own precondition — every file under %s/ parses and no id is duplicated — can only be checked once the layout step has moved the files there, so it is not previewed here.",
			store.DataDirName))
	}
	if _, err := loadStoreForMigration(env); err != nil {
		return gateRefused(output.ErrFileError, err.Error())
	}
	return gateMet()
}

// blockingScanProblems returns the scan problems that must stop a run: the
// files a pending CONTENT step would otherwise be forced to migrate AROUND.
//
// runMigrations' fail-loud gate and reportDryRun's preview of it are ONE
// decision seen from two sides, and they must answer identically or the preview
// predicts an outcome the run does not produce. They have already diverged
// twice by being written twice; sharing this function is what stops a third.
//
// The scoping is what makes `.nibs/README.md` a legal place for a readme. The
// danger the gate names is a link-rewrite danger — a step that
// rewrites edges must see every file that could hold one — so a problem blocks
// exactly when it is, or is about to become, store CONTENT:
//
//   - already under data/ or archive/: the content steps load it;
//   - unreadable, wherever it sits: the layout step cannot prove it is not a
//     nib, so it moves it into data/ (see layoutMovableFiles) and the content
//     steps meet it there;
//   - fence-less at the store root: provably not a nib, never moved, and
//     invisible to Core.Load once the relayout lands — so it blocks nothing.
//     An IRREGULAR file — a FIFO, socket or device named `*.md` — is in this
//     bucket for the same reason and is recorded with unreadable false: the scan
//     proves what it is from the directory entry without opening it, and
//     layoutMovableFiles leaves it where it sits. Filing it under `unreadable`
//     instead made a FIFO at a pre-layout store's root a lockout — every command
//     said to run `nibs migrate`, and migrate refused to migrate around it.
//
// A shape step alone can never be blocked: it only MOVES files, and moves
// nothing it could not classify.
func blockingScanProblems(env migrateEnv, scan *storeScan) []scanProblem {
	if !anyContentPending(scan.pending()) {
		return nil
	}
	l := env.layout()
	var blocking []scanProblem
	for _, p := range scan.problems {
		if p.unreadable || l.IsDataRel(p.path) || l.IsArchivedRel(p.path) {
			blocking = append(blocking, p)
		}
	}
	return blocking
}

// scanProblemLines renders one "path: reason" entry per problem. Both halves come
// from the filesystem — a filename, an os error quoting one — so both pass through
// the control-character boundary (see stripControlChars).
func scanProblemLines(problems []scanProblem) []string {
	lines := make([]string, len(problems))
	for i, p := range problems {
		lines[i] = fmt.Sprintf("%s: %s", stripControlChars(p.path), stripControlChars(p.reason))
	}
	return lines
}

// describeScanProblems renders those entries as the indented lines a refusal
// lists them on, bounded — `nibs check` reports every unclassifiable file in the
// store, so it is the full list the elision points at.
func describeScanProblems(problems []scanProblem) string {
	return echoedList(scanProblemLines(problems), echoedListRemedyCheck)
}

// stillPending re-runs a step's detection after its apply and describes
// whatever still fires, so runMigrations' post-condition covers BOTH detector
// kinds. A content step names the files whose header still matches; a shape
// step, having no per-file answer, reports how many files it would still move.
func stillPending(env migrateEnv, step migrationStep) ([]string, error) {
	if step.isContent() {
		return filesMatching(env, step.pred)
	}
	n, err := step.shape(env)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("%d file(s) still out of place", n)}, nil
}

// filesMatching walks the store and returns (store-relative) paths of the
// files whose scanned header satisfies pred. Used only on runMigrations'
// post-condition failure path, where naming the files is the whole point.
// Files that will not be store content are passed over through the same
// predicate scanStore counts by — they were never counted pending, so they can
// never be what a step failed to fix, and answering that differently here turns
// a document the layout step correctly left alone into "applied but its
// detection still fires", which locks the store.
func filesMatching(env migrateEnv, pred func(fmHeader) bool) ([]string, error) {
	var matches []string
	err := forEachNibFile(env, func(path string, skipped error) error {
		if skipped != nil {
			return nil
		}
		h, err := readFrontMatterHeader(path)
		if err != nil || !willBeStoreContent(env.layout(), storeRelPath(env, path), h) {
			return nil
		}
		if pred(h) {
			matches = append(matches, storeRelPath(env, path))
		}
		return nil
	})
	return matches, err
}

// refuseIfMigrationPending is the gate PersistentPreRunE runs between store
// resolution and core.Load(): every command except the skip-listed ones
// refuses to run on a store with pending migrations, naming the fix.
//
// It takes NO config, and that is deliberate rather than an omission:
// detection reads file headers and directory shapes only, so the gate works on
// a store whose config has not moved into it yet — which is the state every
// pre-layout store is in when this gate first fires.
func refuseIfMigrationPending(nibsRoot string) error {
	pending, err := pendingMigrations(newMigrateEnv(nibsRoot))
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	names := make([]string, len(pending))
	for i, step := range pending {
		names[i] = step.name
	}
	return fmt.Errorf("this nibs store needs migration before commands can run (pending: %s); stop any running `nibs serve`, then run `nibs migrate` (preview with `nibs migrate --dry-run`)",
		strings.Join(names, ", "))
}

// forEachNibFile walks every .md file under the store ROOT through the shared
// per-file classification (nibcore.WalkStoreFiles — subdirectories included so
// archived nibs migrate too, dot directories pruned).
//
// Core.Load walks the same function but from DIFFERENT roots: data/ and
// archive/ only, via nibcore.WalkStoreContent. So the two agree on what any
// individual file IS and deliberately disagree on which directories are in
// scope — a scan restricted to data/ would never see the root-level files the
// layout step exists to move. Every caller that compares a scan result against
// what will LOAD has to bridge that gap itself (see blockingScanProblems).
// Only the enumeration-failure posture is this walk's own.
func forEachNibFile(env migrateEnv, fn func(path string, skipped error) error) error {
	return nibcore.WalkStoreFiles(env.nibsRoot, func(path string, err error) error {
		if err != nil {
			// An entry the walk DECLINED to hand over — a FIFO, socket or device
			// named `*.md` — is one file, not a broken store, so it goes to fn
			// with the reason rather than aborting every command. It cannot share
			// fn's ordinary path because that path OPENS the file, and opening
			// this one never returns.
			if errors.Is(err, nibcore.ErrNotRegularFile) {
				return fn(path, err)
			}
			// A directory that cannot be ENUMERATED stays fatal — unlike a
			// single skippable file, the scan cannot know what it is missing —
			// but with enough context to act on. The offending entry is named by
			// the walk's own error, so this adds the store rather than repeating it.
			return fmt.Errorf("scanning nibs store %s (fix its permissions, or remove the offending entry): %w", env.nibsRoot, err)
		}
		return fn(path, nil)
	})
}

// storeRelPath names a walked file relative to the store root with forward
// slashes — the shape every other nibs surface spells a path in.
func storeRelPath(env migrateEnv, path string) string {
	rel, err := filepath.Rel(env.nibsRoot, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// maxHeaderScanBytes bounds how much of a file readFrontMatterHeader reads.
// It IS internal/nib's front-matter byte cap (not a copied number, so the two
// cannot drift), and a legitimate header is a few hundred bytes.
//
// THE TWO CAPS MEASURE DIFFERENT THINGS, and the difference is worth stating
// because it is tempting to read them as one. MaxFrontMatterBytes bounds the
// block BETWEEN the fences; this bounds bytes READ FROM THE FILE, which also
// include the opening fence line and the closing fence token. So a block in the
// last few bytes below the cap is one nib.Parse accepts and the scan cannot see
// the end of — measured: a block of 262138..262144 bytes parses while the scan
// refuses it, a band `len("---\n") + len("---")` wide, and whitespace-padded
// fences mean no fixed slack closes it exactly.
//
// The scan ABSTAINS there rather than guessing: such a file errors, which
// classifies it unreadable, which is the conservative direction (the layout step
// keeps moving it, content gates refuse rather than migrate around it). What the
// scan must never do is the opposite — call a file a nib that nib.Parse refuses
// — and that it does not do at any size.
const maxHeaderScanBytes = nib.MaxFrontMatterBytes

// errFrontMatterNotClosed is readFrontMatterHeader's answer for a file whose
// front matter opens and never closes — torn, half-written, or a header past the
// scan's byte budget.
//
// A SENTINEL rather than a wrapped io.ErrUnexpectedEOF, for two reasons that
// pull the same way. Its text is nib.Parse's verbatim, so one file reads
// identically whether migrate's scan or Core.Load's diagnostics reports it —
// wrapping appends "unexpected EOF" to one surface and not the other. And it is
// a more precise fact than "input ended early": declaredStoreCorroborated has to
// tell a file it READ COMPLETELY and found unterminated from one it genuinely
// could not read, because those two get opposite answers (see its call site).
var errFrontMatterNotClosed = errors.New("front matter never closed (missing the closing --- fence)")

// fmHeader is the migration-relevant subset of a nib file's front-matter
// header, extracted by a streamed line scan (no YAML parse).
type fmHeader struct {
	// hasFrontMatter reports whether the file opens with a front-matter fence
	// (`---` or `---yaml`, the two nib.Parse accepts, matched by TrimSpace-
	// equivalence — see readFrontMatterHeader). A fence-less .md (a README, a
	// mid-conflict file) is not a nib shape and must be classified as a
	// diagnostic, NOT as a version-0 nib — the zero header would otherwise
	// count it v0-pending, and migrating would rewrite the document into a
	// nib render. nib.Parse enforces the same first-line rule, so the scan
	// and the load classify the file identically.
	hasFrontMatter bool
	// hasIDMarker reports whether the FIRST line inside the fence is a `#`
	// comment. nib.Render writes the nib's own id there and has since the
	// initial commit, and it is the part of the shape ordinary front matter is
	// least likely to reach by accident — a hand-authored header rarely opens
	// with a comment. Presence only: the value is deliberately not matched
	// against the filename (see nibFileFormat.requiresIDMarker).
	hasIDMarker bool
	// values holds every top-level `key: value` line the scan saw, unquoted.
	// PRESENCE is a different question from VALUE and the typed fields below
	// cannot answer it: a nibs-written v0 file renders `version: 0`, so a zero
	// version must not be read as a missing one.
	values map[string]string
	// version is the value of a top-level `version:` line; 0 when the key is
	// absent or its value is not an integer (absent = 0 = legacy, matching
	// nib.Nib.Version; a garbage value surfaces later as an unparseable file).
	version int
	// priority is the unquoted value of a top-level `priority:` line.
	priority string
	// status is the unquoted value of a top-level `status:` line. No migration
	// step keys on it; it is one of the keys nibRenderFormat requires, and the
	// only one whose VALUE that format constrains. On its own it establishes
	// almost nothing — nothing reserves the key, and ordinary notes and docs
	// pages track their own state with it — which is why the format asks for
	// the whole rendered shape rather than this key alone.
	status string
}

// nibFileFormat describes the front matter a renderer ALWAYS writes. It is what
// lets a scanned header be classified as "nibs rendered this file" rather than
// the far weaker "this is markdown with front matter", which is the question
// declaredStoreCorroborated has to answer before `nibs migrate` may rename a
// whole directory and rewrite everything in it.
//
// EVERY RULE HERE NAMES SOMETHING THE RENDERER NEVER OMITS. `version`, `title`
// and `status` are the only three keys renderFrontMatter emits unconditionally;
// every other field carries omitempty. `type:` looks equally dependable — it is
// present on all 666 nibs in this project's own store — but that is the default
// type showing through, not a promise of the format, so requiring it would bet
// on a default.
//
// A DESCRIPTION rather than inline conditionals because a second format is
// coming: beans files carry no `version:`, so the required set is necessarily
// per format and adding one must not force this predicate to be restructured.
// One struct and one method, deliberately — not a registry, not a plugin system.
type nibFileFormat struct {
	// requiresIDMarker requires the first line inside the fence to be a `#`
	// comment, where nib.Render writes the nib's own id.
	//
	// PRESENCE, NOT A MATCH against the filename. The match buys little on top
	// of the keys below, and it asks the wrong question: for "did nibs render
	// this file", a nib render in an oddly-named file is still a nib render.
	requiresIDMarker bool
	// requiredKeys must all be PRESENT in the header. Presence rather than a
	// non-zero value: a nibs-written v0 file renders `version: 0`.
	requiredKeys []string
	// valueRules constrain the keys whose VALUE is itself evidence. A rule
	// applies only where the key is present — absence is requiredKeys' answer to
	// give, and a rule that also fired on it would report one defect as two.
	valueRules map[string]func(string) bool
}

// nibRenderFormat is the shape nib.Render writes.
var nibRenderFormat = nibFileFormat{
	requiresIDMarker: true,
	requiredKeys:     []string{"version", "title", "status"},
	valueRules: map[string]func(string) bool{
		"version": isIntegerHeaderValue,
		"status":  config.IsKnownStatus,
	},
}

// nibFileVerdict is what the layout step concludes about one file it is deciding
// whether to move into data/. Three answers rather than two, because the honest
// middle one is what this step kept getting wrong: its bar used to be nib.Parse's
// (a front-matter fence), which a documentation page clears, and raising it to
// nib.Render's alone would leave a hand-authored nib behind — and a nib left
// outside data/ is gone from every query with nothing said about it.
type nibFileVerdict int

const (
	// notANib: the evidence is complete and negative. Left where it sits, which
	// is what makes `.nibs/README.md` a legal place for a readme.
	notANib nibFileVerdict = iota
	// assumedNib: the evidence fits a nib and fits ordinary content equally well.
	// Moved — losing a nib is the worse failure — but never silently: the run
	// asks, or says which files it assumed.
	assumedNib
	// isNib: the whole shape nib.Render writes. Moved without comment.
	isNib
)

// newerVersionSpeaksForStore reports whether a `version:` above
// nib.CurrentVersion in this file means the STORE was written by a newer nibs.
//
// The key is not a nibs invention — API docs, spec pages and chart front matter
// carry it — so asking it of every fenced file let one documentation page in a
// pre-layout store refuse EVERY command, `nibs migrate` included. That state is
// terminal: the only command that could repair the store is one of the refused
// ones, and the remedy printed ("upgrade nibs") names a version that does not
// exist.
//
// Classification alone is the wrong scope, in the other direction. A future
// format may rename keys or add a status this build has never heard of, so a
// genuine v2 nib need not classify — and that is precisely the file the refusal
// exists for. So two things qualify, and the second is what keeps the refusal
// honest about the future:
//
//   - the file will BE store content, so this build would have to migrate a
//     version it does not understand;
//   - it carries the id comment, which nib.Render has written since the initial
//     commit. Whatever else changes around it, a file opening that way was
//     written by nibs.
func newerVersionSpeaksForStore(l store.Layout, rel string, h fmHeader) bool {
	return willBeStoreContent(l, rel, h) || h.hasIDMarker
}

// willBeStoreContent reports whether a file will be store content once the layout
// step has run: already under data/ or archive/, or destined to be moved there.
//
// It is ONE definition on purpose. Every count of "how much work is pending"
// derives from it — scanStore's per-step counts, and filesMatching's check that
// applying a step actually cleared it — and the two disagreeing is not a cosmetic
// bug: a file counted pending that the layout step will never move leaves a step
// permanently unclearable, and every command refuses a store with pending work.
func willBeStoreContent(l store.Layout, rel string, h fmHeader) bool {
	if l.IsDataRel(rel) || l.IsArchivedRel(rel) {
		return true
	}
	return layoutVerdict(rel, h) != notANib
}

// contentDirRel is a content file's path as it would have looked BEFORE the
// layout step moved it: data/x.md came from x.md, data/sub/y.md from sub/y.md.
// It is what lets layoutVerdict answer "would we have written this here?" about a
// file already in place, since the verdict reads position from the path.
func contentDirRel(l store.Layout, rel string) string {
	for _, dir := range []string{store.DataDirName, store.ArchiveDirName} {
		if trimmed, ok := strings.CutPrefix(rel, dir+"/"); ok {
			return trimmed
		}
	}
	return rel
}

// layoutVerdict classifies a file the layout step is deciding whether to move.
//
// Callers filter data/ and archive/ FIRST: a file already there is store content
// by position, and nothing about its header changes that.
//
// EVIDENCE AND POSITION ARE NOT SYMMETRIC AXES. The rendered shape settles the
// question wherever the file sits, because Core.Load has always loaded nested
// files (internal/nibcore/core.go) and a store somebody organized into folders
// has to keep working. Position only breaks the tie in the middle tier, where
// there is nothing else to break it with: nibs has never written a nib into a
// subdirectory of a pre-layout store root, so `notes/architecture.md` carrying a
// title and a status is a documentation page, while the same header at the root
// could be either.
//
// The middle bar — a `title` and a `status` from the enum — is deliberately one
// ordinary content REACHES. `status: draft` is how note vaults and docs sites
// track a page's own state, and `draft` is one of our six. That is not a defect
// in the bar: no bar can separate a hand-authored nib from a documentation page,
// because the two are the same bytes. The bar's job is to decide which way the
// tie breaks by default, and the verdict's name is what obliges the caller to
// say so.
//
// The store's id prefix is deliberately NOT consulted, though the filename is
// where a nib's identity lives. Its failure mode runs the wrong way: a project
// that changed its prefix keeps older nibs named under the old one, so the test
// would classify real nibs as documents and leave them behind — silently
// orphaning exactly the files this verdict exists to protect. A config-less
// pre-layout store, which is legal, has no prefix to test at all.
func layoutVerdict(rel string, h fmHeader) nibFileVerdict {
	if nibRenderFormat.rendered(h) {
		return isNib
	}
	if !h.hasFrontMatter || strings.Contains(rel, "/") {
		return notANib
	}
	if _, ok := h.values["title"]; !ok {
		return notANib
	}
	if !config.IsKnownStatus(h.values["status"]) {
		return notANib
	}
	return assumedNib
}

// rendered reports whether h is a header this format's renderer produced.
func (f nibFileFormat) rendered(h fmHeader) bool {
	if !h.hasFrontMatter {
		return false
	}
	if f.requiresIDMarker && !h.hasIDMarker {
		return false
	}
	for _, key := range f.requiredKeys {
		if _, ok := h.values[key]; !ok {
			return false
		}
	}
	for key, valid := range f.valueRules {
		if value, ok := h.values[key]; ok && !valid(value) {
			return false
		}
	}
	return true
}

// isIntegerHeaderValue reports whether a scanned scalar is an integer, the same
// test readFrontMatterHeader applies before populating fmHeader.version.
func isIntegerHeaderValue(value string) bool {
	_, err := strconv.Atoi(value)
	return err == nil
}

// readFrontMatterHeader streams a nib file's front-matter header with bufio,
// stopping at the closing `---` fence, and extracts the version and priority
// keys. It is deliberately NOT a YAML parse: detection runs on every command,
// must never write, and needs only two scalar keys — top-level `key: value`
// lines are matched positionally (no leading whitespace), which is exactly how
// nib.Render emits them. A file that does not open with a front-matter fence
// has no front matter and reports hasFrontMatter=false, which callers must
// classify as a diagnostic, never as a v0 nib (see fmHeader.hasFrontMatter).
//
// Fence rule: a line IS a fence iff strings.TrimSpace(line) equals the fence
// token — the same rule nib.Parse applies, pinned to the frontmatter library
// both ultimately answer to: its line handling is a fixed bytes.TrimSpace, so
// the authoritative parse accepts whitespace-padded fences no matter what the
// scan does. A stricter compare here misses a padded closing fence and reads
// BODY lines as header keys — false pending (a migrated store gated forever)
// or false not-pending (a v0 file silently never migrated).
//
// Best-effort about VALUES, never about the shape. A key the scan misreads costs
// at most a wrong count; a file it calls a nib that nib.Parse refuses is counted
// toward a step that can never process it. So the shape questions — does a
// front-matter block open, and does it close — are never answered WRONGLY: where
// the scan cannot see the answer it errors instead of guessing. A header that
// runs past maxHeaderScanBytes without closing is therefore an ERROR rather than
// a best-effort partial, since the limiter's EOF is indistinguishable from the
// file's own and accepting either would accept a header whose end was never seen
// (see maxHeaderScanBytes for the narrow band where that abstention costs a file
// nib.Parse would have taken). The scan only decides whether a migration is PENDING; the
// authoritative parse (and its errors) happen in the step's apply, and
// runMigrations' post-condition catches any residual scan/parse disagreement
// rather than letting it wedge the CLI.
func readFrontMatterHeader(path string) (fmHeader, error) {
	// OpenRegularFile closes the window WalkStoreFiles' classification leaves
	// open: the walk decided this path was a regular file a moment ago, and
	// opening a FIFO blocks forever rather than failing.
	f, err := nibcore.OpenRegularFile(path)
	if err != nil {
		return fmHeader{}, err
	}
	defer func() { _ = f.Close() }()

	var h fmHeader
	sc := bufio.NewScanner(io.LimitReader(f, maxHeaderScanBytes))
	// Raise the token cap from bufio's 64 KiB default to the full header cap:
	// a single over-64KiB header line must not end the scan early with only
	// the keys above it extracted — YAML reads the whole header, so the scan
	// must too, or the two disagree on files YAML accepts.
	sc.Buffer(make([]byte, 64*1024), maxHeaderScanBytes)
	if !sc.Scan() {
		return h, sc.Err()
	}
	// Both opening fences nib.Parse accepts (`---` and `---yaml`; the closing
	// fence is `---` for both), matched by TrimSpace-equivalence per the fence
	// rule above — the scan and the parse must agree on what opens front
	// matter, or they classify the same file differently.
	if fence := strings.TrimSpace(sc.Text()); fence != "---" && fence != "---yaml" {
		return h, sc.Err() // no opening fence: hasFrontMatter stays false
	}
	h.hasFrontMatter = true
	closed := false
	firstLine := true
	for sc.Scan() {
		// line keeps its leading whitespace: the key checks below are
		// positional (a top-level key starts at column 0), so only the
		// fence compare may TrimSpace.
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "---" {
			closed = true
			break // closing fence, whitespace-padded or not (fence rule above)
		}
		if firstLine {
			firstLine = false
			h.hasIDMarker = strings.HasPrefix(line, "#")
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || key == "" || key[0] == ' ' || key[0] == '\t' || key[0] == '#' {
			continue // not a top-level key line (nested item, comment, prose)
		}
		scalar := unquoteHeaderValue(value)
		if h.values == nil {
			h.values = make(map[string]string, 8)
		}
		// Last occurrence wins, which is what the typed fields below have always
		// done — the two views must not disagree about the same header.
		h.values[key] = scalar
		switch key {
		case "version":
			if v, err := strconv.Atoi(scalar); err == nil {
				h.version = v
			}
		case "priority":
			h.priority = scalar
		case "status":
			h.status = scalar
		}
	}
	if err := sc.Err(); err != nil {
		return h, err
	}
	if !closed {
		// RUNNING OUT OF INPUT IS NOT A CLOSING FENCE, and nothing else here can
		// tell the two apart: bufio.ScanLines returns the last partial chunk as
		// an ordinary token, so the real end of a short file and the
		// LimitReader's artificial EOF at maxHeaderScanBytes both end the loop
		// exactly as meeting a fence does. Without this check a header that never
		// closes is reported as a nib carrying no `version` — counted v0 pending
		// — while nib.Parse refuses the very same file, which is the scan/parse
		// divergence this function's fence rule and runMigrations' post-condition
		// both exist to prevent.
		//
		return h, errFrontMatterNotClosed
	}
	return h, nil
}

// unquoteHeaderValue reads a scanned scalar value the way YAML would: trim
// surrounding space, honor one level of single or double quotes, and strip a
// trailing ` # ...` comment. The comment rule mirrors YAML's exactly where it
// matters: an unquoted `#` opens a comment only when PRECEDED BY WHITESPACE
// (or starting the value), so `1 # migrated` is 1 while `1#nospace` stays the
// (invalid, load-refused) scalar `1#nospace`; inside quotes a `#` is content.
// Divergence here is the expensive kind — the scan deciding "pending" while
// the parse disagrees wedges every command (see runMigrations' post-condition)
// — so this must track how YAML reads the two keys the scan extracts, both of
// which hold enum/int scalars that never contain quotes (YAML's escaped-quote
// forms are deliberately not handled).
func unquoteHeaderValue(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '\'' || v[0] == '"') {
		// Quoted scalar: the value ends at the closing quote; anything after
		// it (only a comment can legally follow) is dropped.
		if end := strings.IndexByte(v[1:], v[0]); end >= 0 {
			return v[1 : 1+end]
		}
		return v // unterminated quote — malformed; the load will name it
	}
	if v != "" && v[0] == '#' {
		return "" // the whole value is a comment
	}
	for i := 1; i < len(v); i++ {
		if v[i] == '#' && (v[i-1] == ' ' || v[i-1] == '\t') {
			return strings.TrimRight(v[:i], " \t")
		}
	}
	return v
}

var (
	migrateDryRun     bool
	migrateAllowDirty bool
	migrateForce      bool
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply pending store migrations",
	Long: `Applies every pending store migration, in order. All other commands refuse
to run while a migration is pending, so this is the one command that operates
on an unmigrated store (it loads no App state and is skip-listed in the root
command's PersistentPreRunE).

Git is the plan review and the rollback: when the store directory is a git
repository with uncommitted changes, migrate refuses until they are committed
or stashed (or --allow-dirty overrides). Use --dry-run to list what would be
applied without modifying anything.`,
	Args: codedNoArgs(nil),
	RunE: func(cmd *cobra.Command, args []string) error {
		nibsRoot, err := resolveStoreDir()
		if err != nil {
			return err
		}
		env := newMigrateEnv(nibsRoot)

		// One shared scan serves both the pending decision and --dry-run's
		// per-step counts — the same walk pendingMigrations performs.
		scan, err := scanStore(env)
		if err != nil {
			return migrateCmdError(err)
		}
		// Name every file the scan skipped, up front and on every path: the
		// user must learn about them from the tool, not from a surprise later.
		// Both halves are file-sourced (a filename, an os error quoting one), so
		// they pass through the control-character boundary.
		for _, p := range scan.problems {
			ui.Printf("Note: skipping %s: %s — migration will not treat it as a nib.\n",
				stripControlChars(p.path), stripControlChars(p.reason))
		}
		pending := scan.pending()
		if migrateDryRun {
			return reportDryRun(env, scan)
		}
		if len(pending) == 0 {
			ui.Println("Store is up to date; no migrations pending.")
			return nil
		}

		// What git can and cannot roll back is narrated here; the REFUSALS those
		// observations drive are gates, so the two cannot disagree about whether
		// git was consulted.
		isRepo := reportGitPosture(env)

		if refusals, _ := wouldRefuse(env, scan); len(refusals) > 0 {
			return migrateCmdError(&refusalError{refusals: refusals})
		}

		ui.Println("Stop any running `nibs serve` before migrating.")
		log := func(format string, a ...any) { ui.Printf(format+"\n", a...) }
		if err := runMigrations(env, log); err != nil {
			return migrateCmdError(err)
		}
		ui.Println("Migration complete." + postMigrateCommitHint(isRepo))
		return nil
	},
}

// reportGitPosture prints what migrate can and cannot roll back, and reports
// whether a repository genuinely covers the store (which decides the
// post-migration commit hint).
//
// A git failure means a safety net cannot be evaluated. Say so — silently
// proceeding as if the tree were clean would disable the net without a trace —
// rather than blocking migration on git's availability. The corresponding gates
// (gateStoreGitClean, gateLegacyConfigRecoverable) take the same view and simply
// do not fire.
//
// Both questions go through the env's memoized accessors, so the narration and the
// gates share ONE git invocation each rather than asking the same question twice
// per run.
func reportGitPosture(env migrateEnv) (storeIsRepo bool) {
	_, isRepo, err := env.storeGitState()
	if err != nil {
		ui.Printf("Warning: could not determine the store's git state (%v); the dirty-store safety check was skipped.\n", err)
		isRepo = false
	}
	if !isRepo {
		ui.Printf("Note: the store at %s is not protected by git; consider backing it up before migrating.\n", env.nibsRoot)
	}
	// The layout step is the only migration that reaches OUTSIDE the store: it
	// deletes the project's `.nibs.yml`, in a repository that is frequently not
	// the store's.
	if !migrateAllowDirty && legacyConfigExists(env) {
		if _, gitErr := env.legacyConfigDirty(); gitErr != nil {
			ui.Printf("Warning: could not determine the git state of %s (%v); its dirty-file safety check was skipped.\n",
				env.legacyConfigPath(), gitErr)
		}
	}
	return isRepo
}

// migrateCmdError codes migrate's RunE-level failures for the CLI's error
// boundary (reportExitError), matching upgrade.go's discipline: the
// newer-store refusal is a VALIDATION error (exit 2 — the user must act before
// migrate may run), a precondition refusal carries the code its own gate
// declares, and everything else reaching RunE from the probe or the run is
// store/load I/O (exit 5). The refusals issued from the shared
// PersistentPreRunE gate are deliberately left uncoded, matching that gate's
// siblings.
func migrateCmdError(err error) error {
	var ns *newerStoreError
	if errors.As(err, &ns) {
		return cmdError(false, output.ErrValidation, "%v", err)
	}
	var re *refusalError
	if errors.As(err, &re) {
		return cmdError(false, re.code(), "%v", err)
	}
	return cmdError(false, output.ErrFileError, "%v", err)
}

// reportDryRun lists each pending step with its per-file count, modifying
// nothing. The counts come from the caller's scan — the same single walk that
// decided what is pending.
//
// When the run WOULD refuse, the preview announces every reason. Without that,
// the pending counts plus an unconnected skip note read as "the skipped file is
// harmlessly excluded", and the refusal comes as a surprise. The reasons come
// from wouldRefuse — the same list the run consults — so the preview cannot
// predict an outcome the run does not produce, and cannot go quiet about a gate
// somebody added on one side only.
//
// A gate that cannot be answered in advance says so, and says so UNCONDITIONALLY.
// The note used to live in the `else` of the refusal branch, so it vanished the
// moment any other gate fired — precisely the case where the user fixes what the
// preview named and re-runs, and then meets the unpreviewed refusal anyway. Its
// whole purpose is that its silence must not read as all-clear, which makes
// "printed only when everything else is clear" self-defeating.
func reportDryRun(env migrateEnv, scan *storeScan) error {
	pending := scan.pending()
	if len(pending) == 0 {
		ui.Println("Store is up to date; no migrations pending.")
		return nil
	}
	ui.Println("Pending migrations (nothing modified; run `nibs migrate` to apply):")
	for i, step := range migrationSteps {
		if scan.counts[i] == 0 {
			continue
		}
		ui.Printf("  %s — %s: %d file(s)\n", step.name, step.title, scan.counts[i])
	}
	if relocating, err := storeRelocationPending(env); err == nil && relocating {
		ui.Printf("  the layout step will first move the store %s to %s, where nibs can find it\n",
			env.nibsRoot, filepath.Join(env.layout().ProjectDir(), store.DirName))
	}
	refusals, undecidable := wouldRefuse(env, scan)
	if len(refusals) > 0 {
		ui.Printf("Warning: the real run will refuse — %d precondition(s) are not met:\n", len(refusals))
		for _, r := range refusals {
			ui.Printf("  %s: %s\n", r.gate, r.reason)
		}
	}
	// Only when nothing refuses: a refusal over these same files already names
	// them, and saying it twice reads as two problems.
	if len(refusals) == 0 {
		if assumed, err := assumedNibsPending(env); err == nil && len(assumed) > 0 {
			ui.Printf("Note: the real run %s %d file(s) as nibs — they carry a title and a known status but not the shape nibs writes:\n  %s\n",
				assumedNibsDisposition(), len(assumed), echoedList(sanitizedList(assumed), ""))
		}
	}
	for _, why := range undecidable {
		ui.Printf("Note: %s\n", why)
	}
	ui.Println("Stop any running `nibs serve` before migrating.")
	return nil
}

// migrationReportName is the file a run leaves in the store root recording the
// judgement calls it made: what it could only ASSUME was a nib, and what it
// declined to move. The run says both on stdout too, but a one-time migration's
// output scrolls away — under --force it may never be read at all — and this is
// what a user still has weeks later.
//
// Fence-less on purpose, so this tool's own rule (layoutVerdict) says it is not a
// nib and no later migration needs an exception for it.
const migrationReportName = "migration-report.md"

// layoutStepName is the shape step's name, referenced where a gate has to ask
// whether the store still has the pre-layout shape.
const layoutStepName = "layout"

// migrationMarkerName is the file a run drops in the store root while it is
// working, so an interrupted run is recognizable as OURS afterwards.
//
// It answers a question no inspection of data/ can: a pre-layout store already
// holding data/ is either a crashed migration to resume or somebody's own
// directory, and a note vault under data/ whose pages carry a title and a status
// classifies as ours under layoutVerdict. Presence is the whole signal; the
// contents are diagnostics.
//
// Not a key in config.yml, which was the obvious home: that file's exact bytes
// are already the config relocation's own crash-recovery signal (see
// planConfigRelocation's bytes.Equal), so a marker key in it would make every
// resumed run report a two-config conflict — and it would create a config file
// for a config-less project that never had one.
//
// Deliberately not a .md file and deliberately dot-prefixed, so no walk, scan or
// classifier has to know it exists.
const migrationMarkerName = ".migrating"

// assumedNibsPending is the files the layout step would move without being able
// to prove they are nibs, for the preview to report. It walks the store again
// rather than threading the plan out of wouldRefuse: --dry-run is not a hot path,
// and a second reader of the same rule is safer than a second copy of it.
func assumedNibsPending(env migrateEnv) ([]string, error) {
	moves, err := layoutMovableFiles(env)
	if err != nil {
		return nil, err
	}
	return layoutAssumedPaths(moves), nil
}

// assumedNibsDisposition names what the real run would do about those files,
// which is the whole point of previewing them: with --force it acts, at a
// terminal it asks. (Neither, and wouldRefuse has already reported the refusal.)
func assumedNibsDisposition() string {
	if migrateForce {
		return "will treat"
	}
	return "will ask whether to treat"
}

// postMigrateCommitHint suggests committing the migration's rewrite when the
// store is a git repository, so the converted state becomes the recoverable
// baseline the refusal above assumed.
func postMigrateCommitHint(isRepo bool) string {
	if !isRepo {
		return ""
	}
	return " Review and commit the store's changes (git -C <store> diff / commit)."
}

func init() {
	migrateCmd.Flags().BoolVar(&migrateDryRun, "dry-run", false, "List pending migrations and per-step file counts without modifying anything")
	migrateCmd.Flags().BoolVar(&migrateAllowDirty, "allow-dirty", false, "Migrate even when the store's git repository has uncommitted changes")
	migrateCmd.Flags().BoolVarP(&migrateForce, "force", "f", false, "Treat files that could be either a nib or an ordinary document as nibs, without asking")
	rootCmd.AddCommand(migrateCmd)
}
