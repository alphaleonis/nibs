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
		if commandNeedsNoStore(cmd) {
			return nil
		}
		app, err := initAppForCommand(cmd)
		if err != nil {
			// A TAB press must never fail. The hidden __complete command runs on
			// every one of them, and its output IS the completion list: a shell
			// that gets an error instead falls back to completing FILENAMES, so
			// `nibs <TAB>` outside a project — or inside one that needs migrating
			// — offers the contents of the current directory in place of the
			// subcommand list. Degrading to no App, rather than skipping the
			// resolution outright, keeps the App available wherever a store does
			// resolve, which is what a completer over nib ids will need.
			if isCompletionRequest(cmd) {
				return nil
			}
			return err
		}
		cmd.SetContext(withApp(cmd.Context(), app))
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

// commandNeedsNoStore reports whether cmd can do its whole job without a store,
// so PersistentPreRunE skips resolving one. A command listed here must NOT call
// getApp(): there is no App in its context.
//
// migrate is here because it must run on the very stores the hook refuses; it
// resolves config and the store root itself.
//
// HELP AND COMPLETION ARE TREES, NOT COMMANDS, which is why they are matched by
// lineage rather than by name. `nibs completion bash` executes the "bash"
// subcommand, so a name check would have to list every shell and would miss the
// next one added. Both failed outside a project with "no .nibs directory found" —
// help, which is where a user reads how to create a project, and completion,
// which a shell sources on every new session.
//
// `nibs --help` never failed, and the asymmetry is worth knowing: Cobra's ErrHelp
// path returns before these hooks run, so the FLAG and the SUBCOMMAND reach the
// same output by different routes and only one of them passed through here.
func commandNeedsNoStore(cmd *cobra.Command) bool {
	// Matched on the executed command's own name, so a future `nibs <x> init`
	// would skip the store too and panic in getApp. Every one of these is a
	// direct child of root today; nest one and this needs the lineage treatment
	// below.
	//
	// cmd.Name() is never an ALIAS, which is what makes these names the right
	// keys and not a trap: `query` is the real name of the command also reached
	// as `nibs graphql`, so both spellings land here. Keying the alias instead is
	// how updateNotifySkip silently exempted nothing (nibs-rg07).
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
			return nil, err
		}
	}

	core := nibcore.New(root, cfg)
	if err := core.Load(); err != nil {
		return nil, fmt.Errorf("loading nibs: %w", err)
	}

	// Getting past the gate IS the answer to "does this store need
	// migration?", so record it rather than making a command re-scan for it
	// (see App.MigrationGatePassed).
	return &App{Core: core, MigrationGatePassed: gated}, nil
}

