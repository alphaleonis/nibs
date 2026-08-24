package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/testdata/fixtures"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The diagnostics around the assignment axes and the areas vocabulary name
// commands, and a named command is a factual claim about CLI behavior — the one
// kind of claim in this repo with no compiler and no test behind it. CLAUDE.md
// answers that with a rule ("execute every factual claim you write about CLI
// behavior before committing it"); this file answers it with a gate.
//
// It lifts every backtick-quoted `nibs …` command out of a diagnostic and runs
// it against a FRESH copy of the store that produced that diagnostic, asserting
// it succeeds. The guards this replaces asserted the right remedy was PRESENT
// without ever running the other one the same sentence named, which is how a
// message came to offer two commands of which one always exits 2.
//
// PLACEHOLDERS are where the rule needs a distinction, and it is POSITIONAL:
//
//   - A `<…>` in a positional slot stands for the SUBJECT — the nib the
//     diagnostic is about. Every surface here already holds that id, so leaving
//     it as a placeholder hands the reader a command that exits 3 ("nib not
//     found: <id>"). The gate fails on it.
//   - A `<…>` in a flag-VALUE slot stands for something only the author can
//     choose: which milestone to move a queue to, which declared area to use.
//     That is legitimate, and the surrounding convention (`nibs mv %s --parent
//     <id>`) already spells it that way. The gate substitutes a concrete value
//     from the surface's `choices` map and runs the command anyway, so the
//     grammar is still executed — a placeholder is not a way out of the gate.
//
// Positional-versus-flag-value is decided against the real Cobra tree rather
// than a hardcoded flag list, so a flag whose arity changes cannot silently
// reclassify an argument.

// diagnosticCodeSpans returns the text of every backtick-quoted span in s.
//
// Backticks are paired left to right. A file-sourced value containing a stray
// backtick would shift the pairing, which is why the fixtures below use values
// that hold none; the rendering boundary those messages go through neutralizes
// control characters, not backticks.
func diagnosticCodeSpans(s string) []string {
	parts := strings.Split(s, "`")
	var spans []string
	for i := 1; i < len(parts); i += 2 {
		spans = append(spans, parts[i])
	}
	return spans
}

// diagnosticNibsCommands returns every command a diagnostic prescribes, split
// into arguments with the leading "nibs" removed.
//
// A remedy is not always spelled out in full. These messages offer an
// alternative as a bare flag FRAGMENT continuing the command just named —
// "`nibs set X --clear milestone` or `--clear area`" is two commands, and the
// second is the one that has been going wrong. A fragment is therefore spliced
// onto the command before it: the flags it names replace their namesakes in
// that command, which is exactly how a reader completes the sentence.
//
// Each line is read on its own, so a fragment can never attach itself to a
// command from an unrelated finding further up a report.
func diagnosticNibsCommands(s string) [][]string {
	var cmds [][]string
	for _, line := range strings.Split(s, "\n") {
		var base []string
		for _, span := range diagnosticCodeSpans(line) {
			args := splitDiagnosticCommand(span)
			if len(args) == 0 {
				continue
			}
			if args[0] == "nibs" {
				if len(args) < 2 {
					continue
				}
				base = args[1:]
				cmds = append(cmds, base)
				continue
			}
			if strings.HasPrefix(args[0], "-") && base != nil {
				if spliced, err := spliceFlagFragment(base, args); err == nil {
					cmds = append(cmds, spliced)
				}
			}
		}
	}
	return cmds
}

// spliceFlagFragment completes a trailing flag fragment into the whole command
// the sentence means: the base command with the fragment's flags substituted
// for their namesakes.
func spliceFlagFragment(base, fragment []string) ([]string, error) {
	target, rest, err := resolveDiagnosticCommand(base)
	if err != nil {
		return nil, err
	}
	fragTokens, err := parseDiagnosticArgs(target, fragment)
	if err != nil {
		return nil, err
	}
	replaced := make(map[string]struct{}, len(fragTokens))
	for _, tok := range fragTokens {
		if tok.flag != nil {
			replaced[tok.flag.Name] = struct{}{}
		}
	}
	baseTokens, err := parseDiagnosticArgs(target, rest)
	if err != nil {
		return nil, err
	}
	spliced := append([]string(nil), base[:len(base)-len(rest)]...)
	drop := false
	for _, tok := range baseTokens {
		switch {
		case tok.flag != nil:
			// A replaced flag takes its value with it; a positional ends the
			// span the previous flag governs.
			_, drop = replaced[tok.flag.Name]
		case !tok.value:
			drop = false
		}
		if !drop {
			spliced = append(spliced, tok.text)
		}
	}
	return append(spliced, fragment...), nil
}

