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
// an upward search from the cwd for the NEAREST nibs marker.
//
// A directory named EXPLICITLY — by any of the first three routes — must carry
// positive evidence that it IS a store (see looksLikeStore). Existence alone is
// not enough: a path aimed one level too high resolves to the project tree, and
// `nibs migrate` would then move and rewrite every front-mattered .md it finds
// there while the real store went untouched.
//
// The DISCOVERED route answers with the nearest marker of EITHER kind — a
// `.nibs` store or a pre-layout `.nibs.yml` — because the two are alternatives
// rather than independent searches: a pre-layout project between the cwd and a
// store is the project the user is in, and binding past it to a store belonging
// to someone else is a mutation waiting to happen (see preLayoutProjectError).
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
	//
	// The refusal is deliberately UNCONDITIONAL rather than narrowed to the case
	// where the two disagree. Comparing them with sameDir would accept the
	// self-consistent spelling, which is exactly the spelling that teaches the
	// habit: the same invocation silently changes meaning the moment either value
	// moves, and the flag it would sanction adds nothing --nibs-path does not
	// already say. A rule with no exceptions is also the only one a message can
	// state in one sentence.
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
	// docs recommended for working against another project. Stale scripts and
	// muscle memory keep producing it long after a project is migrated, so what
	// this refusal says about that project has to be OBSERVED rather than assumed.
	//
	// It used to be assumed, from the basename alone: every caller was told "a
	// project still carrying that file has not been migrated" and sent to run
	// `nibs migrate` in the file's directory. For `--config /gone/.nibs.yml` that
	// prescribed a command in a directory that is not there, and for an
	// already-migrated project both halves were false — the file is gone, the
	// project is migrated, and the prescribed migrate answers "Store is up to
	// date" while `--nibs-path <project>/.nibs`, the remedy that actually
	// resolves, went unmentioned.
	//
	// So: stat the file, and look for the store beside it. A `.nibs` directory
	// there is the answer whether or not it has been migrated yet — resolving it
	// is what lets the migration gate speak for it — while a project with no
	// `.nibs` gets preLayoutRemedy, the same three-way answer the discovery route
	// gives, which reads the retired `nibs.path` key and prescribes a command
	// only for the shapes the store-evidence guard accepts.
	//
	// A stat that fails for any reason OTHER than absence is treated as present:
	// the file may well be there, and preLayoutRemedy's own first branch reports
	// an unreadable config as exactly that rather than asserting anything about
	// the project.
	if configPath != "" && filepath.Base(configPath) == store.LegacyProjectConfigFileName {
		projectDir := filepath.Dir(configPath)
		storeDir := filepath.Join(projectDir, store.DirName)
		beside := isDir(storeDir)

		if _, err := os.Stat(configPath); errors.Is(err, fs.ErrNotExist) {
			remedy := "and nothing names a store here; pass --nibs-path with the store directory itself, or run `nibs init` in the project you meant"
			if beside {
				remedy = "but that project's store is right there — pass --nibs-path " + storeDir
			}
			return "", fmt.Errorf("--config names a store's %s, and %s does not exist, %s",
				store.ConfigFileName, configPath, remedy)
		}
		if beside {
			return "", fmt.Errorf("--config now names a store's %s; %s is the pre-layout config, which sits beside the store rather than inside it, so its directory is the project — but this project's store is right there: pass --nibs-path %s (if that store still needs migrating, the command you run will say so)",
				store.ConfigFileName, configPath, storeDir)
		}
		return "", fmt.Errorf("--config now names a store's %s; %s is the pre-layout config, which sits beside the store rather than inside it, so its directory is the project and not a store, and no store sits beside it: %w",
			store.ConfigFileName, configPath, preLayoutRemedy(configPath))
	}
	// The flag's ONLY remaining meaning is "name the store through this file's
	// directory", so the file it names must be the config that store actually
	// reads. Any other basename splits the two apart again inside ONE flag: the
	// store resolves to the directory while resolveCLIStore reads the named file,
	// so `--config <store>/config.yml.bak` persists nibs into the real store under
	// the backup's prefix and id length. Ids derive from filenames, so that is a
	// persisted misnaming, not a display artifact — the same harm the
	// --config/--nibs-path exclusion above refuses.
	//
	// Absence is separated out for the same reason as above: "rename it" is not
	// something the reader can do to a file that is not there, and the directory
	// this would otherwise advise need not exist either. Only a definite ENOENT
	// takes that branch — an unreadable stat leaves the file possibly present,
	// which is the case the rename advice is for.
	if configPath != "" && filepath.Base(configPath) != store.ConfigFileName {
		if _, err := os.Stat(configPath); errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("--config must name a store's %s, and %s does not exist; name the store directory itself with --nibs-path, or point --config at the %s inside it",
				store.ConfigFileName, configPath, store.ConfigFileName)
		}
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
			// which half of the evidence is missing, and say only what was
			// established — the two halves fail for different reasons.
			projectDir := filepath.Dir(explicit)
			named, namedErr := legacyConfigNamesStore(explicit)
			inside, insideErr := isRealImmediateChild(explicit, projectDir)
			if namedErr == nil && named && insideErr == nil {
				why := "nothing in it was written by nibs (no markdown file carries a nibs `status:`)"
				if !inside {
					why = "with symlinks resolved it is not an immediate subdirectory of " + projectDir + ", so a config inside the project cannot authorize moving it"
				}
				return "", fmt.Errorf("%s is named as this project's store by %s, but %s, and `nibs migrate` will not move and rewrite a directory on a config's say-so alone; if these really are your nibs, create %s, move them into it, remove the `nibs.path` key from %s, then run `nibs migrate`",
					explicit, filepath.Join(projectDir, store.LegacyProjectConfigFileName), why,
					filepath.Join(projectDir, store.DirName),
					filepath.Join(projectDir, store.LegacyProjectConfigFileName))
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
	// ONE upward walk, reporting whichever marker it met FIRST — see
	// store.FindNearestMarker. Two walks (one for `.nibs`, one for `.nibs.yml`)
	// cannot answer "which is nearer" without re-deriving depth from the paths
	// they return, and nothing did: a pre-layout sub-project nested under an
	// unrelated ancestor store bound to the ancestor, so `nibs migrate` run in
	// the sub-project moved and rewrote a store its user had never named while
	// the project they were standing in stayed untouched.
	marker, err := store.FindNearestMarker(cwd)
	if err != nil {
		return "", fmt.Errorf("searching for a nibs store: %w", err)
	}
	switch marker.Kind {
	case store.MarkerStore:
		return marker.Path, nil
	case store.MarkerLegacyProject:
		return "", preLayoutProjectError(cwd, marker.Path)
	}
	return "", noStoreFoundError(cwd)
}

