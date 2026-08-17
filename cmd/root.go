package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/safetext"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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
		// migrate is skip-listed because it must run on the very stores this
		// hook refuses below; it resolves config and the store root itself.
		if cmd.Name() == "init" || cmd.Name() == "prime" || cmd.Name() == "version" ||
			cmd.Name() == "catalog" || cmd.Name() == "cheat" || cmd.Name() == "upgrade" ||
			cmd.Name() == "migrate" ||
			(cmd.Name() == "query" && querySchemaOnly) {
			return nil
		}

		root, cfg, err := resolveCLIStore()
		if err != nil {
			return err
		}

		// Refuse to touch a store with pending migrations (or one written by a
		// newer nibs) BEFORE Load ever sees it: migration is explicit, so no
		// command may operate on — let alone rewrite — an unmigrated store.
		//
		// Plain `nibs check` is exempt: it is the read-only diagnostic built
		// for exactly the store states this refusal creates (migrate's own
		// unclean-store refusal points at it), so gating it would send the
		// user in a circle — migrate says "run check", check says "run
		// migrate" — with no working diagnostic for the stores that most need
		// one. Only --fix writes, so only --fix stays gated.
		gated := cmd.Name() != "check" || checkFix
		if gated {
			if err := refuseIfMigrationPending(root); err != nil {
				return err
			}
		}

		core := nibcore.New(root, cfg)
		if err := core.Load(); err != nil {
			return fmt.Errorf("loading nibs: %w", err)
		}

		// Getting past the gate IS the answer to "does this store need
		// migration?", so record it rather than making a command re-scan for it
		// (see App.MigrationGatePassed).
		cmd.SetContext(withApp(cmd.Context(), &App{Core: core, MigrationGatePassed: gated}))
		return nil
	},
	// Runs only after a subcommand succeeds (Cobra skips PostRun on error).
	// Best-effort update notice; never blocks or fails the command.
	PersistentPostRun: func(cmd *cobra.Command, _ []string) {
		maybeNotifyUpdate(cmd)
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

	rootCmd.PersistentFlags().StringVar(&nibsPath, "nibs-path", "", "Path to the .nibs store directory (overrides discovery and NIBS_PATH env var)")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to a store's config file; names the store through its directory (cannot be combined with --nibs-path or NIBS_PATH)")
	installFlagSuggestions(rootCmd)
}

// resolveCLIStore resolves the store every command operates on and loads THAT
// store's config. The two answers come as a pair on purpose: the store
// directory holds its own config, so pointing nibs at another project's store
// carries that project's prefix, id length and defaults with it — a store can
// never be read under a neighboring project's vocabulary. resolveStoreDir keeps
// that invariant true by refusing --config together with --nibs-path/NIBS_PATH,
// the one combination that would supply the store and the config from different
// projects.
//
// Shared by PersistentPreRunE and the skip-listed migrate command so the two
// can never resolve a different store.
func resolveCLIStore() (string, *config.Config, error) {
	storeDir, err := resolveStoreDir()
	if err != nil {
		return "", nil, err
	}

	// --config names a config file explicitly; without it the store's own
	// config.yml is read. Either way the user config layers underneath.
	if configPath != "" {
		cfg, err := config.LoadFromExplicitPathWithUserConfig(configPath)
		if err != nil {
			return "", nil, fmt.Errorf("loading config from %s: %w", configPath, err)
		}
		return storeDir, cfg, nil
	}
	cfg, err := config.LoadStoreWithUserConfig(storeDir)
	if err != nil {
		return "", nil, fmt.Errorf("loading config: %w", err)
	}
	return storeDir, cfg, nil
}

