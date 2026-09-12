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

// Package-level because Cobra's StringVar needs a pointer target at init(),
// before App exists. Read only during PersistentPreRunE.
var nibsPath string
var configPath string

var rootCmd = &cobra.Command{
	Use:   "nibs",
	Short: "A file-based issue tracker for AI-first workflows",
	Long: `Nibs is a lightweight issue tracker that stores issues as markdown files.
Track your work alongside your code and supercharge your coding agent with
a full view of your project.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if commandNeedsNoStore(cmd) {
			return nil
		}
		app, err := initAppForCommand(cmd)
		if err != nil {
			// __complete's output IS the completion list, so a shell that gets an
			// error instead falls back to completing FILENAMES: `nibs <TAB>` outside
			// a project would offer the current directory in place of the subcommand
			// list. Degrade to no App, keeping one wherever a store does resolve.
			if isCompletionRequest(cmd) {
				return nil
			}
			return err
		}
		cmd.SetContext(withApp(cmd.Context(), app))
		return nil
	},
	// Cobra skips PostRun on error, so this runs only after a subcommand
	// succeeds. Best-effort: never blocks or fails the command.
	PersistentPostRun: func(cmd *cobra.Command, _ []string) {
		maybeNotifyUpdate(cmd)
	},
}

func init() {
	// Cobra otherwise prints the usage block and a duplicate "Error: <msg>" on
	// every RunE failure, which mixes prose into --json's JSON-only output
	// contract. reportExitError owns error reporting instead. Propagates to
	// every subcommand.
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true

	rootCmd.PersistentFlags().StringVar(&nibsPath, "nibs-path", "", "Path to the .nibs store directory (overrides discovery and NIBS_PATH env var)")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to a store's config file; names the store through its directory (cannot be combined with --nibs-path or NIBS_PATH)")
	installFlagSuggestions(rootCmd)
}

// commandNeedsNoStore reports whether cmd can do its whole job without a store,
// so PersistentPreRunE skips resolving one. A command listed here must NOT call
// getApp(): there is no App in its context.
//
// migrate is here because it must run on the very stores the hook refuses; it
// resolves the store root itself.
//
// Help and completion are matched by LINEAGE, not name: `nibs completion bash`
// executes the "bash" subcommand, so a name check would have to enumerate every
// shell. (The `--help` FLAG never reaches here — Cobra's ErrHelp path returns
// before these hooks run.)
func commandNeedsNoStore(cmd *cobra.Command) bool {
	// Matched on the executed command's own name, so a future `nibs <x> init`
	// would skip the store too and panic in getApp. All of these are direct
	// children of root today; nest one and it needs the lineage loop below.
	//
	// Name() is never an ALIAS, so these must be real names: `query` is the real
	// name of the command also reached as `nibs graphql`. Keying an alias here
	// silently exempts nothing.
	switch cmd.Name() {
	case "init", "prime", "version", "catalog", "cheat", "upgrade", "migrate":
		return true
	case "query":
		return querySchemaOnly
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "help" || c.Name() == "completion" {
			return true
		}
	}
	return false
}

// initAppForCommand resolves the store, refuses an unmigrated one, and loads it
// into the App a command reads through getApp.
func initAppForCommand(cmd *cobra.Command) (*App, error) {
	root, cfg, err := resolveCLIStore()
	if err != nil {
		return nil, err
	}

	// Refuse a store with pending migrations BEFORE Load ever sees it.
	//
	// Plain `nibs check` is exempt because migrate's unclean-store refusal
	// points AT it: gating it would loop the user — migrate says "run check",
	// check says "run migrate". Only --fix writes, so only --fix stays gated.
	gated := cmd.Name() != "check" || checkFix
	if gated {
		if err := refuseIfMigrationPending(root); err != nil {
			return nil, err
		}
	}

	core := nibcore.New(root, cfg)
	if err := core.Load(); err != nil {
		return nil, fmt.Errorf("loading nibs: %w", err)
	}

	// Getting past the gate IS the answer to "does this store need migration?",
	// so record it instead of making a command re-scan (App.MigrationGatePassed).
	return newApp(core, gated), nil
}

// isCompletionRequest reports whether cmd is the hidden command a shell runs on
// every TAB press.
//
// Distinct from commandNeedsNoStore: `nibs completion <shell>` runs once at
// install and needs no store, while `__complete` could USE one, so it resolves
// a store when there is one and degrades quietly when there is not.
func isCompletionRequest(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		// Cobra registers ShellCompNoDescRequestCmd as an ALIAS of this command
		// and tells them apart with CalledAs(), so Name() answers `__complete`
		// for both spellings.
		if c.Name() == cobra.ShellCompRequestCmd {
			return true
		}
	}
	return false
}

// resolveCLIStore resolves the store every command operates on and loads THAT
// store's config.
//
// CANONICAL INVARIANT: the two answers come as a pair, so a store is always read
// under its OWN vocabulary — pointing nibs at another project's store carries
// that project's prefix, id length and defaults with it, never the cwd
// project's. resolveStoreDir keeps this true by refusing --config together with
// --nibs-path/NIBS_PATH, the one combination that would source the store and the
// config from different projects.
func resolveCLIStore() (string, *config.Config, error) {
	storeDir, err := resolveStoreDir()
	if err != nil {
		return "", nil, err
	}

	// Either way the user config layers underneath.
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

// resolveStoreDir determines the `.nibs` store directory. It is also migrate's
// entry point, so the two can never resolve a different store.
//
// Precedence: --nibs-path > NIBS_PATH > --config's directory > an upward search
// from the cwd for the NEAREST nibs marker.
//
// Whichever route names the directory, it must carry positive evidence of being
// a store — one shared check, bindNamedStore. Existence alone is not enough: a
// path aimed one level too high resolves to the project tree, which `nibs
// migrate` would then move and rewrite.
//
// The discovery route answers with the nearest marker of EITHER kind, since a
// pre-layout project between the cwd and a store is the project the user is in;
// binding past it mutates a store they never named (see preLayoutProjectError).
//
// ADDING A REFUSAL HERE means adding a row to refusal_invariant_test.go's
// storeResolutionRefusalCases by hand — there is no production list for
// TestEveryRefusalNamesAReachablePathAndARunnableCommand to walk, the way it
// walks migrate.go's migrateGates, so it would otherwise go undriven.
func resolveStoreDir() (string, error) {
	// Mutually exclusive: together they source the store and the config from
	// different projects, breaking resolveCLIStore's invariant. Ids derive from
	// filenames, so a nib written that way is permanently MISNAMED, not merely
	// mislabeled. Unconditional even when the two agree — a self-consistent
	// spelling changes meaning the moment either value moves.
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

	// The pre-layout `.nibs.yml` sits BESIDE the store, so its directory is the
	// PROJECT, not a store. Stale scripts keep producing this invocation long
	// after a project is migrated, so what the refusal says about that project is
	// OBSERVED, never assumed from the basename: stat the file and look for the
	// store beside it. Assuming it strands both live cases — `--config
	// /gone/.nibs.yml` gets a command in a directory that is not there, and an
	// already-migrated project gets "run migrate" when migrate has nothing to do.
	//
	// A stat failing for any reason OTHER than absence counts as PRESENT:
	// preLayoutRemedy's first branch reports an unreadable config as exactly
	// that, rather than asserting anything about the project.
	if configPath != "" && filepath.Base(configPath) == store.LegacyProjectConfigFileName {
		projectDir := filepath.Dir(configPath)
		storeDir := filepath.Join(projectDir, store.DirName)
		beside := bindsAsStore(storeDir)

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
	// The flag's only remaining meaning is "name the store through this file's
	// directory", so the file must be the config that store actually reads. Any
	// other basename splits the two apart inside ONE flag — `--config
	// <store>/config.yml.bak` would write into the real store under the backup's
	// prefix — the same misnaming the exclusion above refuses.
	//
	// Only a definite ENOENT takes the absence branch: "rename it" is not
	// something the reader can do to a file that is not there.
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
		// The config lives inside the store, so naming it names the store.
		explicit = filepath.Dir(configPath)
	}

	if explicit != "" {
		// Normalize ONCE here, because every downstream derivation is lexical:
		// filepath.Dir("<p>/.nibs/") is "<p>/.nibs", so a trailing slash — what
		// shell tab completion produces — shifts store.Layout.ProjectDir() one
		// level INTO the store and hides the project's `.nibs.yml` from the gate.
		explicit = filepath.Clean(explicit)
		if abs, err := filepath.Abs(explicit); err == nil {
			explicit = abs
		}
		if info, err := os.Stat(explicit); err != nil || !info.IsDir() {
			return "", fmt.Errorf("nibs store does not exist or is not a directory: %s", explicit)
		}
		return bindNamedStore(explicit)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting current directory: %w", err)
	}
	// ONE walk, reporting whichever marker it met FIRST. Two walks (one per
	// marker kind) cannot answer "which is nearer" without re-deriving depth
	// from the paths they return, and a sub-project that binds to an ancestor
	// store has `nibs migrate` rewrite a store its user never named.
	marker, err := store.FindNearestMarker(cwd)
	if err != nil {
		return "", fmt.Errorf("searching for a nibs store: %w", err)
	}
	switch marker.Kind {
	case store.MarkerStore:
		return bindNamedStore(marker.Path)
	case store.MarkerLegacyProject:
		return "", preLayoutProjectError(cwd, marker.Path)
	}
	return "", noStoreFoundError(cwd)
}

// bindNamedStore validates dir as a nibs store and returns it, or the refusal
// explaining why it is not one.
//
// EVERY route arrives here — the three that name a store explicitly
// (--nibs-path, NIBS_PATH, --config's directory) and the upward walk that
// discovers one — so all four answer "is this a store?" with ONE decision. A
// route deciding on its own is the defect this prevents: a walk that matched
// `.nibs` on its name alone bound a committed `.nibs -> /outside` with no flag
// at all. Sharing costs the walk nothing — for a real `.nibs` directory
// looksLikeStore answers on its name clause, before anything is opened.
func bindNamedStore(dir string) (string, error) {
	is, err := looksLikeStore(dir)
	if err != nil {
		// "Cannot determine" must never be reported as "no evidence": the message
		// below says to run `nibs init` here, which over a real store whose config
		// merely could not be read creates a second, empty store beside the data.
		return "", fmt.Errorf("cannot tell whether %s is a nibs store: %w; repair or remove that file, then re-run", dir, err)
	}
	if !is {
		// The symlink refusal comes FIRST because the branches below are FALSE
		// here: each converges on "create <project>/.nibs and move the nib files
		// in", which the reader cannot do while a link holds that name.
		//
		// A failed Lstat falls through rather than refusing — os.Stat followed this
		// path a moment ago, so an error means the link moved under us.
		if link, linkErr := isSymlink(dir); linkErr == nil && link && filepath.Base(dir) == store.DirName {
			return "", symlinkedStoreError(dir)
		}
		// A `.nibs.yml` that NAMES this directory but no artifact inside it is its
		// own answer: "no .nibs.yml beside it names it" would be false, and `nibs
		// init` is wrong advice when the naming config is real. Say which half of
		// the evidence is missing — the two fail for different reasons.
		projectDir := filepath.Dir(dir)
		declared, resolvedDeclared, declaredErr := legacyDeclaredStorePath(dir)
		named := declaredErr == nil && declared != "" && sameDir(resolvedDeclared, dir)
		inside, insideErr := isRealImmediateChild(dir, projectDir)
		if named && insideErr == nil {
			why := "nothing in it was written by nibs (no markdown file in it was rendered by nibs)"
			if !inside {
				why = "with symlinks resolved it is not an immediate subdirectory of " + projectDir + ", so a config inside the project cannot authorize moving it"
			}
			return "", fmt.Errorf("%s is named as this project's store by %s, but %s, and `nibs migrate` will not move and rewrite a directory on a config's say-so alone; if these really are your nibs, create %s, move them into it, remove the `nibs.path` key from %s, then run `nibs migrate`",
				dir, filepath.Join(projectDir, store.LegacyProjectConfigFileName), why,
				filepath.Join(projectDir, store.DirName),
				filepath.Join(projectDir, store.LegacyProjectConfigFileName))
		}
		// The message states the COMPARISON, not a conclusion about it. sameDir
		// matches text, so on a case-insensitive filesystem — or through a symlink
		// alias — a `.nibs.yml` declaring some other path may be naming this very
		// directory; "no config names it" would then send a user standing on real
		// nibs to `nibs init`. Stating the comparison stays true either way.
		//
		// The declared value is echoed as EVIDENCE (sanitizeFileText: collapsed and
		// bounded, being untrusted file content); the path the reader must act on
		// is the resolved one, which has to survive intact.
		if declaredErr == nil && declared != "" {
			// preLayoutRemedy's precondition is that no store sits beside the
			// pre-layout config. Its other two callers establish that; this
			// route cannot, because the reader names any directory they like.
			// Where a store IS there it is the answer, and the remedy would
			// otherwise tell them to create a directory they already have.
			if storeDir := filepath.Join(projectDir, store.DirName); bindsAsStore(storeDir) {
				return "", fmt.Errorf("%s is not a nibs store: it holds no %s that parses as one, and the %s beside it sets the retired nibs.path to %q, which does not match this path as text — the comparison resolves no symlinks and folds no case, so another name for this same directory does not match either; pass --nibs-path %s, the store this project already has",
					dir, store.ConfigFileName, store.LegacyProjectConfigFileName,
					sanitizeFileText(declared), storeDir)
			}
			return "", fmt.Errorf("%s is not a nibs store: it holds no %s that parses as one, and the %s beside it sets a retired nibs.path that does not match this path as text — the comparison resolves no symlinks and folds no case, so another name for this same directory does not match either; %w",
				dir, store.ConfigFileName, store.LegacyProjectConfigFileName,
				preLayoutRemedy(filepath.Join(projectDir, store.LegacyProjectConfigFileName)))
		}
		// A genuine absence: an unreadable `.nibs.yml` already took the "cannot
		// tell" answer above, so ORDERING is load-bearing — change it and this flat
		// denial becomes the false claim that answer exists to prevent.
		//
		// It also rests on the two reads of that file AGREEING, a property of the
		// file and not of this code, which is why config.ReadConfigFile refuses
		// anything but a regular file.
		return "", fmt.Errorf("%s is not a nibs store: it holds no %s that parses as one, and no %s beside it names it; name the store directory itself (e.g. --nibs-path %s), or run `nibs init` there",
			dir, store.ConfigFileName, store.LegacyProjectConfigFileName,
			filepath.Join(dir, store.DirName))
	}
	return dir, nil
}

// isSymlink reports whether path is ITSELF a symbolic link, which os.Stat cannot
// answer because it follows one. It is the distinction looksLikeStore's name
// clause turns on.
//
// WINDOWS, read off the toolchain: os.Lstat sets ModeSymlink for
// IO_REPARSE_TAG_SYMLINK only, and every other reparse tag — a JUNCTION
// (IO_REPARSE_TAG_MOUNT_POINT) among them — gets ModeIrregular instead
// (go/src/os/types_windows.go, Go 1.26). So a `.nibs` junction reads as a real
// directory here and keeps the name clause. GODEBUG=winsymlink=0 reverts that
// and only makes this guard stricter, so it needs no handling.
//
// That bound is intended: the threat is a link a CLONE materializes, and git
// writes a symlink or a plain file, never a junction. Widening to "not a plain
// directory" would catch cloud-storage placeholders a real store may sit on.
func isSymlink(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}

// symlinkedStoreError refuses a `.nibs` that is a SYMLINK carrying no evidence of
// being a store — see looksLikeStore for why the name clause cannot cover a link.
//
// Links are NOT banned: this is an evidence rule, not a containment one, and
// `.nibs -> ~/sync/proj-nibs` pointing at a genuine store resolves through the
// config clause without reaching here. What is refused is trusting the name over
// the destination.
//
// Where a pre-layout `.nibs.yml` sits beside the link the remedy is
// preLayoutRemedy's, so this route cannot disagree with the other three. The
// link is named first either way — every remedy begins by creating
// `<project>/.nibs`, and the link is holding that name.
//
// The destination is echoed through sanitizeFilePath: bytes a cloned repository
// chose, in a message whose primary consumer is an agent.
func symlinkedStoreError(dir string) error {
	// EvalSymlinks resolves EVERY component, so it can fail on a parent this
	// process cannot traverse even though os.Stat followed the link a moment ago.
	// The link's own value is the honest fallback; where even that fails the
	// clause is dropped rather than filled with dir, which would read as a link
	// pointing at itself.
	where := ""
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		where = " to " + sanitizeFilePath(resolved)
	} else if declared, err := os.Readlink(dir); err == nil {
		where = " to " + sanitizeFilePath(declared)
	}
	lead := fmt.Sprintf("%s is a symlink%s, and what it leads to carries no evidence of being a nibs store: it holds no %s that parses as one. A store's NAME is evidence only for a real directory — a link's name says nothing about where it leads, and `nibs migrate` would move and rewrite everything under there",
		dir, where, store.ConfigFileName)

	// A stat failing for any reason OTHER than absence counts as PRESENT: the
	// `nibs init` advice below would be wrong over a config really there.
	legacy := filepath.Join(filepath.Dir(dir), store.LegacyProjectConfigFileName)
	if _, err := os.Stat(legacy); !errors.Is(err, fs.ErrNotExist) {
		// preLayoutRemedy already says the name is occupied and must be cleared;
		// duplicating that here is how the two come to disagree.
		return fmt.Errorf("%s. What this project needs instead: %w", lead, preLayoutRemedy(legacy))
	}
	// `nibs init` is safe to prescribe ONLY because init refuses through a link
	// itself (refuseSymlinkedStoreDir): MkdirAll follows one, so without that
	// guard this prescription writes the store INTO the destination, which then
	// parses as a store and reopens the hazard. One rule in two files — changing
	// either without the other reopens it.
	return fmt.Errorf("%s; repoint it at a directory that really is a store, or remove the link and run `nibs init` here", lead)
}

