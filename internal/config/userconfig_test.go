package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUserConfigPath(t *testing.T) {
	t.Run("returns path under os.UserConfigDir", func(t *testing.T) {
		got, err := UserConfigPath()
		if err != nil {
			t.Fatalf("UserConfigPath() error = %v", err)
		}

		userConfigDir, err := os.UserConfigDir()
		if err != nil {
			t.Fatalf("os.UserConfigDir() error = %v", err)
		}

		want := filepath.Join(userConfigDir, "nibs", "nibs.yml")
		if got != want {
			t.Errorf("UserConfigPath() = %q, want %q", got, want)
		}
	})
}

func TestLoadUserConfig(t *testing.T) {
	t.Run("returns zero struct when file missing", func(t *testing.T) {
		// Use a non-existent path
		cfg, err := LoadUserConfigFrom("/nonexistent/path/nibs.yml")
		if err != nil {
			t.Fatalf("LoadUserConfigFrom() error = %v, want nil", err)
		}
		if cfg.Nibs.IDLength != 0 {
			t.Errorf("IDLength = %d, want 0", cfg.Nibs.IDLength)
		}
		if cfg.Nibs.HideCompleted != nil {
			t.Errorf("HideCompleted = %v, want nil", cfg.Nibs.HideCompleted)
		}
		if cfg.Nibs.WideMode != nil {
			t.Errorf("WideMode = %v, want nil", cfg.Nibs.WideMode)
		}
	})

	t.Run("parses valid YAML with all fields", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "nibs.yml")

		yaml := `nibs:
  id_length: 6
  hide_completed: false
  wide_mode: true
`
		if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		cfg, err := LoadUserConfigFrom(cfgPath)
		if err != nil {
			t.Fatalf("LoadUserConfigFrom() error = %v", err)
		}
		if cfg.Nibs.IDLength != 6 {
			t.Errorf("IDLength = %d, want 6", cfg.Nibs.IDLength)
		}
		if cfg.Nibs.HideCompleted == nil || *cfg.Nibs.HideCompleted != false {
			t.Errorf("HideCompleted = %v, want ptr(false)", cfg.Nibs.HideCompleted)
		}
		if cfg.Nibs.WideMode == nil || *cfg.Nibs.WideMode != true {
			t.Errorf("WideMode = %v, want ptr(true)", cfg.Nibs.WideMode)
		}
	})

	t.Run("handles partial config with only some fields", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "nibs.yml")

		yaml := `nibs:
  id_length: 8
`
		if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		cfg, err := LoadUserConfigFrom(cfgPath)
		if err != nil {
			t.Fatalf("LoadUserConfigFrom() error = %v", err)
		}
		if cfg.Nibs.IDLength != 8 {
			t.Errorf("IDLength = %d, want 8", cfg.Nibs.IDLength)
		}
		// Unset *bool fields should be nil, not false
		if cfg.Nibs.HideCompleted != nil {
			t.Errorf("HideCompleted = %v, want nil (unset)", cfg.Nibs.HideCompleted)
		}
		if cfg.Nibs.WideMode != nil {
			t.Errorf("WideMode = %v, want nil (unset)", cfg.Nibs.WideMode)
		}
	})
}

func TestBoolAccessors(t *testing.T) {
	t.Run("nil HideCompleted defaults to true", func(t *testing.T) {
		cfg := &Config{}
		// HideCompleted is nil (zero value for *bool)
		if !cfg.HideCompleted() {
			t.Error("HideCompleted() = false, want true (nil defaults to true)")
		}
	})

	t.Run("nil WideMode defaults to true", func(t *testing.T) {
		cfg := &Config{}
		if !cfg.WideMode() {
			t.Error("WideMode() = false, want true (nil defaults to true)")
		}
	})

	t.Run("explicit false HideCompleted returns false", func(t *testing.T) {
		f := false
		cfg := &Config{Nibs: NibsConfig{HideCompleted: &f}}
		if cfg.HideCompleted() {
			t.Error("HideCompleted() = true, want false (explicitly set to false)")
		}
	})

	t.Run("explicit false WideMode returns false", func(t *testing.T) {
		f := false
		cfg := &Config{Nibs: NibsConfig{WideMode: &f}}
		if cfg.WideMode() {
			t.Error("WideMode() = true, want false (explicitly set to false)")
		}
	})

	t.Run("explicit true HideCompleted returns true", func(t *testing.T) {
		tr := true
		cfg := &Config{Nibs: NibsConfig{HideCompleted: &tr}}
		if !cfg.HideCompleted() {
			t.Error("HideCompleted() = false, want true (explicitly set to true)")
		}
	})

	t.Run("Default config accessors return true", func(t *testing.T) {
		cfg := Default()
		if !cfg.HideCompleted() {
			t.Error("Default().HideCompleted() = false, want true")
		}
		if !cfg.WideMode() {
			t.Error("Default().WideMode() = false, want true")
		}
	})
}