// resolveStoreDir determines the `.nibs` store directory.
// Precedence: --nibs-path flag > NIBS_PATH env var > --config's directory >
// an upward search from the cwd for a `.nibs` directory.
//
// A directory named EXPLICITLY — by any of the first three routes — must carry
// positive evidence that it IS a store (see looksLikeStore). Existence alone is
// not enough: a path aimed one level too high resolves to the project tree, and
// `nibs migrate` would then move and rewrite every front-mattered .md it finds
// there while the real store went untouched.
func resolveStoreDir() (string, error) {
	// --config and an explicitly named store are MUTUALLY EXCLUSIVE. Supplied
	// together they break resolveCLIStore's invariant that a store is always read
	// under its own vocabulary: --nibs-path (or NIBS_PATH) wins for the store
	// while --config wins for the config, so `nibs new --nibs-path A --config
	// B/config.yml` writes into store A under B's prefix and id_length. Ids
	// derive from filenames, so that is a persisted misnaming, not a display
	// artifact.
	//
	// Given ALONE, --config stays supported and simply names the store through
	// its containing directory — that is the whole reason the combination is
	// redundant rather than useful.
	if configPath != "" {
		if nibsPath != "" {
			return "", fmt.Errorf("--config and --nibs-path cannot be combined: the config lives inside the store, so each names a store and together they would read %s under %s's prefix and id length; pass --nibs-path %s alone",
				nibsPath, filepath.Dir(configPath), nibsPath)
		}
		if envPath := os.Getenv("NIBS_PATH"); envPath != "" {
			return "", fmt.Errorf("--config cannot be combined with NIBS_PATH (%s): each names a store, so together they would read %s under %s's prefix and id length; unset NIBS_PATH, or drop --config and pass --nibs-path %s",
				envPath, envPath, filepath.Dir(configPath), filepath.Dir(configPath))
		}
	}

	// --config now names a config INSIDE the store. The pre-layout `.nibs.yml`
	// sat beside the store, so its directory is the PROJECT — the single most
	// dangerous thing to mistake for a store, and the invocation the pre-layout
	// docs recommended for working against another project.
	if configPath != "" && filepath.Base(configPath) == store.LegacyProjectConfigFileName {
		return "", fmt.Errorf("--config now names a store's %s; %s is the pre-layout config, which sits beside the store rather than inside it — pass --nibs-path %s instead",
			store.ConfigFileName, configPath, filepath.Join(filepath.Dir(configPath), store.DirName))
	}
	// The flag's ONLY remaining meaning is "name the store through this file's
	// directory", so the file it names must be the config that store actually
	// reads. Any other basename splits the two apart again inside ONE flag: the
	// store resolves to the directory while resolveCLIStore reads the named file,
	// so `--config <store>/config.yml.bak` persists nibs into the real store under
	// the backup's prefix and id length. Ids derive from filenames, so that is a
	// persisted misnaming, not a display artifact — the same harm the
	// --config/--nibs-path exclusion above refuses.
	if configPath != "" && filepath.Base(configPath) != store.ConfigFileName {
		return "", fmt.Errorf("--config must name a store's %s (got %s): it names the store through its directory, so a differently named file would read %s under another file's prefix and id length; rename it, or pass --nibs-path %s",
			store.ConfigFileName, configPath, filepath.Dir(configPath), filepath.Dir(configPath))
	}

	explicit := nibsPath
	if explicit == "" {
		explicit = os.Getenv("NIBS_PATH")
	}
	if explicit == "" && configPath != "" {
		// The config lives inside the store, so naming the config names the
		// store — which is why --config alone is enough to work against
		// another project.
		explicit = filepath.Dir(configPath)
	}

	if explicit != "" {
		// Normalize ONCE, here, because every downstream derivation is lexical:
		// filepath.Dir("<p>/.nibs/") is "<p>/.nibs", so a trailing slash — what
		// shell tab completion produces — shifted store.Layout.ProjectDir() one
		// level INTO the store, hid the project's `.nibs.yml` from the migration
		// gate, and let a nib persist under the default prefix.
		explicit = filepath.Clean(explicit)
		if abs, err := filepath.Abs(explicit); err == nil {
			explicit = abs
		}
		if info, err := os.Stat(explicit); err != nil || !info.IsDir() {
			return "", fmt.Errorf("nibs store does not exist or is not a directory: %s", explicit)
		}
		is, err := looksLikeStore(explicit)
		if err != nil {
			// "Cannot determine" must never be reported as "no evidence": the
			// message below tells the user to run `nibs init` here, and doing that
			// over a real store whose config merely could not be read creates a
			// second, empty store beside their data.
			return "", fmt.Errorf("cannot tell whether %s is a nibs store: %w; repair or remove that file, then re-run", explicit, err)
		}
		if !is {
			// A `.nibs.yml` that NAMES this directory but no artifact inside it is
			// its own answer: "no .nibs.yml beside it names it" would be false, and
			// `nibs init` is the wrong advice when the naming config is real. Say
			// which half of the evidence is missing.
			if named, namedErr := legacyConfigNamesStore(explicit); namedErr == nil && named {
				return "", fmt.Errorf("%s is named as this project's store by %s, but nothing in it was written by nibs (no markdown file carries a nibs `status:`), and `nibs migrate` will not move and rewrite a directory on a config's say-so alone; if these really are your nibs, create %s, move them into it, remove the `nibs.path` key from %s, then run `nibs migrate`",
					explicit, filepath.Join(filepath.Dir(explicit), store.LegacyProjectConfigFileName),
					filepath.Join(filepath.Dir(explicit), store.DirName),
					filepath.Join(filepath.Dir(explicit), store.LegacyProjectConfigFileName))
			}
			return "", fmt.Errorf("%s is not a nibs store: it holds no %s that parses as one, and no %s beside it names it; name the store directory itself (e.g. --nibs-path %s), or run `nibs init` there",
				explicit, store.ConfigFileName, store.LegacyProjectConfigFileName,
				filepath.Join(explicit, store.DirName))
		}
		return explicit, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting current directory: %w", err)
	}
	storeDir, err := store.FindStore(cwd)
	if err != nil {
		return "", fmt.Errorf("searching for a nibs store: %w", err)
	}
	if storeDir == "" {
		return "", noStoreFoundError(cwd)
	}
	return storeDir, nil
}

