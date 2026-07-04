# Code-Review Session Report — nibs-p07b (2026-07-04)

**Subject:** Re-base the non-modal Settings panel OFF bits-ui `Dialog` (which hardcodes `aria-modal="true"`, unoverridable) onto a hand-wired non-modal panel — `<aside role="dialog" aria-modal="false">` via bits-ui `Portal`, `transition:fly`, a new reusable `clickOutside` action, document-level Escape, and focus in/return — plus deletion of the orphaned `ui/sheet/` (11 files). **Changeset character:** interaction- and accessibility-heavy Svelte 5 / TypeScript web-UI change — a real re-implementation of dismissal + focus behavior the deleted Dialog primitive had provided for free (~90 executable production lines changed + 2 test files), with no security/DB/concurrency/migration surface but genuine event-loop-timing and focus-management subtlety.

**Skill chain:** `/decaf-build:auto-dev p07b --report` → implementation subagent → `/decaf-quality:auto-code-review std --max-iterations 3 --report` → two `/code-review` waves (`mid`, then `mid6`). Baseline: `develop` @ `e83df37`.

## 1. Iteration overview

| Iter | Mode | Scope | Verdict | Primary findings | Minor | Validation wave | Fixes applied after |
|------|------|-------|---------|------------------|-------|-----------------|---------------------|
| 1 | mid (9 reviewers; gate-skips only) | full uncommitted p07b changeset | ❌ NEEDS_CHANGES | 1 🟠 High · 2 🟡 Med | 10 | 3 confirmed / 0 refuted | 5 fixed (1 TDD) + hardening; 1 deferred |
| 2 | mid6 (6 reviewers; scoped to 4 modified files) | fix-round delta | ✅ APPROVED | 1 🟡 Med | 5 | 1 refuted (the sole High candidate) | 1 Med + 2 minors (post-approval, inline) |

**Totals:** fixed 8 (1 TDD) · deferred 1 (→ `nibs-bpyh`) · refuted 1 (a High) · skipped 0 · final state APPROVED + a post-approval quality pass, all gates green (build clean, lint 0, web 706/706).

- The loop earned its cost: iteration 1's **High was a genuine regression** — Escape was bound to the portaled `<aside>`, so once the user Tabbed into the (deliberately reachable) non-modal background, Escape no longer closed the panel. That directly defeats the non-modal purpose this nib exists for, and no test caught it (the existing Escape test fired with focus still inside). Caught by 4 reviewers, validator-confirmed, fixed via a document-level listener.
- Iteration 2 **prevented an over-fix**: a "focus-theft on outside-click" High was refuted by tracing the installed Svelte 5.55.0 scheduler (state set in a native pointerdown listener flushes via `queueMicrotask`, between `pointerdown` and `mousedown`, so the clicked element keeps focus). Without validation this would have driven an unnecessary behavioral change.
- Empirical verification, not just reading: iteration 2 instrumented the new Escape listener's add/remove balance across a 7-step open/close/reopen/unmount sequence (3 adds / 3 removes / 0 leaked) and mutation-tested the four scrutinized tests.

## 2. Agent inventory