// splitDiagnosticCommand splits a command the way a shell would for the shapes
// a diagnostic prints: whitespace-separated, with double quotes grouping a
// value so an empty argument (`--area ""`) survives as one.
func splitDiagnosticCommand(s string) []string {
	var args []string
	var cur strings.Builder
	quoted, started := false, false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			started = true
		case !quoted && (r == ' ' || r == '\t'):
			if started {
				args = append(args, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if started {
		args = append(args, cur.String())
	}
	return args
}

// isPlaceholder reports whether an argument is an unfilled `<…>` slot.
func isPlaceholder(arg string) bool {
	return strings.HasPrefix(arg, "<") && strings.HasSuffix(arg, ">") && len(arg) > 2
}

// diagToken is one argument of an extracted command, classified against the
// command it belongs to: a flag, a flag's value, or a positional.
type diagToken struct {
	text  string
	flag  *pflag.Flag // non-nil when this token IS a flag
	value bool        // this token is the preceding flag's value
}

// resolveDiagnosticCommand finds the Cobra command an extracted command names,
// and returns the arguments left after its verb path.
//
// Anything it cannot resolve — an unknown verb, an unknown flag — is a fault in
// the diagnostic, not a reason to pass: the caller fails on the returned error.
func resolveDiagnosticCommand(args []string) (*cobra.Command, []string, error) {
	target, rest, err := rootCmd.Find(args)
	if err != nil {
		return nil, nil, fmt.Errorf("no such command: %v", err)
	}
	if target == rootCmd {
		return nil, nil, fmt.Errorf("%q names no nibs subcommand", strings.Join(args, " "))
	}
	// Populates the inherited half of target.Flags() so a persistent flag such
	// as --nibs-path resolves here too.
	_ = target.InheritedFlags()
	return target, rest, nil
}

// parseDiagnosticArgs classifies rest against target, resolving every flag's
// arity from the real flag set rather than a hardcoded list — so a flag that
// stops taking a value cannot silently reclassify the argument after it.
func parseDiagnosticArgs(target *cobra.Command, rest []string) ([]diagToken, error) {
	lookup := func(name string) *pflag.Flag {
		if f := target.Flags().Lookup(name); f != nil {
			return f
		}
		return rootCmd.PersistentFlags().Lookup(name)
	}
	lookupShort := func(short string) *pflag.Flag {
		if f := target.Flags().ShorthandLookup(short); f != nil {
			return f
		}
		return rootCmd.PersistentFlags().ShorthandLookup(short)
	}

	var tokens []diagToken
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		var f *pflag.Flag
		var name string
		var hasInline bool
		switch {
		case strings.HasPrefix(arg, "--"):
			name, _, hasInline = strings.Cut(strings.TrimPrefix(arg, "--"), "=")
			if f = lookup(name); f == nil {
				return nil, fmt.Errorf("no such flag: --%s", name)
			}
		case len(arg) > 1 && strings.HasPrefix(arg, "-"):
			name, _, hasInline = strings.Cut(strings.TrimPrefix(arg, "-"), "=")
			if f = lookupShort(name); f == nil {
				return nil, fmt.Errorf("no such flag: -%s", name)
			}
		default:
			tokens = append(tokens, diagToken{text: arg})
			continue
		}
		tokens = append(tokens, diagToken{text: arg, flag: f})
		// NoOptDefVal is non-empty exactly for the flags pflag lets stand
		// alone (bools); everything else consumes the next argument.
		if hasInline || f.NoOptDefVal != "" {
			continue
		}
		if i+1 >= len(rest) {
			return nil, fmt.Errorf("flag %q is given no value", f.Name)
		}
		i++
		tokens = append(tokens, diagToken{text: rest[i], value: true})
	}
	return tokens, nil
}

// fillDiagnosticPlaceholders returns args with every flag-value placeholder
// replaced by the value the surface declared for it. A positional placeholder
// is never filled — it is the defect this gate exists to catch — and an
// undeclared one is refused rather than skipped, so a new placeholder cannot
// quietly opt a command out of being run.
func fillDiagnosticPlaceholders(args []string, choices map[string]string) ([]string, error) {
	target, rest, err := resolveDiagnosticCommand(args)
	if err != nil {
		return nil, err
	}
	tokens, err := parseDiagnosticArgs(target, rest)
	if err != nil {
		return nil, err
	}
	filled := append([]string(nil), args[:len(args)-len(rest)]...)
	for _, tok := range tokens {
		text := tok.text
		if isPlaceholder(text) {
			switch {
			case !tok.value:
				return nil, fmt.Errorf("the subject is left as the placeholder %s; this surface holds the id and must interpolate it, or the command exits 3 when run verbatim", text)
			default:
				choice, ok := choices[text]
				if !ok {
					return nil, fmt.Errorf("the placeholder %s has no declared substitute; classify it deliberately (add it to the surface's choices) rather than leaving it unrun", text)
				}
				text = choice
			}
		}
		filled = append(filled, text)
	}
	return filled, nil
}

// resetCommandTreeFlags returns every flag on every command to its default so
// the gate can run an arbitrary extracted command against the rootCmd
// singleton without inheriting the previous one's state. The per-command
// reset*Flags helpers cannot serve here: the gate does not know in advance
// which verb a diagnostic will name.
func resetCommandTreeFlags(cmd *cobra.Command) {
	reset := func(fs *pflag.FlagSet) {
		fs.VisitAll(func(f *pflag.Flag) {
			// A repeatable flag appends across Execute calls on the singleton,
			// so its backing slice has to be emptied rather than re-Set.
			if sv, ok := f.Value.(pflag.SliceValue); ok {
				_ = sv.Replace(nil)
			} else {
				_ = f.Value.Set(f.DefValue)
			}
			f.Changed = false
		})
	}
	reset(cmd.Flags())
	reset(cmd.PersistentFlags())
	for _, sub := range cmd.Commands() {
		resetCommandTreeFlags(sub)
	}
}

// runDiagnosticCommand executes one extracted command against nibsDir.
//
// `nibs check` is driven through runCheck rather than the Cobra pipeline: its
// RunE ends the PROCESS with exit 1 whenever the report is non-empty, which
// would take the test binary down with it — and a non-empty report is the
// command working, not failing. Every fixture here is deliberately non-empty.
func runDiagnosticCommand(t *testing.T, nibsDir string, args []string) error {
	t.Helper()
	if args[0] == "check" {
		t.Cleanup(resetCheckFlags)
		resetCheckFlags()
		core := remedyCore(t, nibsDir, &bytes.Buffer{})
		var err error
		captureStdout(t, func() { _, err = runCheck(&App{Core: core}) })
		return err
	}
	resetCommandTreeFlags(rootCmd)
	defer func() {
		resetCommandTreeFlags(rootCmd)
		rootCmd.SetArgs(nil)
	}()
	full := append([]string{"--nibs-path", nibsDir}, args...)
	_, err := runRootWith(t, full...)
	return err
}

// diagnosticLinesAbout narrows a multi-finding report to the lines that speak
// about the fixture's own subjects, so the sample project's unrelated
// pre-existing findings are neither judged here nor counted. Every subject must
// appear, or the row is proving nothing.
func diagnosticLinesAbout(t *testing.T, diagnostic string, subjects []string) string {
	t.Helper()
	if len(subjects) == 0 {
		return diagnostic
	}
	var kept []string
	for _, line := range strings.Split(diagnostic, "\n") {
		for _, subject := range subjects {
			if strings.Contains(line, subject) {
				kept = append(kept, line)
				break
			}
		}
	}
	joined := strings.Join(kept, "\n")
	for _, subject := range subjects {
		if !strings.Contains(joined, subject) {
			t.Fatalf("the surface says nothing about %s, so this row proves nothing; got:\n%s", subject, diagnostic)
		}
	}
	return joined
}

// remedySurface is one diagnostic this feature prints, plus the fixture that
// produces it.
type remedySurface struct {
	name string
	// store builds a fresh scratch store and returns its .nibs path. It is
	// called once to produce the diagnostic and again per extracted command, so
	// one named remedy cannot invalidate the state the next one is judged in —
	// these messages name ALTERNATIVES, not a sequence.
	store func(t *testing.T) string
	// diagnose runs the surface and returns everything it printed.
	diagnose func(t *testing.T, nibsDir string) string
	// subjects narrows a whole-store report to the lines about this fixture's
	// own nibs; empty means the surface prints one message and all of it counts.
	subjects []string
	// mustName is text the diagnostic has to carry, so a surface that silently
	// stops producing its finding fails instead of vacuously passing.
	mustName []string
	// choices fills the placeholders that name the author's own decision.
	choices map[string]string
	// wantCommands is how many `nibs …` commands the diagnostic must name. A
	// surface whose commands disappear would otherwise pass with nothing run.
	wantCommands int
}

// TestDiagnosticRemediesExecute is the gate: every command these surfaces
// print is run against the state that printed it.
func TestDiagnosticRemediesExecute(t *testing.T) {
	for _, tc := range remedySurfaces() {
		t.Run(tc.name, func(t *testing.T) {
			diagnostic := diagnosticLinesAbout(t, tc.diagnose(t, tc.store(t)), tc.subjects)
			for _, want := range tc.mustName {
				if !strings.Contains(diagnostic, want) {
					t.Fatalf("the surface no longer names %q, so this row proves nothing; got:\n%s", want, diagnostic)
				}
			}
			cmds := diagnosticNibsCommands(diagnostic)
			if len(cmds) != tc.wantCommands {
				// Reported rather than fatal, so a surface that changed how
				// many commands it names still has every one of them run.
				t.Errorf("diagnostic names %d `nibs …` commands, want %d:\n%s", len(cmds), tc.wantCommands, diagnostic)
			}
			for _, args := range cmds {
				filled, err := fillDiagnosticPlaceholders(args, tc.choices)
				if err != nil {
					t.Errorf("`nibs %s`: %v\nin: %s", strings.Join(args, " "), err, diagnostic)
					continue
				}
				if err := runDiagnosticCommand(t, tc.store(t), filled); err != nil {
					t.Errorf("`nibs %s` is prescribed by this diagnostic but fails when run: %v\nin: %s",
						strings.Join(filled, " "), err, diagnostic)
				}
			}
		})
	}
}

// --- fixtures -------------------------------------------------------------

// remedyStore copies the sample project — whose config declares auth, api,
// api/webhooks, web, web/dashboard and infra — and adds the hand-authored files
// a surface needs. The copy is per call, which is what lets each remedy be
// judged against the untouched state.
func remedyStore(files map[string]string) func(t *testing.T) string {
	return func(t *testing.T) string {
		t.Helper()
		nibsDir := filepath.Join(fixtures.CopySampleProject(t), ".nibs")
		for name, content := range files {
			if err := os.WriteFile(dataPath(nibsDir, name), []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
		}
		return nibsDir
	}
}

// remedyStoreWithoutAreas is the same store with the `areas:` block removed, so
// the vocabulary declares nothing while a nib still carries an area.
func remedyStoreWithoutAreas(files map[string]string) func(t *testing.T) string {
	base := remedyStore(files)
	return func(t *testing.T) string {
		t.Helper()
		nibsDir := base(t)
		cfgPath := filepath.Join(nibsDir, "config.yml")
		raw, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		var kept []string
		inAreas := false
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "areas:") {
				inAreas = true
				continue
			}
			if inAreas {
				if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
					continue
				}
				inAreas = false
			}
			kept = append(kept, line)
		}
		if err := os.WriteFile(cfgPath, []byte(strings.Join(kept, "\n")), 0644); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.LoadFromStore(nibsDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Areas) != 0 {
			t.Fatalf("the areas block survived the edit: %v", cfg.AreaPaths())
		}
		return nibsDir
	}
}