func TestBoolFieldsRoundTrip(t *testing.T) {
	t.Run("explicit false survives Save/Load", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg := Default()
		cfg.Nibs.HideCompleted = boolPtr(false)
		cfg.Nibs.WideMode = boolPtr(false)
		cfg.SetConfigDir(tmpDir)

		if err := cfg.Save(tmpDir); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		loaded, err := Load(filepath.Join(tmpDir, ConfigFileName))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if loaded.HideCompleted() {
			t.Error("HideCompleted() = true after round-trip, want false")
		}
		if loaded.WideMode() {
			t.Error("WideMode() = true after round-trip, want false")
		}
		// Verify the pointers are non-nil (not relying on default)
		if loaded.Nibs.HideCompleted == nil {
			t.Error("HideCompleted pointer is nil after round-trip, want non-nil")
		}
		if loaded.Nibs.WideMode == nil {
			t.Error("WideMode pointer is nil after round-trip, want non-nil")
		}
	})

	t.Run("nil fields survive Save/Load as omitted", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg := &Config{
			Nibs: NibsConfig{
				Path:     ".nibs",
				Prefix:   "test-",
				IDLength: 4,
				// HideCompleted and WideMode intentionally nil
			},
		}
		cfg.SetConfigDir(tmpDir)

		if err := cfg.Save(tmpDir); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		loaded, err := Load(filepath.Join(tmpDir, ConfigFileName))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		// nil fields should use defaults via accessor
		if !loaded.HideCompleted() {
			t.Error("HideCompleted() = false, want true (nil -> default)")
		}
		if !loaded.WideMode() {
			t.Error("WideMode() = false, want true (nil -> default)")
		}
	})
}

