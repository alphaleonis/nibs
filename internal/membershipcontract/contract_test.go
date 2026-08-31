package membershipcontract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
)

// TestGeneratedMembershipContractIsFresh is the Go half of the parity pin: the
// committed contract must be byte-identical to what this package renders now.
// It is what turns a rule change that MOVES A FIXTURE ROW'S ANSWER into a
// failing test even when nobody looks at the web; a rule change that moves no
// row renders the same bytes and passes here (see the package comment).
func TestGeneratedMembershipContractIsFresh(t *testing.T) {
	committed, err := os.ReadFile(filepath.Join(moduleRoot(t), filepath.FromSlash(OutputPath)))
	if err != nil {
		t.Fatalf("reading the committed %s: %v — run `task codegen` and commit the result", OutputPath, err)
	}
	if string(committed) != Render() {
		t.Fatalf("%s is stale: the rule, the fixture or the renderer changed without regenerating it — run `task codegen` and commit the result. If the RULE changed, also check that web/src/lib/membership.ts still agrees and that a fixture row exercises the change.", OutputPath)
	}
}

// TestContractFixtureDiscriminatesEachRuleDecision proves the fixture can tell
// the rule from a broken copy of it. A contract whose rows all resolve the same
// way would still be byte-pinned and still be replayed by the web, and would
// still pass while either side dropped a clause — the failure mode a parity test
// is bought to prevent.
//
// Each mutant below undoes exactly one decision ResolvedMilestoneID makes: its
// three clauses, plus its choice to answer with the target's id rather than the
// stored string. The requirement is that its answers differ from the real
// rule's on at least one fixture row, which is the same thing as "a TS mirror
// missing this decision fails membership.test.ts".
//
// The list is a hand-written copy of the rule, not a derivation of it: a new
// decision does not appear here on its own. TestResolvedMilestoneIDShapeIsPinned
// is what makes forgetting to add one loud.
func TestContractFixtureDiscriminatesEachRuleDecision(t *testing.T) {
	f := fixture()
	lookup := fixtureLookup(f)

	mutants := []struct {
		decision string
		fn       func(*nib.Nib, membership.Lookup) string
	}{
		{
			decision: "the subject is not itself a milestone",
			fn: func(b *nib.Nib, lookup membership.Lookup) string {
				if b.Milestone == "" {
					return ""
				}
				target := lookup(b.Milestone)
				if target == nil || target.EffectiveType() != "milestone" {
					return ""
				}
				return target.ID
			},
		},
		{
			decision: "the target exists",
			fn: func(b *nib.Nib, lookup membership.Lookup) string {
				if b.Milestone == "" || b.EffectiveType() == "milestone" {
					return ""
				}
				target := lookup(b.Milestone)
				if target == nil {
					// Trusting the stored id is what "no existence check" means.
					return b.Milestone
				}
				if target.EffectiveType() != "milestone" {
					return ""
				}
				return target.ID
			},
		},
		{
			decision: "the target is milestone-typed",
			fn: func(b *nib.Nib, lookup membership.Lookup) string {
				if b.Milestone == "" || b.EffectiveType() == "milestone" {
					return ""
				}
				target := lookup(b.Milestone)
				if target == nil {
					return ""
				}
				return target.ID
			},
		},
		{
			decision: "the answer is the target's id, not the stored string",
			fn: func(b *nib.Nib, lookup membership.Lookup) string {
				if b.Milestone == "" || b.EffectiveType() == "milestone" {
					return ""
				}
				target := lookup(b.Milestone)
				if target == nil || target.EffectiveType() != "milestone" {
					return ""
				}
				return b.Milestone
			},
		},
	}

	for _, m := range mutants {
		t.Run(m.decision, func(t *testing.T) {
			for _, r := range f {
				want := membership.ResolvedMilestoneID(r.nib, lookup)
				if got := m.fn(r.nib, lookup); got != want {
					return // This row discriminates the decision. That is enough.
				}
			}
			t.Errorf("no fixture row distinguishes the rule from one dropping %q — the contract would pass with that decision missing on either side", m.decision)
		})
	}
}

// wireResolverFile holds gqlgen's field resolvers. Resolver BODIES survive
// regeneration (see the header of internal/graph/resolver.go), so reading this
// file is reading the wire's real behavior, not a scratch copy of it.
const wireResolverFile = "internal/graph/schema.resolvers.go"

