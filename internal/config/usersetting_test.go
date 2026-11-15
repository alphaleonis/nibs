package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/testskip"
	"gopkg.in/yaml.v3"
)

// writeUserConfig writes body as a user config in a fresh temp directory and
// returns its path.
func writeUserConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nibs.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readUserConfigText(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// loadSnapshot reads path the way a caller would before it writes a setting.
func loadSnapshot(t *testing.T, path string) UserConfigSnapshot {
	t.Helper()
	_, snap, err := ReadUserConfig(path)
	if err != nil {
		t.Fatalf("ReadUserConfig() error = %v", err)
	}
	return snap
}

// settingAt decodes the value at keyPath in the user config at path.
func settingAt(t *testing.T, path string, keyPath ...string) any {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(readUserConfigText(t, path)), &doc); err != nil {
		t.Fatalf("the written user config does not parse: %v", err)
	}
	node := doc.Content[0]
	for _, key := range keyPath {
		node = mappingValueOrFail(t, node, key)
	}
	var v any
	if err := node.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func mappingValueOrFail(t *testing.T, node *yaml.Node, key string) *yaml.Node {
	t.Helper()
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	t.Fatalf("no key %q in the written user config", key)
	return nil
}

func TestSetUserSettingKeepsCommentsUnknownKeysAndOrder(t *testing.T) {
	path := writeUserConfig(t, `# head of the file
nibs:
  id_length: 6 # line on id_length
future_key: kept # unknown to this build
tui:
  # head of keys
  keys:
    nib.priority: P
  columns:
    # head of milestones
    milestones: status # line on milestones
    tree: [title]
  # foot of tui
zz_last: [1, 2]
# foot of the file
`)

	if _, _, err := SetUserSetting(path, loadSnapshot(t, path), []string{"tui", "columns", "milestones"}, []string{"status", "priority", "title"}); err != nil {
		t.Fatalf("SetUserSetting() error = %v", err)
	}

	got := readUserConfigText(t, path)
	for _, want := range []string{
		"# head of the file", "# line on id_length", "# unknown to this build", "# head of keys",
		"# head of milestones", "milestones: # line on milestones", "# foot of tui", "# foot of the file",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the write lost the comment %q:\n%s", want, got)
		}
	}
	order := []string{"nibs:", "id_length:", "future_key:", "tui:", "keys:", "nib.priority:", "columns:", "milestones:", "tree:", "zz_last:"}
	last := -1
	for _, key := range order {
		i := strings.Index(got, key)
		if i <= last {
			t.Fatalf("key %q is missing or out of order:\n%s", key, got)
		}
		last = i
	}
	if v := settingAt(t, path, "tui", "columns", "milestones"); !equalStrings(v, "status", "priority", "title") {
		t.Errorf("tui.columns.milestones = %v, want [status priority title]", v)
	}
	if v := settingAt(t, path, "future_key"); v != "kept" {
		t.Errorf("future_key = %v, want kept", v)
	}
	if v := settingAt(t, path, "zz_last"); len(v.([]any)) != 2 {
		t.Errorf("zz_last = %v, want two items", v)
	}
}

// A file with no content holds no node for the edit to carry its comments in.
func TestSetUserSettingKeepsTheCommentsOfAFileWithNoContent(t *testing.T) {
	for _, body := range []string{
		"# nibs user config\n# tui:\n#   hints: false\n",
		"# a\n---\n# b\n",
		"---\n# a\n",
		"# a\n--- # c\n# b\n",
	} {
		path := writeUserConfig(t, body)

		if _, _, err := SetUserSetting(path, loadSnapshot(t, path), []string{"tui", "mouse"}, false); err != nil {
			t.Fatalf("%q: SetUserSetting() error = %v", body, err)
		}

		got := readUserConfigText(t, path)
		if !strings.HasPrefix(got, body) || strings.Count(got, "#") != strings.Count(body, "#") {
			t.Errorf("%q: the comments were not kept exactly once:\n%s", body, got)
		}
		if v := settingAt(t, path, "tui", "mouse"); v != false {
			t.Errorf("%q: tui.mouse = %v, want false", body, v)
		}
	}
}