// isDir reports whether path is a directory that can be stat'd. Anything else —
// absent, a file, unreadable — is not one, which is the answer every caller here
// needs: they are deciding whether to NAME the path as a store.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// preLayoutProjectError explains a walk that met a pre-layout `.nibs.yml` before
// it met any store.
//
// The project boundary is that file's directory, and a project in that shape has
// no store for a command to operate on — the layout step of `nibs migrate` is
// what gives it one. So the answer here is a refusal naming the remedy, NOT a
// bind to whatever store happens to sit further up: an ancestor store belongs to
// a different project, and `nibs migrate` binding to it moved and rewrote it.
//
// The ancestor is still worth NAMING, because the reader may have watched
// commands answer from it until now. That second walk is DIAGNOSTIC ONLY — it
// runs after the binding decision is already made and must never influence one,
// or the single-pass rule this function exists to enforce is back where it
// started.
func preLayoutProjectError(cwd, legacy string) error {
	projectDir := filepath.Dir(legacy)
	shadowed := ""
	if ancestor, err := store.FindStore(projectDir); err == nil && ancestor != "" {
		shadowed = " (a store at " + stripControlChars(ancestor) + " sits further up, but the nearer project is what governs this directory)"
	}
	return fmt.Errorf("no nibs store for %s: the nearest nibs project is %s, which is pre-layout — its config sits beside the store rather than inside it, so no command can run there until it has been migrated%s; %w",
		cwd, projectDir, shadowed, preLayoutRemedy(legacy))
}

