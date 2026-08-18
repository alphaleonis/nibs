package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/reprefix"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/spf13/cobra"
)

var (
	initJSON   bool
	initPrefix string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a nibs project",
	Long:  `Creates a .nibs store directory holding config.yml and data/ in the current directory.`,
	// Target directory comes from --nibs-path / cwd and the prefix from --prefix;
	// no positional args are read.
	Args: codedNoArgs(&initJSON),
	RunE: func(cmd *cobra.Command, args []string) error {
		// The store directory is the only handle: --nibs-path names it
		// directly, otherwise it is `.nibs` under the cwd. Everything else —
		// the project directory, the derived prefix, where the config is
		// written — follows from it.
		nibsDir := nibsPath
		if nibsDir == "" {
			dir, err := os.Getwd()
			if err != nil {
				if initJSON {
					return output.Error(output.ErrFileError, err.Error())
				}
				return err
			}
			nibsDir = filepath.Join(dir, store.DirName)
		}
		projectDir := filepath.Dir(nibsDir)
		dirName := filepath.Base(projectDir)

		// init is skip-listed from the pre-run migration gate, so it is one of
		// the few commands that runs on a store nothing else will touch — which
		// makes it the one command that can give a project a SECOND config.
		// A derived prefix beside a real one wedges `nibs migrate` (it refuses
		// two configs it must not choose between) and the two disagree on the
		// load-bearing fields, so deleting the wrong one silently re-prefixes
		// the project. Both checks run before anything is created.
		if err := refuseExistingProjectConfig(nibsDir, projectDir); err != nil {
			return err
		}
		// AFTER the config check, deliberately. A link pointing at a store that
		// is already initialized is a working setup, and "config.yml already
		// exists" is the answer its owner needs; leading with "not through a
		// link" would tell them their layout is unsupported when every other
		// command resolves it. The guard below is for the case where there is no
		// config yet — which is the only case that WRITES.
		if err := refuseSymlinkedStoreDir(nibsDir); err != nil {
			return err
		}

		// Core.Init creates the store's directories BEFORE prefix validation
		// runs further down. If validation fails, the empty store remains on
		// disk but no config is written. Rerunning `nibs init --prefix <valid>`
		// after fixing the flag is safe because MkdirAll is a no-op on an
		// existing directory.
		core := nibcore.New(nibsDir, nil)
		if err := core.Init(); err != nil {
			if initJSON {
				return output.Error(output.ErrFileError, err.Error())
			}
			return fmt.Errorf("failed to create store directory: %w", err)
		}

		// Compute the project nib ID prefix. The explicit path (user passed
		// --prefix) honors case exactly; the derived path (fallback to
		// dirname) is safe to lowercase since the user didn't choose it.
		// Both paths auto-append a trailing dash if missing, then validate.
		var prefix string
		if initPrefix != "" {
			prefix = initPrefix
		} else {
			prefix = strings.ToLower(dirName)
		}
		if !strings.HasSuffix(prefix, "-") {
			prefix += "-"
		}
		if err := reprefix.ValidatePrefix(prefix); err != nil {
			if initPrefix != "" {
				// Explicit failure — no --prefix hint (tautological).
				return cmdError(initJSON, output.ErrValidation, "%v", err)
			}
			// Derived failure — suggest the escape hatch.
			return cmdError(initJSON, output.ErrValidation,
				"derived prefix %q (from directory %q) is not valid: %v\npass --prefix explicitly",
				prefix, dirName, err)
		}

		// Load user config to seed preferences into the new project
		userCfg, err := config.LoadUserConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load user config: %v\n", err)
			userCfg = &config.UserConfig{}
		}

		// Create default config file with the computed prefix,
		// seeding values from user config where available
		defaultCfg := config.DefaultWithPrefixFromUserConfig(prefix, userCfg)
		defaultCfg.SetStoreDir(nibsDir)
		if _, err := defaultCfg.Save(nibsDir); err != nil {
			if initJSON {
				return output.Error(output.ErrFileError, err.Error())
			}
			return fmt.Errorf("failed to create config: %w", err)
		}

		if initJSON {
			return output.SuccessInit(nibsDir)
		}

		fmt.Println("Initialized nibs project")
		return nil
	},
}