// isCompletionRequest reports whether cmd is the hidden command a shell runs on
// every TAB press.
//
// Distinct from commandNeedsNoStore on purpose. `nibs completion <shell>` runs
// ONCE, at install, and needs no store at all; `__complete` runs constantly and
// would be able to USE one — a completer over nib ids is the obvious next thing
// to want — so it resolves a store when there is one and degrades quietly when
// there is not.
func isCompletionRequest(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == cobra.ShellCompRequestCmd || c.Name() == cobra.ShellCompNoDescRequestCmd {
			return true
		}
	}
	return false
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
// Whichever route names the directory, it must carry positive evidence that it
// IS a store, and that check is ONE function every route shares — see
// bindNamedStore. Existence alone is not enough: a path aimed one level too high
// resolves to the project tree, and `nibs migrate` would then move and rewrite
// every front-mattered .md it finds there while the real store went untouched.
//
// The DISCOVERED route answers with the nearest marker of EITHER kind — a
// `.nibs` store or a pre-layout `.nibs.yml` — because the two are alternatives
// rather than independent searches: a pre-layout project between the cwd and a
// store is the project the user is in, and binding past it to a store belonging
// to someone else is a mutation waiting to happen (see preLayoutProjectError).
// What that walk finds still goes through bindNamedStore: it matches a `.nibs`
// on its NAME, which is evidence for a real directory and no evidence at all for
// a link.
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
		return bindNamedStore(explicit)
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
		// The SAME decision the explicit routes make: a real `.nibs` is accepted
		// on its name, while a `.nibs` SYMLINK has to carry the evidence its name
		// cannot vouch for.
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
// (--nibs-path, NIBS_PATH, --config's containing directory) and the upward walk
// that discovers one — so all four answer "is this a store?" with ONE decision.
// A route answering it on its own is the defect this exists to prevent: with the
// explicit routes consulting looksLikeStore while the walk matched a `.nibs` on
// its name alone, a committed `.nibs -> /outside` bound with no flag at all and
// `nibs migrate` planned to move that outside tree into `<project>/.nibs` and
// rewrite every front-mattered file in it.
//
// Sharing it costs the discovery route nothing: for a REAL `.nibs` directory
// looksLikeStore answers on its name clause, before anything is opened.
func bindNamedStore(dir string) (string, error) {
	is, err := looksLikeStore(dir)
	if err != nil {
		// "Cannot determine" must never be reported as "no evidence": the
		// message below tells the user to run `nibs init` here, and doing that
		// over a real store whose config merely could not be read creates a
		// second, empty store beside their data.
		return "", fmt.Errorf("cannot tell whether %s is a nibs store: %w; repair or remove that file, then re-run", dir, err)
	}
	if !is {
		// A `.nibs` that is a SYMLINK gets its own refusal, and it has to come
		// FIRST — not because the branches below are unhelpful, but because they
		// are false here. Every one of them converges on "create <project>/.nibs
		// and move the nib files into it", which the reader cannot do while a link
		// holds that name, and the "store this project already has" clause tests
		// isDir(<project>/.nibs) — this very directory — so reaching it would
		// advise the path being refused.
		//
		// A failed Lstat falls through rather than refusing: os.Stat already
		// followed this path one moment ago, so an error here means the link moved
		// under us, and the branches below answer for whatever is there now.
		if link, linkErr := isSymlink(dir); linkErr == nil && link && filepath.Base(dir) == store.DirName {
			return "", symlinkedStoreError(dir)
		}
		// A `.nibs.yml` that NAMES this directory but no artifact inside it is
		// its own answer: "no .nibs.yml beside it names it" would be false, and
		// `nibs init` is the wrong advice when the naming config is real. Say
		// which half of the evidence is missing, and say only what was
		// established — the two halves fail for different reasons.
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
		// The naming clause reports what was CHECKED. A `.nibs.yml` beside it
		// that declares some other path is not "no config names it", and the
		// difference matters most where it is least visible: sameDir compares
		// paths as text, so on a case-insensitive filesystem the other path can
		// be this very directory under another spelling. Denying that any
		// config names it then sends a user standing on real nibs to
		// `nibs init`.
		//
		// "It names a different DIRECTORY" is that same unestablished claim
		// wearing a conclusion, and it is false in exactly the cases the
		// comparison is weakest on: a symlink alias and a case variant both
		// fail sameDir while reaching this very directory (`<proj>/link ->
		// nibdata` and `<proj>/nibdata` report one inode, and either spelling
		// resolves as the store). So the clause states the comparison instead
		// — the declared value where this branch has one, and that the match
		// is textual — which stays true whether the two names are two
		// directories or one.
		//
		// What such a config describes is a PRE-LAYOUT PROJECT, so the remedy
		// is preLayoutRemedy's — the same answer the discovery route gives for
		// the same project. It names the declared value, the directory that
		// value resolves to and what to do about it, and it prescribes
		// `nibs migrate` only for the shapes the store-evidence guard accepts.
		// Advice of the form "name it the way the config does" cannot make
		// that distinction: for `docs/nibs`, an absolute path, `.` or `..` —
		// shapes that guard refuses — following it lands on a second refusal
		// whose only advice is the `nibs init` this branch exists to avoid.
		//
		// It also keeps the resolved path out of sanitizeFileText's hands. The
		// declared value is echoed as EVIDENCE, collapsed and bounded because
		// it is untrusted file content; the path the reader has to act on is
		// the resolved one, which must survive intact.
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
		// declaredErr is nil by construction here, so this is a genuine
		// absence: looksLikeStore reaches the same `.nibs.yml` through
		// hasLegacyStoreShape, and an unreadable one has already been reported
		// as the "cannot tell" third answer above. If that ordering ever
		// changes, this flat denial becomes the false claim about an
		// unreadable config that the third answer exists to prevent.
		//
		// It also rests on the two reads of that one file AGREEING, which is
		// a property of the file rather than of this code: a FIFO served a
		// valid `nibs.path` to the first read and malformed YAML to the
		// second, and this branch duly denied that any config named the
		// directory one line after the other read had found one. That is why
		// config.ReadConfigFile refuses anything but a regular file.
		return "", fmt.Errorf("%s is not a nibs store: it holds no %s that parses as one, and no %s beside it names it; name the store directory itself (e.g. --nibs-path %s), or run `nibs init` there",
			dir, store.ConfigFileName, store.LegacyProjectConfigFileName,
			filepath.Join(dir, store.DirName))
	}
	return dir, nil
}