const (
	remedyMilestoneWithArea = `---
version: 2
title: Located waypoint
status: todo
type: milestone
area: web/dashboard
---

Body.
`

	remedyMilestoneWithMilestone = `---
version: 2
title: Assigned waypoint
status: todo
type: milestone
milestone: tnib-m001
---

Body.
`

	remedyMilestoneWithBothAxes = `---
version: 2
title: Doubly assigned waypoint
status: todo
type: milestone
milestone: tnib-m001
area: web/dashboard
---

Body.
`

	remedyStrandedTask = `---
version: 2
title: Stranded work
status: todo
type: task
priority: normal
area: retired/team
---

Body.
`

	remedyQueueMilestone = `---
version: 2
title: Wave one
status: in-progress
type: milestone
---

Body.
`

	remedyQueueStrandedMember = `---
version: 2
title: Stranded member
status: todo
type: task
priority: normal
milestone: rmd-ms01
milestone_order: a
area: retired/team
---

Body.
`

	remedyQueueEnumMember = `---
version: 2
title: Enum-broken member
status: todo
type: task
priority: superhigh
milestone: rmd-ms01
milestone_order: b
---

Body.
`

	// An illegal nest, whose remedy this delta did not touch: the positive
	// control that shows the gate accepts a correctly written remedy rather
	// than merely failing on everything.
	remedyIllegalNestParent = `---
version: 2
title: Leaf task
status: todo
type: task
priority: normal
---

Body.
`

	remedyIllegalNestChild = `---
version: 2
title: Nested feature
status: todo
type: feature
priority: normal
parent: rmd-tk01
---

Body.
`
)

