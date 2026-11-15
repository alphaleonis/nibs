package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/alphaleonis/nibs/internal/safetext"
	"github.com/alphaleonis/nibs/internal/yamlfile"
	"gopkg.in/yaml.v3"
)

// UserConfigRefusal is a refusal to write a setting into the user config at
// Path, because of what the file holds. The file is unchanged, and only an edit
// by hand clears the refusal.
type UserConfigRefusal struct {
	Path string
	msg  string
}

func (e *UserConfigRefusal) Error() string { return e.msg }

func refuseUserSetting(path, format string, a ...any) error {
	return &UserConfigRefusal{Path: path, msg: fmt.Sprintf(format, a...)}
}

// SetUserSetting sets the setting at keyPath in the user config at path to
// value, which is anything yaml.v3 encodes, such as a bool or a []string. It
// returns a snapshot of the file as written, and whether the file differed from
// loaded, the snapshot from the caller's own read. A caller whose file changed
// holds a stale config and should read it again.
//
// The edit applies to the file as it is now, so a change written after loaded
// is kept. It creates the file, its directory and the missing mappings on the
// way to keyPath. Like PlanSetStoredPrefix, it keeps content and comments and
// normalizes the layout, and the comments inside a replaced value are lost with
// it. It writes atomically, keeps the file mode, and writes to the file a
// symlink at path leads to, even one that does not exist yet. It takes no lock,
// so a write by another process between its read and its write is lost.
//
// It returns a *UserConfigRefusal when the file does not load, holds more than
// one YAML document, or holds something other than a mapping on the way to
// keyPath. It returns a plain error when value would leave the file unloadable.
func SetUserSetting(path string, loaded UserConfigSnapshot, keyPath []string, value any) (written UserConfigSnapshot, changedSinceLoad bool, err error) {
	if len(keyPath) == 0 {
		return UserConfigSnapshot{}, false, errors.New("SetUserSetting: empty key path")
	}
	setting := "`" + strings.Join(keyPath, ".") + "`"
	var valueNode yaml.Node
	if err := valueNode.Encode(value); err != nil {
		return UserConfigSnapshot{}, false, fmt.Errorf("encoding %s: %w", setting, err)
	}
	shownPath := safetext.Strip(path)
	inError := func(err error) error {
		return refuseUserSetting(path,
			"the user config %s has an error, so %s was not saved — fix the file, then retry: %s",
			shownPath, setting, safetext.StripBounded(err.Error()))
	}

	data, err := yamlfile.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return UserConfigSnapshot{}, false, inError(err)
	}
	if _, err := parseUserConfig(data); err != nil {
		return UserConfigSnapshot{}, false, inError(err)
	}
	doc, err := yamlfile.SoleDocument(data)
	if errors.Is(err, yamlfile.ErrMultipleDocuments) {
		return UserConfigSnapshot{}, false, refuseUserSetting(path,
			"the user config %s holds more than one YAML document, and saving %s would rewrite it from the first one alone — move anything after the `---` into its own file, or delete the marker if nothing follows it, then retry",
			shownPath, setting)
	}
	if err != nil {
		// The loader reads the first document alone, so a later one can still fail here.
		return UserConfigSnapshot{}, false, inError(err)
	}
	keepText := holdsNoContent(&doc) && len(bytes.TrimSpace(data)) > 0
	if keepText {
		// The file's text goes in front of the new content, so its comments
		// are dropped from the nodes to be written once.
		doc.HeadComment, doc.LineComment, doc.FootComment = "", "", ""
	}
	if at, node := setPath(&doc, keyPath, &valueNode); node != nil {
		return UserConfigSnapshot{}, false, refuseUserSetting(path,
			"in the user config %s, `%s` on line %d is not a mapping, so %s cannot be saved under it — make it a mapping or remove it, then retry",
			shownPath, strings.Join(at, "."), node.Line, setting)
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return UserConfigSnapshot{}, false, err
	}
	if keepText {
		out = append(append(slices.Clone(bytes.TrimRight(data, "\r\n")), '\n'), out...)
	}
	if _, err := parseUserConfig(out); err != nil {
		return UserConfigSnapshot{}, false, fmt.Errorf("setting %s would leave the user config %s unreadable, so it was not saved: %w", setting, shownPath, err)
	}

	target, err := writeTarget(path)
	if err != nil {
		return UserConfigSnapshot{}, false, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return UserConfigSnapshot{}, false, err
	}
	if _, err := yamlfile.WritePreservingMode(target, out); err != nil {
		return UserConfigSnapshot{}, false, err
	}

	current := UserConfigSnapshot{}
	if exists {
		current = snapshotOf(data)
	}
	return snapshotOf(out), current != loaded, nil
}

// holdsNoContent reports whether doc came from a file with no content, such as
// one of only comments, or comments around a bare `---`. yaml.v3 keeps such
// comments only in part, if at all.
func holdsNoContent(doc *yaml.Node) bool {
	if doc.Kind == 0 {
		return true
	}
	if len(doc.Content) != 1 {
		return false
	}
	// An implicit null has no text; `~` and `null` do, and are content.
	n := doc.Content[0]
	return n.Kind == yaml.ScalarNode && n.Tag == "!!null" && n.Value == ""
}

// writeTarget returns the file a write to path replaces: the end of the
// symlink chain at path, which need not exist yet, or path itself.
func writeTarget(path string) (string, error) {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		return target, nil
	}
	// EvalSymlinks fails on a link to a missing file, so follow the chain by
	// hand. The bound ends a link cycle.
	for range 64 {
		info, err := os.Lstat(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return path, nil
		case err != nil:
			return "", err
		case info.Mode()&os.ModeSymlink == 0:
			return path, nil
		}
		link, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(path), link)
		}
		path = link
	}
	return "", fmt.Errorf("%s: too many levels of symbolic links", path)
}