// isSymlink reports whether path is ITSELF a symbolic link, which os.Stat cannot
// answer because it follows one. It is the distinction looksLikeStore's name
// clause turns on: for a real directory the name and the directory are the same
// thing, and for a link they are not.
//
// WINDOWS, read off the toolchain rather than reasoned: os.Lstat sets
// ModeSymlink for IO_REPARSE_TAG_SYMLINK only, and every other reparse tag —
// a JUNCTION (IO_REPARSE_TAG_MOUNT_POINT) among them — gets ModeIrregular
// instead (go/src/os/types_windows.go, Go 1.26). So a `.nibs` junction is read
// as a real directory here and keeps the name clause. Under GODEBUG=winsymlink=0
// that reverts — modePreGo1_23 returns ModeSymlink for a junction too — which
// only makes this guard STRICTER, so it needs no handling of its own.
//
// That is the intended bound rather than a gap left open. The threat is a link a
// CLONE materializes, and git checkout writes a symlink or a plain file, never a
// junction — a junction is something a user typed `mklink /J` for, which is the
// deliberate off-repo store this rule exists to keep working. Widening the test
// to "not a plain directory" would also catch cloud-storage placeholders and
// other reparse tags a real store may legitimately sit on.
func isSymlink(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}

// symlinkedStoreError refuses a `.nibs` that is a SYMLINK carrying no evidence of
// being a store.
//
// looksLikeStore's name clause exists because `.nibs` is the marker nibs itself
// creates and the upward walk recognizes. A LINK is exactly the case that
// reasoning cannot cover: the name is here and the directory is somewhere else,
// so the name says nothing about what is being authorized. A committed
// `.nibs -> /outside` was accepted on every route, and `nibs migrate` then planned
// to sweep that whole tree into `<project>/.nibs` while the project's real nibs
// went untouched and unreferenced.
//
// Links are NOT banned, and this is deliberately an evidence rule rather than a
// containment one: `.nibs -> ~/sync/proj-nibs` pointing at a genuine store is a
// legitimate way to keep nibs out of the code repository, and it resolves through
// the config clause without ever reaching here. What is refused is trusting the
// name over the destination.
//
// The remedy for the PROJECT is preLayoutRemedy's wherever a pre-layout
// `.nibs.yml` is beside the link, so this route cannot disagree with the other
// three about what to do with it. The link is named first either way, because
// every remedy those refusals converge on begins by creating `<project>/.nibs` —
// and the link is holding that name.
//
// The link's destination is echoed through sanitizeFilePath for the same reason a
// `nibs.path` value is: it is bytes a cloned repository chose, in a message whose
// primary consumer is an agent.
func symlinkedStoreError(dir string) error {
	// EvalSymlinks resolves EVERY component, so it can fail on a parent this
	// process cannot traverse even though os.Stat followed the link a moment
	// ago. The link's own value is the honest fallback — it is what the
	// repository committed — and where even that cannot be read the destination
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

	// A stat that fails for any reason OTHER than absence counts as present, the
	// same reading the --config guard takes: preLayoutRemedy's first branch
	// reports an unreadable pre-layout config as exactly that, while the
	// `nibs init` advice below would be wrong over a config that is really there.
	legacy := filepath.Join(filepath.Dir(dir), store.LegacyProjectConfigFileName)
	if _, err := os.Stat(legacy); !errors.Is(err, fs.ErrNotExist) {
		// preLayoutRemedy already says the name is occupied and must be cleared —
		// it is the shared remedy, and duplicating that sentence here is how the
		// two would come to disagree.
		return fmt.Errorf("%s. What this project needs instead: %w", lead, preLayoutRemedy(legacy))
	}
	// `nibs init` is safe to prescribe here only because init refuses through a
	// link itself (refuseSymlinkedStoreDir). Without that guard MkdirAll follows
	// the link, so a reader who runs the prescription without removing the link
	// first writes config.yml and data/ INTO the destination — which then parses
	// as a store and re-opens the very hazard this refusal closed. The two are
	// one rule in two files; changing either without the other reopens it.
	return fmt.Errorf("%s; repoint it at a directory that really is a store, or remove the link and run `nibs init` here", lead)
}

