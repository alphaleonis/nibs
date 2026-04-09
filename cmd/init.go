package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/output"
)

var initJSON bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a nibs project",
	Long:  `Creates a .nibs directory and .nibs.yml config file in the current directory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var projectDir string
		var nibsDir string
		var dirName string

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

		// Load user config to seed preferences into the new project
		userCfg, err := config.LoadUserConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load user config: %v\n", err)
			userCfg = &config.UserConfig{}
		}

		// Create default config file with directory name as prefix,
		// seeding values from user config where available
		defaultCfg := config.DefaultWithPrefixFromUserConfig(dirName+"-", userCfg)
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
	rootCmd.AddCommand(initCmd)
}
