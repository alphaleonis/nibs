# Code Review

**Mode**: mid (explicit) | **Reviewers**: quick-reviewer, broad-reviewer, knowledge-reviewer, consistency-reviewer, design-reviewer, test-reviewer, spec-compliance-reviewer, adversarial-reviewer, performance-reviewer, go-reviewer | **Date**: 2026-07-03
**Source**: local changes (uncommitted — bug fix for work item nibs-sn96)
**Scope**: 3 files changed (2 modified, 1 new), +254/-5 lines
**Spec**: work item nibs-sn96 — `.nibs/nibs-sn96--keyword-search-does-not-match-nib-ids.md` (explicit)
**Validation**: 4 confirmed, 0 refuted, 0 uncertain, 3 waived (corroborated)

## Agent Selection Rationale

Mode was given explicitly (`mid`): floor + gate-matched specialists, cost-aware tiering.

Review team:
- quick-reviewer (always — review floor)
- broad-reviewer (always — review floor)
- knowledge-reviewer — new ID-matching semantics embed behavioral decisions
- consistency-reviewer — new helpers sit beside existing ID-normalization siblings (`normalizeIDInMap`, `mustLoadPrefixedCore`)
- design-reviewer — `Core.Search` result contract changed; new lock-held helper touches the concurrency surface
- test-reviewer — test files in changeset (hard gate)
- spec-compliance-reviewer — spec available: work item nibs-sn96 (hard gate)
- adversarial-reviewer — ≥50 changed executable lines
- performance-reviewer — per-query loop over the full nib map with allocation and sort
- go-reviewer — Go files in changeset (hard gate)
- security-reviewer: skipped — no security-adjacent surface (local in-memory search; no auth/crypto/network/serialization)
- data-migration-reviewer: skipped — no migration artifacts in diff (hard gate)
- dotnet-/typescript-/cpp-/rust-reviewer: skipped — no such files in changeset (hard gates)
- prior-feedback-reviewer: skipped — not a PR review (hard gate)

Model tiering (mid): judgment agents (knowledge, design, spec-compliance, adversarial) on the session model; volume agents (quick, broad, consistency, test, performance, go) and the finding-validators on the mid-tier model.

## Summary

| Severity | Count |
|----------|-------|
| 🔴 Critical | 0 |
| 🟠 High | 0 |
| 🟡 Medium | 2 |
| 🟢 Low | 5 |
| 🔵 Minor | 4 |

Critical/High/Medium/Low are **primary** findings and drive the verdict. **Minor** counts the reported-but-non-blocking findings (Consistency / Testing Gaps / Residual Risks). Pre-existing issues are listed separately and excluded from both.

**Verdict**: ✅ APPROVED

The mechanism itself is sound: lock discipline is correct (`go test -race` clean), the spec's agreed matching semantics are fully implemented and tested, full-text behavior is unchanged, and build/lint/tests all pass. All primary findings are documentation-surface gaps and cheap behavioral tunings around the new capability, not flaws in the fix.

---

## Findings

### #1 🟡 Medium: Search documentation surfaces still describe pure Bleve full-text semantics — ID matching is undiscoverable

| | |
|---|---|
| **File** | `internal/graph/schema.graphqls:331-346` (also `:20`), `cmd/list.go:57-69` and `:282`, `cmd/prompt-full.tmpl:54,332` |
| **Category** | knowledge preservation / API contract docs |
| **Confidence** | 100 (doc-vs-code contradiction, quotes verified) |
| **Found by** | broad-reviewer (Medium), knowledge-reviewer (Medium), design-reviewer (Medium), spec-compliance-reviewer (Low) — validation waived, corroborated ×4 |

**Issue:** Every documented contract for the search surface still says search is purely "Full-text search across slug, title, and body using Bleve query syntax" — the GraphQL `NibFilter.search` schema doc (the primary agent-facing surface via introspection), the CLI `--search/-S` help block and flag description ("Full-text search in title and body"), and the `nibs prime` template (`prompt-full.tmpl`, per CLAUDE.md "the primary interface for AI agents"). None mention the headline capability this fix adds: ID and ID-fragment matching, which follows different rules than Bleve syntax (`5a8k` ID-matches, but `"5a8k"` quoted, `5a8k~` fuzzy, and `id:5a8k` do not). `schema.graphqls:20` also still describes the implicit search ordering as pure relevance; it is now "ID matches first (sorted by ID), then relevance." The `Core.Search` godoc was carefully updated, but that comment is invisible at every consuming boundary. A user or agent reading the docs is effectively told that typing an ID won't work — the exact confusion nibs-sn96 was filed to eliminate.