// TestRenderedTypeIsTheWireType pins the generated file's projection to what the
// wire reports, and pins that to the resolver rather than to prose. Two halves,
// and the contract's whole foundation — that the rows are the client's view of
// the store — needs both:
//
//   - The renderer's own projection is recomputed here on separately built nibs
//     rather than re-read out of rows(), so rendering the STORED `type` is
//     caught. That mutation otherwise regenerates cleanly and replays cleanly
//     (the mirror reads the same wrong column), leaving the typeless row
//     carrying `type: ""`, a value the Nib.type resolver can never emit.
//   - internal/graph's Nib.type resolver is read out of the AST and must still
//     answer through EffectiveType(), and `milestone` must still have no
//     resolver at all. Without this half both sides of the comparison above
//     hardcode the same assumption about the wire and nothing checks it: the
//     resolver could switch to obj.Type and every test in this package would
//     stay green while the generated header's stated foundation became false.
func TestRenderedTypeIsTheWireType(t *testing.T) {
	assertWireProjection(t)

	f := fixture()
	got := rows()
	if len(got) != len(f) {
		t.Fatalf("rows() produced %d rows for %d fixture nibs", len(got), len(f))
	}
	for i, r := range got {
		src := f[i].nib
		if want := src.EffectiveType(); r.Type != want {
			t.Errorf("row %s renders type %q; the wire reports %q", r.ID, r.Type, want)
		}
		if r.Milestone != src.Milestone {
			t.Errorf("row %s renders milestone %q; the wire reports the stored %q verbatim", r.ID, r.Milestone, src.Milestone)
		}
		if r.ID != src.ID {
			t.Errorf("row %d renders id %q, want %q", i, r.ID, src.ID)
		}
	}
}

// assertWireProjection reads the Nib field resolvers and holds the two facts the
// generated header states: `type` is resolved through EffectiveType, and
// `milestone` has no resolver, so the wire reports it verbatim.
func assertWireProjection(t *testing.T) {
	t.Helper()
	path := filepath.Join(moduleRoot(t), filepath.FromSlash(wireResolverFile))
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v — if the resolvers moved, update this guard rather than deleting it", wireResolverFile, err)
	}

	var typeResolver *ast.FuncDecl
	var hasMilestoneResolver bool
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 || fn.Body == nil {
			continue
		}
		if receiverTypeName(fn.Recv.List[0].Type) != "nibResolver" {
			continue
		}
		switch fn.Name.Name {
		case "Type":
			typeResolver = fn
		case "Milestone":
			hasMilestoneResolver = true
		}
	}

	if typeResolver == nil {
		t.Fatalf("no (*nibResolver).Type in %s — the wire's `type` is not resolved where this guard looks, so nothing here knows what the contract's `type` column should hold", wireResolverFile)
	}
	if n := countSelector(typeResolver.Body, "EffectiveType"); n != 1 {
		t.Errorf("(*nibResolver).Type in %s selects EffectiveType %d time(s), want 1 — the wire no longer reports the effective type, so the contract's `type` column and rows() are projecting a field the client never sees", wireResolverFile, n)
	}
	if hasMilestoneResolver {
		t.Errorf("%s now declares a (*nibResolver).Milestone resolver — `milestone` is no longer autobound and reported verbatim, so the contract's `milestone` column is no longer the wire's", wireResolverFile)
	}
}

// countSelector reports how many `X.name` selector expressions appear inside n.
func countSelector(n ast.Node, name string) int {
	count := 0
	ast.Inspect(n, func(node ast.Node) bool {
		if sel, ok := node.(*ast.SelectorExpr); ok && sel.Sel != nil && sel.Sel.Name == name {
			count++
		}
		return true
	})
	return count
}

// receiverTypeName renders a method receiver's type name, pointer stripped.
func receiverTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return receiverTypeName(e.X)
	case *ast.IndexExpr:
		return receiverTypeName(e.X)
	}
	return ""
}

