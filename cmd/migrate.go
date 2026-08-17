package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
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
type migrateEnv struct {
	nibsRoot string
	loadCfg  func() (*config.Config, error)
}

// newMigrateEnv builds the env for a resolved store root.
func newMigrateEnv(nibsRoot string) migrateEnv {
	env := migrateEnv{}
	env.setRoot(nibsRoot)
	return env
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

// migrateConfig reads the project config from wherever it currently lives: an
// explicit --config file, otherwise the store's own config.yml. --config can
// only name a config INSIDE the store — resolveStoreDir refuses one pointed at
// the pre-layout `.nibs.yml` — so the file the layout step relocates is never
// the one read here, and this stays the same derivation resolveCLIStore uses.
//
// A store with no config at all resolves to the defaults, which is the normal
// state of a pre-layout store and must not be an error: the refusal gate has to
// work before the config has moved into the store.
func migrateConfig(nibsRoot string) (*config.Config, error) {
	if configPath != "" {
		return config.LoadFromExplicitPathWithUserConfig(configPath)
	}
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
type migrationStep struct {
	name, title string
	pred        func(fmHeader) bool
	shape       func(migrateEnv) (int, error)
	apply       func(*migrateEnv, *nibcore.StoreLock, logf) error
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
		name:  "layout",
		title: "put the store, the project config and the nib files where the current layout expects them",
		shape: layoutPendingCount,
		apply: applyLayout,
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

// layoutPendingCount reports how many things the layout step would move: every
// nib file still sitting outside data/ and archive/, the project config if it is
// still beside the store rather than inside it, and the store DIRECTORY itself
// if it is still outside `<project>/.nibs`. Zero means the store already has the
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
	if storeRelocationPending(env) {
		n++
	}
	return n, nil
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
// Unlike planStoreRelocation this never errors: detection runs on every command,
// and a relocation blocked by an occupied destination is still pending work —
// planLayout raises that refusal when migrate actually runs.
func storeRelocationPending(env migrateEnv) bool {
	return filepath.Base(env.nibsRoot) != store.DirName && hasLegacyStoreShape(env.nibsRoot)
}

// legacyConfigExists reports whether the pre-layout project config is still
// beside the store. An unreadable path counts as absent: the apply re-checks
// and fails loudly there, and detection must never abort every command.
func legacyConfigExists(env migrateEnv) bool {
	info, err := os.Stat(env.legacyConfigPath())
	return err == nil && !info.IsDir()
}

// layoutMovableFiles returns the store-relative paths (forward slashes) of the
// nib files the layout step must move into data/, in walk order.
//
// Everything already under data/ or archive/ is where it belongs. Everything
// else under the store root is pre-layout content — including files nested in
// subdirectories, which keep their relative shape under data/.
//
// A .md file with NO front matter is deliberately left where it is: it is not
// a nib (nib.Parse refuses it, and Core.Load only ever reported it as a
// diagnostic), so after the migration it simply stops being store content —
// which makes `.nibs/README.md` a legal place for a readme rather than a
// permanent complaint. A file whose header cannot be READ is moved anyway: the
// scan cannot prove it is not a nib, and leaving a real nib behind would drop
// it out of every query.
func layoutMovableFiles(env migrateEnv) ([]string, error) {
	l := env.layout()
	var movable []string
	err := forEachNibFile(env, func(path string) error {
		rel := storeRelPath(env, path)
		if l.IsDataRel(rel) || l.IsArchivedRel(rel) {
			return nil
		}
		h, err := readFrontMatterHeader(path)
		if err == nil && !h.hasFrontMatter {
			return nil
		}
		movable = append(movable, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return movable, nil
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
	// finalRoot is the store root once relocateTo has been honored — the root
	// every path this step writes is derived from.
	finalRoot string
	// movable holds the store-relative paths (forward slashes) of the nib files
	// that move into data/, in walk order. They survive the store relocation
	// unchanged: that is a rename of the directory they sit in.
	movable []string
	// config, when non-nil, relocates the pre-layout project config into the
	// store. Nil when the project has none (already migrated, or never had one).
	config *configRelocation
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

	movable, err := layoutMovableFiles(env)
	if err != nil {
		return nil, err
	}
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
	if !storeRelocationPending(env) {
		return "", nil
	}
	target := filepath.Join(env.layout().ProjectDir(), store.DirName)
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("cannot move the store %s to %s: %s already exists, and %s names %s as this project's store — so one of the two is stale; keep the one holding your nibs, remove the other, then re-run `nibs migrate`",
			env.nibsRoot, target, target, env.legacyConfigPath(), env.nibsRoot)
	}
	return target, nil
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
		// with it intact rather than being reassembled file by file.
		if err := os.Rename(env.nibsRoot, p.relocateTo); err != nil {
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
	if len(p.movable) > 0 {
		log("layout: moved %d nib file(s) into %s/", len(p.movable), store.DataDirName)
	}

	if p.config != nil {
		return p.config.apply(log)
	}
	return nil
}

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

	rewritten, note, stripErr := stripRetiredNibsPath(data, env)
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
		if err := nibcore.AtomicWriteFile(c.dest, c.body, c.perm); err != nil {
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

// stripRetiredNibsPath removes the `nibs.path` key from a legacy config's bytes,
// leaving the rest of the document untouched, and returns a note when dropping
// the key is worth telling the user about.
//
// A `path` naming somewhere OTHER than the store being migrated is refused
// rather than silently dropped — while the directory it names still EXISTS. The
// key is refused because discarding the only record of where a project's nibs
// live would strand them; once that directory is gone there is nothing left to
// strand, and the key is a stale record. Dropping it there is also what makes a
// run interrupted between the store relocation and the config write converge:
// the store is at `.nibs` by then, while the key still names the directory it
// came from.
func stripRetiredNibsPath(data []byte, env migrateEnv) (out []byte, note string, err error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, "", fmt.Errorf("parsing %s: %w", env.legacyConfigPath(), err)
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
		if _, statErr := os.Stat(resolved); statErr == nil {
			return nil, "", fmt.Errorf("%s sets `nibs.path: %s`, which is not the store being migrated (%s) and still exists; migrate that store instead (`nibs migrate --nibs-path %s`), or — if %s is the store you want to keep — remove the retired `nibs.path` key from %s and re-run",
				env.legacyConfigPath(), sanitizeFileText(declared), env.nibsRoot,
				sanitizeFileText(resolved), env.nibsRoot, env.legacyConfigPath())
		}
		note = fmt.Sprintf("dropped the retired `nibs.path: %s` from the config — that directory no longer exists, so the key was a stale record",
			sanitizeFileText(declared))
	}

	deleteMappingKey(nibs, "path")
	rewritten, err := yaml.Marshal(root)
	if err != nil {
		return nil, "", fmt.Errorf("rewriting %s without the retired `nibs.path` key: %w", env.legacyConfigPath(), err)
	}
	return rewritten, note, nil
}

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
		for _, uf := range unparseable {
			problems = append(problems, fmt.Sprintf("unparseable nib file %s: %s", uf.Path, uf.Reason))
		}
		for _, d := range duplicates {
			problems = append(problems, fmt.Sprintf("duplicate id %q: %s shadows %s", d.NibID, d.Loaded, d.Shadowed))
		}
		return nil, fmt.Errorf("refusing to migrate a store that does not load cleanly (repair the files below by hand, `nibs check` reports them too, then re-run `nibs migrate`):\n  %s",
			strings.Join(problems, "\n  "))
	}
	return core, nil
}

// storeScan is the outcome of one shared pass over the store's files: how many
// files each chain step still has pending (indexed parallel to
// migrationSteps), the files the scan could not classify as nibs, and the
// files written by a newer nibs (collected so the eventual refusal can carry
// everything the walk saw — see scanStore).
type storeScan struct {
	counts   []int
	problems []scanProblem
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
type newerStoreError struct{ msg string }

func (e *newerStoreError) Error() string { return e.msg }

// scanStore reads each file's front-matter header ONCE and evaluates the
// newer-store refusal plus every CONTENT step's predicate against it — one
// header walk regardless of how many content steps the chain grows. This probe
// runs on every command, so its cost must stay O(files), not O(files × steps).
//
// SHAPE steps are outside that budget: each answers a directory-structure
// question no per-file header can express, so each performs its own pass —
// today one, layoutPendingCount's, whose directory-prefix check short-circuits
// before reading any file. A chain that grew several shape steps would owe this
// note a rethink. A file with a version above nib.CurrentVersion refuses
// the whole scan (error): it was written by a newer nibs and this build must
// not touch the store. The refusal is raised AFTER the walk completes rather
// than aborting at the first such file, so it can name every newer file and
// every other problem the walk collected — aborting mid-walk used to hide a
// coexisting broken file behind the version refusal until the user repaired
// their way to it.
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

	err := forEachNibFile(env, func(path string) error {
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
		if h.version > nib.CurrentVersion {
			// Not this build's business: no step predicate is evaluated for a
			// newer file, only the refusal below.
			scan.newer = append(scan.newer, fmt.Sprintf("%s (format version %d)", storeRelPath(env, path), h.version))
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
		msg := fmt.Sprintf("%d nib file(s) in this store were written by a newer nibs (this build supports up to format version %d); upgrade nibs:\n  %s",
			len(scan.newer), nib.CurrentVersion, strings.Join(scan.newer, "\n  "))
		if len(scan.problems) > 0 {
			lines := make([]string, len(scan.problems))
			for i, p := range scan.problems {
				lines[i] = fmt.Sprintf("%s: %s", p.path, p.reason)
			}
			msg += fmt.Sprintf("\nthe scan also skipped %d file(s) that cannot be read as nibs:\n  %s",
				len(scan.problems), strings.Join(lines, "\n  "))
		}
		return nil, &newerStoreError{msg: msg}
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
	if refusals := wouldRefuse(env, scan); len(refusals) > 0 {
		return &refusalError{refusals: refusals}
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
			return fmt.Errorf("migration %s applied but its detection still fires — the header scan disagrees with the parsed store for:\n  %s\nplease repair these files by hand (or report a nibs bug); until then commands will keep refusing",
				step.name, strings.Join(stuck, "\n  "))
		}
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

// anyShapePending reports whether any pending step changes the store's directory
// structure. It matters to the gates because a pending shape step means the nib
// files are not yet where Core.Load looks, so anything answered by loading the
// store is answered against a store that is not there yet.
func anyShapePending(pending []migrationStep) bool {
	for _, step := range pending {
		if !step.isContent() {
			return true
		}
	}
	return false
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

// migrateGate is one precondition, evaluated identically by the real run and by
// --dry-run's preview.
type migrateGate struct {
	name  string
	check func(migrateEnv, *storeScan) *refusal
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
	{name: "layout-plan", check: gateLayoutPlan},
	{name: "unclassifiable-content", check: gateContentClassifiable},
	{name: "store-loads-cleanly", check: gateStoreLoadsCleanly},
}

// wouldRefuse runs every gate in migrateGates and returns the ones that failed.
// A store with nothing pending is never refused: the command reports it up to
// date before any gate is consulted.
func wouldRefuse(env migrateEnv, scan *storeScan) []refusal {
	if len(scan.pending()) == 0 {
		return nil
	}
	var refusals []refusal
	for _, g := range migrateGates {
		if r := g.check(env, scan); r != nil {
			r.gate = g.name
			refusals = append(refusals, *r)
		}
	}
	return refusals
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
func gateStoreGitClean(env migrateEnv, _ *storeScan) *refusal {
	if migrateAllowDirty {
		return nil
	}
	dirty, isRepo, err := storeGitStateFn(env.nibsRoot)
	if err != nil || !isRepo || !dirty {
		return nil
	}
	return &refusal{
		code: output.ErrValidation,
		reason: fmt.Sprintf("the store at %s has uncommitted git changes; commit or stash them so the pre-migration state is recoverable, or re-run with --allow-dirty",
			env.nibsRoot),
	}
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
func gateLegacyConfigRecoverable(env migrateEnv, _ *storeScan) *refusal {
	if migrateAllowDirty || !legacyConfigExists(env) {
		return nil
	}
	dirty, err := gitIsDirtyFn(env.layout().ProjectDir(), store.LegacyProjectConfigFileName)
	if err != nil || !dirty {
		return nil
	}
	return &refusal{
		code: output.ErrValidation,
		reason: fmt.Sprintf("%s has uncommitted git changes and this migration deletes it; commit or stash it so the pre-migration state is recoverable, or re-run with --allow-dirty",
			env.legacyConfigPath()),
	}
}

// gateLayoutPlan surfaces every refusal the layout step's PLANNING raises: an
// occupied relocation destination, a name collision in data/, two project
// configs, a `nibs.path` naming a store that still exists. Sharing planLayout is
// what lets the preview name them — the step computes the same plan when it runs.
func gateLayoutPlan(env migrateEnv, scan *storeScan) *refusal {
	if !anyShapePending(scan.pending()) {
		return nil
	}
	if _, err := planLayout(env); err != nil {
		return &refusal{code: output.ErrFileError, reason: err.Error()}
	}
	return nil
}

// gateContentClassifiable is the fail-loud gate over files the scan could not
// classify. Applying a CONTENT step around one is destructive: an unreadable file
// silently loses the edges pointing at it (a v0 `blocking:` transfer to a skipped
// target erases the edge from the source while the target never receives it). A
// fence-less document once LOADED as an empty v0 "nib" the v0 step rewrote into a
// nib render; nib.Parse now refuses it, so this gate and the load gate are two
// layers of the same refusal. Which files endanger anything is
// blockingScanProblems' decision.
func gateContentClassifiable(env migrateEnv, scan *storeScan) *refusal {
	blocking := blockingScanProblems(env, scan)
	if len(blocking) == 0 {
		return nil
	}
	return &refusal{
		code: output.ErrFileError,
		reason: fmt.Sprintf("refusing to migrate around %d file(s) that cannot be read as nibs (move them out of the store or repair them, then re-run `nibs migrate`):\n  %s",
			len(blocking), describeScanProblems(blocking)),
	}
}

// gateStoreLoadsCleanly previews loadStoreForMigration's own refusal: a store
// that does not load — an unparseable file, a duplicate id — must not have a
// content step run over it, because the loaded store is then not a faithful
// picture of the directory.
//
// It is skipped while a SHAPE step is pending, and that limit is inherent rather
// than an oversight: Core.Load walks data/ and archive/, so on a pre-layout store
// the nib files are not there yet and this gate would answer for an empty store.
// Evaluating it honestly would mean performing the move first. The run still
// applies it — at the content step's apply, after the layout step — and
// reportDryRun says so rather than letting the silence read as "all clear".
func gateStoreLoadsCleanly(env migrateEnv, scan *storeScan) *refusal {
	pending := scan.pending()
	if !anyContentPending(pending) || anyShapePending(pending) {
		return nil
	}
	if _, err := loadStoreForMigration(env); err != nil {
		return &refusal{code: output.ErrFileError, reason: err.Error()}
	}
	return nil
}

// blockingScanProblems returns the scan problems that must stop a run: the
// files a pending CONTENT step would otherwise be forced to migrate AROUND.
//
// runMigrations' fail-loud gate and reportDryRun's preview of it are ONE
// decision seen from two sides, and they must answer identically or the preview
// predicts an outcome the run does not produce. They have already diverged
// twice by being written twice; sharing this function is what stops a third.
//
// The scoping (deviation #7) is what makes `.nibs/README.md` a legal place for
// a readme. The danger the gate names is a link-rewrite danger — a step that
// rewrites edges must see every file that could hold one — so a problem blocks
// exactly when it is, or is about to become, store CONTENT:
//
//   - already under data/ or archive/: the content steps load it;
//   - unreadable, wherever it sits: the layout step cannot prove it is not a
//     nib, so it moves it into data/ (see layoutMovableFiles) and the content
//     steps meet it there;
//   - fence-less at the store root: provably not a nib, never moved, and
//     invisible to Core.Load once the relayout lands — so it blocks nothing.
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

// describeScanProblems renders one indented "path: reason" line per problem,
// the shape both the refusal and its preview list them in.
func describeScanProblems(problems []scanProblem) string {
	lines := make([]string, len(problems))
	for i, p := range problems {
		lines[i] = fmt.Sprintf("%s: %s", p.path, p.reason)
	}
	return strings.Join(lines, "\n  ")
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
// Unreadable and fence-less files are passed over, mirroring scanStore — they
// were never counted pending, so they can never be what a step failed to fix.
func filesMatching(env migrateEnv, pred func(fmHeader) bool) ([]string, error) {
	var matches []string
	err := forEachNibFile(env, func(path string) error {
		h, err := readFrontMatterHeader(path)
		if err != nil || !h.hasFrontMatter {
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
func forEachNibFile(env migrateEnv, fn func(path string) error) error {
	return nibcore.WalkStoreFiles(env.nibsRoot, func(path string, err error) error {
		if err != nil {
			// A directory that cannot be ENUMERATED stays fatal — unlike a
			// single skippable file, the scan cannot know what it is missing —
			// but with enough context to act on.
			return fmt.Errorf("scanning nibs store %s (fix its permissions, or remove the offending entry): %w", path, err)
		}
		return fn(path)
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
// cannot drift): a legitimate header is a few hundred bytes, and anything the
// scan cannot reach within this bound is over the parse cap too — not a nib,
// or a file the load-time diagnostics will name anyway.
const maxHeaderScanBytes = nib.MaxFrontMatterBytes

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
	// version is the value of a top-level `version:` line; 0 when the key is
	// absent or its value is not an integer (absent = 0 = legacy, matching
	// nib.Nib.Version; a garbage value surfaces later as an unparseable file).
	version int
	// priority is the unquoted value of a top-level `priority:` line.
	priority string
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
// Best-effort by design: a header larger than maxHeaderScanBytes ends the
// scan with whatever was extracted so far — such a file is over the parse cap
// too, so the load-time diagnostics will name it. The scan only decides
// whether a migration is PENDING; the authoritative parse (and its errors)
// happen in the step's apply, and runMigrations' post-condition catches any
// residual scan/parse disagreement rather than letting it wedge the CLI.
func readFrontMatterHeader(path string) (fmHeader, error) {
	f, err := os.Open(path)
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
	for sc.Scan() {
		// line keeps its leading whitespace: the key checks below are
		// positional (a top-level key starts at column 0), so only the
		// fence compare may TrimSpace.
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "---" {
			break // closing fence, whitespace-padded or not (fence rule above)
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || key == "" || key[0] == ' ' || key[0] == '\t' || key[0] == '#' {
			continue // not a top-level key line (nested item, comment, prose)
		}
		switch key {
		case "version":
			if v, err := strconv.Atoi(unquoteHeaderValue(value)); err == nil {
				h.version = v
			}
		case "priority":
			h.priority = unquoteHeaderValue(value)
		}
	}
	if err := sc.Err(); err != nil {
		return h, err
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

		if refusals := wouldRefuse(env, scan); len(refusals) > 0 {
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
func reportGitPosture(env migrateEnv) (storeIsRepo bool) {
	_, isRepo, err := storeGitStateFn(env.nibsRoot)
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
		if _, gitErr := gitIsDirtyFn(env.layout().ProjectDir(), store.LegacyProjectConfigFileName); gitErr != nil {
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
// One gate genuinely cannot be answered in advance, and the preview says so
// rather than letting its silence read as all-clear: see gateStoreLoadsCleanly.
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
	if storeRelocationPending(env) {
		ui.Printf("  the layout step will first move the store %s to %s, where nibs can find it\n",
			env.nibsRoot, filepath.Join(env.layout().ProjectDir(), store.DirName))
	}
	if refusals := wouldRefuse(env, scan); len(refusals) > 0 {
		ui.Printf("Warning: the real run will refuse — %d precondition(s) are not met:\n", len(refusals))
		for _, r := range refusals {
			ui.Printf("  %s: %s\n", r.gate, r.reason)
		}
	} else if anyContentPending(pending) && anyShapePending(pending) {
		ui.Printf("Note: the content steps' own precondition — every file under %s/ parses and no id is duplicated — can only be checked once the layout step has moved the files there, so it is not previewed here.\n",
			store.DataDirName)
	}
	ui.Println("Stop any running `nibs serve` before migrating.")
	return nil
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
	rootCmd.AddCommand(migrateCmd)
}
