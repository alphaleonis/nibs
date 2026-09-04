package ciguard_test

import (
	"strings"
	"testing"
)

// The expression layer: what a `${{ }}` body NAMES, and which of those names a
// `run:` script may interpolate.
//
// This is an ALLOWLIST, and the inversion is the point. The guard began as a
// denylist of untrusted contexts and never converged: three review rounds each
// closed the dodge the previous one demonstrated and left the next one open —
// `github.ref`, then the `github['head_ref']` index form, then `github.actor`.
// A denylist has no stopping point, because the set of things it must know
// about is written by GitHub and grows without asking. An allowlist fails
// CLOSED on a context nobody has vetted, which is the correct direction for a
// security guard and gives the rule a natural end (nibs-gf83).

// allowedPaths are the property paths a `run:` body may interpolate, as segment
// patterns where `*` matches exactly one segment.
//
// Every entry is here because a `run:` body in this repository uses it — the
// list was derived by emptying it and reading what the workflows then reported,
// not composed from what seemed reasonable — and each names a value GitHub
// computes rather than one a caller supplies:
//
//   - runner.*             the runner's own facts (runner.temp, 4 uses)
//   - matrix.*             the matrix values these workflows declare themselves
//   - steps.*.outputs.*    a value an earlier step in the same job produced
//   - github.repository    fixed by the repository
//   - github.sha           fixed by the commit
//
// `github` is deliberately NOT `github.*`: most of that context is exactly what
// this guard exists to keep out of a script (`github.ref`, `github.head_ref`,
// `github.actor`, `github.event.*`), so it is allowed a path at a time.
//
// `secrets.*` is deliberately ABSENT, though it would have been easy to include
// and an earlier sketch of this list had it. No `run:` body interpolates one:
// the workflows pass secrets through `env:`, which is what GitHub's own guidance
// says to do, because an interpolated secret is pasted into the script text
// where it can reach a log or a process listing. Allowing it would sanction a
// practice nothing here follows. A workflow that genuinely needs one adds it
// here with its reason, which is the fail-closed path working as intended.
//
// TestAllowedPathsAreAllExercised keeps this honest: an entry nothing uses is a
// standing hole in a security allowlist, so the list may not outlive its uses.
var allowedPaths = []string{
	"runner.*",
	"matrix.*",
	"steps.*.outputs.*",
	"github.repository",
	"github.sha",
}

// expressionKeywords are the bare words an expression may contain that are not
// contexts at all.
var expressionKeywords = map[string]bool{"true": true, "false": true, "null": true}

// findExpressions returns the body of every `${{ ... }}` in s, and the full text
// of each, in order.
//
// It scans rather than pattern-matches because `}}` can occur INSIDE a string
// literal — `${{ format('{0}}}', x) }}` — where a lazy `\$\{\{(.*?)\}\}` stops
// early and hands the rest of the expression back as ordinary text. That is the
// gap the third review round measured and could not close by rewording.
func findExpressions(s string) (bodies []string, texts []string) {
	for i := 0; i+2 < len(s); {
		if s[i] != '$' || s[i+1] != '{' || s[i+2] != '{' {
			i++
			continue
		}
		j := i + 3
		for j < len(s) {
			switch s[j] {
			case '\'':
				j = skipLiteral(s, j)
			case '}':
				if j+1 < len(s) && s[j+1] == '}' {
					bodies = append(bodies, s[i+3:j])
					texts = append(texts, s[i:j+2])
					goto next
				}
				j++
			default:
				j++
			}
		}
		// Unterminated: nothing more to find.
		return bodies, texts
	next:
		i = j + 2
	}
	return bodies, texts
}

// skipLiteral returns the index just past the single-quoted literal starting at
// i. GitHub escapes a quote inside a literal by doubling it.
func skipLiteral(s string, i int) int {
	j := i + 1
	for j < len(s) {
		if s[j] != '\'' {
			j++
			continue
		}
		if j+1 < len(s) && s[j+1] == '\'' {
			j += 2
			continue
		}
		return j + 1
	}
	return len(s)
}

// expressionPaths returns the property paths one expression body names.
//
// String literals contribute nothing — `hashFiles('web/.env.example')` names a
// FILE, and reading the `env` out of it was a false positive the denylist had to
// carry a character-class exclusion for. A name followed by `(` is a function
// call, so `hashFiles` itself is not a context either; its arguments are still
// scanned, because `toJSON(github.event)` is as injectable as the field.
//
// An index is folded into the path it indexes: GitHub reads `github['ref']` and
// `github.ref` as the same thing, and a guard that saw only the first spelling
// is what round two found. An index this cannot read statically — a computed
// one — becomes a segment that matches no allowlist entry, so it fails closed.
func expressionPaths(body string) []string {
	var paths []string
	for i := 0; i < len(body); {
		c := body[i]
		switch {
		case c == '\'':
			i = skipLiteral(body, i)
		case isIdentStart(c):
			path, next := readPath(body, i)
			i = next
			if path != "" {
				paths = append(paths, path)
			}
		default:
			i++
		}
	}
	return paths
}

