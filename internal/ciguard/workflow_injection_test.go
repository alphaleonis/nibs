// Package ciguard holds guards over this repository's own CI configuration.
// It has no runtime code: the workflows are data the build never reads, so the
// only place a rule about them can be enforced is the test suite.
package ciguard_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestWorkflowRunStepsDoNotInterpolateUntrustedInput fails when a `run:` body in
// any workflow embeds a caller-controlled value as program text instead of
// passing it through `env:` and reading it as a shell variable.
//
// It reads the parsed YAML rather than the raw file so that only real `run:`
// bodies are judged: `run-name:`, `env:` values and YAML comments all carry the
// same `${{ }}` text without being a script, and a raw scan cannot tell them
// apart.
func TestWorkflowRunStepsDoNotInterpolateUntrustedInput(t *testing.T) {
	dir := filepath.Join(moduleRoot(t), ".github", "workflows")

	var (
		all      []finding
		files    int
		bodies   int
		filesSee []string
	)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if ext := filepath.Ext(path); ext != ".yml" && ext != ".yaml" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			rel = path
		}
		found, seen, scanErr := scanWorkflow(rel, data)
		if scanErr != nil {
			return fmt.Errorf("%s: %w", rel, scanErr)
		}
		files++
		bodies += seen
		filesSee = append(filesSee, fmt.Sprintf("%s (%d run steps)", rel, seen))
		all = append(all, found...)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}

	// A guard that read nothing is green against any tree, including one where
	// the workflows moved or stopped parsing.
	if files == 0 {
		t.Fatalf("no workflow files under %s; this guard is not reading the repository", dir)
	}
	if bodies == 0 {
		t.Fatalf("found no `run:` steps across %d workflow files under %s; this guard is not reading anything it can judge", files, dir)
	}
	t.Logf("scanned %d workflow files: %s", files, strings.Join(filesSee, ", "))

	for _, f := range all {
		t.Errorf("%s: `%s` reaches the shell as program text, not as data — "+
			"GitHub substitutes the expression into the script before any shell parses it, so a value carrying a quote "+
			"ends the string and the rest runs as code. Put it in that step's `env:` and read it as a shell variable.\n"+
			"If %s is genuinely not caller-chosen, add it to allowedPaths with the reason — this guard allows a listed "+
			"path and refuses everything else, so an unreviewed context fails here by design rather than by omission.\n"+
			"  file: %s (line %d)\n  step: %s\n  path: %s\n  context: %s",
			f.file, f.expression, f.context, f.file, f.line, f.stepName, f.path, f.context)
	}
}