**Fix:** Add one or two sentences to the `NibFilter.search` schema description, the `-S` help block/flag description, and `prompt-full.tmpl`: queries that look like a nib ID or ID fragment (single token; substring of the short ID or prefix of the full ID, case-insensitive) also match directly and are returned first. Note: `cmd/list.go:282` omitting "slug" is pre-existing; the ID-matching omission is introduced by this change. The nib's verification checklist has no doc-update item, so this will be silently lost unless done before commit.

---

### #2 🟡 Medium: Per-keystroke web search × substring ID matching floods results for 1-2 character queries

| | |
|---|---|
| **File** | `internal/nibcore/core.go:341-342` (substring branch), `web/src/lib/components/FilterBar.svelte:67-70,118`, `internal/graph/schema.resolvers.go:754-756` |
| **Category** | emergent behavior / relevance degradation |
| **Confidence** | 100 (empirically reproduced by finder and validator) |
| **Found by** | adversarial-reviewer (Medium), design-reviewer (Low) — validator: confirmed, Medium defensible |

**Issue:** The substring branch has no minimum query length. With 4-char short IDs over the 36-char alphabet, a 1-char query matches ≈10.7% of all nibs (1−(35/36)⁴). The web FilterBar fires the search on every `oninput` with no debounce or minimum length, and ID matches are prepended ahead of relevance-ranked text hits. Empirically on this repo's ~252-nib dataset: `nibs list -S a` returns 40 data rows (27 direct short-ID matches plus ancestors pulled in by `includeAncestors`), `-S e` returns 36, while a real word query returns 1. Validator correction: the pre-change baseline was letter-dependent, not uniformly zero (0 results for stopword `a`; ~5.5% for `e` via full-text) — but the change still measurably floods the first keystrokes of every web search and short `nibs list -S` terms. No data hazard (drag reorder is already disabled during search).

**Fix:** Require a minimum substring length (2, arguably 3) in `matchesIDQuery`'s substring branch. Validator verified this is spec-compatible: nibs-sn96 mandates matching down to 3-char fragments (`5a8`) and is silent on shorter; the min-length guard conflicts with none of the spec's examples. A web-layer debounce would help the UI but not CLI/GraphQL callers.

---

### #3 🟢 Low: `SetSearchIndex`/`SearchIndex` seam docs no longer describe what the seam controls