// noStoreFoundError explains a failed upward walk.
//
// "Run nibs init" is the right answer only when there is no nibs project here
// at all. A PRE-LAYOUT project whose data lived outside `.nibs` — the retired
// `nibs.path` key — has no store directory either, and for it that advice is
// actively harmful: it creates an empty store with a derived prefix beside the
// real data and strands it. So the walk that fails is followed by a walk for
// the pre-layout config, and when one is there the message names it and the
// remedy that actually converges.
//
// For the shapes `nibs migrate` can relocate itself, that remedy is
// `nibs migrate --nibs-path <dataDir>` — migrating the store WHERE IT IS. The
// layout step then moves it to `<project>/.nibs`, which is what makes the project
// discoverable afterwards; telling the user to move the files by hand first only
// produced a store whose `.nibs.yml` still named the emptied directory, and no
// filesystem action can make a config VALUE equal `.nibs`.
//
// The command is printed ONLY when the store-evidence guard would accept the
// directory (hasLegacyStoreShape, the same predicate resolveStoreDir consults) —
// otherwise this message would prescribe a command the tool refuses, and its only
// other advice is the `nibs init` it calls harmful. `nibs.path` shapes the guard
// deliberately does not accept — a value naming somewhere other than an immediate
// subdirectory of the project, or a directory holding nothing nibs wrote — get the
// manual remedy instead, which converges for every shape: removing the retired key
// is what stops the relocation refusing over a directory it can no longer account
// for.
//
// The declared VALUE is echoed through sanitizeFileText (untrusted file content
// quoted back, so collapsed and bounded), while the paths that appear as COMMAND
// ARGUMENTS go through stripControlChars and shell quoting only — collapsing or
// truncating those would corrupt the one string the user has to run.
func noStoreFoundError(cwd string) error {
	legacy, err := store.FindLegacyProjectConfig(cwd)
	if err != nil || legacy == "" {
		return fmt.Errorf("no %s directory found in %s or any parent directory (run 'nibs init' to create one)", store.DirName, cwd)
	}
	projectDir := filepath.Dir(legacy)
	target := filepath.Join(projectDir, store.DirName)
	declared, readErr := config.RetiredNibsPath(legacy)
	if readErr != nil {
		// Absence of evidence and unreadable evidence lead to opposite advice, so
		// they must not collapse: `nibs init` here could strand a real store this
		// file names.
		return fmt.Errorf("no %s directory found in %s or any parent directory, and %s — the pre-layout config that would say where this project's nibs live — cannot be read: %v; repair or remove it, then re-run (do NOT run `nibs init` until you know, it would create an empty store beside data that may already exist)",
			store.DirName, cwd, legacy, readErr)
	}
	if declared == "" {
		return fmt.Errorf("no %s directory found in %s or any parent directory, but %s is a pre-layout nibs config with no store beside it; create %s and move this project's nib files into it, then run `nibs migrate`",
			store.DirName, cwd, legacy, target)
	}
	dataDir := declared
	if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(projectDir, dataDir)
	}
	if ok, evErr := hasLegacyStoreShape(dataDir); evErr == nil && ok {
		return fmt.Errorf("no %s directory found in %s or any parent directory, but %s sets the retired `nibs.path: %s`; this project's nibs live in %s — run `nibs migrate --nibs-path %s`, which moves that store to %s and relocates the config into it (do NOT run `nibs init`, which would create an empty store beside the real data)",
			store.DirName, cwd, legacy, sanitizeFileText(declared),
			stripControlChars(dataDir), shellArg(dataDir), target)
	}
	why := "which `nibs migrate` will not relocate for you because it is not an immediate subdirectory of " + projectDir
	if named, namedErr := legacyConfigNamesStore(dataDir); namedErr == nil && named {
		why = "which `nibs migrate` will not relocate for you because nothing in it was written by nibs (no markdown file carries a nibs `status:`)"
	}
	return fmt.Errorf("no %s directory found in %s or any parent directory, but %s sets the retired `nibs.path: %s`; this project's nibs live in %s, %s — create %s, move this project's nib files from %s into it, remove the `nibs.path` key from %s, then run `nibs migrate` (do NOT run `nibs init`, which would create an empty store beside the real data)",
		store.DirName, cwd, legacy, sanitizeFileText(declared),
		stripControlChars(dataDir), why, target, stripControlChars(dataDir), legacy)
}