// The TUI reads its keys from the tui node of a loaded config, so a write
// elsewhere in tui must leave that node decodable.
func TestSetUserSettingLeavesTheTUIKeysReadable(t *testing.T) {
	path := writeUserConfig(t, "tui:\n  keys:\n    nib.priority: P\n    view.pick: [v, V]\n")

	if _, _, err := SetUserSetting(path, loadSnapshot(t, path), []string{"tui", "columns", "tree"}, []string{"title"}); err != nil {
		t.Fatalf("SetUserSetting() error = %v", err)
	}

	cfg, err := LoadUserConfigFrom(path)
	if err != nil {
		t.Fatalf("LoadUserConfigFrom() error = %v", err)
	}
	var tui struct {
		Keys map[string]any `yaml:"keys"`
	}
	if err := cfg.TUI.Decode(&tui); err != nil {
		t.Fatalf("decoding the tui node: %v", err)
	}
	if tui.Keys["nib.priority"] != "P" || !equalStrings(tui.Keys["view.pick"], "v", "V") {
		t.Errorf("tui.keys = %v, want nib.priority: P and view.pick: [v, V]", tui.Keys)
	}
}

func TestSetUserSettingCreatesWhatIsMissing(t *testing.T) {
	tests := []struct {
		name string
		body *string // nil: no file at all
	}{
		{name: "no file and no directory"},
		{name: "an empty file", body: new(string)},
		{name: "no tui mapping", body: ptr("nibs:\n  id_length: 6\n")},
		{name: "tui written with no value", body: ptr("tui:\nnibs:\n  id_length: 6\n")},
		{name: "tui without columns", body: ptr("tui:\n  keys: {nib.priority: P}\n")},
		{name: "columns written as null", body: ptr("tui:\n  columns: ~\n")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nibs", "nibs.yml")
			if tt.body != nil {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(*tt.body), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			if _, _, err := SetUserSetting(path, loadSnapshot(t, path), []string{"tui", "columns", "milestones"}, []string{"status", "title"}); err != nil {
				t.Fatalf("SetUserSetting() error = %v", err)
			}

			if v := settingAt(t, path, "tui", "columns", "milestones"); !equalStrings(v, "status", "title") {
				t.Errorf("tui.columns.milestones = %v, want [status title]\n%s", v, readUserConfigText(t, path))
			}
			if tt.body != nil && strings.Contains(*tt.body, "id_length") {
				if v := settingAt(t, path, "nibs", "id_length"); v != 6 {
					t.Errorf("nibs.id_length = %v, want 6", v)
				}
			}
		})
	}
}

func TestSetUserSettingReplacesAValueOfAnotherKind(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		value any
		check func(any) bool
	}{
		{"a scalar with a list", "tui:\n  columns:\n    milestones: status\n", []string{"status", "title"}, func(v any) bool { return equalStrings(v, "status", "title") }},
		{"a list with a scalar", "tui:\n  columns:\n    milestones: [status, title]\n", "title", func(v any) bool { return v == "title" }},
		{"a mapping with a bool", "tui:\n  columns:\n    milestones: {a: 1}\n", false, func(v any) bool { return v == false }},
		{"a quoted string with a plain one", "tui:\n  columns:\n    milestones: \"x\"\n", "true", func(v any) bool { return v == "true" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeUserConfig(t, tt.body)

			if _, _, err := SetUserSetting(path, loadSnapshot(t, path), []string{"tui", "columns", "milestones"}, tt.value); err != nil {
				t.Fatalf("SetUserSetting() error = %v", err)
			}

			if v := settingAt(t, path, "tui", "columns", "milestones"); !tt.check(v) {
				t.Errorf("tui.columns.milestones = %#v, want %#v\n%s", v, tt.value, readUserConfigText(t, path))
			}
		})
	}
}

// A value it replaces keeps its anchor, so an alias of it reads the new value.
func TestSetUserSettingKeepsTheAnchorOfAReplacedValue(t *testing.T) {
	path := writeUserConfig(t, "tui:\n  columns:\n    milestones: &cols status\n    tree: *cols\n")

	if _, _, err := SetUserSetting(path, loadSnapshot(t, path), []string{"tui", "columns", "milestones"}, []string{"title"}); err != nil {
		t.Fatalf("SetUserSetting() error = %v", err)
	}

	if v := settingAt(t, path, "tui", "columns", "tree"); !equalStrings(v, "title") {
		t.Errorf("tui.columns.tree = %v, want the [title] its alias now reads\n%s", v, readUserConfigText(t, path))
	}
}