// TestScanWorkflowFlagsOnlyRunBodies pins what the scanner treats as a script.
// The distinctions here are the reason it parses YAML: every case below carries
// the same `${{ }}` text, and only some of them are a program.
func TestScanWorkflowFlagsOnlyRunBodies(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []string // expressions expected to be reported
		// wantBodies is what `want` cannot say. A case whose fixture yields no
		// finding proves nothing on its own: "this was rejected as a script"
		// and "this was collected as a script and held no expression" are the
		// same empty result. The count separates them.
		wantBodies int
	}{
		{
			name:       "input interpolated into a run body",
			wantBodies: 1,
			yaml: `
jobs:
  build:
    steps:
      - name: Tag
        run: git tag "${{ inputs.version }}"
`,
			want: []string{"${{ inputs.version }}"},
		},
		{
			name:       "input passed through env and read as a variable",
			wantBodies: 1,
			yaml: `
jobs:
  build:
    steps:
      - name: Tag
        env:
          VERSION: ${{ inputs.version }}
        run: git tag "$VERSION"
`,
		},
		{
			name:       "env indirection is still a substitution",
			wantBodies: 1,
			yaml: `
jobs:
  build:
    steps:
      - name: Tag
        env:
          VERSION: ${{ inputs.version }}
        run: git tag "${{ env.VERSION }}"
`,
			want: []string{"${{ env.VERSION }}"},
		},
		{
			name:       "run-name is not a script",
			wantBodies: 1,
			yaml: `
run-name: "Release ${{ inputs.version }}"
jobs:
  build:
    steps:
      - run: echo hello
`,
		},
		{
			name:       "a shell comment inside a run body is still substituted",
			wantBodies: 1,
			yaml: `
jobs:
  build:
    steps:
      - name: Note
        run: |
          # releasing ${{ inputs.version }}
          echo done
`,
			want: []string{"${{ inputs.version }}"},
		},
		{
			name:       "pull request event data in a run body",
			wantBodies: 1,
			yaml: `
jobs:
  build:
    steps:
      - name: Greet
        run: echo "${{ github.event.pull_request.title }}"
`,
			want: []string{"${{ github.event.pull_request.title }}"},
		},
		{
			// The dispatcher picks the ref a workflow_dispatch run checks out,
			// and GitHub Actions reads `github['ref']` as `github.ref`, so both
			// spellings have to be caught.
			name:       "dispatch ref in a run body, dotted and index form",
			wantBodies: 1,
			yaml: `
jobs:
  build:
    steps:
      - name: Checkout
        run: |
          git checkout "${{ github.ref }}"
          git merge "${{ github['head_ref'] }}"
`,
			want: []string{"${{ github.ref }}", "${{ github['head_ref'] }}"},
		},
		{
			name:       "trusted contexts are left alone",
			wantBodies: 1,
			yaml: `
jobs:
  build:
    steps:
      - name: Publish
        run: goreleaser release --release-notes=${{ runner.temp }}/notes.md ${{ github.repository }}
        env:
          TOKEN: ${{ secrets.GITHUB_TOKEN }}
`,
		},
		{
			// A path inside a string literal is data the expression never
			// evaluates, and `hashFiles` is a function rather than a context —
			// so this names neither. Under the denylist it needed a
			// character-class exclusion to avoid reading the `env` out of the
			// filename; the tokenizer never sees one.
			name:       "a literal filename and a function name are not contexts",
			wantBodies: 1,
			yaml: `
jobs:
  build:
    steps:
      - name: Cache key
        run: echo "${{ hashFiles('web/.env.example') }}"
`,
		},
		{
			// The whole point of the inversion. `github.actor` was still absent
			// from the denylist after three rounds; nobody has to think of it
			// for an allowlist to refuse it.
			name:       "a context nobody vetted fails closed",
			wantBodies: 1,
			yaml: `
jobs:
  build:
    steps:
      - name: Greet
        run: |
          echo "${{ github.actor }}"
          echo "${{ github.triggering_actor }}"
`,
			want: []string{"${{ github.actor }}", "${{ github.triggering_actor }}"},
		},
		{
			// A `}}` inside a string literal used to end the expression early,
			// leaving everything after it as text nothing judged.
			name:       "an expression carrying a literal brace pair is read whole",
			wantBodies: 1,
			yaml: `
jobs:
  build:
    steps:
      - name: Format
        run: echo "${{ format('}}', github.event.issue.body) }}"
`,
			want: []string{"${{ format('}}', github.event.issue.body) }}"},
		},
		{
			name:       "defaults.run is a mapping, not a script",
			wantBodies: 1,
			yaml: `
jobs:
  build:
    defaults:
      run:
        working-directory: ${{ inputs.version }}
    steps:
      - run: echo hello
`,
		},
		{
			name:       "index form and a run body outside jobs.steps",
			wantBodies: 2,
			yaml: `
jobs:
  build:
    steps:
      - name: Outer
        run: |
          echo "${{ inputs['version'] }}"
        with:
          nested:
            run: echo "${{ github['event'].issue.body }}"
`,
			want: []string{"${{ inputs['version'] }}", "${{ github['event'].issue.body }}"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			found, bodies, err := scanWorkflow("test.yml", []byte(tc.yaml))
			if err != nil {
				t.Fatalf("scanWorkflow: %v", err)
			}
			if bodies != tc.wantBodies {
				t.Fatalf("scanWorkflow treated %d values as a run body, want %d", bodies, tc.wantBodies)
			}
			var got []string
			for _, f := range found {
				got = append(got, f.expression)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("finding %d: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// finding is one untrusted expression inside one `run:` body.
type finding struct {
	file       string
	path       string // e.g. jobs.release.steps[6].run
	stepName   string // the sibling `name:`, when the step has one
	line       int
	expression string
	context    string
}

// scanWorkflow returns every untrusted expression in a `run:` body of one
// workflow document, along with the number of `run:` bodies it examined.
func scanWorkflow(file string, data []byte) ([]finding, int, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, 0, err
	}

	var scripts []script
	collectScripts(&doc, "", "", &scripts)

	var found []finding
	for _, s := range scripts {
		bodies, texts := findExpressions(s.body)
		for i, body := range bodies {
			for _, path := range expressionPaths(body) {
				if pathAllowed(path) {
					continue
				}
				found = append(found, finding{
					file:       file,
					path:       s.path,
					stepName:   s.name,
					line:       s.line,
					expression: strings.Join(strings.Fields(texts[i]), " "),
					context:    path,
				})
				break
			}
		}
	}
	return found, len(scripts), nil
}

// script is a `run:` body together with where it was found.
type script struct {
	path string
	name string
	line int
	body string
}

// collectScripts walks the document and records every mapping key `run` whose
// value is a scalar.
func collectScripts(n *yaml.Node, path, name string, out *[]script) {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, child := range n.Content {
			collectScripts(child, path, name, out)
		}
	case yaml.MappingNode:
		// A step names itself; carry that down so a nested `run:` still reports
		// the step a reader would look for.
		if own := scalarValue(n, "name"); own != "" {
			name = own
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			child := join(path, key.Value)
			if key.Value == "run" && value.Kind == yaml.ScalarNode {
				*out = append(*out, script{path: child, name: name, line: value.Line, body: value.Value})
				continue
			}
			collectScripts(value, child, name, out)
		}
	case yaml.SequenceNode:
		for i, child := range n.Content {
			collectScripts(child, fmt.Sprintf("%s[%d]", path, i), name, out)
		}
	}
}

// scalarValue returns the scalar value of key in a mapping node, or "".
func scalarValue(mapping *yaml.Node, key string) string {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key && mapping.Content[i+1].Kind == yaml.ScalarNode {
			return mapping.Content[i+1].Value
		}
	}
	return ""
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

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

// TestAllowedPathsAreAllExercised refuses an allowlist entry no `run:` body
// uses.
//
// An allowlist is only as tight as its shortest justification, and a dead entry
// has none: it is a standing permission nobody is relying on and nobody will
// notice, which is how a list that began as "exactly what we use" drifts into
// "whatever anyone once needed". Requiring every entry to be exercised gives the
// list the same stopping point the inversion gave the rule.
//
// It reads the workflows rather than a fixture, so removing the last use of a
// context fails here and names the entry to drop.
func TestAllowedPathsAreAllExercised(t *testing.T) {
	live := liveRunBodyPaths(t)
	if len(live) == 0 {
		t.Fatal("no expressions found in any `run:` body; this test is not reading the workflows")
	}

	for _, pattern := range allowedPaths {
		used := false
		for _, path := range live {
			if patternMatches(pattern, path) {
				used = true
				break
			}
		}
		if !used {
			t.Errorf("allowedPaths has %q and no `run:` body interpolates it — drop it, because an entry nothing uses is a permission nobody reviewed and nothing will catch",
				pattern)
		}
	}
}

// liveRunBodyPaths returns every property path interpolated by a `run:` body
// across this repository's workflows, allowed or not.
func liveRunBodyPaths(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join(moduleRoot(t), ".github", "workflows")

	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if ext := filepath.Ext(path); ext != ".yml" && ext != ".yaml" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var doc yaml.Node
		if unmarshalErr := yaml.Unmarshal(data, &doc); unmarshalErr != nil {
			return fmt.Errorf("%s: %w", path, unmarshalErr)
		}
		var scripts []script
		collectScripts(&doc, "", "", &scripts)
		for _, sc := range scripts {
			bodies, _ := findExpressions(sc.body)
			for _, body := range bodies {
				paths = append(paths, expressionPaths(body)...)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	return paths
}