// looksLikeStore reports whether dir carries positive evidence of being a nibs
// store, so an explicitly named directory can never silently resolve to an
// ordinary project directory (see resolveStoreDir).
//
// This is structurally an authorization check: it decides whether `nibs migrate`
// may move and rewrite a whole subtree. So the evidence must be something only a
// nibs store PRODUCES, not something a directory might merely be CALLED. Any ONE
// of these suffices:
//
//   - the directory is named `.nibs` — the name IS the marker store.FindStore
//     recognizes, and an empty one is a legal freshly created store;
//   - it holds a config.yml that PARSES as a nibs config: every config
//     `nibs init` writes has a top-level `nibs:` mapping, and the current layout
//     puts it inside the store. This is what keeps `nibs init --nibs-path <dir>`
//     followed by `nibs list --nibs-path <dir>` working for a store the user
//     deliberately put somewhere other than `.nibs`;
//   - a pre-layout `.nibs.yml` beside it NAMES it through the retired
//     `nibs.path` key — see hasLegacyStoreShape. That clause is what keeps a
//     pre-layout store outside `.nibs` reachable by `nibs migrate`, the very
//     population the migration exists to serve.
//
// Deliberately NOT evidence, and each one was an accepted shape that resolved an
// ordinary project directory as a store:
//
//   - a bare `data/` or `archive/` subdirectory. `data/` is a standard Hugo
//     directory and `archive/` is unremarkable anywhere; a Hugo site root had
//     its blog post moved into `data/` and rewritten as a nib render. A store
//     with either but no config.yml can only come from a half-deleted store,
//     and `.nibs`-named stores — every store nibs itself creates or migrates
//     to — are covered by the name clause regardless;
//   - a file merely NAMED config.yml, never parsed. The name is among the most
//     common in software projects, and the check did not even exclude a
//     DIRECTORY by that name;
//   - "a `.nibs.yml` sits beside it and it holds some `*.md`". That accepted
//     any docs/ or notes/ directory of an unmigrated project, and `nibs migrate`
//     then deleted the project's real `.nibs.yml` and relocated it there.
//
// The answer is THREE-WAY. An error means the evidence EXISTS but could not be
// established — a config.yml over config.MaxConfigBytes, a `.nibs.yml` whose
// permissions deny the read. That must never be reported as "no evidence",
// because the refusal for no evidence tells the user to run `nibs init` here,
// over data that is really there.
func looksLikeStore(dir string) (bool, error) {
	if filepath.Base(dir) == store.DirName {
		return true, nil
	}
	switch ok, err := parsesAsNibsConfig(store.NewLayout(dir).ConfigPath()); {
	case err != nil:
		return false, err
	case ok:
		return true, nil
	}
	return hasLegacyStoreShape(dir)
}