// bindsAsStore reports whether dir is a directory bindNamedStore would ACCEPT.
// It is the test every "the store is right there, pass --nibs-path X" advice
// owes, and is narrower than isDir on purpose: isDir follows a symlink, so a
// refusal using it can advise a `.nibs` link the resolver then refuses —
// stranding the reader on a second refusal, which
// TestEveryRefusalNamesAReachablePathAndARunnableCommand forbids.
//
// "Cannot tell" collapses into FALSE: a message may prescribe only a store it
// has ESTABLISHED, and each caller's other branch converges anyway.
func bindsAsStore(dir string) bool {
	if !isDir(dir) {
		return false
	}
	ok, err := looksLikeStore(dir)
	return err == nil && ok
}

// isDir reports whether path is a directory that can be stat'd. Anything else —
// absent, a file, unreadable — is not one.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// preLayoutProjectError explains a walk that met a pre-layout `.nibs.yml` before
// it met any store. Such a project has no store to bind to — the layout step of
// `nibs migrate` is what gives it one — so the answer is a refusal naming the
// remedy, never a bind to an ancestor store belonging to a different project.
//
// The ancestor is still worth NAMING, since the reader may have watched commands
// answer from it. That second walk is DIAGNOSTIC ONLY: it runs after the binding
// decision and must never influence one, or the single-pass rule is undone.
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
// "Run nibs init" is the right answer ONLY here. For a pre-layout project whose
// data lived outside `.nibs` it is harmful — it creates an empty store beside
// the real data and strands it — which is why the walk reports that project as a
// marker of its own and this message is reached only when there is nothing.
func noStoreFoundError(cwd string) error {
	return fmt.Errorf("no %s directory found in %s or any parent directory (run `nibs init` to create one)", store.DirName, cwd)
}

