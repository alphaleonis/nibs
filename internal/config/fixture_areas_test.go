package config_test

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/testdata/fixtures"
)

// The guards below check the sample-project fixture, but they cannot sit beside
// it: `go` excludes any directory named testdata from wildcard package
// matching, so a test under testdata/ is never built by `./...` — the pattern
// every gate here runs. They live with the vocabulary they check instead, which
// `./...` does reach.
//
// They are an EXTERNAL test package for the direction of the import: the
// fixtures helper is a store helper and may well come to need internal/config,
// which an in-package test importing fixtures would turn into a cycle.

// TestSampleProjectDeclaresEveryAssignedArea keeps the fixture's declared
// vocabulary and its `area:` assignments in step. The fixture is what the
// write-side rejection, the area filter and `nibs check` are all demonstrated
// against, so an assignment the config does not declare would make the shipped
// fixture fail those surfaces the moment they land.
func TestSampleProjectDeclaresEveryAssignedArea(t *testing.T) {
	dir := fixtures.SampleProjectDir(t)
	storeDir := filepath.Join(dir, ".nibs")

	areas, err := config.LoadAreasFromStore(storeDir)
	if err != nil {
		t.Fatalf("loading the fixture vocabulary: %v", err)
	}
	if len(areas.Paths()) == 0 {
		t.Fatal("the fixture declares no areas")
	}

	assigned := map[string][]string{}
	for _, content := range []string{store.DataDirName, store.ArchiveDirName} {
		root := filepath.Join(storeDir, content)
		// Both content directories are walked in full: store content is data/
		// INCLUDING its subdirectories, plus archive/, whose nibs stay in the
		// store and remain visible in every query. archive/ is optional — a store
		// that has never archived anything does not have one.
		if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Ext(d.Name()) != ".md" {
				return nil
			}
			area, err := frontMatterArea(path)
			if err != nil {
				return err
			}
			if area != "" {
				rel, relErr := filepath.Rel(storeDir, path)
				if relErr != nil {
					return relErr
				}
				assigned[area] = append(assigned[area], filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking the fixture %s directory: %v", content, err)
		}
	}

	if len(assigned) == 0 {
		t.Fatal("no fixture nib assigns an area; this guard would pass vacuously")
	}
	for area, files := range assigned {
		if !areas.IsValid(area) {
			t.Errorf("nib(s) %v assign area %q, which the fixture config does not declare (declared: %s)",
				files, area, areas.List())
		}
	}
}

// TestSampleProjectStoreFilesMatchGenerator pins every file the fixture's store
// holds to the heredoc that writes it, so regenerating the fixture cannot
// produce a diff. The two are edited by hand and nothing else compares them.
//
// It covers the whole store rather than one file because it once did not: the
// vocabulary moved from config.yml into its own areas.yml and the pin stayed on
// config.yml, leaving the file that actually carries the vocabulary unguarded —
// which is the state this test exists to prevent, just relocated out from under
// it (nibs-068o).
//
// The rows are checked for COMPLETENESS against the store directory, not trusted
// as a list. A hardcoded set is the shape that failed the first time: a file
// added later would be pinned by nobody and nothing would say so. Reading the
// directory means the fixture itself decides what must be covered, so a third
// config file fails this test on the day it lands and cannot be dodged by
// spelling its heredoc differently.
func TestSampleProjectStoreFilesMatchGenerator(t *testing.T) {
	// Every file the store holds directly, and the heredoc marker the generator
	// writes it with. Directories are the store's CONTENT (data/, archive/) and
	// are written per-nib rather than as one document, so they are not here.
	pinned := map[string]string{
		store.ConfigFileName: "ENDCONFIG",
		store.AreasFileName:  "ENDAREAS",
	}

	dir := fixtures.SampleProjectDir(t)
	storeDir := filepath.Join(dir, ".nibs")

	script, err := os.ReadFile(filepath.Join(filepath.Dir(dir), "gen-sample-project.sh"))
	if err != nil {
		t.Fatalf("reading the generator: %v", err)
	}

	entries, err := os.ReadDir(storeDir)
	if err != nil {
		t.Fatalf("reading the fixture store: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, ok := pinned[entry.Name()]; !ok {
			t.Errorf("the fixture store holds %s and no row here pins it to the generator; add one, or this file drifts from the script that claims to write it",
				entry.Name())
		}
	}

	for name, marker := range pinned {
		t.Run(name, func(t *testing.T) {
			shipped, err := os.ReadFile(filepath.Join(storeDir, name))
			if err != nil {
				t.Fatalf("reading the shipped %s: %v", name, err)
			}
			generated := generatorHeredoc(t, string(script), name, marker)
			if generated != string(shipped) {
				t.Errorf("the generator writes a different %s than the one shipped\n--- generator ---\n%s\n--- shipped ---\n%s",
					name, generated, shipped)
			}
		})
	}
}

// generatorHeredoc returns the body the generator writes into the store's file
// of the given name, located by the heredoc that opens it.
func generatorHeredoc(t *testing.T, script, name, marker string) string {
	t.Helper()

	open := fmt.Sprintf("cat > \"$STORE/%s\" << '%s'\n", name, marker)
	start := strings.Index(script, open)
	if start < 0 {
		t.Fatalf("the generator no longer writes %s with the %q heredoc", name, open)
	}
	body := script[start+len(open):]
	end := strings.Index(body, "\n"+marker+"\n")
	if end < 0 {
		t.Fatalf("the generator's %s heredoc is unterminated", name)
	}
	return body[:end+1]
}

// frontMatterArea returns a nib file's `area:` value, or "" when it has none.
// It reads the front matter only — the fenced block the file opens with — so a
// body line that begins the same way is not mistaken for an assignment.
func frontMatterArea(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	opened := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			if opened {
				break
			}
			opened = true
			continue
		}
		if !opened {
			break
		}
		if value, ok := strings.CutPrefix(line, "area:"); ok {
			return strings.TrimSpace(value), nil
		}
	}
	return "", scanner.Err()
}