func remedyAxisFiles() map[string]string {
	return map[string]string{
		"rmd-ax01--located.md":  remedyMilestoneWithArea,
		"rmd-ax02--assigned.md": remedyMilestoneWithMilestone,
		"rmd-ax03--both.md":     remedyMilestoneWithBothAxes,
	}
}

func remedyQueueFiles() map[string]string {
	return map[string]string{
		"rmd-ms01--wave.md":   remedyQueueMilestone,
		"rmd-mb01--member.md": remedyQueueStrandedMember,
	}
}

// remedyQueueEnumFiles is the same wave whose one blocked member is refused for
// an out-of-enum value instead of its area — the cause `nibs check` reports
// whether or not the store declares an areas vocabulary.
func remedyQueueEnumFiles() map[string]string {
	return map[string]string{
		"rmd-ms01--wave.md":   remedyQueueMilestone,
		"rmd-eb01--member.md": remedyQueueEnumMember,
	}
}

// remedyQueueMixedFiles blocks the close on one member of each cause, which is
// the only shape where a store's vocabulary answers for one blocked member and
// not the other.
func remedyQueueMixedFiles() map[string]string {
	files := remedyQueueFiles()
	files["rmd-eb01--member.md"] = remedyQueueEnumMember
	return files
}

func remedySurfaces() []remedySurface {
	axisFiles := remedyAxisFiles()
	strandedFiles := map[string]string{"rmd-st01--stranded.md": remedyStrandedTask}
	queueFiles := remedyQueueFiles()
	nestFiles := map[string]string{
		"rmd-tk01--leaf.md": remedyIllegalNestParent,
		"rmd-ft01--nest.md": remedyIllegalNestChild,
	}
	declared := map[string]string{"<declared>": "web/dashboard"}
	axisSubjects := []string{"rmd-ax01", "rmd-ax02", "rmd-ax03"}

	return []remedySurface{
		{
			name:  "check names the axis offenders",
			store: remedyStore(axisFiles),
			diagnose: func(t *testing.T, nibsDir string) string {
				return remedyCheckOutput(t, nibsDir, false)
			},
			subjects: axisSubjects,
			mustName: []string{"cannot have an area", "cannot be assigned to a milestone"},
			// One per offender: the axis rule reports one nib at a time, so a
			// finding that named two commands would be naming one that does not
			// apply to the nib it is about.
			wantCommands: 3,
		},
		{
			name:  "check --fix names the axis offenders",
			store: remedyStore(axisFiles),
			diagnose: func(t *testing.T, nibsDir string) string {
				return remedyCheckOutput(t, nibsDir, true)
			},
			subjects:     axisSubjects,
			mustName:     []string{"Cannot auto-fix"},
			wantCommands: 3,
		},
		{
			name:         "the load warning names the axis offenders",
			store:        remedyStore(axisFiles),
			diagnose:     remedyLoadWarnings,
			subjects:     axisSubjects,
			mustName:     []string{"loads as written"},
			wantCommands: 6, // one clear per offender, plus `nibs check` on each line
		},
		{
			name:         "a stored area the vocabulary retired refuses the write",
			store:        remedyStore(strandedFiles),
			diagnose:     remedyStrandedWriteRefusal,
			mustName:     []string{`invalid area "retired/team"`, "must be one of"},
			choices:      declared,
			wantCommands: 2,
		},
		{
			name:     "a stored area in a store that declares none refuses the write",
			store:    remedyStoreWithoutAreas(strandedFiles),
			diagnose: remedyStrandedWriteRefusal,
			mustName: []string{`invalid area "retired/team"`, "declares no areas"},
			// Only the clear: this branch diagnoses a store with no declared
			// value to put in an --area, so naming one would prescribe a
			// command with no satisfiable argument.
			wantCommands: 1,
		},
		{
			name:     "close refuses a queue whose member carries a retired area",
			store:    remedyStore(queueFiles),
			diagnose: remedyCloseRefusal,
			mustName: []string{"rmd-mb01", "No escape and no retry"},
			choices:  declared,
			// The two escapes the member's own refusal names, plus the pointer
			// at `nibs check` — which is a claim about another tool as well as
			// a command, so TestCloseRefusalNamesNoSilentDiagnostic checks that
			// the report it names actually answers.
			wantCommands: 3,
		},
		{
			name:  "check names an undeclared area",
			store: remedyStore(strandedFiles),
			diagnose: func(t *testing.T, nibsDir string) string {
				return remedyCheckOutput(t, nibsDir, false)
			},
			subjects:     []string{"rmd-st01"},
			mustName:     []string{`area "retired/team" is not declared`},
			choices:      declared,
			wantCommands: 2,
		},
		{
			name:  "check --fix names an undeclared area",
			store: remedyStore(strandedFiles),
			diagnose: func(t *testing.T, nibsDir string) string {
				return remedyCheckOutput(t, nibsDir, true)
			},
			subjects:     []string{"rmd-st01"},
			mustName:     []string{"Cannot auto-fix"},
			choices:      declared,
			wantCommands: 2,
		},
		{
			name:     "the area filter refuses a path the vocabulary does not declare",
			store:    remedyStore(nil),
			diagnose: remedyAreaFilterRefusal,
			mustName: []string{`"nosuch" is not a declared area`, "must be one of"},
			// The declared set IS the repair, and it is already in the message,
			// so this refusal names no command. The row is here to enroll the
			// surface: one added later is executed rather than trusted.
			wantCommands: 0,
		},
		{
			name:     "the area filter in a store declaring no areas says that instead",
			store:    remedyStoreWithoutAreas(nil),
			diagnose: remedyAreaFilterRefusal,
			mustName: []string{"declares no areas", "config.yml"},
			// No command either, and here for a stronger reason than above: the
			// repair is a config edit, so any `nibs …` this named would be one
			// with no satisfiable argument in the very state it reports.
			wantCommands: 0,
		},
		{
			name:  "check names an illegal nest (unchanged sibling, the control)",
			store: remedyStore(nestFiles),
			diagnose: func(t *testing.T, nibsDir string) string {
				return remedyCheckOutput(t, nibsDir, false)
			},
			subjects:     []string{"rmd-ft01"},
			mustName:     []string{"is parented under"},
			choices:      map[string]string{"<id>": "tnib-e001"},
			wantCommands: 1,
		},
	}
}