// parsesAsNibsConfig reports whether path is a regular file holding YAML with a
// top-level `nibs:` MAPPING — the shape of every config `nibs init` writes
// (config.Config always marshals its `nibs` key), and the one artifact a nibs
// store cannot be mistaken about. A directory, a dangling symlink, unparseable
// YAML, or a document whose `nibs` key is a scalar all read as "not a store":
// each of those is a DETERMINATE no.
//
// A file that is there but whose bytes could not be obtained — permissions, or
// over config.MaxConfigBytes — returns the error instead, because a size refusal
// reported as absence of evidence made a real store answer "is not a nibs store
// … or run `nibs init` there".
func parsesAsNibsConfig(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	data, err := config.ReadConfigFile(path)
	if err != nil {
		return false, err
	}
	// A node tree rather than a struct probe: yaml.v3 populates a struct's
	// *yaml.Node field with a zero-Kind node, so "the key is a mapping" is not
	// answerable that way. mappingValue is the same accessor the layout step's
	// rewrite uses.
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, nil
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return false, nil
	}
	nibs := mappingValue(doc.Content[0], "nibs")
	return nibs != nil && nibs.Kind == yaml.MappingNode, nil
}

// hasLegacyStoreShape reports whether dir is a store `nibs migrate` may relocate:
// a pre-layout project's `.nibs.yml` NAMES it through the retired `nibs.path`
// key, AND something inside it was written by nibs (see
// declaredStoreCorroborated).
//
// PARENT-ONLY, DELIBERATELY. The `.nibs.yml` is looked for in dir's PARENT and
// nowhere else, so only a store that is an immediate subdirectory of the project
// can be confirmed this way. The retired key accepted more — a nested value like
// `docs/nibs`, an absolute path, `.`, `..` — and this predicate refuses all of
// them on purpose:
//
//   - it is an authorization decision (what it accepts, `nibs migrate` may
//     relocate and rewrite), and requiring the naming config to sit in the named
//     directory's parent is what makes `nibs.path: ..` or `/etc` unable to
//     authorize anything outside the project;
//   - the migration engine derives the project from the store's parent
//     (store.Layout.ProjectDir), so for a store that is not a direct child the
//     relocation target and the `.nibs.yml` it must delete would both be wrong.
//
// The shapes refused here are still SERVED — noStoreFoundError prints a manual
// remedy for them, and it prints the `--nibs-path` command only for the shapes
// this predicate accepts, so the tool never prescribes a command it refuses.
//
// A `.nibs.yml` carrying no `nibs.path` describes a store at `<project>/.nibs`,
// which looksLikeStore's name clause already accepts, so there is nothing for
// this clause to add there.
func hasLegacyStoreShape(dir string) (bool, error) {
	named, err := legacyConfigNamesStore(dir)
	if err != nil || !named {
		return false, err
	}
	return declaredStoreCorroborated(dir)
}

// legacyConfigNamesStore reports whether the pre-layout `.nibs.yml` beside dir
// names dir itself through the retired `nibs.path` key. It is the NAMING half of
// hasLegacyStoreShape, split out so a refusal can say which half failed.
func legacyConfigNamesStore(dir string) (bool, error) {
	projectDir := filepath.Dir(dir)
	legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
	info, err := os.Stat(legacy)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, nil
	}
	declared, err := config.RetiredNibsPath(legacy)
	if err != nil {
		return false, err
	}
	if declared == "" {
		return false, nil
	}
	if !filepath.IsAbs(declared) {
		declared = filepath.Join(projectDir, declared)
	}
	return sameDir(declared, dir), nil
}