// TestRuleIsComputableFromTheWireProjection asserts the rule reads nothing the
// contract cannot ship. Every answer is recomputed over nibs rebuilt from the
// three projected columns alone and must equal the answer over the full fixture
// nib, so a clause reading a field outside the projection reddens here instead
// of shipping a contract the web is structurally unable to satisfy.
//
// The boundary is exactly "does a fixture row VARY that field", and the two
// off-projection fields fall on opposite sides of it: a clause on `parent:`
// reddens this test, because t1 and t2 carry one; a clause on `status:` does
// not, because no fixture row sets it. The bound is the fixture's, as everywhere
// else here — only the shape pin sees a field read the fixture is blind to.
func TestRuleIsComputableFromTheWireProjection(t *testing.T) {
	got := rows()
	projected := make([]fixtureNib, 0, len(got))
	for _, r := range got {
		projected = append(projected, fixtureNib{
			nib:     &nib.Nib{ID: r.ID, Type: r.Type, Milestone: r.Milestone},
			aliases: r.Aliases,
		})
	}
	lookup := fixtureLookup(projected)

	for i, r := range got {
		if want := membership.ResolvedMilestoneID(projected[i].nib, lookup); want != r.Resolved {
			t.Errorf("row %s answers %q over the full nib but %q over the fields the wire carries — the rule reads something the contract cannot ship, so no TS mirror can agree with it", r.ID, r.Resolved, want)
		}
	}
}

// pinnedRuleShape is the structural fingerprint of ResolvedMilestoneID's body.
// It exists because neither the fixture nor the mutant list above is derived
// from the rule: both are hand-written copies that a new clause does not reach.
// A fourth clause over the three fields the fixture already varies moves no
// row's answer, so it renders identical bytes, replays identically and passes
// every other test in this package and in the web suite.
//
// The fingerprint is deliberately structural rather than textual: it survives
// comment edits, gofmt and local renames, and moves only when the rule gains or
// loses a field read, a branch, a return or a boolean operator — which every
// added clause must do.
var pinnedRuleShape = ruleShape{
	selectors:  map[string]int{"Milestone": 2, "EffectiveType": 2, "ID": 1},
	ifs:        2,
	returns:    3,
	logicalOps: 2,
}

// ruleShape counts the structural features of a function body.
type ruleShape struct {
	selectors  map[string]int
	ifs        int
	returns    int
	logicalOps int
}

func (s ruleShape) String() string {
	names := make([]string, 0, len(s.selectors))
	for name := range s.selectors {
		names = append(names, fmt.Sprintf("%s×%d", name, s.selectors[name]))
	}
	sort.Strings(names)
	return fmt.Sprintf("selectors{%s} ifs=%d returns=%d logicalOps=%d",
		strings.Join(names, " "), s.ifs, s.returns, s.logicalOps)
}

// TestResolvedMilestoneIDShapeIsPinned is the backstop for the direction the
// fixture cannot see. It reddens on any change to the rule's shape and its
// message states the obligation such a change carries — neither the fixture nor
// the mutant list follows automatically.
//
// Modeled on internal/membership/consumer_guard_test.go, which reads the same
// kind of evidence out of the AST rather than trusting prose. The residual it
// does not close: swapping one operator for another of the same kind leaves the
// fingerprint unchanged — one comparison for another, and `||` for `&&`, since
// shapeOf counts both connectives into logicalOps — so that is caught only if it
// moves a fixture row's answer.
func TestResolvedMilestoneIDShapeIsPinned(t *testing.T) {
	path := filepath.Join(moduleRoot(t), filepath.FromSlash("internal/membership/membership.go"))
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing internal/membership/membership.go: %v — if the rule moved, update this guard rather than deleting it", err)
	}

	var body *ast.BlockStmt
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == "ResolvedMilestoneID" {
			body = fn.Body
			break
		}
	}
	if body == nil {
		t.Fatal("ResolvedMilestoneID not found in internal/membership/membership.go — the guard cannot go quiet by losing its subject")
	}

	got := shapeOf(f, body)
	if got.String() != pinnedRuleShape.String() {
		t.Errorf("ResolvedMilestoneID's shape changed:\n  got  %s\n  want %s\n"+
			"If the change is BEHAVIOR-PRESERVING — a split if, an extracted helper beside it, a rename — re-pin pinnedRuleShape and stop; nothing else here applies.\n"+
			"If it adds or changes a DECISION, neither the fixture in contract.go nor the mutant list in TestContractFixtureDiscriminatesEachRuleDecision follows it automatically: add a fixture row whose answer the change moves, add a mutant that undoes the new decision, mirror the change in web/src/lib/membership.ts, run `task codegen`, and re-pin.\n"+
			"If the rule's body moved to another FILE, point this guard at that file: shapeOf follows a helper in the same file and nothing further, and TestPinnedRuleShapeIsNotDegenerate refuses the pin that a moved-out body would leave behind.",
			got, pinnedRuleShape)
	}
}