func TestLoadWithUserConfig(t *testing.T) {
	t.Run("project values override user values", func(t *testing.T) {
		projectDir := t.TempDir()
		userCfgDir := t.TempDir()

		// User config: id_length=8, hide_completed=false
		userYAML := `nibs:
  id_length: 8
  hide_completed: false
`
		userCfgPath := filepath.Join(userCfgDir, "nibs.yml")
		if err := os.WriteFile(userCfgPath, []byte(userYAML), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		// Project config: id_length=6, hide_completed=true
		projectYAML := `nibs:
  prefix: "proj-"
  id_length: 6
  hide_completed: true
`
		projectCfgPath := filepath.Join(projectDir, ConfigFileName)
		if err := os.WriteFile(projectCfgPath, []byte(projectYAML), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		cfg, err := LoadWithUserConfigPath(projectDir, userCfgPath)
		if err != nil {
			t.Fatalf("LoadWithUserConfigPath() error = %v", err)
		}

		// Project values should win
		if cfg.Nibs.IDLength != 6 {
			t.Errorf("IDLength = %d, want 6 (project override)", cfg.Nibs.IDLength)
		}
		if !cfg.HideCompleted() {
			t.Error("HideCompleted() = false, want true (project override)")
		}
		if cfg.Nibs.Prefix != "proj-" {
			t.Errorf("Prefix = %q, want \"proj-\"", cfg.Nibs.Prefix)
		}
	})

	t.Run("nil project fields fall through to user config", func(t *testing.T) {
		projectDir := t.TempDir()
		userCfgDir := t.TempDir()

		// User config: hide_completed=false, wide_mode=false
		userYAML := `nibs:
  hide_completed: false
  wide_mode: false
`
		userCfgPath := filepath.Join(userCfgDir, "nibs.yml")
		if err := os.WriteFile(userCfgPath, []byte(userYAML), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		// Project config: only prefix, no bool fields
		projectYAML := `nibs:
  prefix: "proj-"
`
		projectCfgPath := filepath.Join(projectDir, ConfigFileName)
		if err := os.WriteFile(projectCfgPath, []byte(projectYAML), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		cfg, err := LoadWithUserConfigPath(projectDir, userCfgPath)
		if err != nil {
			t.Fatalf("LoadWithUserConfigPath() error = %v", err)
		}

		// User config values should apply for unset project fields
		if cfg.HideCompleted() {
			t.Error("HideCompleted() = true, want false (user config fallthrough)")
		}
		if cfg.WideMode() {
			t.Error("WideMode() = true, want false (user config fallthrough)")
		}
	})

	t.Run("user config id_length applies when project omits it", func(t *testing.T) {
		projectDir := t.TempDir()
		userCfgDir := t.TempDir()

		// User config: id_length=8
		userYAML := `nibs:
  id_length: 8
`
		userCfgPath := filepath.Join(userCfgDir, "nibs.yml")
		if err := os.WriteFile(userCfgPath, []byte(userYAML), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		// Project config: no id_length
		projectYAML := `nibs:
  prefix: "proj-"
`
		projectCfgPath := filepath.Join(projectDir, ConfigFileName)
		if err := os.WriteFile(projectCfgPath, []byte(projectYAML), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		cfg, err := LoadWithUserConfigPath(projectDir, userCfgPath)
		if err != nil {
			t.Fatalf("LoadWithUserConfigPath() error = %v", err)
		}

		// User config's id_length should apply since project didn't set it
		if cfg.Nibs.IDLength != 8 {
			t.Errorf("IDLength = %d, want 8 (user config fallthrough)", cfg.Nibs.IDLength)
		}
	})

	t.Run("no user config file, project only", func(t *testing.T) {
		projectDir := t.TempDir()

		projectYAML := `nibs:
  prefix: "proj-"
  id_length: 5
  hide_completed: false
`
		projectCfgPath := filepath.Join(projectDir, ConfigFileName)
		if err := os.WriteFile(projectCfgPath, []byte(projectYAML), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		// Non-existent user config path
		cfg, err := LoadWithUserConfigPath(projectDir, "/nonexistent/user/nibs.yml")
		if err != nil {
			t.Fatalf("LoadWithUserConfigPath() error = %v", err)
		}

		// Should work exactly like LoadFromDirectory
		if cfg.Nibs.Prefix != "proj-" {
			t.Errorf("Prefix = %q, want \"proj-\"", cfg.Nibs.Prefix)
		}
		if cfg.Nibs.IDLength != 5 {
			t.Errorf("IDLength = %d, want 5", cfg.Nibs.IDLength)
		}
		if cfg.HideCompleted() {
			t.Error("HideCompleted() = true, want false")
		}
		// WideMode not set in project -> system default true
		if !cfg.WideMode() {
			t.Error("WideMode() = false, want true (system default)")
		}
	})

	t.Run("no project config, user only", func(t *testing.T) {
		// Empty directory with no .nibs.yml
		projectDir := t.TempDir()
		userCfgDir := t.TempDir()

		userYAML := `nibs:
  id_length: 6
  hide_completed: false
  wide_mode: false
`
		userCfgPath := filepath.Join(userCfgDir, "nibs.yml")
		if err := os.WriteFile(userCfgPath, []byte(userYAML), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		cfg, err := LoadWithUserConfigPath(projectDir, userCfgPath)
		if err != nil {
			t.Fatalf("LoadWithUserConfigPath() error = %v", err)
		}

		// User config values should apply
		if cfg.Nibs.IDLength != 6 {
			t.Errorf("IDLength = %d, want 6 (from user config)", cfg.Nibs.IDLength)
		}
		if cfg.HideCompleted() {
			t.Error("HideCompleted() = true, want false (from user config)")
		}
		if cfg.WideMode() {
			t.Error("WideMode() = true, want false (from user config)")
		}
		// System defaults should fill in the rest
		if cfg.Nibs.Path != DefaultNibsPath {
			t.Errorf("Path = %q, want %q (system default)", cfg.Nibs.Path, DefaultNibsPath)
		}
		if cfg.Nibs.DefaultStatus != "todo" {
			t.Errorf("DefaultStatus = %q, want \"todo\" (system default)", cfg.Nibs.DefaultStatus)
		}
		// ConfigDir should be the project directory
		if cfg.ConfigDir() != projectDir {
			t.Errorf("ConfigDir() = %q, want %q", cfg.ConfigDir(), projectDir)
		}
	})
}

func TestDefaultWithPrefixFromUserConfig(t *testing.T) {
	t.Run("seeds id_length from user config", func(t *testing.T) {
		userCfgDir := t.TempDir()
		userYAML := `nibs:
  id_length: 6
  hide_completed: false
`
		userCfgPath := filepath.Join(userCfgDir, "nibs.yml")
		if err := os.WriteFile(userCfgPath, []byte(userYAML), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		userCfg, err := LoadUserConfigFrom(userCfgPath)
		if err != nil {
			t.Fatalf("LoadUserConfigFrom() error = %v", err)
		}

		cfg := DefaultWithPrefixFromUserConfig("myapp-", userCfg)
		if cfg.Nibs.IDLength != 6 {
			t.Errorf("IDLength = %d, want 6 (from user config)", cfg.Nibs.IDLength)
		}
		if cfg.Nibs.Prefix != "myapp-" {
			t.Errorf("Prefix = %q, want \"myapp-\"", cfg.Nibs.Prefix)
		}
		if cfg.HideCompleted() {
			t.Error("HideCompleted() = true, want false (from user config)")
		}
	})

	t.Run("uses system defaults when user config is empty", func(t *testing.T) {
		userCfg := &UserConfig{}
		cfg := DefaultWithPrefixFromUserConfig("myapp-", userCfg)

		if cfg.Nibs.IDLength != 4 {
			t.Errorf("IDLength = %d, want 4 (system default)", cfg.Nibs.IDLength)
		}
		if !cfg.HideCompleted() {
			t.Error("HideCompleted() = false, want true (system default)")
		}
		if !cfg.WideMode() {
			t.Error("WideMode() = false, want true (system default)")
		}
	})

	t.Run("nil user config uses all system defaults", func(t *testing.T) {
		cfg := DefaultWithPrefixFromUserConfig("myapp-", nil)

		if cfg.Nibs.IDLength != 4 {
			t.Errorf("IDLength = %d, want 4", cfg.Nibs.IDLength)
		}
		if cfg.Nibs.Prefix != "myapp-" {
			t.Errorf("Prefix = %q, want \"myapp-\"", cfg.Nibs.Prefix)
		}
	})
}