**Iteration 1 (mid, explicit):** quick, broad (floor); knowledge, consistency, design (substantive-change specialists); adversarial (>50 exec lines); test, typescript (hard gates); spec-compliance (spec available). Skipped by gate/risk: security, performance, prior-feedback, data-migration, dotnet/cpp/go/rust. **No roster cap** (mid). 3 validators dispatched (findings #1/#2/#3).

**Iteration 2 (mid6 — roster capped to 6, scoped to the 4 modified files):** quick, broad (floor); typescript, test, adversarial, design (best-fit specialists). **Dropped by the mid6 cap vs iter 1:** knowledge, consistency, spec-compliance. 1 validator (focus-theft). The cap fit the delta — a small event-handling/focus fix; natural ablation showed no dropped-lane finding was missed (spec-compliance had already passed clean; consistency's iter-1 items were fixed).

**Non-reviewer agents (harness-reported usage):**

| Agent | Role | Tokens | Tool calls | Duration |
|-------|------|-------:|-----------:|---------:|
| implementation subagent (auto-dev Step 2) | general-purpose | 91,758 | 27 | 384.6s |
| iter-1 review orchestrator | general-purpose | 153,243 | 31 | 1,787.4s |
| iter-1 fix round | general-purpose | 77,454 | 31 | 326.3s |
| iter-2 review orchestrator | general-purpose | 146,885 | 26 | 2,134.5s |
| iter-2 post-approval fix pass | **main context, inline — not a subagent** | not reported | not reported | not reported |

[Unverified] whether each review-orchestrator figure includes its child reviewer/validator subagents.

## 3. Token usage

**Review-side — reviewers + validators** (verbatim from each iteration's Session Metrics):

*Iteration 1 (mid):* quick 82,601 · broad 120,719 · knowledge 74,421 · consistency 109,293 · design 63,164 · adversarial 32,895 (stub) + 74,216 (retry) · test 90,434 · typescript 86,282 · spec-compliance 63,887 · validators 57,084 + 85,915 + 56,854. **Subtotal ≈ 997,765** (includes the wasted 32,895 adversarial stub).

*Iteration 2 (mid6):* quick 131,733 · broad 157,648 · typescript 105,491 · test 133,610 · adversarial 33,894 (failed) + 81,676 (retry) · design 77,195 · validator 69,962. **Subtotal ≈ 791,209** (includes the wasted 33,894 adversarial stub).

**Sub-totals & ratio:**
- Review-side reviewers+validators: **≈ 1,788,974** across both iterations. Plus orchestrators 153,243 + 146,885 = **300,128** (children-inclusion [Unverified]).
- Build-side metered: implementation 91,758 + iter-1 fix round 77,454 = **169,212**. The iter-2 post-approval fix pass (inline) is **not reported**.
- **build : review ratio** ≈ 169,212 : 1,788,974 ≈ **1 : 10.6** on metered subagent tokens (reviewers+validators only). [Inference] directional — build-side excludes the unmetered inline fix pass; review-side excludes orchestrators + main-context triage.
- **Waste:** two adversarial stub dispatches burned **66,789** tokens (32,895 + 33,894) producing zero findings (see §4).

**Unmeasured:** main-context orchestration (planning/triage/report) and the iter-2 inline fix pass — recorded as missing, not estimated.

## 4. Process observations

- ⚠️ **deviation / 💡 tuning candidate — adversarial-reviewer failed its FIRST dispatch in BOTH iterations.** Iter 1: a memory-context-only stub (0 tool calls, ~2.4s). Iter 2: **corrupted output containing an injection-looking "enable more verbose responses" verbosity-toggle string** (0 tool calls, ~5.5s). Both recovered via re-dispatch with a hardened prompt. Two-for-two on the same agent is a pattern, not noise — and the iter-2 payload resembling an injected instruction is worth scrutiny. Highest-value signal in this session.
- ⚠️ **deviation — post-approval fix pass at iteration 2.** The loop reached APPROVED (Step 6 = done), but the surviving Medium was a `try/finally` cleanup defect in a test *this session's own fix round added* (leaks a stray `<button>` into the shared DOM on failure), plus two cheap on-topic minors (`setTimeout(0)`→`tick()`; `focus({ preventScroll: true })`). Applied inline + re-verified with the full gate; no re-review (test-only + one flag). Same off-loop pattern as the qj7m session — recurring enough to be a candidate for an explicit in-loop "APPROVED-with-self-introduced-defects" branch.
- ✅ **held — validation prevented an over-fix.** The iter-2 "focus-theft" High was refuted by scheduler tracing; the loop correctly did NOT change behavior, routing only a Low `preventScroll` residual to Minor (which the post-approval pass then took).
- ✅ **held — the High was real and the fix verified.** Document-level Escape listener; add/remove balance and no-op-when-closed empirically instrumented in iter 2.
- ✅ **held — mid6 cap fit the delta.** Scoped re-review of 4 files with 6 agents; dropped lanes (knowledge/consistency/spec) showed no missed evidence.
- ✅ **held — deletion hygiene.** `ui/sheet/` removal verified free of dangling imports; only the in-scope changes + expected deletions remained in the tree after both waves' throwaway probe files were cleaned up.
- ✅ **held — clean deferral.** The `clickOutside` portaled-descendant gap was correctly deferred (no live bug; validator corrected the "vmaq will break" urgency against nibs-qj7m) → `nibs-bpyh`.

## 5. Timeline

| Phase | Wall-clock |
|-------|-----------:|
| Implementation subagent | 384.6s |
| Iter-1 review orchestrator (end-to-end) | 1,787.4s (~29.8 min) |
| Iter-1 fix round | 326.3s |
| Iter-2 review orchestrator (end-to-end) | 2,134.5s (~35.6 min) |
| Iter-2 post-approval fix pass + gate | not separately metered |
| Longest single reviewer | iter-2 broad-reviewer 1,115.6s (~18.6 min) |

The two review waves dominate wall-clock (~65 min combined), each gated by its slowest reviewer (iter-2 broad ~18.6 min). [Inference] reviewers ran concurrently, so wave time ≪ sum of reviewer durations. Non-productive share: the two adversarial stubs (~8s wall-clock but a full re-dispatch each, extending both waves).

## 6. Per-agent yield

| Agent | Iter 1 (found/unique) | Iter 2 (found/unique) | Drove a verdict or fix? |
|-------|-----------------------|-----------------------|-------------------------|
| quick-reviewer | 1 / 0 | 1 / 0 | Co-drove iter-1 High (Escape); raised the iter-2 focus-theft High (**refuted**) |
| broad-reviewer | 3 / 2 | 1 / 0 | Co-drove iter-1 High; corroborated iter-2 Medium; instrumented the listener-balance verification |
| knowledge-reviewer | 1 / 1 | — (dropped by cap) | Sole finder of #2 (load-bearing microtask rationale) — fixed |
| consistency-reviewer | 2 / 1 | — (dropped by cap) | C1/C2 consistency — fixed |
| design-reviewer | 3 / 0 | 3 / 2 | Co-found #3 (deferred); iter-2 forward-looking design observations (awareness) |
| adversarial-reviewer | 2 / 0 | 2 / 0 | Corroboration; **failed first dispatch both iterations** (see §4) |
| test-reviewer | 5 / 5 | 2 / 1 | Testing-gap coverage; sole finder of the iter-2 Medium (try/finally leak) — fixed |
| typescript-reviewer | 1 / 1 | 0 / 0 | RR2 Node-narrow; iter-2 assurance-only (read Portal source to confirm timing) |
| spec-compliance-reviewer | 0 / 0 | — (dropped by cap) | Assurance: all 6 p07b requirements Covered, 0 gaps |

**Verdict-driver concentration:** iter-1 High (Escape) was multiply-corroborated (4 finders) — no single-agent dependency; iter-2 had no surviving verdict-driver (the sole High candidate was refuted). **Fix-driver spread:** the fixed items came from knowledge (#2), consistency (C1/C2), test (iter-2 Medium + gaps), typescript (RR2) — broad specialist coverage, not concentration. **Assurance-only:** typescript (iter 2) and spec-compliance (iter 1) surfaced no unique defects but cleared real hypotheses (Portal timing sound; spec fully met). **Natural ablation:** the mid6 cap dropped knowledge/consistency/spec in iter 2 with no evidence they were missed on a small event-handling delta.

---

**Outcome vs cost:** ~1.79M metered review-side tokens across two waves (plus orchestrators and an unmetered inline pass) to catch and fix a real non-modal-defeating Escape regression, refute a plausible-but-wrong focus-theft High, and clean up self-introduced test debt — ending green and spec-complete. For an accessibility/interaction re-implementation that replaces primitive-provided behavior by hand, the empirical listener/focus verification was worth the spend. **Caveats:** sample of one; the changeset is small-but-subtle (event-loop timing, focus), so this session speaks to interaction-heavy UI, not to logic- or data-heavy changes; the two most notable process signals — a recurring adversarial-agent dispatch failure and an off-loop post-approval pass — are deviations, and the injection-looking iter-2 stub warrants a closer look.