// bindsAsStore reports whether dir is a directory bindNamedStore would ACCEPT.
// It is the test every "the store is right there, pass --nibs-path X" advice
// owes, and it is narrower than isDir on purpose: isDir follows a symlink, so
// three refusals advised a `.nibs` link the resolver then refused, stranding the
// reader one command later on a second refusal — the shape
// TestEveryRefusalNamesAReachablePathAndARunnableCommand exists to forbid.
//
// "Cannot tell" collapses into FALSE, deliberately. A message may prescribe only
// a store it has ESTABLISHED, and each caller's other branch is a remedy that
// converges anyway; advising a directory whose evidence could not be read would
// print a command the resolver answers with "cannot tell whether …".
func bindsAsStore(dir string) bool {
	if !isDir(dir) {
		return false
	}
	ok, err := looksLikeStore(dir)
	return err == nil && ok
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
// legacy, where its nibs are and which remedy converges. It is shared by the
// three refusals that reach a pre-layout project — the upward walk meeting one,
// `--config` aimed straight at the file, and a named directory whose `.nibs.yml`
// declares some other store — so they can never disagree about what to do about
// it.
//
// PRECONDITION: no `.nibs` that BINDS AS A STORE sits beside legacy. Every caller
// establishes that (the walk binds to a store in the same directory before it
// looks for this file; the --config guard and bindNamedStore's naming clause both
// consult bindsAsStore first), and the "no store beside it" branch below states it
// as fact. It is load-bearing rather than tidy: three branches below tell the
// reader to CREATE that directory, and a fourth prescribes a migration whose
// destination it is.
//
// A NON-store may still be sitting on the name — a `.nibs` symlink leading
// somewhere that carries no store evidence is the shape symlinkedStoreError hands
// over from, and a dangling one blocks MkdirAll the same way. That is why the
// obstruction clause below exists rather than the precondition simply forbidding
// it: "no store beside it" stays true, while "create <project>/.nibs" would be
// unfollowable in silence. Stating it once here is also what lets that caller
// delegate instead of writing a fifth copy of this remedy.
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
// EVERY string here that came from the config crosses a boundary, and which one
// depends on what the string is FOR. The declared VALUE is quoted evidence, so it
// goes through sanitizeFileText — collapsed onto a line and bounded — and is
// printed with %q. The resolved directory is a path the reader goes and looks at,
// so it goes through sanitizeFilePath, which bounds it but leaves its spaces
// alone: the value is joined onto the project directory, and a config may be a
// megabyte, so an unbounded interpolation repeated that megabyte per site. The
// path that appears as a COMMAND ARGUMENT gets shellArg — stripControlChars plus
// shell quoting, unbounded, because truncating the one string the reader has to
// run would corrupt it, and that branch is reached only for a directory the
// filesystem could open.
//
// What all three answer is the SEMANTIC channel, not the terminal one: a value
// from a cloned repository's `.nibs.yml` sits in the same sentence as a command
// the reader is told to run, and this CLI's stated primary consumer is an agent
// primed to follow instructions. %q alone does NOT close it, and the claim that it
// did was false in the exact way that mattered: %q's delimiter is the double
// quote, the span's is the backtick, and %q escapes the first and never the
// second. A value carrying a backtick therefore closed the span quoting it and
// rendered as prose plus a runnable `nibs migrate --nibs-path /etc` — three times
// in one message, once per interpolation, since the derived path and the command
// argument carry the same bytes. The backtick is answered where every one of these
// renderings passes: safetext.Strip substitutes it, so no config value can put a
// delimiter into a message. %q stays for what it does do — bounding the value with
// a visible pair of quotes so a reader can see where it ends.
func preLayoutRemedy(legacy string) error {
	projectDir := filepath.Dir(legacy)
	target := filepath.Join(projectDir, store.DirName)
	// Every remedy below needs the name `<project>/.nibs` — three tell the reader
	// to CREATE it and the fourth migrates a store TO it — so something already
	// sitting on that name without being a store makes all four unfollowable.
	// Said ONCE here rather than per branch, which is also what lets the caller
	// that met such a thing hand over instead of writing its own remedy.
	//
	// os.Lstat, not isDir: a link is what occupies the name in the shape this was
	// written for, and a DANGLING one blocks MkdirAll just as effectively while
	// isDir reports it absent.
	obstruction := ""
	if _, err := os.Lstat(target); err == nil && !bindsAsStore(target) {
		obstruction = target + " is already taken by something that is not a nibs store, and every remedy here needs that name — move or remove it first; then "
	}
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
			return fmt.Errorf("%s%s sets the retired `nibs.path: %q`, but %s does not exist — so this project's nib files are not where the config says they are; find them, create %s and move them into it, then run `nibs migrate` (do NOT run `nibs init`, which would create an empty store beside data that may already exist)",
				obstruction, legacy, sanitizeFileText(declared),
				sanitizeFilePath(dataDir), target)
		}
		// flattenReason, not %v: an OS error embeds the path it failed on, and that
		// path is built from the declared value — so interpolating the error raw
		// reopens the very channel sanitizeFileText closes one argument earlier.
		// Reached on POSIX by a permission error, and by a YAML error quoting file
		// contents.
		return fmt.Errorf("%s sets the retired `nibs.path: %q`, whose contents cannot be read (%s) — so whether this project's nibs are in %s cannot be determined; resolve that (mount the volume, fix its permissions), then re-run (do NOT run `nibs init`, and do NOT remove the `nibs.path` key: it is the only record of where the nibs are)",
			legacy, sanitizeFileText(declared), flattenReason(evErr.Error()),
			sanitizeFilePath(dataDir))
	}
	// Naming AND containment both have to hold before "nothing in it was written
	// by nibs" is the accurate reason: a `nibs.path` satisfied only by a symlink
	// out of the project fails on containment, and saying anything about the
	// directory's contents there would answer a question that was never asked.
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
// ordinary project directory (see resolveStoreDir).
//
// This is structurally an authorization check: it decides whether `nibs migrate`
// may move and rewrite a whole subtree. So the evidence must be something only a
// nibs store PRODUCES, not something a directory might merely be CALLED. Any ONE
// of these suffices:
//
//   - the directory is named `.nibs` AND IS A REAL DIRECTORY — the name IS the
//     marker store.FindStore recognizes, and an empty one is a legal freshly
//     created store. A SYMLINK named `.nibs` is deliberately not covered: the
//     name is what this clause trusts, and for a link the name and the directory
//     it leads to are different things, so a committed `.nibs -> /outside` was
//     bound as the store on every route and `nibs migrate` planned to sweep that
//     tree into `<project>/.nibs`. Such a link falls through to the clauses
//     below and is accepted only on the evidence they read, which is what keeps
//     a deliberate `.nibs -> ~/sync/proj-nibs` working;
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
//     every store nibs creates or migrates to is named `.nibs`. That covers it
//     under the name clause whenever `.nibs` is a real directory, which is what
//     nibs itself makes — and nibs never makes one behind a link, because init
//     refuses to create a store through one (refuseSymlinkedStoreDir). So a
//     config-less `.nibs` link is not a store this tool left behind, and refusing
//     it strands nothing;
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
		link, err := isSymlink(dir)
		if err != nil {
			return false, err
		}
		if !link {
			return true, nil
		}
		// A SYMLINK named `.nibs` falls through to the evidence below instead of
		// short-circuiting — see symlinkedStoreError.
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
//
// The IsRegular check below is NOT what keeps a FIFO here from hanging the
// process — config.ReadConfigFile refuses an irregular file for every reader.
// It survives because it picks which of the two answers an irregular file gets:
// without it a pipe named config.yml would come back as the reader's error, and
// "cannot tell whether this is a store" is the wrong answer about a path that
// plainly holds no config. Deleting it costs determinacy, not liveness.
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
// The shapes refused here are still SERVED — preLayoutRemedy prints a manual
// remedy for them, and it prints the `--nibs-path` command only for the shapes
// this predicate accepts, so the tool never prescribes a command it refuses.
// Every refusal that has to answer for a declared store routes through it for
// exactly that reason.
//
// A `.nibs.yml` carrying no `nibs.path` describes a store at `<project>/.nibs`,
// which looksLikeStore's name clause accepts whenever that is a real directory —
// so there is nothing for this clause to add there. Where `.nibs` is a SYMLINK
// the name clause does not fire and this one cannot stand in for it: it needs the
// config to NAME the directory, and a config carrying no `nibs.path` names
// nothing. A pre-layout store reached through a link is therefore refused, and
// converted by the manual remedy rather than through the link — see
// symlinkedStoreError.
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
func legacyConfigNamesStore(dir string) (bool, error) {
	declared, resolved, err := legacyDeclaredStorePath(dir)
	if err != nil || declared == "" {
		return false, err
	}
	return sameDir(resolved, dir), nil
}

