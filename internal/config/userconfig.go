package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// UserConfig holds user-level configuration that provides defaults
// across all projects. Project-level config (.nibs.yml) overrides these.
type UserConfig struct {
	Nibs UserNibsConfig `yaml:"nibs"`
}

// UserNibsConfig defines user-level nib settings.
type UserNibsConfig struct {
	IDLength      int   `yaml:"id_length,omitempty"`
	HideCompleted *bool `yaml:"hide_completed,omitempty"`
	WideMode      *bool `yaml:"wide_mode,omitempty"`
}

// UserConfigPath returns the path to the user-level nibs config file.
// On Linux: ~/.config/nibs/nibs.yml
// On macOS: ~/Library/Application Support/nibs/nibs.yml
// On Windows: %AppData%/nibs/nibs.yml
func UserConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "nibs", "nibs.yml"), nil
}

// LoadUserConfig loads the user config from the OS-standard location.
// Returns a zero-value UserConfig (no error) when the file doesn't exist.
// Also returns zero-value if the config path cannot be determined
// (e.g., os.UserConfigDir() fails due to missing HOME).
func LoadUserConfig() (*UserConfig, error) {
	path, err := UserConfigPath()
	if err != nil {
		// Can't determine config dir (no HOME, etc.) — treat as no user config
		return &UserConfig{}, nil
	}
	return LoadUserConfigFrom(path)
}

// LoadUserConfigFrom loads user config from the given path.
// Returns a zero-value UserConfig (no error) when the file doesn't exist.
func LoadUserConfigFrom(path string) (*UserConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &UserConfig{}, nil
		}
		return nil, err
	}

	var cfg UserConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadWithUserConfig loads project config from startDir, with user config
// from the OS-standard location providing defaults for unset fields.
func LoadWithUserConfig(startDir string) (*Config, error) {
	userCfgPath, err := UserConfigPath()
	if err != nil {
		// Can't determine user config path; fall back to project-only
		return LoadFromDirectory(startDir)
	}
	return LoadWithUserConfigPath(startDir, userCfgPath)
}

// LoadFromExplicitPathWithUserConfig loads project config from a specific file path
// (rather than searching upward), with user config from the OS-standard location
// providing defaults for unset fields. Used when --config flag is provided.
func LoadFromExplicitPathWithUserConfig(configPath string) (*Config, error) {
	// User config is advisory — degrade gracefully on errors
	userCfg, err := LoadUserConfig()
	if err != nil {
		userCfg = &UserConfig{}
	}

	cfg, err := loadRaw(configPath)
	if err != nil {
		return nil, err
	}

	applyUserDefaults(cfg, userCfg)
	applySystemDefaults(cfg)
	return cfg, nil
}

// LoadWithUserConfigPath loads project config from startDir, with user config
// from the given path providing defaults for unset fields.
// This variant accepts an explicit user config path for testing.
//
// Layering order: project config > user config > system defaults.
// The raw project config is loaded first (without system defaults),
// then user config fills in unset fields, then system defaults fill the rest.
func LoadWithUserConfigPath(startDir string, userConfigPath string) (*Config, error) {
	// Load user config — graceful on all errors (user config is advisory)
	userCfg, err := LoadUserConfigFrom(userConfigPath)
	if err != nil {
		userCfg = &UserConfig{}
	}

	// Load raw project config (without system defaults applied)
	configPath, err := FindConfig(startDir)
	if err != nil {
		return nil, err
	}

	var cfg *Config
	if configPath == "" {
		// No project config found -- start with empty config
		cfg = &Config{}
		cfg.configDir = startDir
	} else {
		cfg, err = loadRaw(configPath)
		if err != nil {
			return nil, err
		}
	}

	// Layer 2: fill in unset fields from user config
	applyUserDefaults(cfg, userCfg)

	// Layer 3: fill in remaining unset fields from system defaults
	applySystemDefaults(cfg)

	return cfg, nil
}

// DefaultWithPrefixFromUserConfig creates a new default config with the given
// prefix, seeding values from the user config where available.
// Used by `nibs init` to persist user preferences into the new project config.
func DefaultWithPrefixFromUserConfig(prefix string, userCfg *UserConfig) *Config {
	cfg := Default()
	cfg.Nibs.Prefix = prefix
	if userCfg != nil {
		if userCfg.Nibs.IDLength != 0 {
			cfg.Nibs.IDLength = userCfg.Nibs.IDLength
		}
		if userCfg.Nibs.HideCompleted != nil {
			cfg.Nibs.HideCompleted = boolPtr(*userCfg.Nibs.HideCompleted)
		}
		if userCfg.Nibs.WideMode != nil {
			cfg.Nibs.WideMode = boolPtr(*userCfg.Nibs.WideMode)
		}
	}
	return cfg
}

// applyUserDefaults fills in unset project config fields from user config.
// Project values always take precedence over user values.
func applyUserDefaults(cfg *Config, userCfg *UserConfig) {
	if userCfg.Nibs.IDLength != 0 && cfg.Nibs.IDLength == 0 {
		cfg.Nibs.IDLength = userCfg.Nibs.IDLength
	}
	if userCfg.Nibs.HideCompleted != nil && cfg.Nibs.HideCompleted == nil {
		cfg.Nibs.HideCompleted = boolPtr(*userCfg.Nibs.HideCompleted)
	}
	if userCfg.Nibs.WideMode != nil && cfg.Nibs.WideMode == nil {
		cfg.Nibs.WideMode = boolPtr(*userCfg.Nibs.WideMode)
	}
}
