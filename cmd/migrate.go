package cmd

import (
	"bufio"
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
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

// migrateEnv carries the filesystem coordinates a migration step operates on.
// cfg is the project configuration the CLI resolved (the same one every other
// command runs under): content steps load a scoped throwaway Core with it so
// short-form link ids canonicalize under the project's real prefix.
type migrateEnv struct {
	nibsRoot string
	cfg      *config.Config
}

// newMigrateEnv builds the env for a resolved store root.
func newMigrateEnv(nibsRoot string, cfg *config.Config) migrateEnv {
	return migrateEnv{nibsRoot: nibsRoot, cfg: cfg}
}

// logf is the migration engine's progress sink, so runMigrations stays
// testable without capturing stdout.
type logf func(format string, a ...any)

// migrationStep is one ordered entry in the migration chain.
//
// pred answers "does this FILE need the step?" over its scanned front-matter
// header — a pure predicate, never a Core, never a full YAML parse, and NEVER
// a write. Detection ("does this store need the step?") is one shared
// filesystem walk (scanStore) that reads each file's header ONCE and evaluates
// every step's pred plus the newer-store check against it, so the per-command
// pre-run probe stays O(files) no matter how many steps the chain grows.
// Detection runs on every command (the PersistentPreRunE refusal) and a second
// time under the store lock, where detect-gates-apply is what makes every step
// idempotent and a crashed run resumable by re-running.
//
// apply performs the migration and is FAIL-LOUD: the first error aborts the
// run. Content steps load a scoped throwaway Core (the existing load pipeline
// does canonicalization) rather than growing a second parse pipeline. The
// *StoreLock is runMigrations' whole-run store lock, threaded through as the
// Core migration methods' proof-of-lock parameter.
type migrationStep struct {
	name, title string
	pred        func(fmHeader) bool
	apply       func(migrateEnv, *nibcore.StoreLock, logf) error
}

// migrationSteps is the ordered migration chain. A future format bump (v2)
// appends one more entry with no engine change — but note the watcher's
// legacy-shape breadcrumb (handleChanges in internal/nibcore/watcher.go)
// restates this chain's detection for files arriving into a LIVE serve:
// below-current versions are covered there by nib.CurrentVersion, while a new
// step NOT keyed on the version must be mirrored into that condition by hand,
// or its files arrive with no breadcrumb.
//
// The load-bearing invariant is NOT the entries' order — it is that ONLY the
// version-bump step may write the version stamp. `version: 1` is v0-blocking's
// completion record: a step that stamped a still-v0 file would mark its
// `blocking:` edges migrated without transferring them, and because v0
// detection keys on the version, nothing would ever return to finish the job —
// the edges silently vanish from every view (the retired load-time migration
// carried the same guard, refusing to persist exactly that half-migrated
// shape). Because NormalizeLegacyPriorities honors the rule — a still-v0 file
// it rewrites renders `version: 0`, so isV0Header keeps firing — the chain
// converges to the same store whichever order the steps run in; v0-blocking
// runs first simply so one run converts a doubly-legacy file in one pass.
var migrationSteps = []migrationStep{
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
func applyPriorityDeferred(env migrateEnv, lock *nibcore.StoreLock, log logf) error {
	core, err := loadStoreForMigration(env)
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
func applyV0Blocking(env migrateEnv, lock *nibcore.StoreLock, log logf) error {
	core, err := loadStoreForMigration(env)
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
	core := nibcore.New(env.nibsRoot, env.cfg)
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

// scanStore walks the store ONCE, reading each file's front-matter header a
// single time and evaluating the newer-store refusal plus every chain step's
// predicate against it. One walk regardless of how many steps the chain grows
// — this probe runs on every command, so its cost must stay O(files), not
// O(files × steps). A file with a version above nib.CurrentVersion refuses
// the whole scan (error): it was written by a newer nibs and this build must
// not touch the store. The refusal is raised AFTER the walk completes rather
// than aborting at the first such file, so it can name every newer file and
// every other problem the walk collected — aborting mid-walk used to hide a
// coexisting broken file behind the version refusal until the user repaired
// their way to it.
func scanStore(env migrateEnv) (*storeScan, error) {
	scan := &storeScan{counts: make([]int, len(migrationSteps))}
	err := forEachNibFile(env, func(path string) error {
		h, err := readFrontMatterHeader(path)
		if err != nil {
			// Per-file degradation, matching Core.Load: skip with the file's
			// name kept, never abort the probe for every command.
			scan.problems = append(scan.problems, scanProblem{path: storeRelPath(env, path), reason: err.Error()})
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
			if step.pred(h) {
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
	lock, err := nibcore.AcquireStoreLock(env.nibsRoot)
	if err != nil {
		return fmt.Errorf("acquiring store lock: %w", err)
	}
	defer func() { _ = lock.Release() }()

	scan, err := scanStore(env)
	if err != nil {
		return err
	}
	pending := scan.pending()

	// FAIL-LOUD GATE, same posture as loadStoreForMigration: applying around
	// a file the scan could not classify is destructive — an unreadable file
	// silently loses edges pointing at it. (A fence-less document once LOADED
	// as an empty v0 "nib" the v0 step rewrote into a nib render; nib.Parse
	// now refuses it, so this gate plus the load gate are two layers of the
	// same refusal.) Refuse while anything is actually pending; a store with
	// nothing to apply never reaches here (the command reports up-to-date
	// first).
	if len(pending) > 0 && len(scan.problems) > 0 {
		lines := make([]string, len(scan.problems))
		for i, p := range scan.problems {
			lines[i] = fmt.Sprintf("%s: %s", p.path, p.reason)
		}
		return fmt.Errorf("refusing to migrate around %d file(s) that cannot be read as nibs (move them out of the store or repair them, then re-run `nibs migrate`):\n  %s",
			len(scan.problems), strings.Join(lines, "\n  "))
	}

	for _, step := range pending {
		log("applying %s: %s", step.name, step.title)
		if err := step.apply(env, lock, log); err != nil {
			return fmt.Errorf("migration step %s failed: %w", step.name, err)
		}
	}
	for _, step := range pending {
		stuck, err := filesMatching(env, step.pred)
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

// refuseIfMigrationPending is the gate PersistentPreRunE runs between
// resolveNibsPath and core.Load(): every command except the skip-listed ones
// refuses to run on a store with pending migrations, naming the fix.
func refuseIfMigrationPending(nibsRoot string, cfg *config.Config) error {
	pending, err := pendingMigrations(newMigrateEnv(nibsRoot, cfg))
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

// forEachNibFile walks every store .md file through the SHARED store-content
// definition (nibcore.WalkStoreFiles — subdirectories included so archived
// nibs migrate too, dot directories pruned). Core.Load walks through the same
// function, so what the scans probe and what loads can never disagree; only
// the enumeration-failure posture is this walk's own.
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
		cfg, err := loadCLIConfig()
		if err != nil {
			return err
		}
		nibsRoot, err := resolveNibsPath(nibsPath, cfg)
		if err != nil {
			return err
		}
		env := newMigrateEnv(nibsRoot, cfg)

		// One shared scan serves both the pending decision and --dry-run's
		// per-step counts — the same walk pendingMigrations performs.
		scan, err := scanStore(env)
		if err != nil {
			return migrateCmdError(err)
		}
		// Name every file the scan skipped, up front and on every path: the
		// user must learn about them from the tool, not from a surprise later.
		for _, p := range scan.problems {
			ui.Printf("Note: skipping %s: %s — migration will not treat it as a nib.\n", p.path, p.reason)
		}
		pending := scan.pending()
		if migrateDryRun {
			return reportDryRun(scan)
		}
		if len(pending) == 0 {
			ui.Println("Store is up to date; no migrations pending.")
			return nil
		}

		// Git safety net: refuse a dirty store repo — the pre-migration state
		// must be recoverable, and an uncommitted change would be entangled
		// with the migration's rewrite in the diff. isRepo is "a repository
		// genuinely covers the store", not merely "inside a repo": a store
		// gitignored by an enclosing repository has no rollback net (see
		// realStoreGitState) and gets the backup suggestion instead.
		dirty, isRepo, gitErr := storeGitStateFn(env.nibsRoot)
		if gitErr != nil {
			// A genuine git failure means the safety net cannot be evaluated.
			// Say so — silently proceeding as if the store were clean would
			// disable the net without a trace — and fall back to the backup
			// suggestion rather than blocking migration on git's availability.
			ui.Printf("Warning: could not determine the store's git state (%v); the dirty-store safety check was skipped.\n", gitErr)
			isRepo = false
		}
		if isRepo && dirty && !migrateAllowDirty {
			return cmdError(false, output.ErrValidation,
				"the store at %s has uncommitted git changes; commit or stash them so the pre-migration state is recoverable, or re-run with --allow-dirty", env.nibsRoot)
		}
		if !isRepo {
			ui.Printf("Note: the store at %s is not protected by git; consider backing it up before migrating.\n", env.nibsRoot)
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

// migrateCmdError codes migrate's RunE-level failures for the CLI's error
// boundary (reportExitError), matching upgrade.go's discipline: the
// newer-store refusal is a VALIDATION error (exit 2, like the dirty-git
// refusal — the user must act before migrate may run), while everything else
// reaching RunE from the probe or the run is store/load I/O (exit 5). The
// refusals issued from the shared PersistentPreRunE gate are deliberately
// left uncoded, matching that gate's siblings.
func migrateCmdError(err error) error {
	var ns *newerStoreError
	if errors.As(err, &ns) {
		return cmdError(false, output.ErrValidation, "%v", err)
	}
	return cmdError(false, output.ErrFileError, "%v", err)
}

// reportDryRun lists each pending step with its per-file count, modifying
// nothing. The counts come from the caller's scan — the same single walk that
// decided what is pending. When scan problems coexist with the pending steps,
// the preview also announces the refusal the real run will raise (see
// runMigrations' fail-loud gate) — without it, the pending counts plus an
// unconnected skip note read as "the skipped file is harmlessly excluded",
// and the real run's refusal comes as a surprise.
func reportDryRun(scan *storeScan) error {
	if len(scan.pending()) == 0 {
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
	if len(scan.problems) > 0 {
		lines := make([]string, len(scan.problems))
		for i, p := range scan.problems {
			lines[i] = fmt.Sprintf("%s: %s", p.path, p.reason)
		}
		ui.Printf("Warning: the real run will refuse to migrate around %d file(s) that cannot be read as nibs; move them out of the store or repair them first:\n  %s\n",
			len(scan.problems), strings.Join(lines, "\n  "))
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
