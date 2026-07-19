package mdsection

import "testing"

func TestFind(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		heading    string
		matchLevel int
		wantText   string
		wantFound  bool
	}{
		{
			name:      "basic section between two headings",
			body:      "## Goal\n\nBuild the thing.\n\n## Key Decisions\n\n- Use GraphQL\n- File-based storage\n\n## Notes\n\nOther stuff.",
			heading:   "Key Decisions",
			wantText:  "\n- Use GraphQL\n- File-based storage\n",
			wantFound: true,
		},
		{
			name:      "case insensitive match",
			body:      "## key decisions\n\n- Works with lowercase\n",
			heading:   "Key Decisions",
			wantText:  "\n- Works with lowercase\n",
			wantFound: true,
		},
		{
			name:      "heading at end of document",
			body:      "## Goal\n\nShip it.\n\n## Notes\n\nTrailing content with no next heading.",
			heading:   "Notes",
			wantText:  "\nTrailing content with no next heading.\n",
			wantFound: true,
		},
		{
			name:      "empty body",
			body:      "",
			heading:   "Anything",
			wantText:  "",
			wantFound: false,
		},
		{
			name:      "section not found",
			body:      "## Goal\n\nSome text.",
			heading:   "Key Decisions",
			wantText:  "",
			wantFound: false,
		},
		{
			name:      "sub-headings are part of the section",
			body:      "## Design\n\nOverview.\n\n### Details\n\nSub-section content.\n\n## Next",
			heading:   "Design",
			wantText:  "\nOverview.\n\n### Details\n\nSub-section content.\n",
			wantFound: true,
		},
		{
			name:      "prefix match with parenthetical",
			body:      "## Key Decisions (Phase 2)\n\n- Decision one\n",
			heading:   "Key Decisions",
			wantText:  "\n- Decision one\n",
			wantFound: true,
		},
		{
			name:      "does not prefix match non-parenthetical suffix",
			body:      "## Notes on Design\n\nDesign notes.\n\n## Notes\n\nActual notes.\n",
			heading:   "Notes",
			wantText:  "\nActual notes.\n",
			wantFound: true,
		},
		{
			name:      "finds h1 heading including sub-headings",
			body:      "# Overview\n\nTop-level content.\n\n## Details\n\nSub content.",
			heading:   "Overview",
			wantText:  "\nTop-level content.\n\n## Details\n\nSub content.\n",
			wantFound: true,
		},
		{
			name:      "finds h3 heading stopped by h2",
			body:      "## Parent\n\n### Child\n\nChild content.\n\n## Next",
			heading:   "Child",
			wantText:  "\nChild content.\n",
			wantFound: true,
		},
		{
			name:      "duplicate headings returns first match",
			body:      "## Notes\n\nFirst.\n\n## Notes\n\nSecond.",
			heading:   "Notes",
			wantText:  "\nFirst.\n",
			wantFound: true,
		},
		// --- exact-preferred matching (an exact heading wins over a parenthetical suffix) ---
		{
			// P2 regression: a parenthetical "(Phase 1)" ordered BEFORE the exact
			// heading must not win — the exact "## Key Decisions" section is returned.
			name:      "exact heading wins over an earlier parenthetical suffix",
			body:      "## Key Decisions (Phase 1)\n- old\n\n## Key Decisions\n- keep\n",
			heading:   "Key Decisions",
			wantText:  "- keep\n",
			wantFound: true,
		},
		{
			// Two-pass × level-gate interaction: the exact heading is at the WRONG
			// level ("### Foo", level 3) while a parenthetical heading sits at the
			// RIGHT level ("## Foo (Bar)", level 2). Pass 1 (exact) finds no level-2
			// exact heading — the gate rejects the level-3 "### Foo" — so pass 2 falls
			// back to the level-2 parenthetical. This discriminates a two-pass whose
			// EXACT pass drops the level gate: that bug would return the level-3
			// "### Foo" content ("\nlevel-three exact.\n") instead.
			name:       "exact pass level gate defers to a right-level parenthetical fallback",
			body:       "### Foo\n\nlevel-three exact.\n\n## Foo (Bar)\n\nlevel-two paren.\n",
			heading:    "Foo",
			matchLevel: 2,
			wantText:   "\nlevel-two paren.\n",
			wantFound:  true,
		},
		// --- level-aware matching ---
		{
			name:       "wildcard matches any level (h2)",
			body:       "## Sub\n\nLevel-two content.\n",
			heading:    "Sub",
			matchLevel: 0,
			wantText:   "\nLevel-two content.\n",
			wantFound:  true,
		},
		{
			name:       "wildcard matches any level (h3)",
			body:       "### Sub\n\nLevel-three content.\n",
			heading:    "Sub",
			matchLevel: 0,
			wantText:   "\nLevel-three content.\n",
			wantFound:  true,
		},
		{
			name:       "spelled level 3 does not match level 2",
			body:       "## Sub\n\nLevel-two content.\n",
			heading:    "Sub",
			matchLevel: 3,
			wantText:   "",
			wantFound:  false,
		},
		{
			name:       "spelled level 2 matches level 2",
			body:       "## Sub\n\nLevel-two content.\n",
			heading:    "Sub",
			matchLevel: 2,
			wantText:   "\nLevel-two content.\n",
			wantFound:  true,
		},
		{
			name:       "spelled level 2 does not match level 3",
			body:       "### Sub\n\nLevel-three content.\n",
			heading:    "Sub",
			matchLevel: 2,
			wantText:   "",
			wantFound:  false,
		},
		{
			name:       "spelled level 3 matches level 3 among mixed levels",
			body:       "## Sub\n\nTwo.\n\n### Sub\n\nThree.\n",
			heading:    "Sub",
			matchLevel: 3,
			wantText:   "\nThree.\n",
			wantFound:  true,
		},
		{
			name:       "parenthetical suffix respects level (no match at wrong level)",
			body:       "### Key Decisions (Phase 2)\n\n- Decision one\n",
			heading:    "Key Decisions",
			matchLevel: 2,
			wantText:   "",
			wantFound:  false,
		},
		{
			name:       "parenthetical suffix still matches at the right level",
			body:       "### Key Decisions (Phase 2)\n\n- Decision one\n",
			heading:    "Key Decisions",
			matchLevel: 3,
			wantText:   "\n- Decision one\n",
			wantFound:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := Find(tt.body, tt.heading, tt.matchLevel)
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
			if got != tt.wantText {
				t.Errorf("content = %q, want %q", got, tt.wantText)
			}
		})
	}
}

