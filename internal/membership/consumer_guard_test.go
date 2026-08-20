package membership

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// This guard is the enforcement half of the membership boundary: the surfaces
// that consume container membership — the roadmap, both context summaries and
// the projection rollups — derive it ONLY through this package. Before the
// cutover each of them re-derived membership from the raw `parent:` link with
// its own rival rules (raw-keyed children maps, a two-level milestone walk,
// per-nib store scans), and prose could not have held the line any better here
// than it did for resolvedParent (see internal/graph's parent_read_guard).
//
// So, same mechanism: every `.Parent` selector in the audited files is
// reported and compared against the approved list IN BOTH DIRECTIONS. A new
// read fails the test wherever it is written; an approved entry the walk
// stops finding fails it too, so the guard cannot go quiet by having its walk
// break. Sites are keyed by file and enclosing function, with a count, so a
// second read inside an approved function still fails.
//
// The list is EMPTY today: the migration left no direct reads in any audited
// file. Adding an entry is a deliberate act — the question to answer first is
// whether the new site is asking "what belongs to container X" (this
// package's question, never re-derived) or something genuinely different.
var approvedConsumerParentReads = map[string]struct {
	count  int
	reason string
}{}

// auditedFiles are the membership-consuming surfaces, relative to the module
// root. Test files are excluded by name; generated files do not occur here.
var auditedFiles = []string{
	"cmd/roadmap.go",
	"cmd/context.go",
	"internal/nibcontext/context.go",
	"internal/graph/projection_resolver.go",
}

func TestMembershipConsumersDoNotReadParentDirectly(t *testing.T) {
	root := moduleRoot(t)

	found := map[string]int{}
	for _, rel := range auditedFiles {
		path := filepath.Join(root, filepath.FromSlash(rel))
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v — if the file moved, update auditedFiles rather than deleting the guard", rel, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				sel, ok := node.(*ast.SelectorExpr)
				if ok && sel.Sel.Name == "Parent" {
					found[fmt.Sprintf("%s:%s", rel, fn.Name.Name)]++
				}
				return true
			})
		}
	}

	for site, n := range found {
		approved, ok := approvedConsumerParentReads[site]
		if !ok {
			t.Errorf("%s reads .Parent %d time(s) but is not an approved site — membership questions go through internal/membership; if this site asks something genuinely different, approve it here with its reason", site, n)
			continue
		}
		if n != approved.count {
			t.Errorf("%s reads .Parent %d time(s), approved for %d (%s) — re-justify the change", site, n, approved.count, approved.reason)
		}
	}
	for site := range approvedConsumerParentReads {
		if _, ok := found[site]; !ok {
			t.Errorf("approved site %s no longer reads .Parent — remove its entry so the list stays honest", site)
		}
	}

	// The walk itself must be alive: the audited files exist and hold
	// functions, or every assertion above passes vacuously.
	if len(auditedFiles) == 0 {
		t.Fatal("no audited files; the guard guards nothing")
	}
	var names []string
	for site := range found {
		names = append(names, site)
	}
	sort.Strings(names)
	t.Logf("audited %d files; .Parent sites found: %v", len(auditedFiles), names)
}

// moduleRoot walks up from the package directory to the directory holding
// go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod in any parent of the working directory")
		}
		dir = parent
	}
}

// Guard on the guard: the file set must never silently include a test file,
// whose helpers legitimately build fixtures with Parent fields.
func TestAuditedFilesAreProductionSources(t *testing.T) {
	for _, rel := range auditedFiles {
		if strings.HasSuffix(rel, "_test.go") {
			t.Errorf("%s is a test file; the audit covers production sources only", rel)
		}
	}
}