// noStoreFoundError explains an upward walk that met no nibs marker at all.
//
// "Run nibs init" is the right answer only here. A PRE-LAYOUT project whose data
// lived outside `.nibs` — the retired `nibs.path` key — has no store directory
// either, and for it that advice is actively harmful: it creates an empty store
// with a derived prefix beside the real data and strands it. The single upward
// walk reports that project as a marker of its own, so this message is reached
// only when there is genuinely nothing.
func noStoreFoundError(cwd string) error {
	return fmt.Errorf("no %s directory found in %s or any parent directory (run `nibs init` to create one)", store.DirName, cwd)
}

// preLayoutRemedy answers, for a project whose pre-layout `.nibs.yml` is at
// legacy, where its nibs are and which remedy converges. It is shared by the two
// refusals that reach a pre-layout project — the upward walk meeting one, and
// `--config` aimed straight at the file — so the two can never disagree about
// what to do about it.
//
// PRECONDITION: no `.nibs` sits beside legacy. Both callers establish that (the
// walk binds to a store in the same directory before it looks for this file;
// the --config guard stats for one first), and the "no store beside it" branch
// below states it as fact.
//
// For the shapes `nibs migrate` can relocate itself, the remedy is
// `nibs migrate --nibs-path <dataDir>` — migrating the store WHERE IT IS. The
// layout step then moves it to `<project>/.nibs`, which is what makes the project
// discoverable afterwards; telling the user to move the files by hand first only
// produced a store whose `.nibs.yml` still named the emptied directory, and no
// filesystem action can make a config VALUE equal `.nibs`.
//
// The guard's answer is THREE-WAY here as everywhere else. A declared directory
// that could not be read gets neither reason clause below: both of them state a
// definite fact about its contents, and both carry an instruction — move the files
// out of it, remove the key that records where they are — that is destructive when
// the fact is unknown.
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
// quoted back, so collapsed and bounded) and printed with %q, while the paths that
// appear as COMMAND ARGUMENTS go through stripControlChars and shell quoting only —
// collapsing or truncating those would corrupt the one string the user has to run.
//
// %q rather than %s because the rendering boundary handles deception and nothing
// handles the SEMANTIC channel: a value from a cloned repository's `.nibs.yml` sits
// in the same sentence as a command the reader is told to run, and with ~180 runes
// of prose to work with it could close its own markdown span and address the reader
// directly — whose stated primary consumer is an agent primed to follow
// instructions. Quoting cannot terminate its own delimiter.
func preLayoutRemedy(legacy string) error {
	projectDir := filepath.Dir(legacy)
	target := filepath.Join(projectDir, store.DirName)
	declared, readErr := config.RetiredNibsPath(legacy)
	if readErr != nil {
		// Absence of evidence and unreadable evidence lead to opposite advice, so
		// they must not collapse: `nibs init` here could strand a real store this
		// file names.
		// flattenReason for the same reason as evErr below: a YAML parse failure
		// quotes the offending line, so this error carries file CONTENTS and not
		// only a path.
		return fmt.Errorf("%s — the pre-layout config that would say where this project's nibs live — cannot be read: %s; repair or remove it, then re-run (do NOT run `nibs init` until you know, it would create an empty store beside data that may already exist)",
			legacy, flattenReason(readErr.Error()))
	}
	if declared == "" {
		return fmt.Errorf("%s is a pre-layout nibs config with no store beside it; create %s and move this project's nib files into it, then run `nibs migrate`",
			legacy, target)
	}
	dataDir := declared
	if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(projectDir, dataDir)
	}
	ok, evErr := hasLegacyStoreShape(dataDir)
	if evErr == nil && ok {
		return fmt.Errorf("%s sets the retired `nibs.path: %q`; this project's nibs live in %s — run `nibs migrate --nibs-path %s`, which moves that store to %s and relocates the config into it (do NOT run `nibs init`, which would create an empty store beside the real data)",
			legacy, sanitizeFileText(declared),
			stripControlChars(dataDir), shellArg(dataDir), target)
	}
	if evErr != nil {
		// The evidence could not be established, which is a THIRD answer and not
		// "no evidence" — the reason clauses below assert a definite fact about
		// dataDir's contents, and asserting one about a directory that could not
		// be enumerated is how this message came to tell a user that a directory
		// holding a real nib held nothing. Both of the instructions the reason
		// clauses carry are withheld here: moving files out of a directory nobody
		// can read, and discarding the only record of where those files are.
		// An UNUSABLE path belongs with absence, not with the third answer. A name
		// the filesystem rejects on sight names nothing, on any volume, under any
		// permissions — so "find the files, create <project>/.nibs, move them in"
		// is the remedy that applies, while "mount the volume, fix its permissions"
		// sends the reader to check something that cannot help and simultaneously
		// tells them to keep a key that points nowhere. isUnusablePath is
		// deliberately narrow (see its platform files): a permission error or a
		// disconnected volume stays in the third answer, because there the
		// directory may be real and full of nibs.
		if errors.Is(evErr, fs.ErrNotExist) || isUnusablePath(evErr) {
			return fmt.Errorf("%s sets the retired `nibs.path: %q`, but %s does not exist — so this project's nib files are not where the config says they are; find them, create %s and move them into it, then run `nibs migrate` (do NOT run `nibs init`, which would create an empty store beside data that may already exist)",
				legacy, sanitizeFileText(declared),
				stripControlChars(dataDir), target)
		}
		// flattenReason, not %v: an OS error embeds the path it failed on, and that
		// path is built from the declared value — so interpolating the error raw
		// reopens the very channel sanitizeFileText closes one argument earlier.
		// Reached on POSIX by a permission error, and by a YAML error quoting file
		// contents.
		return fmt.Errorf("%s sets the retired `nibs.path: %q`, whose contents cannot be read (%s) — so whether this project's nibs are in %s cannot be determined; resolve that (mount the volume, fix its permissions), then re-run (do NOT run `nibs init`, and do NOT remove the `nibs.path` key: it is the only record of where the nibs are)",
			legacy, sanitizeFileText(declared), flattenReason(evErr.Error()),
			stripControlChars(dataDir))
	}
	// Naming AND containment both have to hold before "nothing in it was written
	// by nibs" is the accurate reason: a `nibs.path` satisfied only by a symlink
	// out of the project fails on containment, and saying anything about the
	// directory's contents there would answer a question that was never asked.
	why := "which `nibs migrate` will not relocate for you because, with symlinks resolved, it is not an immediate subdirectory of " + projectDir
	named, namedErr := legacyConfigNamesStore(dataDir)
	inside, insideErr := isRealImmediateChild(dataDir, projectDir)
	if namedErr == nil && named && insideErr == nil && inside {
		why = "which `nibs migrate` will not relocate for you because nothing in it was written by nibs (no markdown file carries a nibs `status:`)"
	}
	return fmt.Errorf("%s sets the retired `nibs.path: %q`; this project's nibs live in %s, %s — create %s, move this project's nib files from %s into it, remove the `nibs.path` key from %s, then run `nibs migrate` (do NOT run `nibs init`, which would create an empty store beside the real data)",
		legacy, sanitizeFileText(declared),
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
//     its blog post moved into `data/` and rewritten as a nib render. Such a
//     store DOES occur — `nibs init` creates the directories before it validates
//     the prefix, so a rejected prefix leaves a config-less store behind (see
//     cmd/init.go) — but that store is reached by re-running `nibs init`, and
//     every store nibs creates or migrates to is named `.nibs` and covered by
//     the name clause regardless;
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
// key, AND either something inside it was written by nibs or it is empty (see
// declaredStoreCorroborated).
//
// PARENT-ONLY, DELIBERATELY, AND ON THE RESOLVED PATH. The `.nibs.yml` is looked
// for in dir's PARENT and nowhere else, and dir must resolve — symlinks and all —
// to a directory really inside that parent (see isRealImmediateChild), so only a
// store that is an immediate subdirectory of the project can be confirmed this
// way. The retired key accepted more — a nested value like `docs/nibs`, an
// absolute path, `.`, `..` — and this predicate refuses all of them on purpose:
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
	inside, err := isRealImmediateChild(dir, filepath.Dir(dir))
	if err != nil || !inside {
		return false, err
	}
	return declaredStoreCorroborated(dir)
}

