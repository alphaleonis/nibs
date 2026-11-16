package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/testsupport/userconfigtest"
)

// writeFile writes text to path, creating its directory.
func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

const brokenUserConfig = "nibs:\n  id_length: 8\n  hide_completed: [\n"

func TestLayeredLoadReportsAUserConfigThatDidNotLoad(t *testing.T) {
	projectDir := t.TempDir()
	writeFile(t, storeConfigPath(t, projectDir), "nibs:\n  prefix: proj-\n")
	storeDir := filepath.Join(projectDir, ".nibs")
	userPath := filepath.Join(t.TempDir(), "nibs.yml")
	writeFile(t, userPath, brokenUserConfig)

	cfg, failure, err := LoadStoreWithUserConfigPath(storeDir, userPath)
	if err != nil {
		t.Fatalf("LoadStoreWithUserConfigPath() error = %v", err)
	}
	if cfg.Nibs.Prefix != "proj-" {
		t.Errorf("Prefix = %q, want the project's proj-", cfg.Nibs.Prefix)
	}
	if want := Default().Nibs.IDLength; cfg.Nibs.IDLength != want || !cfg.HideCompleted() {
		t.Errorf("IDLength = %d, HideCompleted = %v, want the system defaults %d and true", cfg.Nibs.IDLength, cfg.HideCompleted(), want)
	}
	if failure == nil {
		t.Fatal("failure = nil, want the user config's load error")
	}
	if failure.Path != userPath || failure.Cause == nil {
		t.Errorf("failure = %+v, want Path %q and a cause", failure, userPath)
	}
	msg := failure.Message()
	if !strings.Contains(msg, userPath) || !strings.Contains(msg, "line 3") {
		t.Errorf("Message() = %q, want the path and the YAML error's line", msg)
	}
}

func TestLayeredLoadReportsNoFailureForALoadableOrAbsentUserConfig(t *testing.T) {
	projectDir := t.TempDir()
	storeDir := storeDirFor(t, projectDir)
	userDir := t.TempDir()
	valid := filepath.Join(userDir, "valid.yml")
	writeFile(t, valid, "nibs:\n  id_length: 8\n")

	for _, path := range []string{valid, filepath.Join(userDir, "absent.yml")} {
		_, failure, err := LoadStoreWithUserConfigPath(storeDir, path)
		if err != nil || failure != nil {
			t.Errorf("%s: failure = %+v, err = %v, want neither", filepath.Base(path), failure, err)
		}
	}
}

// Both entry points that commands use find the user config through
// os.UserConfigDir.
func TestCommandLoadersReportAUserConfigThatDidNotLoad(t *testing.T) {
	userPath := userconfigtest.WriteUserConfig(t, brokenUserConfig)
	projectDir := t.TempDir()
	configPath := storeConfigPath(t, projectDir)
	writeFile(t, configPath, "nibs:\n  prefix: proj-\n")

	loaders := map[string]func() (*Config, *UserConfigFailure, error){
		"LoadStoreWithUserConfig": func() (*Config, *UserConfigFailure, error) {
			return LoadStoreWithUserConfig(filepath.Dir(configPath))
		},
		"LoadFromExplicitPathWithUserConfig": func() (*Config, *UserConfigFailure, error) {
			return LoadFromExplicitPathWithUserConfig(configPath)
		},
	}
	for name, load := range loaders {
		cfg, failure, err := load()
		if err != nil {
			t.Fatalf("%s: error = %v", name, err)
		}
		if cfg.Nibs.Prefix != "proj-" || cfg.Nibs.IDLength != Default().Nibs.IDLength {
			t.Errorf("%s: prefix %q, id length %d, want proj- over the system defaults", name, cfg.Nibs.Prefix, cfg.Nibs.IDLength)
		}
		if failure == nil || failure.Path != userPath {
			t.Errorf("%s: failure = %+v, want one for %s", name, failure, userPath)
		}
	}

	if layer := LoadUserLayer(); layer.Failure == nil || layer.Config == nil || layer.Path != userPath {
		t.Errorf("LoadUserLayer() = %+v, want an empty config and a failure for %s", layer, userPath)
	}
}

// A value of the wrong type makes yaml.v3 quote the value in its error, so the
// file's own bytes reach the message.
func TestUserConfigFailureMessageStripsTerminalControls(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "nibs.yml")
	writeFile(t, userPath, "nibs: \"\\e[2J\\e]0;owned\\a\\nsecond line\"\n")

	layer := LoadUserLayerFrom(userPath)
	if layer.Failure == nil {
		t.Fatal("premise failed: the user config loaded")
	}
	if raw := layer.Failure.Cause.Error(); !strings.Contains(raw, "\x1b") {
		t.Fatalf("premise failed: the cause does not quote the escape: %q", raw)
	}
	msg := layer.Failure.Message()
	if strings.ContainsAny(msg, "\x1b\a\n\r") {
		t.Errorf("Message() = %q, want no control characters", msg)
	}
}

// An OS error and a yamlfile refusal both quote the path the message already
// names.
func TestUserConfigFailureMessageNamesThePathOnce(t *testing.T) {
	dir := t.TempDir()
	notAFile := filepath.Join(dir, "nibs.yml")
	if err := os.Mkdir(notAFile, 0o755); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(dir, "unreadable.yml")
	failures := map[string]*UserConfigFailure{
		"a directory":       LoadUserLayerFrom(notAFile).Failure,
		"permission denied": {Path: unreadable, Cause: &fs.PathError{Op: "open", Path: unreadable, Err: fs.ErrPermission}},
	}
	wants := map[string]string{
		"a directory":       "it is a directory, not a regular file",
		"permission denied": "open: permission denied",
	}
	for name, failure := range failures {
		if failure == nil {
			t.Fatalf("%s: premise failed: no failure", name)
		}
		msg := failure.Message()
		if strings.Count(msg, failure.Path) != 1 || !strings.Contains(msg, wants[name]) || strings.HasSuffix(msg, "…") {
			t.Errorf("%s: Message() = %q, want the path once and the whole cause %q", name, msg, wants[name])
		}
	}
}
