package graph

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
)

// This file is the in-place record of every site in internal/graph that touches
// the stored `parent:` link directly, and the guard that keeps that record
// honest. Parent-ness itself has one definition — resolvedParent — and a site
// that re-derives it from the raw b.Parent string stays self-consistent while
// disagreeing with every other surface. That is how the split in nibs-oa4m
// arose, and prose could not hold the line: a doc comment counting the
// exceptions ("one caller is deliberately not routed through here") was wrong
// by eight the last time anyone counted.
//
// So the exception set is derived from the source instead, and compared against
// the approved list below IN BOTH DIRECTIONS. A new raw read fails the test
// wherever it is written, including inside a helper extracted out of an
// existing function; an approved entry the walk stops finding fails it too, so
// the guard cannot go quiet by having its walk break.
//
// Three mechanism decisions, each of which could have been made the other way:
//
//  1. GENERATED FILES ARE SKIPPED, by the canonical "Code generated … DO NOT
//     EDIT." marker rather than by filename. generated.go carries five .Parent
//     selectors that no one writes by hand. schema.resolvers.go deliberately
//     does NOT carry that marker and IS walked — gqlgen rewrites it, but its
//     resolver bodies are hand-written and are exactly where a raw read would
//     appear.
//
//  2. EVERY .Parent SELECTOR IS REPORTED, including the ones that read a
//     GraphQL input struct rather than a nib (input.Parent in the create and
//     update resolvers). Those are not stored-link reads at all and could have
//     been filtered out — but the walk is syntactic and cannot tell a
//     *nib.Nib from a *model.UpdateNibInput, so any filter would be a
//     heuristic on the receiver's spelling. A heuristic that drops input.Parent
//     silently drops a genuine read that happens to be spelled the same way.
//     They are carried in the list with that reason instead, which costs three
//     entries and buys a walk with no silent-drop rule in it.
//
//  3. WRITES ARE COVERED ALONGSIDE READS. A selector-based walk sees both, and
//     assignments to b.Parent are the sites that CREATE a stored spelling —
//     the thing every reader then has to resolve. Each entry says which it is.
//
// Sites are keyed by file and enclosing function, not by line, so ordinary
// edits above a site do not churn the list. The count is part of the entry, so
// a second raw read added inside an already-approved function still fails.

// parentReadSite is one approved place where the stored parent link is touched
// directly. Reason is why that site is not routed through resolvedParent — it
// is the record finding #1 of nibs-x3zp asked for, and it is what a reviewer
// reads when the guard fails on a new entry.
type parentReadSite struct {
	count  int
	reason string
}

// approvedParentReads is the golden list: every non-test, non-generated site in
// this package that names .Parent, with its justification. Adding a site here
// is a deliberate act — the question to answer first is whether the site is
// asking "does this nib have a parent", which is resolvedParent's question and
// must not be re-derived, or something else (change detection, the stored
// spelling as the subject, a diagnostic).
var approvedParentReads = map[string]parentReadSite{
	// The rule itself. Every "does this nib have a parent" question in the
	// package resolves through here; this is the one body allowed to ask the
	// stored field directly, because reading it is what it is for.
	"filters.go:resolvedParent": {count: 2, reason: "the resolved-parent rule's own implementation"},

	// Writes, not reads. validateAndSetParent decides whether the parent CHANGED
	// from the resolved old value (see its doc), then stores the normalized new
	// one. These two selectors are the assignments that produce a stored
	// spelling — the thing every reader above then has to resolve.
	"resolver.go:(*Resolver).validateAndSetParent":      {count: 2, reason: "writes: clears the link, and stores the normalized new parent"},
	"schema.resolvers.go:(*mutationResolver).CreateNib": {count: 5, reason: "4 read input.Parent (a GraphQL input struct, not a stored link); 1 writes the normalized parent onto the new nib"},

	// A chain walk, not a parent-ness decision. The stored link is the next
	// iteration's argument to Reader.Get, which resolves it — a link naming no
	// nib ends the walk there, which is the same answer resolving would give.
	"resolver.go:(*Resolver).activateParentChain": {count: 1, reason: "the stored link is the next iteration's input to Reader.Get, which resolves it"},

	// UpdateNib's remaining selectors are two input.Parent probes (was the field
	// supplied at all — a question about the REQUEST, not the nib) and the
	// activation early-out. The early-out asks only whether there is any link to
	// start a walk from; a link naming no nib makes activateParentChain a no-op
	// on its first Reader.Get, so resolving here would change nothing but the
	// cost. The type-change gate in this same function is NOT here: it resolves.
	"schema.resolvers.go:(*mutationResolver).UpdateNib": {count: 4, reason: "2 read input.Parent (was the field supplied); 2 start the activation walk, which resolves each rung itself"},

	// The stored spelling IS this field's answer — storedParentId exists so a
	// link naming no nib stays inspectable after parentId went resolved.
	"schema.resolvers.go:(*nibResolver).StoredParentID": {count: 2, reason: "the raw stored link is this field's contract; parentId is the resolved reading"},

	// Correct only because GetSnapshot resolves ids the way Get does (exact,
	// then the configured prefix). That dependency is not obvious from the call
	// site, so it is pinned by TestParentResolverDependsOnGetSnapshotIDResolution
	// rather than left to a reader to notice.
	"schema.resolvers.go:(*nibResolver).Parent": {count: 2, reason: "GetSnapshot runs the same exact-then-prefix lookup as Get, so this resolves; pinned by TestParentResolverDependsOnGetSnapshotIDResolution"},

	// A diagnostic formatter. It deliberately shows BOTH spellings — the stored
	// one and what it resolves to — which is the whole point of the message, so
	// routing it through the rule would delete the information it exists to
	// carry.
	"bulkreorder.go:describeParent": {count: 3, reason: "diagnostic formatter that deliberately reports the stored spelling AND its resolution"},
}

