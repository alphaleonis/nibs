package cmd

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/alphaleonis/nibs/internal/config"
)

// documentedFlagShape matches a flag — or an alternation of flags sharing one
// operand, the shape the grammar lines use — followed by a `<placeholder>`.
//
// The alternation is the whole point: `--after|--before|--first <anchor>` reads as
// "pick one of these three, then supply the anchor", so EVERY member of it claims to
// take a value, not just the last one. That is precisely how a value-less flag hid in
// one of these lines.
var documentedFlagShape = regexp.MustCompile(`(--[a-z][a-z0-9-]*(?:\|--[a-z][a-z0-9-]*)*)[ \t]+<[^>\n]+>`)

// flagValueTypes maps every flag name registered anywhere in the command tree to the
// set of value types it is registered with. A name can legitimately appear on several
// commands, so the set is what decides: a flag that is a bool EVERYWHERE can never
// accept an operand, whatever surface says otherwise.
func flagValueTypes(t *testing.T) map[string]map[string]bool {
	t.Helper()
	types := map[string]map[string]bool{}
	record := func(f *pflag.Flag) {
		if types[f.Name] == nil {
			types[f.Name] = map[string]bool{}
		}
		types[f.Name][f.Value.Type()] = true
	}

	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.Flags().VisitAll(record)
		c.PersistentFlags().VisitAll(record)
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)

	// Tripwire: an empty map would make every assertion below pass vacuously.
	if len(types) == 0 {
		t.Fatal("no flags found on the command tree; the shape check would be vacuous")
	}
	return types
}

// docSurface is one body of agent-facing text and a name to report it by.
type docSurface struct {
	name string
	text string
}

// agentFacingDocSurfaces collects the text an agent is pointed at to learn the
// grammar. CLAUDE.md sends agents to `nibs cheat` FIRST precisely so they do not
// guess, and the project rule is to STOP on any nibs error — so a grammar here that
// cannot be run does not merely mislead, it halts the run that trusted it.
func agentFacingDocSurfaces(t *testing.T) []docSurface {
	t.Helper()
	surfaces := []docSurface{
		{"nibs cheat", cheatSheet(config.Default())},
		{"nibs prime", renderedSlimPrompt(t)},
		{"nibs prime --full", renderedFullPrompt(t)},
	}
	for _, topic := range []string{"filters", "recipes", "areas"} {
		out, err := execCatalog(t, topic)
		if err != nil {
			t.Fatalf("catalog %s: %v", topic, err)
		}
		surfaces = append(surfaces, docSurface{"nibs catalog " + topic, out})
	}
	for _, c := range rootCmd.Commands() {
		if text := c.Long + "\n" + c.Short; strings.TrimSpace(text) != "" {
			surfaces = append(surfaces, docSurface{"nibs " + c.Name() + " --help", text})
		}
	}
	return surfaces
}

// TestDocumentedFlagShapesTakeValues pins that a flag documented with an operand can
// actually accept one.
//
// Writing `--first <anchor>` when `--first` is a bool documents a command that cannot
// be run. It shipped once: `nibs mv <id> --queue --after|--before|--first <anchor>`
// was written directly beneath a sibling line carrying the same shape, and `--first`
// takes no anchor. The stray anchor is parsed as a second positional id, so the
// failure names an argument COUNT and points nowhere near the real mistake.
//
// Whether a flag takes a value is not something this test has to infer — pflag records
// it, and the type is read the same way cmd/close_test.go reads it. So this is a
// mechanical check on a class of claim that otherwise has nothing behind it: prose is
// the only artifact here with no compiler and no test.
func TestDocumentedFlagShapesTakeValues(t *testing.T) {
	types := flagValueTypes(t)

	for _, surface := range agentFacingDocSurfaces(t) {
		for _, m := range documentedFlagShape.FindAllStringSubmatch(surface.text, -1) {
			for _, spelled := range strings.Split(m[1], "|") {
				name := strings.TrimPrefix(spelled, "--")
				seen, known := types[name]
				if !known {
					// Not a registered flag anywhere. That may be a real defect (a
					// documented flag that does not exist) but it is a different
					// claim with different false positives, so it is not judged here.
					continue
				}
				if seen["bool"] && len(seen) == 1 {
					t.Errorf("%s documents %q as taking a value (%q), but --%s is a bool everywhere it is registered and takes no operand",
						surface.name, spelled, m[0], name)
				}
			}
		}
	}
}

// TestDocumentedFlagShapeCheckMatchesRealGrammars is the tripwire for the regex: if it
// stops matching the shapes these surfaces actually use, the test above passes while
// checking nothing. It asserts the scan finds the known-good grammars rather than
// trusting that zero failures means zero coverage.
func TestDocumentedFlagShapeCheckMatchesRealGrammars(t *testing.T) {
	var found []string
	for _, surface := range agentFacingDocSurfaces(t) {
		for _, m := range documentedFlagShape.FindAllStringSubmatch(surface.text, -1) {
			for _, spelled := range strings.Split(m[1], "|") {
				found = append(found, strings.TrimPrefix(spelled, "--"))
			}
		}
	}
	sort.Strings(found)

	// Value-taking flags that the grammar lines demonstrably render with an operand.
	for _, want := range []string{"after", "before", "parent"} {
		if !slicesContains(found, want) {
			t.Errorf("the shape scan never matched --%s with an operand; the regex no longer "+
				"matches the grammars these surfaces use, so the check is vacuous (matched: %v)", want, found)
		}
	}
}

func slicesContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
