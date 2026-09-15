package config

import (
	"os"
	"path/filepath"

	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/yamlfile"
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
	data, err := yamlfile.ReadFile(path)
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

	return loadWithUserLayer(configPath, userCfg)
}

// LoadStoreWithUserConfigPath loads an already-resolved store's config: the
// project config over the user config at userConfigPath, over system defaults.
func LoadStoreWithUserConfigPath(storeDir string, userConfigPath string) (*Config, error) {
	// User config is advisory; a load error drops the user layer.
	userCfg, err := LoadUserConfigFrom(userConfigPath)
	if err != nil {
		userCfg = &UserConfig{}
	}
	return loadWithUserLayer(store.NewLayout(storeDir).ConfigPath(), userCfg)
}

// loadWithUserLayer loads the project config at configPath over userCfg, over
// system defaults.
func loadWithUserLayer(configPath string, userCfg *UserConfig) (*Config, error) {
	cfg, err := loadRaw(configPath)
	if err != nil {
		return nil, err
	}
	overlayUserNibs(&cfg.Nibs, userCfg.Nibs, true)
	applySystemDefaults(cfg)
	return cfg, nil
}

// DefaultWithPrefixFromUserConfig returns a default config with the given prefix,
// seeded from userCfg where it sets a value. userCfg may be nil.
func DefaultWithPrefixFromUserConfig(prefix string, userCfg *UserConfig) *Config {
	cfg := Default()
	cfg.Nibs.Prefix = prefix
	if userCfg != nil {
		overlayUserNibs(&cfg.Nibs, userCfg.Nibs, false)
	}
	return cfg
}

// overlayUserNibs copies each field the user config sets into dst. With onlyUnset,
// a field dst already holds (a non-nil *bool, a non-zero IDLength) is kept.
// IDLength stays a plain int: 0 is not a legal length, so it can mean unset.
func overlayUserNibs(dst *NibsConfig, user UserNibsConfig, onlyUnset bool) {
	if user.IDLength != 0 && (!onlyUnset || dst.IDLength == 0) {
		dst.IDLength = user.IDLength
	}
	if user.HideCompleted != nil && (!onlyUnset || dst.HideCompleted == nil) {
		dst.HideCompleted = boolPtr(*user.HideCompleted)
	}
	if user.WideMode != nil && (!onlyUnset || dst.WideMode == nil) {
		dst.WideMode = boolPtr(*user.WideMode)
	}
}
