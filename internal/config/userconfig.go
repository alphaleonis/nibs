package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/alphaleonis/nibs/internal/safetext"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/yamlfile"
	"gopkg.in/yaml.v3"
)

// UserConfig holds defaults applied across all projects. A value set in a
// project's <store>/config.yml wins over the one here.
type UserConfig struct {
	Nibs UserNibsConfig `yaml:"nibs"`
	// TUI holds the TUI's settings, which only the user config carries. It
	// stays a node, whatever its shape, so a malformed block costs only the TUI
	// settings and the TUI can name the line of each entry it ignores.
	TUI yaml.Node `yaml:"tui"`
}

type UserNibsConfig struct {
	IDLength      int   `yaml:"id_length,omitempty"`
	HideCompleted *bool `yaml:"hide_completed,omitempty"`
}

// UserConfigPath returns nibs/nibs.yml under os.UserConfigDir.
func UserConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "nibs", "nibs.yml"), nil
}

// LoadUserConfigFrom returns a zero-value UserConfig and no error when path does
// not exist.
func LoadUserConfigFrom(path string) (*UserConfig, error) {
	cfg, _, err := ReadUserConfig(path)
	return cfg, err
}

// UserConfigSnapshot identifies the user config file as one read found it, so
// SetUserSetting can tell whether the file changed since. The zero value stands
// for a file that did not exist.
type UserConfigSnapshot struct {
	exists bool
	sum    [sha256.Size]byte
}

func snapshotOf(data []byte) UserConfigSnapshot {
	return UserConfigSnapshot{exists: true, sum: sha256.Sum256(data)}
}

// ReadUserConfig is LoadUserConfigFrom that also returns a snapshot of the file
// it read, to hand to SetUserSetting. A file that is read but does not parse
// still gets its snapshot, beside the error. The snapshot is the zero value when
// the file does not exist or cannot be read.
func ReadUserConfig(path string) (*UserConfig, UserConfigSnapshot, error) {
	data, err := yamlfile.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &UserConfig{}, UserConfigSnapshot{}, nil
		}
		return nil, UserConfigSnapshot{}, err
	}
	cfg, err := parseUserConfig(data)
	return cfg, snapshotOf(data), err
}

// parseUserConfig is the check that decides whether a user config loads.
func parseUserConfig(data []byte) (*UserConfig, error) {
	var cfg UserConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// UserLayer is the user config as loaded to sit under a project config.
type UserLayer struct {
	// Config is the user config. It is empty when the file is absent or did
	// not load, and nil only in a zero UserLayer.
	Config *UserConfig
	// Path is the file the user config was read from, or "" when the OS gives
	// no location to look in.
	Path string
	// Failure says why the file did not load, or nil when it loaded or is
	// absent.
	Failure *UserConfigFailure
}

// UserConfigFailure is a user config file that did not load, so its layer is
// left out.
type UserConfigFailure struct {
	Path  string
	Cause error
}

// Message describes the failure in one line, naming the file and the cause.
// Both pass through the safetext boundary, so the line is safe to show in a
// terminal.
func (f *UserConfigFailure) Message() string {
	return fmt.Sprintf("the user config %s did not load, so its settings are ignored: %s",
		safetext.Strip(f.Path), safetext.StripBounded(f.cause()))
}

// cause is the text of Cause without the path that Message names already, so
// the length cap of StripBounded falls on the explanation alone.
func (f *UserConfigFailure) cause() string {
	var pathErr *fs.PathError
	if errors.As(f.Cause, &pathErr) && pathErr.Path == f.Path {
		return pathErr.Op + ": " + pathErr.Err.Error()
	}
	// yamlfile.ReadFile opens a refusal with the path.
	if rest, ok := strings.CutPrefix(f.Cause.Error(), f.Path+" "); ok {
		return "it " + rest
	}
	return f.Cause.Error()
}

// LoadUserLayer loads the user config from the OS-standard location. It never
// fails: a file that does not load gives an empty Config and a Failure.
func LoadUserLayer() UserLayer {
	path, err := UserConfigPath()
	if err != nil {
		return UserLayer{Config: &UserConfig{}}
	}
	return LoadUserLayerFrom(path)
}

// LoadUserLayerFrom is LoadUserLayer for the user config at path.
func LoadUserLayerFrom(path string) UserLayer {
	user, err := LoadUserConfigFrom(path)
	if err != nil {
		return UserLayer{Config: &UserConfig{}, Path: path, Failure: &UserConfigFailure{Path: path, Cause: err}}
	}
	return UserLayer{Config: user, Path: path}
}

// LoadStoreWithUserConfig loads an already-resolved store's config, layering the
// user config from the OS-standard location underneath. The failure is as for
// LoadStoreWithUserConfigPath.
func LoadStoreWithUserConfig(storeDir string) (*Config, *UserConfigFailure, error) {
	userCfgPath, err := UserConfigPath()
	if err != nil {
		// No user config location; skip the user layer.
		cfg, err := LoadFromStore(storeDir)
		return cfg, nil, err
	}
	return LoadStoreWithUserConfigPath(storeDir, userCfgPath)
}

// LoadFromExplicitPathWithUserConfig loads the project config at configPath (the
// --config route), with the user config layered underneath. The failure is as
// for LoadStoreWithUserConfigPath.
func LoadFromExplicitPathWithUserConfig(configPath string) (*Config, *UserConfigFailure, error) {
	user := LoadUserLayer()
	cfg, err := loadWithUserLayer(configPath, user.Config)
	return cfg, user.Failure, err
}

// LoadStoreWithUserConfigPath loads an already-resolved store's config: the
// project config over the user config at userConfigPath, over system defaults.
//
// A user config that does not load leaves its layer out and is returned as the
// failure, which is nil otherwise. The caller decides how to report it.
func LoadStoreWithUserConfigPath(storeDir string, userConfigPath string) (*Config, *UserConfigFailure, error) {
	user := LoadUserLayerFrom(userConfigPath)
	cfg, err := loadWithUserLayer(store.NewLayout(storeDir).ConfigPath(), user.Config)
	return cfg, user.Failure, err
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
}