// --- diagnostic producers --------------------------------------------------

// remedyCore loads a store the way a command does, with the warn writer routed
// to buf so the load-time diagnostics can be read back.
func remedyCore(t *testing.T, nibsDir string, buf *bytes.Buffer) *nibcore.Core {
	t.Helper()
	cfg, err := config.LoadFromStore(nibsDir)
	if err != nil {
		t.Fatal(err)
	}
	core := nibcore.New(nibsDir, cfg)
	core.SetWarnWriter(buf)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return core
}

// remedyCheckOutput runs the report and returns what it printed. runCheck is
// driven directly because checkCmd's RunE exits the process on a non-empty
// report, and every fixture here is deliberately non-empty.
func remedyCheckOutput(t *testing.T, nibsDir string, fix bool) string {
	t.Helper()
	t.Cleanup(resetCheckFlags)
	resetCheckFlags()
	checkFix = fix
	core := remedyCore(t, nibsDir, &bytes.Buffer{})
	var runErr error
	out := captureStdout(t, func() { _, runErr = runCheck(&App{Core: core}) })
	if runErr != nil {
		t.Fatalf("runCheck error = %v", runErr)
	}
	return out
}

// remedyLoadWarnings returns the stderr diagnostics loadFromDisk emits, which
// every command against the store prints before doing anything else.
func remedyLoadWarnings(t *testing.T, nibsDir string) string {
	t.Helper()
	var buf bytes.Buffer
	remedyCore(t, nibsDir, &buf)
	return buf.String()
}