| | |
|---|---|
| **File** | `internal/nibcore/core.go:96-98`, `internal/nibcore/search_index.go:8-13` |
| **Category** | contract drift / knowledge preservation |
| **Confidence** | 100 (doc-vs-behavior drift; proven by the changeset's own test edit) |
| **Found by** | design-reviewer (Low), adversarial-reviewer (Low) — validation waived, corroborated ×2 |

**Issue:** `SetSearchIndex`'s doc says Core uses the injected index "instead of lazily initializing a Bleve index", and the `SearchIndex` interface doc says it "abstracts full-text search so that nibcore.Core can work with pluggable implementations." After this change an injected index no longer fully determines search results: `Core.Search` unions in-memory ID matches on top of whatever the index returns — an injected no-op index no longer means "search returns nothing," which is precisely why `TestSearch_WithInjectedNoOpIndex` had to change its query from "NoOp" to "Bleve". Only test doubles inject today, but a future implementation (persistent, remote, or result-filtering, e.g. permission-scoped) cannot control or opt out of the ID-match layer, and ID matches computed from `c.nibs` would leak around it.

**Fix:** One sentence on the interface and/or `SetSearchIndex` doc: Core unions direct ID matches on top of index results; implementations control only the full-text leg.

---

### #4 🟢 Low: ID-match leg is unbounded while the full-text leg is capped at `DefaultSearchLimit`

| | |
|---|---|
| **File** | `internal/nibcore/core.go:285` vs `core.go:294,317-327` (`DefaultSearchLimit` at `search_index.go:6`) |
| **Category** | design consistency |
| **Confidence** | 75 (validated) |
| **Found by** | broad-reviewer (Low–Medium), design-reviewer (Low); considered-and-dismissed by knowledge-, spec-compliance-, and adversarial-reviewer — validator: confirmed at Low, should stand |

**Issue:** Bleve hits are capped at `DefaultSearchLimit` (1000); the prepended ID-match set has no cap, so `Search` can exceed the only limit named in this path. Validator corrections: the `Search` godoc does not itself promise a total limit (the original "violates its own documented limit" framing was too strong), and no pagination exists anywhere downstream today — exceeding 1000 ID matches would require roughly 9,300+ nibs at the worst-case 1-char match rate, an order of magnitude beyond this tool's design envelope, with no memory-exhaustion vector (results are bounded by the resident nib count). The residual value is hygiene: if pagination or a caller-supplied limit is ever added, the ID-match layer sits outside that mechanism and will need an explicit budget decision.

**Fix:** Either cap `idMatchesLocked` output (or slice the combined result) to `DefaultSearchLimit`, or document in the doc comment why the ID leg is intentionally unbounded.

---

### #5 🟢 Low: The multi-word/Bleve-syntax exclusion from ID matching is emergent and undocumented in production code

| | |
|---|---|
| **File** | `internal/nibcore/core.go:329-343`, `internal/nibcore/search_id_test.go:144,158-159,191` |
| **Category** | knowledge preservation / implicit invariant |
| **Confidence** | 100 for the factual core (validated); maintainer-risk scenarios are [Inference] |
| **Found by** | knowledge-reviewer (Medium), consistency-reviewer (minor), test-reviewer (minor) — validator: confirmed, severity corrected Medium → Low |

**Issue:** The intended "single-token queries only" rule is not encoded anywhere: `matchesIDQuery` has no whitespace/word-count check — the exclusion falls out of the ID alphabet (`[0-9a-z]` per `internal/nib/id.go:11` plus prefix), since a query containing spaces or Bleve operators can never substring/prefix-match an ID. Meanwhile the tests (`TestSearch_IDMatch_MultiWordQuerySkipsIDMatching`, table case "multi-word query skips id matching") are named as if an explicit rule exists. A future maintainer who tokenizes the query would see tests fail with no stated rationale for why the exclusion is load-bearing. Validator nuance: the "whitespace-containing prefix quietly widens matching" scenario is only reachable via unvalidated paths (hand-edited `.nibs.yml`, GraphQL per-request prefix override) — the CLI paths (`nibs init --prefix`, `nibs config set-prefix`) validate via `ValidatePrefix` (`internal/reprefix/reprefix.go:30-38`), which rejects whitespace.

**Fix:** 1-2 sentences on `matchesIDQuery` (or `idMatchesLocked`): the whitespace/Bleve-syntax exclusion is intentional and emergent from the ID alphabet — do not tokenize the query here. Optionally note it in nibs-sn96's fix notes.

---

### #6 🟢 Low: Trailing-whitespace ID paste silently gets no ID match — the headline paste scenario regresses to the original bug

| | |
|---|---|
| **File** | `internal/nibcore/core.go:333-343`, `internal/nibcore/search_id_test.go:192` |
| **Category** | assumption violation / UX |
| **Confidence** | 75 (validated; empirically reproduced) |
| **Found by** | adversarial-reviewer (Medium-Low); considered-and-dismissed by design- and spec-compliance-reviewer — validator: confirmed at Low, fix recommended |

**Issue:** Pastes from terminal output, tables, or double-click selection commonly carry a trailing space, and any space disables ID matching: `nibs list -S 'sn96'` finds nibs-sn96; `nibs list -S 'sn96 '` returns "No nibs found" — silently reproducing the original bug symptom for the feature's own headline scenario. The test pins trailing-space → no-match as expected, but that encodes the emergent behavior rather than a considered choice. The "self-correcting on next keystroke" dismissal only applies to interactive boxes (nothing auto-trims anywhere: web FilterBar, CLI, and TUI all pass the raw string through), and not at all to one-shot CLI invocations.

**Fix:** `query = strings.TrimSpace(query)` at the top of `matchesIDQuery`. Validator traced all affected tests: only the `"id fragment with trailing space"` table row flips (expected `false` → `true`; update its name/comment accordingly); genuine multi-word queries (internal spaces) are untouched, and the Bleve leg is untouched since the trim is local to the ID matcher. Nothing else in the repo depends on trailing-space-means-no-match.

---

### #7 🟢 Low: `matchesIDQuery` re-lowercases loop-invariant `query`/`prefix` on every iteration

| | |
|---|---|
| **File** | `internal/nibcore/core.go:333-343` (called per-nib from `core.go:320-323`) |
| **Category** | hot-path waste (minor) |
| **Confidence** | 100 (deterministic code fact) |
| **Found by** | performance-reviewer (Low), go-reviewer (Low), broad-reviewer (trivial) — validation waived, corroborated ×3 |

**Issue:** `idMatchesLocked` calls `matchesIDQuery` once per entry in `c.nibs`, and each call re-lowercases `query` and `prefix` even though only `id` varies — 2 redundant `strings.ToLower` allocations per nib per search, while holding `c.mu.RLock()` (blocking writers). Negligible at current scale, but pure waste in brand-new code with a trivial fix.

**Fix:** Lowercase `query` and `c.configPrefix()` once in `idMatchesLocked` before the loop and pass pre-lowered values down, keeping only `id = strings.ToLower(id)` per iteration (or keep the pure signature and accept the cost — but hoisting is free now).

---

## Pre-existing Issues

### P1 🟢 Low: `core.go` is gofmt-dirty at HEAD (import ordering, struct alignment)

| | |
|---|---|
| **File** | `internal/nibcore/core.go` |
| **Category** | formatting / process |
| **Confidence** | 100 |
| **Found by** | quick-reviewer (pre_existing), go-reviewer (pre_existing) |

**Issue:** `gofmt -l` flags `internal/nibcore/core.go` both before and after this diff (import-group ordering of `internal/nib`/`internal/config`/`internal/search`, struct-literal alignment). Verified against `git show HEAD:...` — pre-existing, not introduced or worsened. `golangci-lint` (what `task lint` runs) does not flag it, so the repo's lint gate passes; a drive-by `gofmt -w` at some point would clear it, outside this changeset.

---

## Minor Findings

### Consistency

- `internal/nibcore/search_id_test.go:12-28` — `setupTestCoreWithPrefix` duplicates the existing same-package helper `mustLoadPrefixedCore` (`internal/nibcore/mentions_test.go:15-31`): identical `New(nibsDir, config.DefaultWithPrefix(...))` + `SetWarnWriter(nil)` + `Load()` sequence, and all 5 call sites pass the exact `"nibs-"` literal the existing helper hardcodes. The new helper also returns bare `*Core`, breaking the `(*Core, string)` return shape of every sibling setup helper. (consistency-reviewer; test-reviewer noted the duplication mirrors the `setupTestCoreWithRequireIfMatch` precedent, but the more-specific existing helper makes direct reuse trivial here)
- `internal/nibcore/search_id_test.go:30-36` — `resultIDs` duplicates `rawIDList` (`internal/nibcore/mentions_test.go:714-722`): same package, same behavior (extract `.ID` preserving order), same stated purpose (legible error messages). (consistency-reviewer)
- `CHANGELOG.md` — no `[Unreleased]` entry for this user-visible bug fix. Evidence on the convention is mixed: fix commits `6ac676d` and `1194968` carried their own CHANGELOG entries, while feature commit `ade965e` did not; knowledge-reviewer read the `chore: changelog for vX` commits as batch-at-release and dismissed this. Flagged so it isn't forgotten before commit. (broad-reviewer; dissent from knowledge-reviewer)

### Testing Gaps

- `internal/nibcore/search_test.go:398-410` — the updated comment says "Core-level ID matching works even without an index", but no test positively asserts that (e.g. `core.Search("noop1")` returning the nib under `NoOpSearchIndex`); the test only asserts a non-matching query returns 0 results. The production code structurally guarantees it (`idMatchesLocked` never touches `c.searchIndex`), so risk is low — one added assertion would make the test suite match the claim. (broad-reviewer)

---

## Agent Summary

| Agent | Issues Found | Unique Issues |
|-------|:------------:|:-------------:|
| quick-reviewer | 1 | 0 |
| broad-reviewer | 5 | 2 |
| knowledge-reviewer | 2 | 0 |
| consistency-reviewer | 3 | 2 |
| design-reviewer | 4 | 0 |
| test-reviewer | 1 | 0 |
| spec-compliance-reviewer | 1 | 0 |
| adversarial-reviewer | 3 | 1 |
| performance-reviewer | 1 | 0 |
| go-reviewer | 2 | 0 |
| **Total** | **12** | |

Notes:
- **Issues Found**: Total consolidated findings attributed to this agent (primary + minor + pre-existing; shared findings count for each finder)
- **Unique Issues**: Findings reported ONLY by this agent and no other
- Total = 7 primary + 4 minor + 1 pre-existing = 12 distinct consolidated findings

---

## Specialist Notes

### Requirement Coverage Matrix (spec-compliance-reviewer)

Verdict: **Compliant** — all agreed matching semantics from nibs-sn96 are implemented and tested; no requirement gaps, no scope creep.

| # | Requirement (from nibs-sn96) | Status | Evidence |
|---|---|---|---|
| R1 | Typing an ID/fragment surfaces that nib | Met | `Search` union in `core.go:294-310`; `TestSearch_IDMatch_ShortIDExact` / `_FullIDExact` |
| R2 | Case-insensitive | Met | `ToLower` on query/id/prefix (`core.go:334-336`); uppercase-query table cases |
| R3 | Full form: prefix match (`nibs-5a8k`, `nibs-5a` match; `nibs-a8k` doesn't) | Met | `core.go:338-339`; "full form exact/prefix/non-prefix fragment" cases |
| R4 | Short form: substring match (`5a8k`, `5a8`, `a8k`) | Met | `core.go:341-342`; "short id substring" cases |
| R5 | Bare `nibs` / `nibs-` must not drag in every nib | Met | `len(query) > len(prefix)` guard; `TestSearch_IDMatch_BarePrefixDoesNotMatchAll` |
| R6 | Union of full-text hits + ID matches, nib appears once | Met | dedup via `seen` map (`core.go:295-309`); `TestSearch_IDMatch_UnionWithTextHits` |
| R7 | BM25 relevance preserved for text results | Met | text hits appended in Bleve order; union test asserts `cc33` before `bb22` |
| R8 | Fix home benefits both web and CLI | Met | `Core.Search` is the single path: resolver `schema.resolvers.go:740` ← web keyword filter and `cmd/list.go:87-88` |
| R9 | Full-text search unchanged | Met | zero changes to `internal/search/`; existing suite passes with one necessary query swap |
| V1 | TDD: failing test first | [Unverified] | write-order cannot be established from an uncommitted snapshot; by inspection the new tests are genuinely red against the old code |
| V2 | Full-text-unchanged verification | Met | `go test ./...` all green (fresh `-count=1` run of nibcore) |
| V3 | Works in web keyword box and CLI `-S` | Met [structural] | both consume the `Nibs` resolver → `Core.Search` in-process; code path verified, not a live end-to-end run |
| V4 | `task build` (no warnings), `task lint`, `task test` | Met (Go side, verified by running) | build clean, lint 0 issues, Go tests pass; web vitest half not run (no web files touched) |

### Adversarial Probe Notes (adversarial-reviewer)

Depth tier: DEEP — full consumer trace (GraphQL resolver, CLI `list -S`, web UI, injected-index seam) plus empirical probes of the patched binary against this repo's real ~255-nib dataset. Bleve parse-error probes (`-`, `title:`) error out before the ID-match union runs, but realistic ID-like pastes (`sn96"`, `sn96 AND`, `(`) parse cleanly; only non-ID-like inputs error, and those cannot substring-match alphanumeric short IDs.

### Validation Wave

4 findings validated (mid-tier validators), 3 waived as corroborated quotable facts:

- **#2 (short-query flood)**: confirmed. Corrections applied: pre-change baseline is letter-dependent (0 for stopword `a`, ~5.5% for `e`), row counts corrected to data rows (40/36); validator additionally verified the min-length fix is provably spec-compatible.
- **#4 (unbounded ID leg)**: confirmed at Low — should stand as a hygiene finding, not be dismissed; "documented limit" framing softened (the godoc promises no total limit).
- **#5 (emergent exclusion)**: confirmed; severity corrected Medium → Low (tests fail loudly if the invariant breaks; prefix-widening only reachable via unvalidated config paths — CLI paths validate via `ValidatePrefix`).
- **#6 (trailing-space paste)**: confirmed at Low with fix recommended; validator traced the proposed `TrimSpace` against all tests and consumers — only one table row flips, no adverse interactions.

### Considered But Not Flagged (all agents)

**Suppressed at consolidation:**
- `seen` as `map[string]bool` vs `map[string]struct{}` (consistency-reviewer, medium confidence) — suppressed: package evidence is mixed; nibcore itself uses `map[string]bool` in `migrate.go:10`, `link_health.go:135-137`, `link_queries.go:19` (go-reviewer verified), while `mentions.go` uses `struct{}` for its dedup unions. Both idioms are established in-package.
- `prefix` vs `configPrefix` parameter naming (consistency-reviewer, low confidence) — suppressed: pre-existing split between shared-helper naming (`configPrefix`) and local-scratch naming (`prefix`); the new helper plausibly falls in either bucket.
- Duplicated one-line sort-by-ID comparator (`core.go:325` vs `mentions.go:111`) (consistency-reviewer) — suppressed: go-reviewer verified the identical pattern at `mentions.go:111` is the local package precedent; extracting a helper for a one-line lambda is judgment, not drift.
- O(N) full scan of `c.nibs` per `Search()` call (quick-reviewer note) — dismissed by performance-reviewer with quantitative reasoning: sub-millisecond at this tool's scale (hundreds to low-thousands of in-memory nibs); revisit only if scale assumptions change.

**By agent (highlights):**
- **quick-reviewer**: Bleve-syntax queries (quotes/operators/field prefixes) intentionally get no ID-priority; mixed-case sort ordering can't manifest (generated IDs are lowercase); archived nibs in ID matching match the pre-existing Bleve-path behavior.
- **broad-reviewer**: asymmetric prefix-vs-substring semantics deliberate and tested; BM25 ordering test verified passing; GraphQL `ApplySorting` confirmed — explicit sort fully overrides the new implicit order, consistent with schema docs.
- **knowledge-reviewer**: fix-placement rationale adequately recorded in nibs-sn96; bare-prefix guard rationale preserved across three surfaces; `search_test.go` comment precisely captures the forced query change.
- **test-reviewer**: BM25 ordering in `TestSearch_IDMatch_UnionWithTextHits` verified deterministic (structural score gap; ran 100×, no flakes); random-ID collision impossible (all test nibs set IDs explicitly); every existing `search_test.go` query checked against every literal ID — no accidental new ID-match hits.
- **design-reviewer**: lock discipline correct (`idMatchesLocked` under RLock; `configPrefix` reads immutable state); snapshot skew between unlocked Bleve search and RLock'd scan benign, pre-existing in shape; `matchesIDQuery` deliberately placed apart from `normalizeIDInMap` (fuzzy search matching vs exact identity resolution); archived nibs appear symmetrically in both union halves; the new ordering promise is invisible to CLI/web (both re-sort) — only raw GraphQL callers see it.
- **spec-compliance-reviewer**: fix in `Core.Search` satisfies the spec's "nibcore wrapper" option in substance; ID-matches-first inter-block ordering is spec-silent and reasonable; `"NoOp"` → `"Bleve"` necessary, not test-weakening; nib checkboxes still unchecked / status in-progress — close-out hygiene for uncommitted work.
- **adversarial-reviewer**: Bleve parse errors abort achievable ID matches only for non-ID-like inputs — not worth restructuring the lock flow; leading-hyphen `-sn96` returns everything except the target (Bleve negation, pre-existing); foreign-prefix nibs degrade gracefully to substring-on-full-ID; `Close()` racing `Search` pre-existing; uppercase Bleve operators (`AND`/`OR`) as ID fragments vanishingly rare.
- **performance-reviewer**: `sort.Slice` reflection overhead negligible at 0-5 element result size; RLock hold-time extension microseconds at scale; no N+1, unbounded growth, or missing pagination introduced.
- **go-reviewer**: `go test -race` clean; `TrimPrefix`/`HasPrefix` empty-prefix semantics correct and tested; map iteration nondeterminism neutralized by the sort; test idioms match sibling tests exactly; no goroutine/channel/context/typed-nil/error-discipline issues (constructs not touched).
- **consistency-reviewer**: `idMatchesLocked` lock-comment phrasing matches `computeStoredETag` precedent; "reports whether" doc phrasing is repo-wide convention; new-file-per-concern test split matches package practice.
