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
	"github.com/spf13/cobra"
)

var (
	initJSON   bool
	initPrefix string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a nibs project",
	Long:  `Creates a .nibs directory and .nibs.yml config file in the current directory.`,
	// Target directory comes from --nibs-path / cwd and the prefix from --prefix;
	// no positional args are read.
	Args: codedNoArgs(&initJSON),
	RunE: func(cmd *cobra.Command, args []string) error {
		var projectDir string
		var nibsDir string
		var dirName string

		// Note: both branches below ((*nibcore.Core).Init at the explicit-
		// path branch and nibcore.Init at the Getwd branch) create the .nibs/
		// directory via MkdirAll BEFORE prefix validation runs further down.
		// If validation fails, the empty .nibs/ remains on disk but .nibs.yml
		// is not written. Rerunning `nibs init --prefix <valid>` after fixing
		// the flag is safe because MkdirAll is a no-op on an existing
		// directory — the two code paths intentionally share this property.
		if nibsPath != "" {
			// Use explicit path for nibs directory
			nibsDir = nibsPath
			projectDir = filepath.Dir(nibsDir)
			dirName = filepath.Base(projectDir)
			// Create the directory using Core.Init to set up .gitignore
			core := nibcore.New(nibsDir, nil)
			if err := core.Init(); err != nil {
				if initJSON {
					return output.Error(output.ErrFileError, err.Error())
				}
				return fmt.Errorf("failed to create directory: %w", err)
			}
		} else {
			// Use current working directory
			dir, err := os.Getwd()
			if err != nil {
				if initJSON {
					return output.Error(output.ErrFileError, err.Error())
				}
				return err
			}

			if err := nibcore.Init(dir); err != nil {
				if initJSON {
					return output.Error(output.ErrFileError, err.Error())
				}
				return fmt.Errorf("failed to initialize: %w", err)
			}

			projectDir = dir
			nibsDir = filepath.Join(dir, ".nibs")
			dirName = filepath.Base(dir)
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
		defaultCfg.SetConfigDir(projectDir)
		if err := defaultCfg.Save(projectDir); err != nil {
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

func init() {
	initCmd.Flags().BoolVar(&initJSON, "json", false, "Output as JSON")
	initCmd.Flags().StringVar(&initPrefix, "prefix", "",
		"Project nib ID prefix (default: derived from directory name, lowercased)")
	rootCmd.AddCommand(initCmd)
}