// declaredStoreCorroborated reports whether dir holds anything only nibs writes,
// so a `.nibs.yml` cannot authorize a relocation on its own say-so.
//
// The naming config is untrusted content: a cloned repository chooses its own
// `nibs.path`, and pre-layout `nibs init` NEVER wrote a value other than `.nibs`
// — so a config pointing somewhere else is always hand-authored. Without
// corroboration a repository could name any of its own subdirectories and have
// nibs print `nibs migrate --nibs-path <that dir>`, which moves every
// front-mattered .md under it into data/ and rewrites each one as a nib render.
//
// The corroborating artifact is a `status:` from the hardcoded enum in a file's
// front matter: nib.Render writes it into every nib, while Hugo, Jekyll and
// docs-site front matter does not carry it. Deliberately NOT keyed on the id
// matching the config's prefix and id length — a project that changed its prefix
// keeps nibs named under the old one, and refusing its real store would be worse
// than the risk this closes.
//
// A directory with NO markdown at all is corroborated: a freshly created store
// legitimately holds nothing, and there is nothing there to rewrite.
func declaredStoreCorroborated(dir string) (bool, error) {
	sawMarkdown := false
	err := nibcore.WalkStoreFiles(dir, func(path string, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		sawMarkdown = true
		h, hErr := readFrontMatterHeader(path)
		if hErr != nil || !h.hasFrontMatter {
			return nil
		}
		if config.IsKnownStatus(h.status) {
			return errStoreCorroborated
		}
		return nil
	})
	if errors.Is(err, errStoreCorroborated) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return !sawMarkdown, nil
}

// errStoreCorroborated stops declaredStoreCorroborated's walk at the first nib it
// finds; it never reaches a caller.
var errStoreCorroborated = errors.New("nib file found")

// shellArg renders a path for a copy-pasteable command line, quoting it when it
// carries a character a POSIX shell would split or expand. Control characters are
// neutralized first (see stripControlChars) but nothing is collapsed or
// truncated: this is the argument the user has to run, so it must survive intact.
func shellArg(path string) string {
	clean := stripControlChars(path)
	if clean != "" && !strings.ContainsAny(clean, " \t\"'$&|;<>()*?[]{}#!~`\\") {
		return clean
	}
	return "'" + strings.ReplaceAll(clean, "'", `'\''`) + "'"
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
//
// Positional-argument (arg-count) errors reach this boundary already coded:
// commands wire their Args field to the codedNoArgs/codedExactArgs/
// codedMinimumNArgs/codedMaximumNArgs helpers (cmd/args.go) — or, for the two
// commands with bespoke arg logic (archive, query), an inline validator that
// routes through cmdError — never stock cobra.NoArgs/ExactArgs/etc., so an arity
// violation is a VALIDATION_ERROR (exit 2) and, for --json commands, the {error}
// envelope — matching value-validation errors instead of Cobra's plain-text
// exit 1.
//
// The writer is wrapped in safetext.Writer, which makes this a STRUCTURAL
// boundary for file-sourced text rather than one every message has to remember:
// refusals here quote filenames and front-matter scalars an attacker may choose,
// and unlike stdout this channel carries no styled output, so nothing is lost by
// neutralizing it wholesale. Newlines survive — multi-file refusals list one file
// per line.
func reportExitError(stderr io.Writer, err error) int {
	if err == nil {
		return output.ExitOK
	}
	stderr = safetext.NewWriter(stderr)
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

// filterReleasedBlockers returns shallow copies of the given nibs with
// satisfied blockers removed from BlockedBy for display purposes.
// The original in-memory nibs from Core are not mutated. It applies the same
// convention (config.StatusReleasesDependents) the blocking graph uses, so a
// completed or scrapped blocker drops out while a deferred one stays — the
// set-aside work is coming back, so it still blocks.
func filterReleasedBlockers(nibs []*nib.Nib, reader graph.NibReader) []*nib.Nib {
	result := make([]*nib.Nib, len(nibs))
	for i, b := range nibs {
		result[i] = filterReleasedBlockersOne(b, reader)
	}
	return result
}

// filterReleasedBlockersOne returns a shallow copy of the nib with satisfied
// blockers removed from BlockedBy. The original nib is not mutated.
func filterReleasedBlockersOne(b *nib.Nib, reader graph.NibReader) *nib.Nib {
	if len(b.BlockedBy) == 0 {
		clone := *b
		return &clone
	}
	active := make([]string, 0, len(b.BlockedBy))
	for _, blockerID := range b.BlockedBy {
		if blocker, err := reader.Get(blockerID); err == nil {
			if !reader.Config().StatusReleasesDependents(blocker.Status) {
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
