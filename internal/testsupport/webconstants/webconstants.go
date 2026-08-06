// Package webconstants reads the web UI's hardcoded vocabulary out of
// web/src/lib/constants.ts so Go-side guards can pin it against the Go
// definitions.
//
// The web restates the vocabulary because GraphQL does not serve it, and more
// than one Go package has to check that restatement: internal/config pins the
// status names, cmd pins the query language's status groups layered on top of
// them. Scraping TypeScript with regexes is fragile enough that two independent
// copies of the scraper would drift — hardening one guard's parsing would
// silently leave the other accepting what it used to. The primitives live here
// so there is one copy to harden.
//
// Deliberately free of any dependency on testing: callers own how a failure is
// reported, which lets each guard phrase its own domain-specific message.
package webconstants

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Path is the module-root-relative path of the file every guard reads. Failure
// messages quote it, so they name a path the reader can open from the repo root
// no matter which package's test printed it.
const Path = "web/src/lib/constants.ts"

// Source returns the contents of the web constants file.
//
// A missing file is an error, never a skip: the guards exist because nobody
// notices this duplication drifting, and a guard that quietly stops guarding is
// the failure it is meant to catch.
func Source() (string, error) {
	root, err := moduleRoot()
	if err != nil {
		return "", err
	}
	full := filepath.Join(root, filepath.FromSlash(Path))
	b, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", Path, err)
	}
	return string(b), nil
}

// moduleRoot walks up from the working directory (the package directory under
// `go test`) to the directory holding go.mod, so the path to the web sources
// does not depend on how deeply the calling package is nested.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod in any parent of the working directory")
		}
		dir = parent
	}
}

// ParseStringArray extracts the string literals from
// `export const <name> = [...]` in the TypeScript source.
//
// An empty result is an error rather than an empty slice: a guard comparing
// against nothing passes against anything.
func ParseStringArray(src, name string) ([]string, error) {
	decl := regexp.MustCompile(`export const ` + regexp.QuoteMeta(name) + `\s*(?::[^=]*)?=\s*\[([^\]]*)\]`)
	m := decl.FindStringSubmatch(src)
	if m == nil {
		return nil, fmt.Errorf("no `export const %s = [...]` array literal in %s — if it was renamed or made derived, the guard needs updating, not deleting",
			name, Path)
	}
	items := regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(m[1], -1)
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it[1])
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s in %s parsed as empty; the guard would pass against anything", name, Path)
	}
	return out, nil
}

// Diff compares two vocabularies as SETS, returning the names in want that got
// lacks and the names in got that want lacks.
//
// Order is deliberately not compared. The web and Go orders differ on purpose —
// Go lists statuses most-active-first for its help text, the web in workflow
// order for its checkboxes — so pinning order would fail on a correct codebase
// and invite someone to "fix" one surface into being wrong for itself.
func Diff(got, want []string) (missing, extra []string) {
	inGot := make(map[string]bool, len(got))
	for _, g := range got {
		inGot[g] = true
	}
	inWant := make(map[string]bool, len(want))
	for _, w := range want {
		inWant[w] = true
	}
	for _, w := range want {
		if !inGot[w] {
			missing = append(missing, w)
		}
	}
	for _, g := range got {
		if !inWant[g] {
			extra = append(extra, g)
		}
	}
	return missing, extra
}