// A user config that does not load is one the user has to fix by hand, so the
// writer refuses it and leaves every byte as it was.
func TestSetUserSettingRefusesAFileInError(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{"a YAML syntax error", "tui:\n  columns: [status\nnibs: {\n", "has an error"},
		{"a document that is not a mapping", "- just\n- a list\n", "has an error"},
		{"a known key of the wrong type", "nibs:\n  id_length: six\n", "has an error"},
		{"a duplicate key", "tui: {}\ntui: {}\n", "has an error"},
		{"a second document", "tui: {}\n---\nother: 1\n", "more than one YAML document"},
		{"a syntax error in a second document", "tui: {}\n---\nother: [\n", "has an error"},
		{"tui as a scalar", "tui: keys\n", "`tui` on line 1 is not a mapping"},
		{"columns as a list", "tui:\n  columns: [status]\n", "`tui.columns` on line 2 is not a mapping"},
		{"tui as an alias", "base: &b {keys: {}}\ntui: *b\n", "`tui` on line 2 is not a mapping"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeUserConfig(t, tt.body)

			_, _, err := SetUserSetting(path, UserConfigSnapshot{}, []string{"tui", "columns", "milestones"}, []string{"title"})

			var refusal *UserConfigRefusal
			if !errors.As(err, &refusal) {
				t.Fatalf("error = %v (%T), want a *UserConfigRefusal", err, err)
			}
			if refusal.Path != path {
				t.Errorf("refusal path = %q, want %q", refusal.Path, path)
			}
			for _, want := range []string{tt.wantMsg, path} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), want)
				}
			}
			if got := readUserConfigText(t, path); got != tt.body {
				t.Errorf("the refused write changed the file:\n%s", got)
			}
		})
	}
}

func TestSetUserSettingAppliesTheEditToAFileChangedSinceLoad(t *testing.T) {
	path := writeUserConfig(t, "nibs:\n  id_length: 6\n")
	loaded := loadSnapshot(t, path)

	// Another nibs process, or an editor, writes the file after this one loaded it.
	changed := "nibs:\n  id_length: 8 # set elsewhere\ntui:\n  keys: {nib.priority: P}\n"
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}

	written, changedSinceLoad, err := SetUserSetting(path, loaded, []string{"tui", "columns", "tree"}, []string{"title"})
	if err != nil {
		t.Fatalf("SetUserSetting() error = %v", err)
	}

	if !changedSinceLoad {
		t.Error("changedSinceLoad = false, want true for a file written after the load")
	}
	if v := settingAt(t, path, "nibs", "id_length"); v != 8 {
		t.Errorf("nibs.id_length = %v, want the 8 written after the load", v)
	}
	if v := settingAt(t, path, "tui", "keys", "nib.priority"); v != "P" {
		t.Errorf("tui.keys.nib.priority = %v, want the P written after the load", v)
	}
	if !strings.Contains(readUserConfigText(t, path), "# set elsewhere") {
		t.Errorf("the comment written after the load was lost:\n%s", readUserConfigText(t, path))
	}
	if v := settingAt(t, path, "tui", "columns", "tree"); !equalStrings(v, "title") {
		t.Errorf("tui.columns.tree = %v, want [title]", v)
	}
	if now := loadSnapshot(t, path); written != now {
		t.Error("the returned snapshot does not match the file as written")
	}
}

