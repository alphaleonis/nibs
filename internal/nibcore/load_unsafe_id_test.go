package nibcore

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/safetext"
	"github.com/alphaleonis/nibs/internal/testskip"
)

// Each name derives an id that nib.ValidateIDText refuses.
func TestLoadSkipsAFileWhoseNameGivesAnUnsafeID(t *testing.T) {
	names := map[string]string{
		"escape":                 "bad\x1b[31mevil--x.md",
		"bell":                   "bad\ab--x.md",
		"C1 control":             "bad\u009bc--x.md",
		"right-to-left override": "bad\u202ed--x.md",
		"backtick":               "bad`e--x.md",
	}
	for label, name := range names {
		t.Run(label, func(t *testing.T) {
			nibsDir := setupNibsDir(t)
			dataDir := storeData(t, nibsDir)
			writeNibFile(t, dataDir, "good1--ok.md", diagValidNib)
			if err := os.WriteFile(filepath.Join(dataDir, name), []byte(diagValidNib), 0o644); err != nil {
				testskip.Unavailable(t, testskip.HostileFilenames, "os.WriteFile(%q): %v", name, err)
				return
			}

			var warn bytes.Buffer
			core := New(nibsDir, config.Default())
			core.SetWarnWriter(&warn)
			if err := core.Load(); err != nil {
				t.Fatalf("Load() = %v, want nil", err)
			}

			ids := make([]string, 0, 1)
			for _, n := range core.All() {
				ids = append(ids, n.ID)
			}
			if len(ids) != 1 || ids[0] != "good1" {
				t.Errorf("loaded ids = %q, want only good1", ids)
			}

			// The warn writer keeps backticks, so the stripped id is asserted on, not the path.
			got := warn.String()
			id, _ := nib.ParseFilename(name, "")
			if want := `unusable nib id "` + safetext.Strip(id) + `"`; !strings.Contains(got, "skipping unparseable nib file") || !strings.Contains(got, want) {
				t.Errorf("no skip warning with %s:\n%s", want, got)
			}
			if !strings.Contains(got, "rename the file") {
				t.Errorf("the skip warning does not say to rename the file:\n%s", got)
			}

			result := core.CheckAllLinks()
			if len(result.UnparseableFiles) != 1 || result.UnparseableFiles[0].Path != "data/"+name {
				t.Errorf("UnparseableFiles = %+v, want the one skipped file", result.UnparseableFiles)
			}

			raw, err := os.ReadFile(filepath.Join(dataDir, name))
			if err != nil || string(raw) != diagValidNib {
				t.Errorf("the skipped file changed on disk (err %v)", err)
			}
		})
	}
}

func TestCreateRefusesAnUnsafeID(t *testing.T) {
	for _, id := range []string{"tnib-\x1b[31mabcd", "tnib-\u202eabcd", "tnib-a`bcd"} {
		t.Run(safetext.Strip(id), func(t *testing.T) {
			nibsDir := setupNibsDir(t)
			core := New(nibsDir, config.Default())
			if err := core.Load(); err != nil {
				t.Fatalf("Load() = %v", err)
			}
			err := core.Create(&nib.Nib{ID: id, Title: "Probe", Status: "todo", Type: "task"})
			if !errors.Is(err, nib.ErrIDNotText) {
				t.Fatalf("Create(%q) = %v, want an error wrapping nib.ErrIDNotText", id, err)
			}
			entries, err := os.ReadDir(storeData(t, nibsDir))
			if err != nil {
				t.Fatalf("reading data dir: %v", err)
			}
			if len(entries) != 0 {
				t.Errorf("a refused create wrote %d file(s)", len(entries))
			}
		})
	}
}