// preLayoutRemedy answers, for a project whose pre-layout `.nibs.yml` is at
// legacy, where its nibs are and which remedy converges. Shared by the three
// refusals that reach a pre-layout project, so they can never disagree.
//
// PRECONDITION: no `.nibs` that BINDS AS A STORE sits beside legacy. Every
// caller establishes that via bindsAsStore, and it is load-bearing — three
// branches below tell the reader to CREATE that directory and a fourth
// prescribes a migration whose destination it is. A NON-store may still occupy
// the name and block MkdirAll, which is what the obstruction clause is for.
//
// `nibs migrate --nibs-path <dataDir>` migrates the store WHERE IT IS, after
// which the layout step moves it to `<project>/.nibs`. Moving files by hand
// first cannot converge: no filesystem action makes a config VALUE equal
// `.nibs`. That command prints ONLY for shapes hasLegacyStoreShape accepts, so
// the message never prescribes a command the tool refuses; every other shape
// gets the manual remedy, which converges because removing the retired key is
// what stops the relocation refusing.
//
// Config-sourced strings cross a boundary chosen by what the string is FOR: the
// declared VALUE is quoted evidence (sanitizeFileText, collapsed and bounded), a
// path the reader looks at keeps its spaces (sanitizeFilePath), and a path the
// reader RUNS gets shellArg. All three answer the SEMANTIC channel and %q does
// not — %q escapes the double quote, while these values sit inside a
// backtick-delimited span that safetext.Strip substitutes. %q stays for the
// visible bounding quotes.
func preLayoutRemedy(legacy string) error {
	projectDir := filepath.Dir(legacy)
	target := filepath.Join(projectDir, store.DirName)
	// Every remedy below needs the name `<project>/.nibs`, so anything already on
	// that name without being a store makes all four unfollowable. Said once here
	// rather than per branch.
	//
	// os.Lstat, not isDir: a DANGLING link blocks MkdirAll just as effectively
	// while isDir reports it absent.
	obstruction := ""
	if _, err := os.Lstat(target); err == nil && !bindsAsStore(target) {
		obstruction = target + " is already taken by something that is not a nibs store, and every remedy here needs that name — move or remove it first; then "
	}
	declared, readErr := config.RetiredNibsPath(legacy)
	if readErr != nil {
		// Absence and unreadability lead to OPPOSITE advice, so they must not
		// collapse: `nibs init` here could strand a real store this file names.
		// flattenReason because a YAML parse failure quotes the offending line, so
		// this error carries file CONTENTS and not only a path.
		return fmt.Errorf("%s — the pre-layout config that would say where this project's nibs live — cannot be read: %s; repair or remove it, then re-run (do NOT run `nibs init` until you know, it would create an empty store beside data that may already exist)",
			legacy, flattenReason(readErr.Error()))
	}
	if declared == "" {
		return fmt.Errorf("%s%s is a pre-layout nibs config with no store beside it; create %s and move this project's nib files into it, then run `nibs migrate`",
			obstruction, legacy, target)
	}
	dataDir := declared
	if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(projectDir, dataDir)
	}
	ok, evErr := hasLegacyStoreShape(dataDir)
	if evErr == nil && ok {
		return fmt.Errorf("%s%s sets the retired `nibs.path: %q`; this project's nibs live in %s — run `nibs migrate --nibs-path %s`, which moves that store to %s and relocates the config into it (do NOT run `nibs init`, which would create an empty store beside the real data)",
			obstruction, legacy, sanitizeFileText(declared),
			sanitizeFilePath(dataDir), shellArg(dataDir), target)
	}
	if evErr != nil {
		// A THIRD answer, not "no evidence": the reason clauses below assert a
		// definite fact about dataDir's contents, and both instructions they carry
		// (move the files out; discard the only record of where they are) are
		// destructive when that fact is unknown.
		//
		// An UNUSABLE path belongs with ABSENCE instead — a name the filesystem
		// rejects on sight names nothing on any volume, so "mount the volume, fix
		// its permissions" cannot help while telling the reader to keep a key
		// pointing nowhere. isUnusablePath is deliberately narrow (see its platform
		// files): a permission error stays in the third answer, where the directory
		// may be real and full of nibs.
		if errors.Is(evErr, fs.ErrNotExist) || isUnusablePath(evErr) {
			return fmt.Errorf("%s%s sets the retired `nibs.path: %q`, but %s does not exist — so this project's nib files are not where the config says they are; find them, create %s and move them into it, then run `nibs migrate` (do NOT run `nibs init`, which would create an empty store beside data that may already exist)",
				obstruction, legacy, sanitizeFileText(declared),
				sanitizeFilePath(dataDir), target)
		}
		// flattenReason, not %v: an OS error embeds the path it failed on, which is
		// built from the declared value — interpolating it raw reopens the channel
		// sanitizeFileText closes one argument earlier.
		return fmt.Errorf("%s sets the retired `nibs.path: %q`, whose contents cannot be read (%s) — so whether this project's nibs are in %s cannot be determined; resolve that (mount the volume, fix its permissions), then re-run (do NOT run `nibs init`, and do NOT remove the `nibs.path` key: it is the only record of where the nibs are)",
			legacy, sanitizeFileText(declared), flattenReason(evErr.Error()),
			sanitizeFilePath(dataDir))
	}
	// Naming AND containment must both hold before "nothing in it was written by
	// nibs" is the accurate reason: a `nibs.path` satisfied only by a symlink out
	// of the project fails on containment, where the contents were never asked
	// about.
	why := "which `nibs migrate` will not relocate for you because, with symlinks resolved, it is not an immediate subdirectory of " + projectDir
	named, namedErr := legacyConfigNamesStore(dataDir)
	inside, insideErr := isRealImmediateChild(dataDir, projectDir)
	if namedErr == nil && named && insideErr == nil && inside {
		why = "which `nibs migrate` will not relocate for you because nothing in it was written by nibs (no markdown file in it was rendered by nibs)"
	}
	return fmt.Errorf("%s%s sets the retired `nibs.path: %q`; this project's nibs live in %s, %s — create %s, move this project's nib files from %s into it, remove the `nibs.path` key from %s, then run `nibs migrate` (do NOT run `nibs init`, which would create an empty store beside the real data)",
		obstruction, legacy, sanitizeFileText(declared),
		sanitizeFilePath(dataDir), why, target, sanitizeFilePath(dataDir), legacy)
}

