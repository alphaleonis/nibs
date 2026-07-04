# Code Review

**Mode**: mid (explicit) | **Reviewers**: quick-reviewer, broad-reviewer, knowledge-reviewer, consistency-reviewer, design-reviewer, test-reviewer, spec-compliance-reviewer, adversarial-reviewer, performance-reviewer, go-reviewer | **Date**: 2026-07-03
**Source**: local changes (uncommitted) — iteration 2 of the nibs-sn96 review-fix loop
**Scope**: 9 files changed (8 modified +107/−10, plus new 259-line test file `internal/nibcore/search_id_test.go`)
**Spec**: work item nibs-sn96 (explicit — named by the invoking session; `.nibs/nibs-sn96--keyword-search-does-not-match-nib-ids.md`)
**Validation**: 3 confirmed, 1 refuted, 0 uncertain

**Review focus (caller instruction)**: regressions and new issues introduced by this fix round (the 7 applied findings from iteration 1's review) — iteration 1's accepted union design was not re-litigated.

## Agent Selection Rationale

Mode was given explicitly (`mid`): floor + every gate-matched specialist; no roster cap.

- quick-reviewer (always — review floor)
- broad-reviewer (always — review floor)
- knowledge-reviewer — new behavioral decisions embedded this round (minIDFragmentLen=2 rationale, cap-after-sort, emergent-exclusion comment)
- consistency-reviewer — sibling ID-handling and test-helper code exists to compare against (`mentions.go`, `mentions_test.go`)
- design-reviewer — `Core.Search` public contract semantics and `SearchIndex` seam contract changed
- security-reviewer: skipped — change adds pure in-process string comparison on an existing query path; no auth/crypto/serialization/file-I/O/privilege surface touched
- test-reviewer — test files present in changeset (hard gate matched)
- spec-compliance-reviewer — spec available: work item nibs-sn96 (hard gate matched)
- adversarial-reviewer — ≥50 changed executable lines
- performance-reviewer — per-search O(n) scan + sort over the full nib map
- go-reviewer — Go files present (hard gate matched)
- data-migration-reviewer: skipped — no migration artifacts in diff (hard gate)
- dotnet/typescript/cpp/rust-reviewers: skipped — no such files in changeset (hard gates)
- prior-feedback-reviewer: skipped — not a PR review (hard gate)

Model tiering (`mid`): judgment agents (knowledge, design, spec-compliance, adversarial) ran on the session model; volume agents (quick, broad, consistency, test, performance, go) and the 4 finding-validators ran mid-tier (sonnet).

## Summary

| Severity | Count |
|----------|-------|
| 🔴 Critical | 0 |
| 🟠 High | 1 |
| 🟡 Medium | 2 |
| 🟢 Low | 0 |
| 🔵 Minor | 5 |

Critical/High/Medium/Low are **primary** findings and drive the verdict. **Minor** counts the reported-but-non-blocking findings (Consistency / Testing Gaps / Residual Risks). No pre-existing issues were reattributed.

**Verdict**: ❌ NEEDS_CHANGES (one High primary finding)

---

## Findings

### #1 🟠 High: Emergent-exclusion comment codifies an ID-charset invariant that nothing enforces — non-conforming IDs break it today

| | |
|---|---|
| **File** | `internal/nibcore/core.go:343` (doc comment) and `:358-368` (`matchesLoweredIDQuery`) |
| **Category** | assumption-unvalidated / knowledge preservation |
| **Confidence** | 100 (promoted — 2 independent finders at 75) |
| **Found by** | knowledge-reviewer (High/SHOULD), adversarial-reviewer (Medium — dissent noted) |
| **Validation** | CONFIRMED — validator independently reproduced the behavioral trace and the missing cross-references |

**Issue:** The new doc comment claims "Queries with internal whitespace or Bleve operators are excluded emergently: the ID alphabet ([0-9a-z] plus the prefix) can never contain them… This is intentional — do not tokenize the query here." Two facets, one root:

1. **The invariant is factually false for real data.** IDs come from filenames (`ParseFilename`, called from `loadNib` with zero charset validation), not from the generator's `idAlphabet`. A nib file `task-42--fix.md` under config prefix `nibs-` yields ID `task-42`; `TrimPrefix` no-ops, the short ID retains the hyphen, and the Bleve negation query `-42` (user intent: *exclude* term 42) substring-matches and returns `task-42` as an ID match — while the same query simultaneously means NOT-42 to the full-text leg. Plain query `task` ID-matches every old-prefix nib. `reprefix.go`'s own comments (lines 103-109) document that legacy/foreign prefixes routinely violate the strict charset the comment assumes.
2. **No cross-references in either direction.** The enforcement sites (`nib.idAlphabet` in `internal/nib/id.go`, `prefixPattern` in `internal/reprefix/reprefix.go:17`) are not cited in the comment, and nothing at `prefixPattern` signals that search's emergent exclusion now depends on its charset — while `reprefix.go`'s `maxPrefixLen` comment explicitly invites loosening.

**Fix:** Make the exclusion explicit instead of emergent: gate the substring branch on the query matching the ID charset (e.g. an `isIDFragment(query)` helper checking all chars are in `[0-9a-z]`), which closes the behavioral edge outright; then narrow the comment to say operator queries are rejected by the charset gate. At minimum (doc-only alternative): cite `nib.idAlphabet` and `reprefix.ValidatePrefix` in the `matchesIDQuery` comment, and add one line at `prefixPattern` noting nibcore's ID-match exclusion depends on this charset.

---

### #2 🟡 Medium: `minIDFragmentLen`'s value is quoted verbatim in user-facing docs with no back-pointer from the constant

| | |
|---|---|
| **File** | `internal/nibcore/core.go:341` |
| **Category** | duplicate-logic / knowledge preservation |
| **Confidence** | 75 |
| **Found by** | knowledge-reviewer (SHOULD; normalized down from High per validator severity assessment) |
| **Validation** | CONFIRMED — all three quoting surfaces verified; validator assessed High as overweighted (doc-hygiene nit, one-line fix); repo has one precedent for such back-pointers (`internal/nibcontext/context.go:261`) |

**Issue:** The value 2 is quoted as prose in `internal/graph/schema.graphqls:336` ("at least 2 characters"), the regenerated `internal/graph/model/models_gen.go:97`, and `cmd/list.go:72` ("min 2 characters"). Nothing tells a future editor which doc surfaces quote the constant. Changing `minIDFragmentLen` would silently desync user- and agent-facing documentation with no failing signal. (CHANGELOG.md also quotes "2" but is a historical record, exempt; `cmd/prompt-full.tmpl` does not quote the number.)

**Fix:** Append to the `minIDFragmentLen` comment: "Quoted in user-facing docs: schema.graphqls (NibFilter.search — regenerate after editing) and cmd/list.go --search help; update those when changing this value."

---

### #3 🟡 Medium: Query normalization duplicated between test-only wrapper and production path — tests validate the non-production copy

| | |
|---|---|
| **File** | `internal/nibcore/core.go:355` (`matchesIDQuery`) duplicating `:321` (`idMatchesLocked`) |
| **Category** | duplicate-logic |
| **Confidence** | 100 (promoted — 2 independent finders) |
| **Found by** | knowledge-reviewer (Medium/COULD), design-reviewer (Low — dissent noted; validator also leans Low) |
| **Validation** | CONFIRMED — no production caller of `matchesIDQuery`; no Search-level test uses an uppercase query, so the regression scenario is real |

**Issue:** `strings.ToLower(strings.TrimSpace(query))` appears twice — in `idMatchesLocked` (production) and in `matchesIDQuery` (no production callers; only `TestMatchesIDQuery` exercises it). The table test's uppercase/whitespace rows validate the wrapper's normalization, not production's. If a maintainer drops `ToLower` from `idMatchesLocked` (plausibly "redundant" since IDs are lowercase), `TestMatchesIDQuery` stays green and no Search-level test uses an uppercase query — case-insensitivity regresses in production with all tests passing. (Whitespace trimming *is* covered end-to-end by `TestSearch_IDMatch_TrailingWhitespaceTrimmed`.)

**Fix:** Extract `func normalizeIDQuery(q string) string { return strings.ToLower(strings.TrimSpace(q)) }` and call it from both sites — or add one Search-level uppercase-query test (e.g. `core.Search("5A8K")`) so the production pipeline's case handling is pinned.

---

## Minor Findings

### Consistency

- `cmd/list.go:69-71` (also `cmd/prompt-full.tmpl:54`, `CHANGELOG.md:11`) — Docs added this round claim ID matches are "listed before full-text hits" on surfaces where that ordering never manifests: `nibs list` always passes a default ORDER sort (`buildNibSort`, cmd/list.go:273-274) and the web keyword box hardwires `sort: { field: ORDER }` (web/src/lib/queries.ts:145); `ApplySorting` discards the ID-first order. Only raw GraphQL queries without `sort` (e.g. `nibs query`) exhibit ID-first ordering. Reword or qualify the claim on those surfaces; keep it on the `nibs query` example where it is accurate. (spec-compliance-reviewer, Low, anchor 100; independently corroborated by the validator that refuted former finding A2 on the same facts, and verified by the orchestrator)
- `internal/nibcore/search_id_test.go:13` — `setupTestCoreWithPrefix` re-implements the tmpDir/MkdirAll/New/SetWarnWriter/Load boilerplate of the same-package sibling `mustLoadPrefixedCore` (mentions_test.go:17-31); every call site passes the literal "nibs-" prefix, so the parameter buys nothing. Reuse the existing helper. (consistency-reviewer, Medium, anchor 100)
- `internal/nibcore/search_id_test.go:31` — `resultIDs` duplicates the same-package helper `rawIDList` (mentions_test.go:716-722); behaviorally identical. (consistency-reviewer + test-reviewer, Low, anchor 100)
- `internal/nibcore/search_index.go:5` — `DefaultSearchLimit`'s doc ("the default maximum number of search results") now contradicts its new per-leg-cap role: `Core.Search` documents "each leg is independently capped", so combined results may reach 2× the constant. Reword the constant's doc. (design-reviewer, Low, anchor 100)
- `internal/nibcore/search_index.go:30` — `NoOpSearchIndex`'s doc ("Useful for tests that … don't need search functionality") no longer implies search inertness: Core's ID leg fires regardless of the injected index — the trap this changeset itself hit (the "NoOp"→"Bleve" query fix in search_test.go). Extend the doc to say injecting it silences only the full-text leg. (design-reviewer, Low, anchor 75)

---

## Agent Summary

| Agent | Issues Found | Unique Issues |
|-------|:------------:|:-------------:|
| quick-reviewer | 0 | 0 |
| broad-reviewer | 0 | 0 |
| knowledge-reviewer | 3 | 1 |
| consistency-reviewer | 2 | 1 |
| design-reviewer | 3 | 2 |
| test-reviewer | 1 | 0 |
| spec-compliance-reviewer | 1 | 1 |
| adversarial-reviewer | 1 | 0 |
| performance-reviewer | 0 | 0 |
| go-reviewer | 0 | 0 |
| **Total** | **8** | |

Notes:
- **Issues Found**: consolidated findings (primary + minor) attributed to the agent, shared findings counted for each finder; the validator-refuted finding is excluded.
- **Unique Issues**: findings reported only by that agent.

---

## Specialist Notes

### Requirement Coverage Matrix (spec-compliance-reviewer)

Original nibs-sn96 requirements:

| Req | Description | Status |
|-----|-------------|--------|
| R1 | ID surfaces the nib — full form, short form, fragment | Covered |
| R2 | Union of full-text hits and direct in-memory ID match | Covered |
| R3 | BM25 relevance preserved for text results | Covered |
| R4 | Home in nibcore wrapper so web + CLI both benefit | Covered (`Core.Search`) |
| R5 | Case-insensitive | Covered |
| R6 | Match both full and short forms | Covered |
| R7 | Substring on short ID + prefix on full ID; bare `nibs`/`nibs-` matches nothing | Covered |
| V1 | TDD failing test first | Unverifiable from artifacts |
| V2 | Full-text search unchanged | Covered (one justified query-literal adaptation) |
| V3 | Works in web keyword box and CLI `nibs list -S` | Covered for matching; ordering docs partial (Minor finding above) |
| V4 | build/lint/test pass | Verified by multiple reviewers (`go build`, `go vet`, `golangci-lint` 0 issues, `go test ./internal/nibcore/... -race` pass); full `task test` incl. web suite not run in review |

Iteration-2 fix items: F1 min fragment length 2 — Covered; F2 TrimSpace — Covered; F3 cap at DefaultSearchLimit — Covered (cap-after-sort, deterministic, tested); F4 doc updates on all surfaces — Partial (ordering claim inaccurate on 2 of 3 surfaces — Minor finding); F5 seam docs — Covered; F6 emergent-exclusion comment — Present but its invariant is unenforced (Finding #1); F7 hoisted lowercasing — Covered (per-ID lowering correctly retained since filename-derived IDs aren't guaranteed lowercase).

### Adversarial Probe Notes (adversarial-reviewer)

Depth tier: standard. Verified excluded: single-token Bleve operators on conforming IDs (`title:5a`, `5a8k~`, `5a*`, `+aa`) — the untokenized-substring check does real work; nib created between the unlocked index query and the RLock appears via ID leg only — benign, all consumers treat results as an opaque list; result-size doubling to 2000 + ancestors — no consumer breaks (`seen` dedupes, `includeAncestors` dedupes via `present`, `ApplySorting` length-agnostic); TrimSpace asymmetry between legs — no divergent outcome constructible; deterministic cap dropping an exact match — not constructible at realistic scale.

### Validation Wave

4 validators dispatched (all primary findings: 1 High corroborated-with-dissent, 1 Medium corroborated-with-dissent, 1 single-finder High, 1 single-finder Low), mid-tier per mode policy. Results: #1 confirmed (line corrected to core.go:343); #2 confirmed (severity normalized High→Medium per validator assessment — validation may lower, never raise); #3 confirmed; former #4 (minIDFragmentLen=2 web noise, adversarial-reviewer, Low@75) **refuted** — see below. No findings waived (both multi-finder findings carried dissenting severities and were validated).

### Considered But Not Flagged (all agents)

**Refuted by validator:**
- minIDFragmentLen=2 first-screen noise in web search-as-you-type (adversarial-reviewer, Low@75) — refuted: the web UI's only nibs query hardwires `sort: ORDER`, and `ApplySorting` fully re-sorts, so spurious ID matches are interleaved at natural tree position, never pinned above relevance hits; residual effect is ~0.23% spurious inclusion, interleaved. The math and the no-debounce observation were accurate; the consequence was not.

**Suppressed by the confidence gate (anchor < 75):**
- BM25 relative-scoring assertion in `TestSearch_IDMatch_UnionWithTextHits` couples the test to Bleve internals — could flip on a Bleve version bump with no change to code this repo owns (test-reviewer Medium@50; broad-reviewer noted the same in its own CBNF). Deterministic today across repeated `-race` runs; both signals point the same way.
- Direct manipulation of unexported `core.nibs`/`core.mu` in `TestIDMatchesLocked_CappedAtDefaultSearchLimit` (test-reviewer Low@50; go-reviewer concurred at 25 — no race, no watcher goroutine, `-race` clean; rationale comment present).
- `NibReader.Search` (internal/graph/interfaces.go:13) carries no ordering contract while the GraphQL schema now promises one — an alternate implementation could silently break the documented ordering (design-reviewer Low@50).

**Dismissed with sound reasoning (grouped by agent):**
- quick: `matchesIDQuery` wrapper without production caller (@25 — defensible test seam; actionable slice captured in Finding #3); CLI help omits "whitespace trimmed" detail (@25 cosmetic); prefix branch bypassing min length — intentional and test-pinned; 2× combined limit — documented; gofmt drift — verified pre-existing via `git stash`.
- broad: same wrapper/gofmt/2×-cap conclusions; verified the "NoOp"→"Bleve" test change was necessary, not cosmetic; swept other tests for accidental ID-matching queries — none.
- knowledge: "~10%" one-char-match claim verified accurate (1−(35/36)⁴ ≈ 10.7%); Bleve keyword-field claim verified against internal/search/index.go:40-45; hyphen-in-prefix technicality absorbed by Finding #1.
- consistency: prompt-full.tmpl terseness matches that file's own one-line-gloss convention; `sort.Slice` matches package precedent; Locked-suffix naming correct; CHANGELOG format matches siblings.
- design: pathological-prefix invariant concern (@25) — absorbed into Finding #1's theme; lock discipline verified sound (both legs read one `c.nibs` snapshot under a single RLock; `c.config` never reassigned); future ID-native SearchIndex tolerated by dedup.
- spec-compliance: minIDFragmentLen=2 vs the spec's "fragment" expectation — no conflict (spec's own anti-flood clause sanctions a floor; full-form prefix branch exempt); empty-prefix config-drift edge (@25); dual-match nib leaving its BM25 slot — inherent to accepted design.
- performance: per-nib `strings.ToLower(id)` (@50 — Go fast path returns the original string with no allocation when unchanged; not a regression from this round); sort-then-cap O(m log m) (@50 — the cap-after-sort is a determinism fix); `TrimPrefix` before length check (@25).
- go: `matches[:DefaultSearchLimit]` backing-array retention — false positive (elements copied into fresh slice; confidence 0); `sort.Slice` vs `slices.SortFunc` (@50 — package precedent); `map[string]bool` vs `struct{}` — both coexist in package; test's manual RLock (@25, see suppressed).

---

## Recurring Findings

| File | Category | Occurrences | First Seen |
|------|----------|-------------|------------|
| `internal/nibcore/core.go` | knowledge preservation / implicit invariant | 2 | 2026-07-03 (iteration 1 #5: emergent exclusion undocumented → this round documented it; iteration 2 #1: the documented invariant is unenforced) |
| `cmd/list.go` + search doc surfaces | doc-surface accuracy | 2 | 2026-07-03 (iteration 1 #1: ID matching undiscoverable in docs → docs added; iteration 2 Minor: added ordering claim overstates behavior on sorted surfaces) |