// readPath reads one identifier and the accessors chained onto it, returning
// the dotted path and the index just past it. It returns "" for a function
// call, whose arguments the caller goes on to scan.
func readPath(s string, i int) (string, int) {
	start := i
	for i < len(s) && isIdentPart(s[i]) {
		i++
	}
	segments := []string{s[start:i]}

	for {
		j := skipSpace(s, i)
		if j >= len(s) {
			break
		}
		switch s[j] {
		case '(':
			// A function call. Skip the name only — the arguments are scanned
			// as ordinary expression text by the caller's loop.
			return "", j + 1
		case '.':
			k := skipSpace(s, j+1)
			if k >= len(s) || !isIdentStart(s[k]) {
				return strings.Join(segments, "."), j + 1
			}
			start := k
			for k < len(s) && isIdentPart(s[k]) {
				k++
			}
			segments = append(segments, s[start:k])
			i = k
		case '[':
			k := skipSpace(s, j+1)
			if k < len(s) && s[k] == '\'' {
				end := skipLiteral(s, k)
				segments = append(segments, strings.Trim(s[k:end], "'"))
				i = end
			} else {
				// A computed index. Unreadable statically, so it becomes a
				// segment nothing allows.
				segments = append(segments, "<computed>")
				i = k
			}
			if m := skipSpace(s, i); m < len(s) && s[m] == ']' {
				i = m + 1
			}
		default:
			return strings.Join(segments, "."), i
		}
	}
	return strings.Join(segments, "."), i
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '-'
}

// pathAllowed reports whether a property path is one a `run:` body may name.
// Matching is segment-wise and the lengths must agree, so a longer path than an
// entry describes is refused rather than accepted by its prefix.
func pathAllowed(path string) bool {
	if expressionKeywords[strings.ToLower(path)] {
		return true
	}
	for _, pattern := range allowedPaths {
		if patternMatches(pattern, path) {
			return true
		}
	}
	return false
}

// patternMatches reports whether one allowlist pattern describes one path.
// Segment-wise, and the lengths must agree: a path longer than the pattern
// describes is refused rather than accepted by its prefix, so `runner.temp.evil`
// is not covered by `runner.*`.
func patternMatches(pattern, path string) bool {
	want := strings.Split(pattern, ".")
	got := strings.Split(path, ".")
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if want[i] != "*" && !strings.EqualFold(want[i], got[i]) {
			return false
		}
	}
	return true
}

// TestExpressionPathsReadsWhatGitHubReads pins the tokenizer against the shapes
// a regex over the same text gets wrong. Each row is a spelling the previous
// denylist either missed or misread; they are the reason this is a scanner.
func TestExpressionPathsReadsWhatGitHubReads(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{"a plain path", " inputs.version ", []string{"inputs.version"}},
		{"a deep path", " github.event.pull_request.title ", []string{"github.event.pull_request.title"}},
		{
			// Round two's High finding: the denylist's dot-only pattern walked
			// past this, and GitHub reads it as `github.head_ref`.
			name: "an index reads as the path it indexes",
			body: " github['head_ref'] ",
			want: []string{"github.head_ref"},
		},
		{
			// The false positive the denylist needed a character-class
			// exclusion for: this names a FILE, and there is no `env` context
			// in it at all.
			name: "a literal contributes nothing, and a function is not a context",
			body: " hashFiles('web/.env.example') ",
			want: nil,
		},
		{
			// A function is not a context, but its arguments are still
			// expressions — this is the laundering an allowlist must not miss.
			name: "a function's arguments are still scanned",
			body: " toJSON(github.event) ",
			want: []string{"github.event"},
		},
		{
			name: "operators and literals around a path",
			body: " matrix.os == 'ubuntu-latest' && '1' || '' ",
			want: []string{"matrix.os"},
		},
		{
			// Two properties in one row. An index this cannot read statically
			// must not collapse to the bare root — `github` alone would be a
			// shorter path that an entry could allow — and the index is itself
			// an expression, so whatever it names is scanned in its own right.
			name: "a computed index fails closed, and its own expression is still read",
			body: " github[format('{0}', github.event.number)] ",
			want: []string{"github.<computed>", "github.event.number"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expressionPaths(tt.body)
			if len(got) != len(tt.want) {
				t.Fatalf("expressionPaths(%q) = %v, want %v", tt.body, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("path %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestFindExpressionsRespectsStringLiterals is the third review round's measured
// gap as a test: `\$\{\{(.*?)\}\}` stops at the first `}}`, which a literal can
// contain, and hands the rest of the expression back as ordinary text — where
// nothing judges it.
func TestFindExpressionsRespectsStringLiterals(t *testing.T) {
	bodies, texts := findExpressions("echo ${{ format('}}', github.event.issue.body) }}")

	if len(bodies) != 1 {
		t.Fatalf("found %d expressions %v, want 1 — a `}}` inside a literal ended it early", len(bodies), texts)
	}
	if got := expressionPaths(bodies[0]); len(got) != 1 || got[0] != "github.event.issue.body" {
		t.Errorf("paths = %v, want [github.event.issue.body] — the argument after the literal was not read", got)
	}
}

func TestPathAllowed(t *testing.T) {
	allowed := []string{"runner.temp", "matrix.os", "steps.version.outputs.prerelease", "github.repository", "github.sha", "true"}
	refused := []string{
		"github.ref", "github.head_ref", "github.actor", "github.event.issue.body",
		"inputs.version", "env.VERSION", "secrets.GITHUB_TOKEN",
		// A bare root must not inherit its children's permission, and a path
		// longer than an entry describes must not be accepted by its prefix.
		"github", "runner", "runner.temp.evil", "steps.version.outputs",
	}
	for _, p := range allowed {
		if !pathAllowed(p) {
			t.Errorf("pathAllowed(%q) = false, want true", p)
		}
	}
	for _, p := range refused {
		if pathAllowed(p) {
			t.Errorf("pathAllowed(%q) = true, want false — this guard refuses what it has not vetted", p)
		}
	}
}