// looksLikeStore reports whether dir carries positive evidence of being a nibs
// store, so an explicitly named directory can never silently resolve to an
// ordinary project directory.
//
// CANONICAL STATEMENT OF THE EVIDENCE RULE; other sites point here. This is
// structurally an AUTHORIZATION check — it decides whether `nibs migrate` may
// move and rewrite a whole subtree — so the evidence must be something a nibs
// store PRODUCES, not something a directory may merely be CALLED. Any one of:
//
//   - named `.nibs` AND a REAL DIRECTORY. The name is the marker
//     store.FindStore recognizes, and an empty one is a legal fresh store. A
//     SYMLINK named `.nibs` is deliberately not covered: for a link the name
//     and the directory it leads to are different things, so it falls through
//     to the clauses below — which is what keeps a deliberate
//     `.nibs -> ~/sync/proj-nibs` working;
//   - it holds a config.yml that PARSES as a nibs config, keeping a store the
//     user deliberately put somewhere other than `.nibs` reachable;
//   - a pre-layout `.nibs.yml` beside it NAMES it through the retired
//     `nibs.path` key (hasLegacyStoreShape), keeping a pre-layout store outside
//     `.nibs` reachable by `nibs migrate`.
//
// NOT evidence, since each accepts an ordinary project directory: a bare `data/`
// or `archive/` subdirectory (`data/` is standard in Hugo); a file merely NAMED
// config.yml; or "a `.nibs.yml` beside it and some `*.md` in it", which accepts
// any docs/ or notes/ directory. A config-less store does occur, but every store
// nibs creates is named `.nibs` and never behind a link
// (refuseSymlinkedStoreDir), so refusing a config-less `.nibs` LINK strands
// nothing.
//
// THREE-WAY: an error means evidence EXISTS but could not be established (a
// config.yml over config.MaxConfigBytes, an unreadable `.nibs.yml`). Reporting
// that as "no evidence" sends the user to run `nibs init` over real data.
func looksLikeStore(dir string) (bool, error) {
	if filepath.Base(dir) == store.DirName {
		link, err := isSymlink(dir)
		if err != nil {
			return false, err
		}
		if !link {
			return true, nil
		}
		// A SYMLINK named `.nibs` falls through to the evidence below.
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
// top-level `nibs:` MAPPING — the shape of every config `nibs init` writes. A
// directory, a dangling symlink, unparseable YAML, or a scalar `nibs` key are
// each a DETERMINATE no; bytes that could not be obtained (permissions, or over
// config.MaxConfigBytes) return the error, since a size refusal reported as
// absence made a real store answer "is not a nibs store … run `nibs init`".
//
// The IsRegular check is NOT what keeps a FIFO from hanging the process —
// config.ReadConfigFile refuses an irregular file for every reader. It picks
// WHICH answer one gets: without it a pipe named config.yml returns the reader's
// error, and "cannot tell" is wrong about a path that plainly holds no config.
// Deleting it costs determinacy, not liveness.
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
	// A node tree rather than a struct probe: yaml.v3 leaves a struct's
	// *yaml.Node field zero-Kind, so "the key is a mapping" is unanswerable that
	// way. mappingValue is the accessor the layout step's rewrite uses.
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

// hasLegacyStoreShape reports whether dir is a store `nibs migrate` may
// relocate: a pre-layout project's `.nibs.yml` NAMES it through the retired
// `nibs.path` key, AND either something inside it was written by nibs or it is
// empty (declaredStoreCorroborated).
//
// PARENT-ONLY AND ON THE RESOLVED PATH. The `.nibs.yml` is sought in dir's
// PARENT and nowhere else, and dir must resolve — symlinks and all — to a
// directory really inside that parent (isRealImmediateChild). The retired key
// accepted more (`docs/nibs`, an absolute path, `.`, `..`); all are refused:
//
//   - this authorizes relocation and rewriting, and requiring the naming config
//     to sit in the named directory's parent is what stops `nibs.path: ..` or
//     `/etc` authorizing anything outside the project;
//   - the migration engine derives the project from the store's parent
//     (store.Layout.ProjectDir), so for a non-child store the relocation target
//     and the `.nibs.yml` it must delete would both be wrong.
//
// Refused shapes are still SERVED: preLayoutRemedy prints a manual remedy, and
// prints the `--nibs-path` command only for shapes this accepts.
//
// This clause cannot stand in for looksLikeStore's name clause where `.nibs` is
// a SYMLINK: it needs the config to NAME the directory, and a `.nibs.yml`
// carrying no `nibs.path` names nothing. A pre-layout store behind a link is
// therefore refused and converted by the manual remedy instead.
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
// names dir itself through the retired `nibs.path` key — the NAMING half of
// hasLegacyStoreShape, split out so a refusal can say which half failed.
func legacyConfigNamesStore(dir string) (bool, error) {
	declared, resolved, err := legacyDeclaredStorePath(dir)
	if err != nil || declared == "" {
		return false, err
	}
	return sameDir(resolved, dir), nil
}

// legacyDeclaredStorePath returns the store the pre-layout `.nibs.yml` beside dir
// names through the retired `nibs.path` key: the raw value and that value
// resolved against the project directory. Both are empty when there is no such
// file, it carries no `nibs.path`, or on ANY error — so the error must be
// checked before the values, or unreadable evidence reads as absent evidence.
//
// The raw value comes back alongside the resolved one so a refusal can say WHAT
// the config names: the comparison is textual (sameDir), so "no `.nibs.yml`
// beside it names it" is a claim the resolved-path check does not establish.
//
// A DRIVE-RELATIVE `nibs.path` (`C:proj`) is not absolute by filepath.IsAbs, so
// the Join below yields `<project>\C:proj`, which Windows can never create.
// Measured: it then refuses through the same route as any other unusable
// `nibs.path` and prints the manual remedy, so it needs no case of its own.
func legacyDeclaredStorePath(dir string) (declared, resolved string, err error) {
	projectDir := filepath.Dir(dir)
	legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
	info, err := os.Stat(legacy)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", nil
		}
		return "", "", err
	}
	if info.IsDir() {
		return "", "", nil
	}
	declared, err = config.RetiredNibsPath(legacy)
	if err != nil || declared == "" {
		return "", "", err
	}
	resolved = declared
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(projectDir, resolved)
	}
	return declared, resolved, nil
}