// TestFindExact pins FindExact's exact-only contract: it matches an exact heading
// (case-insensitively, honoring the level gate) but NEVER falls back to a
// parenthetical-suffix heading — the crucial difference from Find.
func TestFindExact(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		heading    string
		matchLevel int
		wantText   string
		wantFound  bool
	}{
		{
			name:      "matches an exact heading",
			body:      "## Key Decisions\n\n- Use GraphQL\n",
			heading:   "Key Decisions",
			wantText:  "\n- Use GraphQL\n",
			wantFound: true,
		},
		{
			name:      "case insensitive exact match",
			body:      "## key decisions\n\n- lower\n",
			heading:   "Key Decisions",
			wantText:  "\n- lower\n",
			wantFound: true,
		},
		{
			// The load-bearing difference from Find: a lone parenthetical heading is
			// NOT matched by FindExact (Find WOULD fall back to it).
			name:      "does NOT fall back to a lone parenthetical heading",
			body:      "## Key Decisions (Phase 1)\n\n- Decision one\n",
			heading:   "Key Decisions",
			wantText:  "",
			wantFound: false,
		},
		{
			// An exact heading is found even when a parenthetical one also exists.
			name:      "finds the exact heading alongside a parenthetical one",
			body:      "## Key Decisions (Phase 1)\n\n- old\n\n## Key Decisions\n\n- exact\n",
			heading:   "Key Decisions",
			wantText:  "\n- exact\n",
			wantFound: true,
		},
		{
			name:       "respects the level gate (no exact match at the wrong level)",
			body:       "## Key Decisions\n\n- two\n",
			heading:    "Key Decisions",
			matchLevel: 3,
			wantText:   "",
			wantFound:  false,
		},
		{
			name:       "matches an exact heading at the spelled level",
			body:       "### Key Decisions\n\n- three\n",
			heading:    "Key Decisions",
			matchLevel: 3,
			wantText:   "\n- three\n",
			wantFound:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := FindExact(tt.body, tt.heading, tt.matchLevel)
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
			if got != tt.wantText {
				t.Errorf("content = %q, want %q", got, tt.wantText)
			}
		})
	}
}