// TestRawParentReadsAreAllApproved is the totality guard for the list above.
func TestRawParentReadsAreAllApproved(t *testing.T) {
	found := parentSelectorSites(t)

	for key, gotCount := range found {
		approved, ok := approvedParentReads[key]
		if !ok {
			t.Errorf("%s reads or writes the stored parent link (%d site(s)) but is not in "+
				"approvedParentReads. Parent-ness has one definition — resolvedParent — so either route "+
				"this through it, or add an entry saying why the raw stored link is the right reading here.",
				key, gotCount)
			continue
		}
		if approved.count != gotCount {
			t.Errorf("%s names .Parent %d time(s), approved for %d. A raw parent access was added or "+
				"removed inside an already-approved site; re-read it and update the count and the reason.",
				key, gotCount, approved.count)
		}
	}

	keys := make([]string, 0, len(approvedParentReads))
	for key := range approvedParentReads {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		// An entry with no reason is an approval nobody justified, which is the
		// state this list exists to prevent — the count alone would silence the
		// guard while recording nothing about why the site is exempt.
		if strings.TrimSpace(approvedParentReads[key].reason) == "" {
			t.Errorf("%s is approved with an empty reason; the reason is the record, not decoration", key)
		}
		if _, ok := found[key]; !ok {
			t.Errorf("%s is approved in approvedParentReads but the source walk did not find it. Either "+
				"the site is gone (drop the entry) or the walk is no longer reading what it is meant to — "+
				"the second is the failure this direction exists to catch.", key)
		}
	}
}

// parentSelectorSites parses this package's non-test, non-generated sources and
// returns, per enclosing function, how many .Parent selectors it contains.
//
// The key is "file.go:funcName", with the receiver type spelled out for a
// method ("resolver.go:(*Resolver).validateAndSetParent"). A selector outside
// any function declaration is keyed to "file.go:<package-level>" so it cannot
// escape the guard by living in a var initializer.
func parentSelectorSites(t *testing.T) map[string]int {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	sites := make(map[string]int)
	walked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if isGeneratedSource(src) {
			continue
		}
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		walked++

		// Count within each function first, then attribute whatever is left to
		// package level — a selector can legally sit in a var initializer, and
		// dropping it would be a silent hole in the guard.
		inFuncs := 0
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if n := countParentSelectors(fn); n > 0 {
				sites[name+":"+funcKey(fn)] += n
				inFuncs += n
			}
		}
		if total := countParentSelectors(file); total > inFuncs {
			sites[name+":<package-level>"] = total - inFuncs
		}
	}

	// A walk that reads nothing passes both directions against an empty list, so
	// prove it actually opened this package's sources.
	if walked == 0 {
		t.Fatal("the walk parsed no files; it is not reading this package's sources")
	}
	return sites
}

// countParentSelectors reports how many `X.Parent` selector expressions appear
// anywhere inside n, reads and writes alike (an assignment's left-hand side is
// a selector too).
func countParentSelectors(n ast.Node) int {
	count := 0
	ast.Inspect(n, func(node ast.Node) bool {
		if sel, ok := node.(*ast.SelectorExpr); ok && sel.Sel != nil && sel.Sel.Name == "Parent" {
			count++
		}
		return true
	})
	return count
}

// funcKey renders a function's name, qualified by its receiver type for a
// method, so two methods named alike on different types stay distinguishable.
func funcKey(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return "(" + typeString(fn.Recv.List[0].Type) + ")." + fn.Name.Name
}

// typeString renders the receiver's type as it is written in source, covering
// the two shapes a receiver can take (T and *T).
func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.Ident:
		return t.Name
	default:
		return fmt.Sprintf("%T", expr)
	}
}

// isGeneratedSource reports whether src carries the canonical generated-code
// marker, which by convention appears on a line of its own before the package
// clause. Matching the marker rather than a filename means a newly generated
// file is excluded the day it appears, and a file that stops being generated is
// walked again without anyone remembering to update a skip list.
func isGeneratedSource(src []byte) bool {
	for line := range strings.SplitSeq(string(src), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			return false
		}
		if strings.HasPrefix(line, "// Code generated ") && strings.HasSuffix(line, " DO NOT EDIT.") {
			return true
		}
	}
	return false
}

// TestReportRawParentSites prints the derived surface as pastable list entries.
// It asserts nothing — it exists so the approved list is regenerated FROM the
// source it describes rather than hand-transcribed, which is how the
// caller-counting comment it replaces went wrong. Opt-in, because its output is
// noise on an ordinary run.
//
//	NIBS_REPORT_PARENT_SITES=1 scripts/go-test-capped.sh ./internal/graph/ \
//	  -run TestReportRawParentSites -v
func TestReportRawParentSites(t *testing.T) {
	if os.Getenv("NIBS_REPORT_PARENT_SITES") == "" {
		t.Skip("set NIBS_REPORT_PARENT_SITES=1 to print the derived raw-parent surface")
	}
	found := parentSelectorSites(t)
	keys := make([]string, 0, len(found))
	for key := range found {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		t.Logf("%q: {count: %d, reason: %q},", key, found[key], "")
	}
}