// remedyStrandedWriteRefusal returns the refusal an ordinary write meets on a
// nib whose stored `area:` the vocabulary no longer declares.
func remedyStrandedWriteRefusal(t *testing.T, nibsDir string) string {
	t.Helper()
	resetCommandTreeFlags(rootCmd)
	t.Cleanup(func() {
		resetCommandTreeFlags(rootCmd)
		rootCmd.SetArgs(nil)
	})
	_, err := runRootWith(t, "--nibs-path", nibsDir, "set", "rmd-st01", "--title", "Renamed")
	if err == nil {
		t.Fatal("a write to a nib carrying an undeclared area must be refused, got nil")
	}
	return err.Error()
}

// remedyAreaFilterRefusal returns the refusal `nibs list --area` raises for a
// path the store's vocabulary does not declare. It is the READ side of the same
// vocabulary the write refusals above speak for, and the one refusal here that a
// caller meets without having written anything.
func remedyAreaFilterRefusal(t *testing.T, nibsDir string) string {
	t.Helper()
	resetCommandTreeFlags(rootCmd)
	t.Cleanup(func() {
		resetCommandTreeFlags(rootCmd)
		rootCmd.SetArgs(nil)
	})
	_, err := runRootWith(t, "--nibs-path", nibsDir, "list", "--area", "nosuch")
	if err == nil {
		t.Fatal("an undeclared area must be refused rather than listed as empty, got nil")
	}
	return err.Error()
}

