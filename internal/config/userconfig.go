package config

import (
	"os"
	"path/filepath"

	"github.com/alphaleonis/nibs/internal/store"
	"gopkg.in/yaml.v3"
)

// UserConfig holds defaults applied across all projects. A value set in a
// project's <store>/config.yml wins over the one here.
type UserConfig struct {
	Nibs UserNibsConfig `yaml:"nibs"`
}

type UserNibsConfig struct {
	IDLength      int   `yaml:"id_length,omitempty"`
	HideCompleted *bool `yaml:"hide_completed,omitempty"`
	WideMode      *bool `yaml:"wide_mode,omitempty"`
}

// UserConfigPath returns nibs/nibs.yml under os.UserConfigDir.
func UserConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "nibs", "nibs.yml"), nil
}

// LoadUserConfig loads the user config from the OS-standard location. A missing
// file, or no location to look in, gives a zero-value UserConfig and no error.
func LoadUserConfig() (*UserConfig, error) {
	path, err := UserConfigPath()
	if err != nil {
		return &UserConfig{}, nil
	}
	return LoadUserConfigFrom(path)
}

// LoadUserConfigFrom returns a zero-value UserConfig and no error when path does
// not exist.
func LoadUserConfigFrom(path string) (*UserConfig, error) {
	data, err := ReadConfigFile(path)
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

// LoadStoreWithUserConfig loads an already-resolved store's config, layering the
// user config from the OS-standard location underneath.
func LoadStoreWithUserConfig(storeDir string) (*Config, error) {
	userCfgPath, err := UserConfigPath()
	if err != nil {
		// No user config location; skip the user layer.
		return LoadFromStore(storeDir)
	}
	return LoadStoreWithUserConfigPath(storeDir, userCfgPath)
}

// LoadFromExplicitPathWithUserConfig loads the project config at configPath (the
// --config route), with the user config layered underneath.
func LoadFromExplicitPathWithUserConfig(configPath string) (*Config, error) {
	// User config is advisory; a load error drops the user layer.
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

// LoadStoreWithUserConfigPath loads an already-resolved store's config: the
// project config over the user config at userConfigPath, over system defaults.
func LoadStoreWithUserConfigPath(storeDir string, userConfigPath string) (*Config, error) {
	// User config is advisory; a load error drops the user layer.
	userCfg, err := LoadUserConfigFrom(userConfigPath)
	if err != nil {
		userCfg = &UserConfig{}
	}

	cfg, err := loadRaw(store.NewLayout(storeDir).ConfigPath())
	if err != nil {
		return nil, err
	}

	applyUserDefaults(cfg, userCfg)
	applySystemDefaults(cfg)

	return cfg, nil
}

// DefaultWithPrefixFromUserConfig returns a default config with the given prefix,
// seeded from userCfg where it sets a value. userCfg may be nil.
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

// applyUserDefaults fills project fields left unset — a nil *bool, an IDLength of
// zero — from the user config.
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