// legacyDeclaredStorePath returns the store the pre-layout `.nibs.yml` beside dir
// names through the retired `nibs.path` key: the value as the file writes it, and
// that value resolved against the project directory. Both are empty when there is
// no such file or it carries no `nibs.path`, and on any error — which is why the
// error must be checked before the values are read, or unreadable evidence reads
// as absent evidence.
//
// The raw value is returned alongside the resolved one because a refusal has to
// be able to say WHAT the config names rather than only that it does not name the
// directory in hand — the comparison is textual (see sameDir), so "no `.nibs.yml`
// beside it names it" is a claim the resolved-path check does not establish.
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

// declaredStoreCorroborated reports whether dir holds a file nibs plausibly
// wrote, so a `.nibs.yml` cannot authorize a relocation on its own say-so.
//
// The naming config is untrusted content: a cloned repository chooses its own
// `nibs.path`, and pre-layout `nibs init` NEVER wrote a value other than `.nibs`
// — so a config pointing somewhere else is always hand-authored. Without
// corroboration a repository could name any of its own subdirectories and have
// nibs print `nibs migrate --nibs-path <that dir>`, which moves every
// front-mattered .md under it into data/ and rewrites each one as a nib render.
//
// WHAT THIS STOPS IS AN ACCIDENT, NOT AN ADVERSARY, and reading it as more than
// that is how the predicate gets left alone when it should not be. The
// corroborating artifact is a file carrying everything nib.Render ALWAYS writes:
// the id comment on the first line inside the fence, then `version`, `title` and
// `status`, the three keys renderFrontMatter emits unconditionally. The rule
// itself lives in nibRenderFormat; one file anywhere under dir passing it is
// enough.
//
// A single `status:` used to be the whole bar, and ordinary content reached it:
// note vaults and docs sites track a page's own state that way, so a `notes/`
// directory was renamed to `.nibs` and every file in it rewritten as a nib
// render. The rendered shape is much harder to meet by accident — a
// hand-authored header rarely opens with a comment — but it is still a SHAPE,
// never provenance: anyone who knows the rule can write a file that passes, and
// whoever authors `.nibs.yml` authors the .md files beside it. So against a
// repository that CHOSE its `nibs.path` this proves nothing, and no further
// raising of the bar would change that — measured, in
// TestCorroborationDocMatchesWhatTheArtifactProves.
//
// isRealImmediateChild is what answers that case, and it is the reason this one
// may stay this weak: the named directory has to resolve, symlinks and all, to a
// real child of the project, so a hostile config reaches nothing outside the
// checkout it ships in. Corroboration narrows which directories inside that
// checkout it can name; containment is what bounds where they can be.
//
// Deliberately NOT keyed on the id matching the config's prefix and id length —
// a project that changed its prefix keeps nibs named under the old one, and
// refusing its real store would be worse than the risk this closes.
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
			// An entry the walk declined to open is DEFINITE evidence of nothing,
			// not undecided evidence: a FIFO carries no front matter whatever it
			// is named, so the walk continues rather than answering "cannot tell"
			// — which is the third answer, reserved for evidence that exists and
			// could not be read. It also cannot be read here to find out: opening
			// it is the hang the walk exists to avoid.
			if errors.Is(walkErr, nibcore.ErrNotRegularFile) {
				return nil
			}
			return walkErr
		}
		h, hErr := readFrontMatterHeader(path)
		// A file whose front matter never CLOSES was read completely — the scan
		// saw every byte and extracted the keys — so it is determinate evidence
		// and must not reach the third answer. The distinction is the whole
		// difference between the two branches here: "cannot tell" is for evidence
		// that EXISTS AND COULD NOT BE READ (a permission error, an unmounted
		// volume), where the directory may be real and full of nibs, and its
		// remedy says so — "mount the volume, fix its permissions". Answering a
		// torn nib that way locked every command out of a pre-layout project,
		// including the `nibs migrate` that would fix it, and prescribed mounting
		// a volume that was never absent. A half-written nib is still a nib
		// somebody wrote, which is exactly what this walk is asking about.
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