// remedyCloseRefusal returns the refusal `nibs close` raises when a queue
// member's own front matter is what the write path will not accept.
func remedyCloseRefusal(t *testing.T, nibsDir string) string {
	t.Helper()
	resetCommandTreeFlags(rootCmd)
	t.Cleanup(func() {
		resetCommandTreeFlags(rootCmd)
		rootCmd.SetArgs(nil)
	})
	// close takes its summary from a channel, never inline.
	summaryPath := filepath.Join(t.TempDir(), "summary.md")
	if err := os.WriteFile(summaryPath, []byte("Wave closed.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := runRootWith(t, "--nibs-path", nibsDir, "close", "rmd-ms01",
		"--as", "completed", "--unassign-open", "--summary", "@"+summaryPath)
	if err == nil {
		t.Fatal("closing a queue whose member cannot be written must be refused, got nil")
	}
	return err.Error()
}

// --- the gate's own logic --------------------------------------------------

// TestDiagnosticCommandExtraction pins the extractor and the placeholder rule
// the gate rests on, so a change that quietly stops finding commands (and
// therefore stops running any) fails here rather than passing everywhere.
func TestDiagnosticCommandExtraction(t *testing.T) {
	tests := []struct {
		name       string
		diagnostic string
		choices    map[string]string
		wantFilled [][]string
		wantErr    string
	}{
		{
			name:       "no code span names no command",
			diagnostic: "loads as written, but the nib is refused",
		},
		{
			name:       "a code span that is not a nibs command is ignored",
			diagnostic: "the key is `area:` in front matter",
		},
		{
			name:       "an interpolated subject runs as written",
			diagnostic: "clear it with `nibs set tnib-x001 --clear area`",
			wantFilled: [][]string{{"set", "tnib-x001", "--clear", "area"}},
		},
		{
			name:       "two commands in one sentence are both extracted",
			diagnostic: "`nibs set tnib-x001 --clear milestone` or `nibs set tnib-x001 --clear area`",
			wantFilled: [][]string{
				{"set", "tnib-x001", "--clear", "milestone"},
				{"set", "tnib-x001", "--clear", "area"},
			},
		},
		{
			name:       "a flag fragment completes the command before it",
			diagnostic: "clear it with `nibs set tnib-x001 --clear milestone` or `--clear area`",
			wantFilled: [][]string{
				{"set", "tnib-x001", "--clear", "milestone"},
				{"set", "tnib-x001", "--clear", "area"},
			},
		},
		{
			name:       "a fragment attaches only within its own line",
			diagnostic: "`nibs set tnib-x001 --clear milestone`\nsomething else `--clear area`",
			wantFilled: [][]string{{"set", "tnib-x001", "--clear", "milestone"}},
		},
		{
			name:       "a flag-value placeholder is the author's choice and is filled",
			diagnostic: "point it at one with `nibs set tnib-x001 --milestone <id>`",
			choices:    map[string]string{"<id>": "tnib-m001"},
			wantFilled: [][]string{{"set", "tnib-x001", "--milestone", "tnib-m001"}},
		},
		{
			name:       "a bool flag does not swallow the argument after it",
			diagnostic: "run `nibs check --fix`",
			wantFilled: [][]string{{"check", "--fix"}},
		},
		{
			name:       "a subject placeholder is refused",
			diagnostic: "clear it with `nibs set <id> --clear area`",
			wantErr:    "the subject is left as the placeholder <id>",
		},
		{
			name:       "an undeclared flag-value placeholder is refused rather than skipped",
			diagnostic: "assign one with `nibs set tnib-x001 --area <declared>`",
			wantErr:    "has no declared substitute",
		},
		{
			name:       "an unknown flag is refused",
			diagnostic: "try `nibs set tnib-x001 --clearr area`",
			wantErr:    "no such flag: --clearr",
		},
		{
			name:       "an unknown verb is refused",
			diagnostic: "try `nibs unassign tnib-x001`",
			wantErr:    "no such command",
		},
		{
			name:       "a span naming no verb at all is refused",
			diagnostic: "point it elsewhere with `nibs --nibs-path <dir>`",
			wantErr:    "names no nibs subcommand",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmds := diagnosticNibsCommands(tt.diagnostic)
			if tt.wantErr != "" {
				if len(cmds) != 1 {
					t.Fatalf("extracted %d commands, want 1", len(cmds))
				}
				_, err := fillDiagnosticPlaceholders(cmds[0], tt.choices)
				if err == nil {
					t.Fatalf("fillDiagnosticPlaceholders() = nil error, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if len(cmds) != len(tt.wantFilled) {
				t.Fatalf("extracted %d commands, want %d: %v", len(cmds), len(tt.wantFilled), cmds)
			}
			for i, args := range cmds {
				filled, err := fillDiagnosticPlaceholders(args, tt.choices)
				if err != nil {
					t.Fatalf("fillDiagnosticPlaceholders(%v) error = %v", args, err)
				}
				if strings.Join(filled, " ") != strings.Join(tt.wantFilled[i], " ") {
					t.Errorf("command %d = %v, want %v", i, filled, tt.wantFilled[i])
				}
			}
		})
	}
}

// --- claims the gate cannot execute ---------------------------------------

// TestCloseRefusalNamesNoSilentDiagnostic covers the half of a diagnostic the
// gate above cannot run: a claim about what ANOTHER tool will say.
//
// `nibs close`'s own-front-matter refusal names `nibs check` as the surface
// that lists every offending member, and the reasons it prints itself are
// capped. For an enum or axis cause check has always answered; for an
// undeclared `area:` it answers only where a vocabulary is declared, since a
// store declaring none is exempt by design. So the answer depends on each
// blocked member's CAUSE and not on the store alone, and the table therefore
// varies both: the vocabulary, and which guard the member trips.
//
// The implication is asserted in both directions rather than the presence or
// absence of a sentence: name `nibs check` if and only if `nibs check` names
// every member this refusal blocked. Each row states its blocked set, and the
// row first checks the refusal really named it — a fixture that stopped
// blocking would otherwise assert nothing while still passing.
func TestCloseRefusalNamesNoSilentDiagnostic(t *testing.T) {
	tests := []struct {
		name    string
		store   func(t *testing.T) string
		blocked []string
	}{
		{
			name:    "a declared vocabulary, an undeclared area",
			store:   remedyStore(remedyQueueFiles()),
			blocked: []string{"rmd-mb01"},
		},
		{
			name:    "no vocabulary at all, an undeclared area",
			store:   remedyStoreWithoutAreas(remedyQueueFiles()),
			blocked: []string{"rmd-mb01"},
		},
		{
			name:    "no vocabulary at all, an out-of-enum value",
			store:   remedyStoreWithoutAreas(remedyQueueEnumFiles()),
			blocked: []string{"rmd-eb01"},
		},
		{
			name:    "no vocabulary at all, one blocked member of each cause",
			store:   remedyStoreWithoutAreas(remedyQueueMixedFiles()),
			blocked: []string{"rmd-eb01", "rmd-mb01"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refusal := remedyCloseRefusal(t, tt.store(t))
			report := remedyCheckOutput(t, tt.store(t), false)
			for _, id := range tt.blocked {
				if !strings.Contains(refusal, id) {
					t.Fatalf("the row claims %s blocks the close, but the refusal does not name it:\n%s", id, refusal)
				}
			}
			var silent []string
			for _, id := range tt.blocked {
				if !strings.Contains(report, id) {
					silent = append(silent, id)
				}
			}
			names := strings.Contains(refusal, "nibs check")
			answers := len(silent) == 0
			if names && !answers {
				t.Errorf("the refusal points at `nibs check`, which reports nothing for %s:\nrefusal: %s\nreport:\n%s", strings.Join(silent, ", "), refusal, report)
			}
			if answers && !names {
				t.Errorf("`nibs check` names every blocked member and the refusal withholds the pointer:\nrefusal: %s\nreport:\n%s", refusal, report)
			}
		})
	}
}

// TestAreaDiagnosticClaimMatchesWhatExecutes pins the claim beside the command,
// for the area finding.
//
// The gate above runs the two remedies the finding names and would pass just as
// well if ordinary edits went through — so the claim that every update KEEPING
// the value is refused is the half it cannot reach. Both shapes are run here:
// an edit that touches something else, and the retype the axis findings offer
// as an alternative, which is precisely why this finding does not offer it.
func TestAreaDiagnosticClaimMatchesWhatExecutes(t *testing.T) {
	store := remedyStore(map[string]string{"rmd-st01--stranded.md": remedyStrandedTask})

	if err := runDiagnosticCommand(t, store(t), []string{"set", "rmd-st01", "--title", "Renamed"}); err == nil {
		t.Error("an update that keeps the undeclared value must be refused, got nil")
	}
	if err := runDiagnosticCommand(t, store(t), []string{"set", "rmd-st01", "-t", "bug"}); err == nil {
		t.Error("a retype that keeps the undeclared value must be refused, got nil")
	}

	report := diagnosticLinesAbout(t, remedyCheckOutput(t, store(t), false), []string{"rmd-st01"})
	if !strings.Contains(report, "every update that keeps the value is refused") {
		t.Errorf("the report should state the claim the runs above confirm, got:\n%s", report)
	}
}

// TestAxisDiagnosticClaimMatchesWhatExecutes pins the claim beside the command.
//
// The axis diagnostics used to say every OTHER update of an offender is
// refused, and justified withholding the retype on the same ground. Both are
// false in a state nothing had checked: a milestone whose queue is empty and
// whose `area:` is declared takes `-t task` and exits 0 — the retype is then the
// only escape that keeps the assignment, which is why `--fix` names it as the
// author's choice. What holds in EVERY state is narrower, and is what the
// message now says.
func TestAxisDiagnosticClaimMatchesWhatExecutes(t *testing.T) {
	files := map[string]string{"rmd-rt01--retypable.md": remedyMilestoneWithArea}
	store := remedyStore(files)

	// The state the old claim did not cover: empty queue, declared area.
	if err := runDiagnosticCommand(t, store(t), []string{"set", "rmd-rt01", "-t", "task"}); err != nil {
		t.Fatalf("the retype must succeed on an empty queue with a declared area: %v", err)
	}

	report := diagnosticLinesAbout(t, remedyCheckOutput(t, store(t), false), []string{"rmd-rt01"})
	if strings.Contains(report, "every other update of this nib is refused") {
		t.Errorf("the report claims every other update is refused, but the retype above exited 0:\n%s", report)
	}
	if !strings.Contains(report, "every update that keeps the type and") {
		t.Errorf("the report should state the claim that holds in every state, got:\n%s", report)
	}
}