func TestSetUserSettingReportsAnUnchangedFile(t *testing.T) {
	path := writeUserConfig(t, "nibs:\n  id_length: 6\n")

	written, changedSinceLoad, err := SetUserSetting(path, loadSnapshot(t, path), []string{"tui", "hints"}, false)
	if err != nil {
		t.Fatalf("SetUserSetting() error = %v", err)
	}
	if changedSinceLoad {
		t.Error("changedSinceLoad = true for a file nothing touched since the load")
	}

	// A second write from the returned snapshot sees the file as its own.
	if _, changedSinceLoad, err = SetUserSetting(path, written, []string{"tui", "mouse"}, false); err != nil {
		t.Fatalf("second SetUserSetting() error = %v", err)
	}
	if changedSinceLoad {
		t.Error("changedSinceLoad = true after a write from the snapshot the previous write returned")
	}

	// A file that did not exist at load reads as changed once something creates it.
	missing := filepath.Join(t.TempDir(), "nibs.yml")
	absent := loadSnapshot(t, missing)
	if err := os.WriteFile(missing, []byte("nibs: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, changedSinceLoad, err = SetUserSetting(missing, absent, []string{"tui", "hints"}, false); err != nil {
		t.Fatalf("SetUserSetting() error = %v", err)
	}
	if !changedSinceLoad {
		t.Error("changedSinceLoad = false for a file created after a load that found none")
	}
}

func TestSetUserSettingPreservesTheFileMode(t *testing.T) {
	testskip.NeedPosixFileModes(t, t.TempDir())

	path := writeUserConfig(t, "nibs:\n  id_length: 6\n")
	// chmod rather than a mode handed to the write, which umask would narrow.
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SetUserSetting(path, loadSnapshot(t, path), []string{"tui", "hints"}, false); err != nil {
		t.Fatalf("SetUserSetting() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %v, want 0600", perm)
	}
}

// A dotfile manager can link the user config from a repository; the setting
// belongs in the file the link leads to.
func TestSetUserSettingWritesThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles-nibs.yml")
	if err := os.WriteFile(target, []byte("nibs:\n  id_length: 6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "nibs.yml")
	if err := os.Symlink(target, link); err != nil {
		testskip.SymlinkUnavailable(t, err)
	}

	if _, _, err := SetUserSetting(link, loadSnapshot(t, link), []string{"tui", "hints"}, false); err != nil {
		t.Fatalf("SetUserSetting() error = %v", err)
	}

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the user config is no longer a symlink (err %v)", err)
	}
	if v := settingAt(t, target, "tui", "hints"); v != false {
		t.Errorf("tui.hints in the link target = %v, want false", v)
	}
}

// A link made before its target exists still leads the write to the target.
func TestSetUserSettingWritesThroughADanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "nibs.yml")
	link := filepath.Join(dir, "nibs.yml")
	if err := os.Symlink(filepath.Join("dotfiles", "nibs.yml"), link); err != nil {
		testskip.SymlinkUnavailable(t, err)
	}

	if _, _, err := SetUserSetting(link, loadSnapshot(t, link), []string{"tui", "hints"}, false); err != nil {
		t.Fatalf("SetUserSetting() error = %v", err)
	}

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the user config is no longer a symlink (err %v)", err)
	}
	if v := settingAt(t, target, "tui", "hints"); v != false {
		t.Errorf("tui.hints in the link target = %v, want false", v)
	}
}

// A value of the wrong type for a key the loader knows would leave a file the
// loader drops and the writer refuses.
func TestSetUserSettingRefusesAValueTheLoaderWouldReject(t *testing.T) {
	body := "nibs:\n  id_length: 6\n"
	path := writeUserConfig(t, body)

	_, _, err := SetUserSetting(path, loadSnapshot(t, path), []string{"nibs", "id_length"}, "six")

	if err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("error = %v, want one saying the write would leave the file unreadable", err)
	}
	if got := readUserConfigText(t, path); got != body {
		t.Errorf("the refused write changed the file:\n%s", got)
	}
}

func TestReadUserConfigSnapshotsAFileThatDoesNotParse(t *testing.T) {
	path := writeUserConfig(t, "tui: [\n")
	_, snap, err := ReadUserConfig(path)
	if err == nil {
		t.Fatal("ReadUserConfig() accepted a YAML syntax error")
	}
	if snap == (UserConfigSnapshot{}) {
		t.Error("the snapshot of a file that does not parse reads as an absent file")
	}
}

func equalStrings(v any, want ...string) bool {
	items, ok := v.([]any)
	if !ok || len(items) != len(want) {
		return false
	}
	for i, item := range items {
		if item != want[i] {
			return false
		}
	}
	return true
}

func ptr(s string) *string { return &s }
