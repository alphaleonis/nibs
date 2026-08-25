package testskip_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestSymlinkSkipsAreEnrolled is the decay guard for the whole mechanism: a skip
// this package never saw is a skip nothing counts, which puts the fixture back in
// the state the package exists to leave — green on a machine where it never ran.
//
// The idiom it looks for is the exact one the repo used everywhere before
// testskip: an `if` whose condition tests os.Symlink, with a t.Skip inside. A
// symlink skip written some other way (a helper that stores the error and skips
// three lines later, a message that never says "symlink") still escapes, so this
// is a guard against COPY-PASTE rather than a totality proof — copy-paste is how
// all fifteen of the original sites came to exist.
func TestSymlinkSkipsAreEnrolled(t *testing.T) {
	root := moduleRoot(t)

	var offenders []string
	fset := token.NewFileSet()
	walked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// skipTree, shared with TestNoTestFilesUnderTestdata: a test file
			// outside the surface `./...` builds is not this module's to police,
			// and a restored .worktrees/ checkout is full of them.
			if path != root && skipTree(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		walked++
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			stmt, ok := n.(*ast.IfStmt)
			if !ok {
				return true
			}
			if !mentionsSelector(stmt.Init, "os", "Symlink") && !mentionsSelector(stmt.Cond, "os", "Symlink") {
				return true
			}
			if skip := findSkipCall(stmt.Body); skip != nil {
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					rel = path
				}
				offenders = append(offenders, rel+":"+strconv.Itoa(fset.Position(skip.Pos()).Line))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	// A walk that reads nothing passes against any tree.
	if walked == 0 {
		t.Fatalf("found no _test.go under %s; this guard is not reading the module", root)
	}

	for _, offender := range offenders {
		t.Errorf("%s skips on os.Symlink failing without going through testskip, so nothing counts that skip and a run where it fired "+
			"is indistinguishable from one where the guard passed. Call testskip.SymlinkUnavailable(t, err) instead.", offender)
	}
}

// moduleRoot walks up from this package's directory to the go.mod above it.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above this package")
		}
		dir = parent
	}
}

// mentionsSelector reports whether n contains a `pkg.name` selector.
func mentionsSelector(n ast.Node, pkg, name string) bool {
	if n == nil {
		return false
	}
	found := false
	ast.Inspect(n, func(node ast.Node) bool {
		sel, ok := node.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return true
		}
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == pkg {
			found = true
		}
		return true
	})
	return found
}

// findSkipCall returns the first `<something>.Skip…` call inside n.
func findSkipCall(n ast.Node) *ast.CallExpr {
	var out *ast.CallExpr
	ast.Inspect(n, func(node ast.Node) bool {
		if out != nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && strings.HasPrefix(sel.Sel.Name, "Skip") {
			out = call
			return false
		}
		return true
	})
	return out
}