func TestReplace(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		heading    string
		matchLevel int
		newContent string
		want       string
	}{
		{
			name:       "replace section between headings",
			body:       "## Goal\n\nShip it.\n\n## Current Focus\n\nOld focus.\n\n## Notes\n\nStuff.",
			heading:    "Current Focus",
			newContent: "\nNew focus here.\n",
			want:       "## Goal\n\nShip it.\n\n## Current Focus\n\nNew focus here.\n\n## Notes\n\nStuff.",
		},
		{
			name:       "section not found returns body unchanged",
			body:       "## Goal\n\nShip it.",
			heading:    "Missing",
			newContent: "\nWon't appear.\n",
			want:       "## Goal\n\nShip it.",
		},
		{
			name:       "replace section at end of document",
			body:       "## Goal\n\nShip it.\n\n## Notes\n\nOld notes.",
			heading:    "Notes",
			newContent: "\nNew notes here.",
			want:       "## Goal\n\nShip it.\n\n## Notes\n\nNew notes here.",
		},
		{
			name:       "replace preserves sub-headings in other sections",
			body:       "## A\n\nContent A.\n\n### Sub-A\n\nSub content.\n\n## B\n\nContent B.\n\n## C\n\nContent C.",
			heading:    "B",
			newContent: "\nReplaced B.\n",
			want:       "## A\n\nContent A.\n\n### Sub-A\n\nSub content.\n\n## B\n\nReplaced B.\n\n## C\n\nContent C.",
		},
		// --- exact-preferred matching ---
		{
			// The exact "## Key Decisions" is replaced even though a parenthetical
			// "(Phase 1)" heading precedes it; the parenthetical section is untouched.
			name:       "replaces the exact heading, not an earlier parenthetical",
			body:       "## Key Decisions (Phase 1)\n- old\n\n## Key Decisions\n- keep\n",
			heading:    "Key Decisions",
			newContent: "\n- new\n",
			want:       "## Key Decisions (Phase 1)\n- old\n\n## Key Decisions\n\n- new\n",
		},
		// --- level-aware matching ---
		{
			name:       "spelled level 3 does not clobber level 2 (unchanged)",
			body:       "## Sub\n\nOld two.\n\n### Child\n\nChild content.\n",
			heading:    "Sub",
			matchLevel: 3,
			newContent: "\nReplacement.\n",
			want:       "## Sub\n\nOld two.\n\n### Child\n\nChild content.\n",
		},
		{
			name:       "spelled level 2 replaces the level-2 heading",
			body:       "## Sub\n\nOld two.\n\n## Next\n\nKeep.\n",
			heading:    "Sub",
			matchLevel: 2,
			newContent: "\nReplacement.\n",
			want:       "## Sub\n\nReplacement.\n\n## Next\n\nKeep.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Replace(tt.body, tt.heading, tt.newContent, tt.matchLevel)
			if got != tt.want {
				t.Errorf("Replace() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestSet(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		matchLevel  int
		appendLevel int
		heading     string
		content     string
		want        string
	}{
		{
			name:        "replaces existing section",
			body:        "## Goal\n\nShip it.\n\n## Notes\n\nOld notes.\n\n## End",
			matchLevel:  0,
			appendLevel: 2,
			heading:     "Notes",
			content:     "\nNew notes.\n",
			want:        "## Goal\n\nShip it.\n\n## Notes\n\nNew notes.\n\n## End",
		},
		{
			name:        "appends new section when not found",
			body:        "## Goal\n\nShip it.",
			matchLevel:  0,
			appendLevel: 2,
			heading:     "Key Decisions",
			content:     "\n- Decision one\n",
			want:        "## Goal\n\nShip it.\n\n## Key Decisions\n\n- Decision one\n",
		},
		{
			name:        "appends to empty body",
			body:        "",
			matchLevel:  0,
			appendLevel: 2,
			heading:     "Notes",
			content:     "\nSome notes.\n",
			want:        "\n## Notes\n\nSome notes.\n",
		},
		{
			name:        "appends h3 section",
			body:        "## Parent\n\nSome content.",
			matchLevel:  0,
			appendLevel: 3,
			heading:     "Details",
			content:     "\nDetail text.\n",
			want:        "## Parent\n\nSome content.\n\n### Details\n\nDetail text.\n",
		},
		{
			name:        "appends h1 section",
			body:        "## Existing\n\nContent.",
			matchLevel:  0,
			appendLevel: 1,
			heading:     "Top Level",
			content:     "\nTop-level text.\n",
			want:        "## Existing\n\nContent.\n\n# Top Level\n\nTop-level text.\n",
		},
		{
			name:        "append level zero clamps to h1",
			body:        "## Existing\n\nContent.",
			matchLevel:  0,
			appendLevel: 0,
			heading:     "Added",
			content:     "\nNew content.\n",
			want:        "## Existing\n\nContent.\n\n# Added\n\nNew content.\n",
		},
		{
			name:        "negative append level clamps to h1",
			body:        "## Existing\n\nContent.",
			matchLevel:  0,
			appendLevel: -1,
			heading:     "Added",
			content:     "\nNew content.\n",
			want:        "## Existing\n\nContent.\n\n# Added\n\nNew content.\n",
		},
		// --- exact-preferred matching ---
		{
			// Set finds the exact heading (not the earlier parenthetical) and
			// replaces it in place rather than appending a duplicate.
			name:        "replaces the exact heading, not an earlier parenthetical",
			body:        "## Key Decisions (Phase 1)\n- old\n\n## Key Decisions\n- keep\n",
			matchLevel:  0,
			appendLevel: 2,
			heading:     "Key Decisions",
			content:     "\n- new\n",
			want:        "## Key Decisions (Phase 1)\n- old\n\n## Key Decisions\n\n- new\n",
		},
		// --- level-aware matching ---
		{
			name:        "spelled matchLevel misses same-name lower level and appends instead of clobbering",
			body:        "## Sub\n\nOld two.\n\n### Child\n\nChild content.",
			matchLevel:  3,
			appendLevel: 3,
			heading:     "Sub",
			content:     "\nBrand new.\n",
			want:        "## Sub\n\nOld two.\n\n### Child\n\nChild content.\n\n### Sub\n\nBrand new.\n",
		},
		{
			name:        "wildcard matchLevel replaces the existing level-2 section",
			body:        "## Sub\n\nOld two.\n\n## Next\n\nKeep.\n",
			matchLevel:  0,
			appendLevel: 2,
			heading:     "Sub",
			content:     "\nUpdated.\n",
			want:        "## Sub\n\nUpdated.\n\n## Next\n\nKeep.\n",
		},
		{
			name:        "spelled matchLevel replaces a match at that exact level",
			body:        "## Sub\n\nTwo.\n\n### Sub\n\nOld three.\n",
			matchLevel:  3,
			appendLevel: 3,
			heading:     "Sub",
			content:     "\nNew three.\n",
			want:        "## Sub\n\nTwo.\n\n### Sub\n\nNew three.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SetAtLevel(tt.body, tt.matchLevel, tt.appendLevel, tt.heading, tt.content)
			if got != tt.want {
				t.Errorf("SetAtLevel() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

// TestSetWildcard verifies the wildcard-match Set wrapper: it matches an existing
// heading regardless of the level it is spelled at (proving it delegates to
// SetAtLevel with AnyLevel), and appends at the requested level when absent.
func TestSetWildcard(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		appendLevel int
		heading     string
		content     string
		want        string
	}{
		{
			name:        "matches a level-3 heading despite appendLevel 2 (wildcard)",
			body:        "## Parent\n\nTwo.\n\n### Sub\n\nOld three.\n",
			appendLevel: 2,
			heading:     "Sub",
			content:     "\nNew three.\n",
			want:        "## Parent\n\nTwo.\n\n### Sub\n\nNew three.\n",
		},
		{
			name:        "appends at appendLevel when no match",
			body:        "## Goal\n\nShip it.",
			appendLevel: 2,
			heading:     "Key Decisions",
			content:     "\n- Decision one\n",
			want:        "## Goal\n\nShip it.\n\n## Key Decisions\n\n- Decision one\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Set(tt.body, tt.appendLevel, tt.heading, tt.content)
			if got != tt.want {
				t.Errorf("Set() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}