// legacyConfigNamesStore reports whether the pre-layout `.nibs.yml` beside dir
// names dir itself through the retired `nibs.path` key. It is the NAMING half of
// hasLegacyStoreShape, split out so a refusal can say which half failed.
//
// A DRIVE-RELATIVE `nibs.path` — `C:proj`, meaning "proj, relative to the current
// directory on drive C:" — is not absolute by filepath.IsAbs, so it falls into the
// Join below and yields `<project>\C:proj`, a path with a colon mid-component that
// Windows can never create. Measured: `C:proj` then fails ENOENT and `C:` fails
// ERROR_INVALID_NAME, so both refuse and print the manual remedy.
//
// Left as-is rather than rejected up front. The shape is already refused, by the
// same route every other unusable `nibs.path` takes, and a dedicated error would
// have to explain a Windows path spelling nobody writes on purpose in a YAML file.
// Reject it explicitly only if a real project is ever seen carrying one.
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

// isRealImmediateChild reports whether dir, with every symlink resolved, sits
// directly inside parent with every symlink resolved.
//
// This is the CONTAINMENT half of hasLegacyStoreShape's parent-only rule, and the
// rule is worth nothing without it. Every other comparison in this chain is
// lexical (filepath.Dir, Abs, Clean), and a symlink satisfies "an immediate
// subdirectory of the project" while pointing anywhere on the filesystem —
// meanwhile the store walk OPENS its root, so it enumerates whatever the link
// leads to. A repository shipping `.nibs.yml` (`path: store`) plus a committed
// `store -> /somewhere/else` therefore got nibs to prescribe
// `nibs migrate --nibs-path <repo>/store`, which moved that whole tree into
// `<repo>/.nibs`, rewrote every front-mattered file in it as a nib render, and
// deleted the repository's `.nibs.yml`.
//
// A link that stays INSIDE the project is still accepted: this is a containment
// test, not a ban on symlinks — a store reached through a link within the project
// relocates correctly, because renaming the link moves the store nibs addresses.
// A store on another volume reached by a link OUT of the project is refused, which
// matches how the same store spelled as an absolute `nibs.path` has always been
// treated: the manual remedy noStoreFoundError prints converges for both.
//
// WINDOWS, measured rather than reasoned (the concern was that the two arguments
// might normalize differently and make the containment check answer at random):
//
//   - CASE AND 8.3 SHORT NAMES ARE SAFE. filepath.EvalSymlinks upper-cases the
//     drive letter and rewrites every component to its real on-disk spelling via
//     FindFirstFile, so both arguments arrive canonical no matter how either was
//     typed. A lower-cased drive, a lower-cased component, and a `PROJEC~1` alias
//     all returned true, on either side or both.
//   - UNC IS EXEMPT FROM THAT NORMALIZATION and therefore is NOT safe.
//     normVolumeName returns the volume untouched when it is longer than two
//     bytes, which every `\\server\share` is. Measured against a real share:
//     EvalSymlinks(`\\localhost\C$\…`) and EvalSymlinks(`\\LOCALHOST\c$\…`) both
//     succeed and both keep the case they were given, so a UNC parent and child
//     that reached here from different origins answer FALSE for the same
//     directory.
//
// The UNC direction is conservative — a false negative refuses and prints the
// manual remedy, it does not authorize a relocation — which is why it is
// documented here rather than papered over with a case-folding special case that
// would have to guess at the remote volume's semantics.
func isRealImmediateChild(dir, parent string) (bool, error) {
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return false, err
	}
	return sameDir(filepath.Dir(realDir), realParent), nil
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
// An EMPTY directory is corroborated, and only an empty one: a store `nibs init`
// created but never wrote to legitimately holds nothing, so requiring an artifact
// there would refuse a real store. The exemption is keyed on os.ReadDir finding no
// entries rather than on the walk finding no markdown, because what acceptance
// authorizes is a whole-directory os.Rename plus deletion of the project's
// `.nibs.yml` — a mutation that has nothing to do with file CONTENTS. An asset
// directory holding only `style.css` and `img/logo.svg` was renamed to `.nibs`
// wholesale, and so was a note vault whose markdown all lived under `.obsidian/`
// (the walk prunes dot directories, so it saw none).
//
// A file whose header cannot be READ makes the answer UNDECIDED rather than
// negative. layoutMovableFiles moves such a file into data/ precisely because the
// scan cannot prove it is not a nib; the authorizer reading the same file as proof
// that nothing here was written by nibs is the opposite conclusion from the same
// evidence.
func declaredStoreCorroborated(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return true, nil
	}
	err = nibcore.WalkStoreFiles(dir, func(path string, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		h, hErr := readFrontMatterHeader(path)
		if hErr != nil {
			return hErr
		}
		if h.hasFrontMatter && config.IsKnownStatus(h.status) {
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
	return false, nil
}

// errStoreCorroborated stops declaredStoreCorroborated's walk at the first nib it
// finds; it never reaches a caller.
var errStoreCorroborated = errors.New("nib file found")

// shellArg renders a path for a copy-pasteable command line, quoting it when it
// carries a character the local shell would split or expand. Nothing is collapsed
// or truncated — unlike sanitizeFileText, which does both — because this is the
// argument the user has to run.
//
// The quoting is PLATFORM-SPECIFIC, and it has to be: the trigger set and the
// quote character both differ. A POSIX shell splits on a space and reads a
// backslash as an escape; cmd.exe and PowerShell split on a space, treat the
// backslash as an ordinary separator, and disagree with sh about which quote
// character delimits an argument. Rendering one spelling for both platforms is
// what shellarg_windows.go exists to stop — see its comment for the measurements.
//
// It is NOT byte-preserving. stripControlChars runs first and maps every
// non-printable rune to a space, so a path really containing one yields a command
// naming a different path. That is the safe direction here rather than a defect: a
// substituted path cannot satisfy legacyConfigNamesStore, so the prescribed command
// is refused rather than acting on the wrong directory. Do not reach for this where
// the bytes themselves have to survive.
func shellArg(path string) string {
	clean := stripControlChars(path)
	if clean != "" && !strings.ContainsAny(clean, shellArgQuoteTriggers) {
		return clean
	}
	return quoteShellArg(clean)
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
	sanitized := safetext.NewWriter(stderr)
	// The writer holds a rune split across writes, so anything left at the end of
	// this call has to be emitted or it disappears after Fprintf reported it
	// written. Reachable only if a format string stops mid-rune; both here end in a
	// newline, and the Flush means that stays a property rather than a dependency.
	defer func() { _ = sanitized.Flush() }()
	stderr = sanitized
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