// TestPinnedRuleShapeIsNotDegenerate is the guard on the guard. Re-pinning is
// the one instruction above that always applies, so a pin that watches nothing
// has to be refused rather than accepted quietly — and the degenerate pin is
// reachable: move ResolvedMilestoneID's body into a helper in another file of
// package membership and what is left behind is `return impl(b, lookup)`, whose
// honest fingerprint is selectors{} ifs=0 returns=1 logicalOps=0. Every later
// clause would then live outside this guard's view. shapeOf follows a same-FILE
// helper, so that variant re-pins to a shape that still watches the rule; this
// floor is what makes the cross-file one loud instead of free.
//
// The numbers are the rule as it stands — three clauses over three distinct
// fields, 2 ifs joined by 2 boolean operators, 3 returns. They are a FLOOR, not
// a second copy of the pin: a rule that legitimately grows stays above them.
func TestPinnedRuleShapeIsNotDegenerate(t *testing.T) {
	const advice = " — a pin this small watches nothing; if the rule moved, point the guard at where it moved to rather than pinning what it left behind"
	if n := len(pinnedRuleShape.selectors); n < 3 {
		t.Errorf("pinnedRuleShape names %d distinct field reads, want at least 3%s", n, advice)
	}
	if pinnedRuleShape.ifs < 2 {
		t.Errorf("pinnedRuleShape has ifs=%d, want at least 2%s", pinnedRuleShape.ifs, advice)
	}
	if pinnedRuleShape.returns < 3 {
		t.Errorf("pinnedRuleShape has returns=%d, want at least 3%s", pinnedRuleShape.returns, advice)
	}
	if pinnedRuleShape.logicalOps < 2 {
		t.Errorf("pinnedRuleShape has logicalOps=%d, want at least 2%s", pinnedRuleShape.logicalOps, advice)
	}
}

// shapeOf counts the structural features of a function body: how often each
// field or method name is selected, and how many branches, returns and boolean
// operators the body carries.
//
// A call to a plain function declared in the SAME FILE is followed into that
// function's body, so extracting the rule into a helper beside it keeps the
// rule in view instead of leaving a fingerprint that watches the call. Calls
// through a value (the Lookup parameter) and method calls are not followed —
// there is nothing lexical to follow — and the visited set stops recursion.
func shapeOf(file *ast.File, body *ast.BlockStmt) ruleShape {
	sameFile := map[string]*ast.FuncDecl{}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Body != nil {
			sameFile[fn.Name.Name] = fn
		}
	}

	s := ruleShape{selectors: map[string]int{}}
	visited := map[string]bool{}
	var walk func(ast.Node)
	walk = func(root ast.Node) {
		ast.Inspect(root, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.SelectorExpr:
				if n.Sel != nil {
					s.selectors[n.Sel.Name]++
				}
			case *ast.IfStmt:
				s.ifs++
			case *ast.ReturnStmt:
				s.returns++
			case *ast.BinaryExpr:
				if n.Op == token.LAND || n.Op == token.LOR {
					s.logicalOps++
				}
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok && !visited[id.Name] {
					if fn, ok := sameFile[id.Name]; ok {
						visited[id.Name] = true
						walk(fn.Body)
					}
				}
			}
			return true
		})
	}
	walk(body)
	return s
}

// The TS half of the contract: the mirror, the generated fixture, and the call
// site that joins them.
const (
	mirrorModule   = "web/src/lib/membership.ts"
	mirrorSymbol   = "resolvedMilestoneId"
	contractSymbol = "MEMBERSHIP_CONTRACT"
	contractModule = "membershipContract"
	replayCallOpen = "it.each("
	replayCallSite = replayCallOpen + contractSymbol + ")"
)

