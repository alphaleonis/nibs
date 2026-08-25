package testskip_test

import (
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoTestFilesUnderTestdata closes the same blindness this package exists
// for, one step earlier: a guard that never EXECUTES is as invisible as a guard
// that silently skipped, and a test file under testdata/ never executes at all.
// `go` excludes any directory named testdata from wildcard package matching, so
// `./...` — the pattern the test gate, CI and the linter all run — never builds
// the package, and nothing in the output says a package went unmatched.
//
// The walk looks for `testdata` anywhere in the path rather than for one known
// directory, because the point is to catch the NEXT one: the trap is re-entered
// by putting a test beside the data it reads, wherever that data happens to be.
func TestNoTestFilesUnderTestdata(t *testing.T) {
	root := moduleRoot(t)

	var offenders []string
	testdataDirs := 0
	walked := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			// cmd/go exempts the module root from the name rule, and so must
			// this: a checkout named _work or .src is still the tree to walk.
			if path != root && skipTree(d.Name()) {
				return fs.SkipDir
			}
			walked[rel] = true
			if d.Name() == "testdata" {
				testdataDirs++
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		if underTestdata(rel) {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	// A walk that finds no testdata at all is passing against the wrong tree.
	if testdataDirs == 0 {
		t.Fatalf("found no testdata directory under %s; this guard is not reading the module", root)
	}
	// A walk narrowed to less than `./...` builds is the subtler vacuity, and
	// counting testdata directories cannot see it — the two real ones still
	// count while a whole subtree goes unread. The go tool is the only oracle
	// for what the pattern reaches, so ask it rather than trusting skipTree.
	for _, dir := range wildcardPackageDirs(t, root) {
		rel, relErr := filepath.Rel(root, dir)
		if relErr != nil || strings.HasPrefix(rel, "..") {
			t.Fatalf("`go list ./...` reported %s, which is not under %s: the oracle and the walk disagree about the module root", dir, root)
		}
		if !walked[filepath.ToSlash(rel)] {
			t.Errorf("the walk never entered %s, which `go list ./...` does reach: skipTree excludes a tree the gates build, "+
				"so a test file stranded under it would go unreported.", filepath.ToSlash(rel))
		}
	}

	for _, offender := range offenders {
		t.Errorf("%s will never run: `go` excludes any directory named testdata from wildcard package matching, so "+
			"`./...` — the pattern the test gate, CI and the linter all run — never builds or lints it, and a guard "+
			"that never executes is indistinguishable from one that passed. Move it into a package `./...` reaches, "+
			"reading its data either from that package's own testdata/ by relative path (cmd/membership_pin_test.go "+
			"is the worked example) or, for the sample project, through the testdata/fixtures helpers "+
			"(internal/store/fixture_copy_test.go).", offender)
	}
}

// skipTree reports whether a directory of this name sits outside the package
// surface `./...` matches, so neither guard in this package should walk into
// it: a file there is not the developer's to move, and naming it sends them
// after something no edit in their own tree can fix.
//
// It mirrors cmd/go's own rule (cmd/go/internal/modload/search.go — "Avoid
// .foo, _foo, and testdata subdirectory trees", plus the vendor prune) minus
// testdata, which is the one tree these guards exist to read. The dot clause is
// what keeps .worktrees/ out: a git worktree of an older revision restores test
// files that `./...` never builds, and reporting them turns the gate red in a
// tree where nothing is wrong.
//
// node_modules is the single deviation — `./...` does reach it, but it holds no
// Go and ~24k entries. TestNoTestFilesUnderTestdata cross-checks every skip
// against `go list ./...`, so the deviation fails loudly the day a Go package
// appears under it. A build-output directory gets no entry here: the walk only
// reads filenames, so descending one costs nothing, and skipping `dist` by bare
// name blinded this guard to a subtree the pattern does build.
func skipTree(name string) bool {
	return strings.HasPrefix(name, ".") ||
		strings.HasPrefix(name, "_") ||
		name == "vendor" ||
		name == "node_modules"
}

// wildcardPackageDirs returns the directory of every package `go list ./...`
// matches from root. What the pattern reaches is a fact about the go tool, not
// about the names this file happens to exclude, so the guard's coverage claim
// is checked against the tool rather than restated.
func wildcardPackageDirs(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", "./...")
	cmd.Dir = root
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list ./... in %s: %v\n%s", root, err, stderr.String())
	}
	var dirs []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			dirs = append(dirs, filepath.Clean(line))
		}
	}
	if len(dirs) == 0 {
		t.Fatalf("`go list ./...` matched no package in %s; this guard has no oracle to check its skip rule against", root)
	}
	return dirs
}

// underTestdata reports whether a module-relative slash path has a testdata
// directory above it. Any component but the last is a directory, and the
// exclusion takes the whole subtree, not just the directory itself.
func underTestdata(rel string) bool {
	parts := strings.Split(rel, "/")
	for _, part := range parts[:len(parts)-1] {
		if part == "testdata" {
			return true
		}
	}
	return false
}