// isRealImmediateChild reports whether dir, with every symlink resolved, sits
// directly inside parent with every symlink resolved.
//
// The CONTAINMENT half of hasLegacyStoreShape's parent-only rule, without which
// the rule is worth nothing: every other comparison in that chain is lexical
// (filepath.Dir, Abs, Clean), so a symlink satisfies "an immediate subdirectory
// of the project" while pointing anywhere — and the store walk OPENS its root.
// A repository could otherwise ship `.nibs.yml` (`path: store`) plus a committed
// `store -> /somewhere/else` and have the relocation sweep that tree.
//
// A link staying INSIDE the project is still accepted — containment, not a ban
// on symlinks. One leading OUT is refused, matching how the same store spelled
// as an absolute `nibs.path` is treated.
//
// WINDOWS: case and 8.3 short names are safe, since filepath.EvalSymlinks
// upper-cases the drive letter and rewrites each component to its on-disk
// spelling. UNC IS EXEMPT from that normalization and is NOT safe: normVolumeName
// returns any volume longer than two bytes untouched, so a UNC parent and child
// reaching here from different origins answer FALSE for the same directory. That
// direction is conservative — a false negative refuses and prints the manual
// remedy — so it is documented rather than papered over.
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

// declaredStoreCorroborated reports whether dir holds a file nibs plausibly
// wrote, so a `.nibs.yml` cannot authorize a relocation on its own say-so.
//
// The naming config is untrusted: pre-layout `nibs init` NEVER wrote a
// `nibs.path` other than `.nibs`, so a config pointing elsewhere is always
// hand-authored. Uncorroborated, a repository could name any of its own
// subdirectories and have nibs print `nibs migrate --nibs-path <that dir>`,
// moving every front-mattered .md under it into data/ and rewriting each one.
//
// THIS STOPS AN ACCIDENT, NOT AN ADVERSARY. The corroborating artifact carries
// everything nib.Render always writes (the rule lives in nibRenderFormat), and
// one file anywhere under dir passing is enough. A shape is never provenance —
// anyone who knows the rule can write a file that passes. isRealImmediateChild
// answers THAT case and is why this one may stay weak: corroboration narrows
// which directories a config can name, containment bounds where they can be.
//
// Deliberately NOT keyed on the id matching the config's prefix and id length: a
// project that changed its prefix keeps nibs named under the old one.
//
// An EMPTY directory is corroborated, and only an empty one — a store `nibs
// init` created but never wrote to legitimately holds nothing. Keyed on
// os.ReadDir finding no entries rather than the walk finding no markdown,
// because acceptance authorizes a whole-directory os.Rename plus deletion of the
// project's `.nibs.yml`, a mutation unrelated to file CONTENTS that an asset
// directory would otherwise qualify for.
//
// A header that cannot be READ leaves the answer UNDECIDED, not negative:
// layoutMovableFiles moves such a file into data/ precisely because the scan
// cannot prove it is not a nib.
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
			// DEFINITE evidence of nothing, not undecided: a FIFO carries no front
			// matter whatever it is named, and opening it to find out is the hang
			// the walk exists to avoid.
			if errors.Is(walkErr, nibcore.ErrNotRegularFile) {
				return nil
			}
			return walkErr
		}
		h, hErr := readFrontMatterHeader(path)
		// Front matter that never CLOSES was still read completely — every byte
		// seen, the keys extracted — so it is DETERMINATE and must not reach the
		// third answer, which is for evidence that exists and could not be read and
		// whose remedy says "mount the volume, fix its permissions". A half-written
		// nib is still a nib somebody wrote, which is what this walk asks about.
		if hErr != nil && !errors.Is(hErr, errFrontMatterNotClosed) {
			return hErr
		}
		if nibRenderFormat.rendered(h) {
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

// errStoreCorroborated stops declaredStoreCorroborated's walk at the first nib
// it finds; it never reaches a caller.
var errStoreCorroborated = errors.New("nib file found")

// shellArg renders a path for a copy-pasteable command line, quoting it when it
// carries a character the local shell would split or expand. Nothing is
// collapsed or truncated — unlike sanitizeFileText — because this is the
// argument the user has to run.
//
// The quoting is PLATFORM-SPECIFIC because the trigger set and the quote
// character both differ; see shellarg_windows.go for the measurements.
//
// NOT byte-preserving: stripControlChars maps every non-printable rune to a
// space, so a path really containing one yields a command naming a DIFFERENT
// path. Safe in this direction — a substituted path cannot satisfy
// legacyConfigNamesStore, so the command is refused rather than acting on the
// wrong directory — but do not reach for this where the bytes must survive.
func shellArg(path string) string {
	clean := stripControlChars(path)
	if clean != "" && !strings.ContainsAny(clean, shellArgQuoteTriggers) {
		return clean
	}
	return quoteShellArg(clean)
}

// reportExitError is the single, testable error boundary for the CLI: it maps
// each error's structured CODE to a stable exit status via output.ExitCode
// (NOT_FOUND→3, VALIDATION→2, CONFLICT→4, IO/file→5, anything else→1,
// success→0). Every error-carrying path exits through here — `nibs check` is the
// one command that also calls os.Exit directly, for its issues-found status.
//
// A Reported *output.CodedError prints nothing to stderr: the command already
// wrote the user-visible report to stdout, and duplicating it would corrupt
// callers piping `2>&1 | jq`. Uncoded errors print here, replacing Cobra's
// auto-print (silenced via rootCmd.SilenceErrors).
//
// Arg-count errors arrive already coded: commands wire Args to the
// codedNoArgs/codedExactArgs/codedMinimumNArgs/codedMaximumNArgs helpers
// (cmd/args.go), or to a bespoke validator that routes through cmdError —
// never stock cobra.NoArgs/ExactArgs. So an arity violation is a
// VALIDATION_ERROR (exit 2) with the {error} envelope under --json, matching
// value-validation errors instead of Cobra's plain-text exit 1.
//
// The writer is wrapped in safetext.Writer, making this a STRUCTURAL boundary
// for file-sourced text rather than one every message must remember: refusals
// here quote filenames and front-matter scalars an attacker may choose, and this
// channel carries no styled output, so nothing is lost by neutralizing it
// wholesale. Newlines survive — multi-file refusals list one file per line.
func reportExitError(stderr io.Writer, err error) int {
	if err == nil {
		return output.ExitOK
	}
	sanitized := safetext.NewWriter(stderr)
	// The writer holds a rune split across writes, so anything left at the end of
	// this call must be emitted or it disappears after Fprintf reported it
	// written. The Flush keeps that a property rather than a dependency on both
	// format strings ending in a newline.
	defer func() { _ = sanitized.Flush() }()
	stderr = sanitized
	var ce *output.CodedError
	if errors.As(err, &ce) {
		if !ce.Reported {
			_, _ = fmt.Fprintf(stderr, "Error: %s\n", ce.Msg)
		}
		return output.ExitCode(ce.Code)
	}
	// Best-effort: a broken writer must still exit non-zero so the shell sees it.
	_, _ = fmt.Fprintf(stderr, "Error: %s\n", err.Error())
	if isIOError(err) {
		return output.ExitIO
	}
	return output.ExitError
}

// isIOError reports whether an uncoded error is a filesystem/IO failure, so the
// boundary can map it to output.ExitIO. Coded errors already carry
// output.ErrFileError; this covers plain errors from os/fs calls.
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

// filterReleasedBlockers returns shallow copies of the given nibs with satisfied
// blockers removed from BlockedBy for display. Core's in-memory nibs are not
// mutated. It defers to config.StatusReleasesDependents, the same convention the
// blocking graph uses, so a deferred blocker still blocks — that work is coming
// back — while a completed or scrapped one drops out.
func filterReleasedBlockers(nibs []*nib.Nib, reader graph.NibReader) []*nib.Nib {
	result := make([]*nib.Nib, len(nibs))
	for i, b := range nibs {
		result[i] = filterReleasedBlockersOne(b, reader)
	}
	return result
}

// filterReleasedBlockersOne returns a shallow copy of the nib with satisfied
// blockers removed from BlockedBy. The original is not mutated.
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