// TestWebImportsTheContract keeps the mechanism armed. The TS-side pins live in
// one file — it drives the generated fixture through the mirror, and it carries
// the compile-time row-subset guard — while the freshness test above keeps
// passing forever whatever happens to it, because it compares Go to Go.
//
// This is where the module differs from its internal/webvocab sibling:
// vocabulary.ts is imported by app code, so deleting its consumer breaks the
// build. The generated contract is imported only by a test, so deleting its
// consumer would otherwise be free.
//
// It asserts the WIRING rather than three separate facts: ONE *.test.ts must
// import MEMBERSHIP_CONTRACT, import resolvedMilestoneId, and contain the call
// site joining them. Requiring the call site is what makes deleting the
// four-line it.each block — cheaper than deleting the file, and the obvious
// response to a reddening replay — as loud as deleting the file. Requiring one
// file to do all three stops a future production importer of
// resolvedMilestoneId from satisfying the mirror half on the replay's behalf.
//
// Skips are checked in two scopes, because the two forms have two different
// reaches. A `.only` anywhere in the file narrows the WHOLE file to the marked
// tests, so it is checked file-wide — and it has to be, because vitest's
// allowOnly defaults to !CI, which makes a committed `.only` silent under `task
// test` and loud only in CI. A `.skip`/`.todo` reaches only what it marks, so it
// is checked on the describes ENCLOSING the replay and nowhere else: one on an
// unrelated test in the same file leaves the replay running, and failing for it
// would be asserting something this guard has not checked. A skip on the replay
// call itself needs no check of its own — it changes that call's text, so the
// wiring assertion catches it.
//
// What it does not close: a contributor can still gut the replay's body while
// leaving the call site standing. It catches the cheap disarm — the one that
// happens while chasing an unrelated red — not a determined one.
func TestWebImportsTheContract(t *testing.T) {
	root := moduleRoot(t)
	srcDir := filepath.Join(root, "web", "src")

	var scanned int
	var contractImporters, mirrorImporters, replayFiles, disarmed []string
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if ext := filepath.Ext(path); ext != ".ts" && ext != ".svelte" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		slashed := filepath.ToSlash(rel)
		if slashed == OutputPath {
			return nil // The generated module declares the symbol; it does not consume it.
		}
		scanned++
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(content)

		importsContract := importsSymbolFrom(text, contractSymbol, contractModule)
		if importsContract {
			contractImporters = append(contractImporters, slashed)
		}
		// The mirror module itself declares the symbol; it does not consume it.
		importsMirror := slashed != mirrorModule && importsSymbolFrom(text, mirrorSymbol, "membership")
		if importsMirror {
			mirrorImporters = append(mirrorImporters, slashed)
		}

		at := replayCallAt(text)
		if !importsContract || !importsMirror || !strings.HasSuffix(slashed, ".test.ts") || at < 0 {
			return nil
		}
		replayFiles = append(replayFiles, slashed)
		disarmed = append(disarmed, disarmingForms(slashed, text, at)...)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v — if the web sources moved, update this guard rather than deleting it", srcDir, err)
	}

	// The walk itself must be alive, or every assertion below passes vacuously.
	if scanned == 0 {
		t.Fatalf("no .ts or .svelte files under %s; the guard guards nothing", srcDir)
	}
	if len(replayFiles) == 0 {
		t.Errorf("no *.test.ts under web/src both imports %s and %s AND calls %s — "+
			"the generated contract at %s is replayed by nobody, and the freshness test above cannot notice because it compares Go to Go.\n"+
			"  files importing %s: %v\n  files importing %s from %s: %v\n"+
			"If the replay was restructured rather than removed, point replayCallOpen and contractSymbol at the new call form rather than deleting this guard.",
			contractSymbol, mirrorSymbol, replayCallSite, OutputPath,
			contractSymbol, contractImporters, mirrorSymbol, mirrorModule, mirrorImporters)
	}
	if len(disarmed) > 0 {
		t.Errorf("the parity replay is present but does not run: %v — a skipped replay is a disarmed contract", disarmed)
	}
	t.Logf("scanned %d web sources; replay in %v; %s imported by %v; %s used by %v",
		scanned, replayFiles, contractSymbol, contractImporters, mirrorSymbol, mirrorImporters)
}

// replayCallAt returns the offset of the call driving the contract through the
// mirror, or -1. The argument is compared rather than the whole call text, so
// the namespace form (`it.each(contract.MEMBERSHIP_CONTRACT)`) counts too.
func replayCallAt(text string) int {
	for i := 0; ; {
		k := strings.Index(text[i:], replayCallOpen)
		if k < 0 {
			return -1
		}
		at := i + k
		i = at + len(replayCallOpen)
		end := strings.IndexByte(text[i:], ')')
		if end < 0 {
			return -1
		}
		if arg := strings.TrimSpace(text[i : i+end]); arg == contractSymbol || strings.HasSuffix(arg, "."+contractSymbol) {
			return at
		}
	}
}