// refuseSymlinkedStoreDir stops `nibs init` from creating a store through a
// `.nibs` that is a SYMLINK.
//
// Core.Init is os.MkdirAll on `<store>/data`, and MkdirAll follows a link — so
// for a project carrying a committed `.nibs -> /outside` the store was created
// inside the link's destination. That directory then holds a config.yml that
// PARSES, which is the one artifact a nibs store cannot be mistaken about: every
// route binds it from then on, and `nibs migrate`'s layout step moves its
// front-mattered files into data/ and rewrites each as a nib render. The
// resolution guard refuses that tree on sight (see cmd/root.go's
// symlinkedStoreError) — and this is how a reader following that very refusal
// used to hand it the evidence it was missing.
//
// NO EXEMPTION, including an empty destination. Setting up
// `.nibs -> ~/sync/proj-nibs` and running `nibs init` to populate it was a real
// workflow and this refuses it, which is a recorded decision rather than an
// oversight: at the moment init runs, nothing on disk tells that shape apart from
// the hazard above, and "the destination happens to be empty" is a fact about the
// victim's filesystem rather than about the link. The remedy names the directory
// instead of reaching it through a link, which says the same thing without a
// guess.
//
// It costs that remedy the derived prefix, and the message says so rather than
// letting it surprise: `nibs init --nibs-path <dir>` derives the prefix from
// <dir>'s PARENT, so a store outside the project is named after whatever contains
// it (measured: `--nibs-path ~/sync/proj-nibs` yields `sync-`).
//
// os.Lstat, not os.Stat: Stat follows the link, which is the whole thing being
// guarded against. A link that leads NOWHERE is refused by the same rule and for
// its own reason — MkdirAll answers it with `mkdir <link>: file exists`, which
// names a path the reader sees as a link, calls it a file, and reports existence
// as the problem.
func refuseSymlinkedStoreDir(nibsDir string) error {
	info, err := os.Lstat(nibsDir)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	// Where the link leads is the fact the reader acts on, so it is worth two
	// attempts: the resolved path, then the link's own value when resolution
	// fails (a dangling link, or a parent this process cannot traverse). The
	// clause is dropped rather than filled with nibsDir, which would describe a
	// link pointing at itself.
	where := ""
	if resolved, resolveErr := filepath.EvalSymlinks(nibsDir); resolveErr == nil {
		where = " to " + sanitizeFilePath(resolved)
	} else if declared, linkErr := os.Readlink(nibsDir); linkErr == nil {
		where = " to " + sanitizeFilePath(declared) + ", which is not there"
	}
	return cmdError(initJSON, output.ErrValidation,
		"%s is a symlink%s, and `nibs init` will not create a store through one: the store would land at the link's other end rather than in this project, which is how a repository that ships a link gets its own tree adopted as the project's store. Remove or repoint the link, then re-run. To keep this project's store elsewhere on purpose, name that directory with --nibs-path — and --prefix with it, because a store outside the project derives its prefix from its own parent — then point %s at it",
		nibsDir, where, nibsDir)
}

// refuseExistingProjectConfig stops `nibs init` from adding a config to a
// project that already has one, in either layout: inside the store
// (<store>/config.yml), or beside it as the pre-layout `.nibs.yml`. The
// pre-layout case names `nibs migrate` rather than init, because migrating is
// what that project actually needs.
func refuseExistingProjectConfig(nibsDir, projectDir string) error {
	storeConfig := store.NewLayout(nibsDir).ConfigPath()
	if _, err := os.Stat(storeConfig); err == nil {
		return cmdError(initJSON, output.ErrValidation,
			"%s already exists; nibs init will not overwrite a project's %s", storeConfig, store.ConfigFileName)
	}
	legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
	if _, err := os.Stat(legacy); err == nil {
		return cmdError(initJSON, output.ErrValidation,
			"%s already has a pre-layout config (%s); run `nibs migrate` to move it into the store rather than `nibs init`, which would create a second config with a derived prefix",
			projectDir, legacy)
	}
	return nil
}

func init() {
	initCmd.Flags().BoolVar(&initJSON, "json", false, "Output as JSON")
	initCmd.Flags().StringVar(&initPrefix, "prefix", "",
		"Project nib ID prefix (default: derived from directory name, lowercased)")
	rootCmd.AddCommand(initCmd)
}