// disarmingForms reports the vitest forms in text that would stop the replay at
// offset at from running: any `.only` in the file, unless the mark is on one of
// the replay's own enclosing describes, and a skip or todo form on one of those
// describes.
func disarmingForms(file, text string, at int) []string {
	enclosing := enclosingDescribes(text, at)

	var out []string
	onlyMarksTheReplay := false
	for _, open := range enclosing {
		if strings.HasPrefix(open, "describe.only") {
			onlyMarksTheReplay = true
		}
		for _, form := range []string{"describe.skip", "describe.todo", "xdescribe("} {
			if strings.HasPrefix(open, form) {
				out = append(out, file+": the replay's enclosing "+form)
			}
		}
	}
	if !onlyMarksTheReplay {
		for _, form := range []string{"describe.only", "it.only", "test.only"} {
			if strings.Contains(text, form) {
				out = append(out, file+": "+form+" narrows the file to the marked tests, and the replay is not one of them")
			}
		}
	}
	return out
}

// enclosingDescribes returns the opening line of each describe block enclosing
// the offset, nearest first. Enclosure is read from INDENTATION — a describe
// indented less than the replay encloses it, one at the same depth is a sibling
// — which is what keeps a skipped sibling block in the same file from reading as
// a skipped replay.
func enclosingDescribes(text string, at int) []string {
	lines := strings.Split(text, "\n")
	target := strings.Count(text[:at], "\n")
	depth := indentWidth(lines[target])

	var out []string
	for i := target - 1; i >= 0; i-- {
		open := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(open, "describe") && !strings.HasPrefix(open, "xdescribe") {
			continue
		}
		if n := indentWidth(lines[i]); n < depth {
			depth = n
			out = append(out, open)
		}
	}
	return out
}

// indentWidth is the number of leading space or tab characters.
func indentWidth(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

// importsSymbolFrom reports whether text binds symbol from a module whose
// specifier is module or ends in "/"+module.
//
// It reads whole import STATEMENTS rather than lines, so the multi-line form
// web/src/lib/constants.ts already uses for the other Go-generated module
// satisfies it, and it accepts the namespace form (`import * as ns from …`
// together with an `ns.symbol` use), which web/src uses for the shadcn component
// modules. A line beginning with `//` is not a statement, so a mention of the
// symbol in prose cannot satisfy the guard; an import commented out with `/* */`
// can, but that file no longer compiles, so the disarm defeats itself.
func importsSymbolFrom(text, symbol, module string) bool {
	for _, stmt := range importStatements(text) {
		spec, ok := quotedSpecifier(stmt)
		if !ok || (spec != module && !strings.HasSuffix(spec, "/"+module)) {
			continue
		}
		if strings.Contains(stmt, symbol) {
			return true
		}
		if ns, ok := namespaceBinding(stmt); ok && strings.Contains(text, ns+"."+symbol) {
			return true
		}
	}
	return false
}

// importStatements returns every ES import statement in text, each joined onto
// one line: a statement runs from its `import ` line to the line completing its
// quoted specifier.
func importStatements(text string) []string {
	lines := strings.Split(text, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		stmt := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(stmt, "import ") {
			continue
		}
		for {
			if _, complete := quotedSpecifier(stmt); complete || i+1 >= len(lines) {
				break
			}
			i++
			stmt += " " + strings.TrimSpace(lines[i])
		}
		out = append(out, stmt)
	}
	return out
}

// quotedSpecifier returns the first quoted string in an import statement, which
// is its module specifier.
func quotedSpecifier(stmt string) (string, bool) {
	i := strings.IndexAny(stmt, `"'`)
	if i < 0 {
		return "", false
	}
	j := strings.IndexByte(stmt[i+1:], stmt[i])
	if j < 0 {
		return "", false
	}
	return stmt[i+1 : i+1+j], true
}

// namespaceBinding returns the local name a namespace import binds.
func namespaceBinding(stmt string) (string, bool) {
	k := strings.Index(stmt, "* as ")
	if k < 0 {
		return "", false
	}
	ns := strings.TrimSpace(stmt[k+len("* as "):])
	if end := strings.IndexAny(ns, " \t,;}"); end >= 0 {
		ns = ns[:end]
	}
	return ns, ns != ""
}

// moduleRoot walks up from the package directory to the directory holding
// go.mod, so the generated file's path does not depend on test nesting depth.
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
