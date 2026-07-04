# Code Review

**Mode**: mid (explicit) | **Reviewers**: quick, broad, knowledge, consistency, design, typescript, test | **Date**: 2026-07-04
**Source**: local uncommitted changes on `develop` (nib nibs-qj7m — canonical radio primitive + SegmentedControl extraction)
**Scope**: 6 files changed (2 modified +22/-18, 4 new: SegmentedControl.svelte 43, SegmentedControl.test.ts 69, radio-group.harness.svelte 14, radio-group.test.ts 35)
**Spec**: none found
**Validation**: 3 confirmed, 0 refuted, 0 uncertain (3 primary findings, all validated)

## Agent Selection Rationale

Changeset classification: small Svelte 5 / TypeScript web-UI change (~40 executable lines + 3 test files). TS/JS/Svelte files present; test files present; a new reusable component contract + a generalized vendored primitive contract. No security, DB, async/concurrency, or migration surface.

- **quick-reviewer** (always) — floor · mid-tier
- **broad-reviewer** (always) — floor · mid-tier
- **knowledge-reviewer** — substantive change embedding a canonical-vs-segmented split decision · session model
- **consistency-reviewer** — new component vs. sibling `ui/*` vendoring idiom · mid-tier
- **design-reviewer** — new shared `SegmentedControl` API + generalized primitive contract/boundary · session model
- **typescript-reviewer** (hard gate) — TypeScript/Svelte files present · mid-tier
- **test-reviewer** (hard gate) — 3 test files present · mid-tier
- security-reviewer: skipped — no security-adjacent surface (auth/crypto/input/network/IO/secrets)
- adversarial-reviewer: skipped — <50 executable lines, no high-risk domain
- performance-reviewer: skipped — no DB/loops-with-I/O/async/caching in the diff
- spec-compliance-reviewer: skipped — no spec available (hard gate)
- data-migration / dotnet / cpp / go / rust / prior-feedback: skipped — hard gates not met (no migration artifacts, C#, C/C++, Go, Rust; not a PR)

Mode chosen: explicit (`mid`). Model tiering (Step 2d, mid): judgment agents (knowledge, design) on the session model; volume agents (quick, broad, consistency, typescript, test) and all validators mid-tier. Pre-flight gates: the 3 relevant test files pass (17 tests, `cd web && npx vitest run --reporter=agent`); no dedicated lint/typecheck npm script exists (`build` is plain `vite build`), so build/lint were not separately run.

## Summary

| Severity | Count |
|----------|-------|
| 🔴 Critical | 0 |
| 🟠 High | 0 |
| 🟡 Medium | 3 |
| 🟢 Low | 0 |
| 🔵 Minor | 4 |

Critical/High/Medium/Low are **primary** findings and drive the verdict. **Minor** counts the reported-but-non-blocking findings (Consistency / Testing Gaps / Residual Risks). Pre-existing issues are listed separately and excluded from both.

**Verdict**: ✅ APPROVED (only Medium primary findings — all three are real, confirmed, and worth addressing, but none is a merge blocker)

---

## Findings

### #1 🟡 Medium: Canonical `radio-group-item` types props as raw `ItemProps` and owns an inline `children` snippet — a consumer-passed `children` is silently dropped

| | |
|---|---|
| **File** | `web/src/lib/components/ui/radio-group/radio-group-item.svelte:10` (and :20-33) |
| **Category** | type-safety / idiom drift |
| **Confidence** | 100 (4 finders + validator confirmed) |
| **Found by** | broad-reviewer (High), consistency-reviewer (Medium), design-reviewer (Medium), typescript-reviewer (Medium) — validator: **confirmed** |

**Issue:** The rewritten primitive destructures its props as bare `RadioGroupPrimitive.ItemProps`, which — per bits-ui's `WithChild<...>` (`node_modules/bits-ui/.../radio-group/types.d.ts`) — includes an optional `children?: Snippet<[{checked}]>`. The component then declares its own inline `{#snippet children({ checked })}` (the indicator) *and* spreads `{...restProps}` onto the same `<RadioGroupPrimitive.Item>`. The validator traced Svelte's `spread_props` `get` handler (`node_modules/svelte/.../reactivity/props.js`), which walks the props array back-to-front and returns the first match: the inline snippet is appended last, so a caller's `children` is unreachable — silently discarded with no compile-time or runtime signal. Every sibling primitive that owns an internal indicator snippet closes this gap: `ui/checkbox/checkbox.svelte` uses `WithoutChildrenOrChild<CheckboxPrimitive.RootProps>`; `dropdown-menu-checkbox-item.svelte` / `dropdown-menu-radio-item.svelte` strip via `WithoutChildrenOrChild`/`WithoutChild` and then explicitly forward `childrenProp`. This one does neither.

**Impact is latent today** (validator-confirmed): no current call site passes `children` to the vendored Item — the only consumer is `radio-group.harness.svelte`, which passes `value`/`aria-label`. Because the canonical shadcn pattern labels items via an external `<Label>`/`aria-label`, a future author following the pattern likely won't pass `children`. The dissent (broad rated **High**) rests on the next planned consumer, nibs-vmaq's theme selector, wanting per-option labels; three specialists rated Medium since nothing triggers the defect in the current change. Consolidated at Medium: a type-honesty/idiom gap in a primitive with zero current production consumers.

**Fix:** Match the sibling idiom exactly:
```svelte
<script lang="ts">
	import { RadioGroup as RadioGroupPrimitive } from "bits-ui";
	import { cn, type WithoutChildrenOrChild } from "$lib/utils.js";
	import CircleIcon from "@lucide/svelte/icons/circle";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: WithoutChildrenOrChild<RadioGroupPrimitive.ItemProps> = $props();
</script>
```
(The icon-import change also resolves Minor finding below.) Optionally add a one-line note that labels go via `aria-label`/`<Label>`, not children, so the next author (nibs-vmaq) doesn't reach for the old children-as-label pattern.

---

### #2 🟡 Medium: `radio-group.test.ts` docblock claims a pill-skin-regression guard the assertions cannot enforce

| | |
|---|---|
| **File** | `web/src/lib/components/ui/radio-group/radio-group.test.ts:5-9` |
| **Category** | test-quality (comment-code mismatch; test can't catch its named regression) |
| **Confidence** | 100 (empirically reproduced by test-reviewer and by the validator) |
| **Found by** | test-reviewer (Medium) — validator: **confirmed** |

**Issue:** The describe-block comment states the suite "guards the primitive against regressing back to a segmented-pill skin," but every assertion checks only `role="radio"`, `aria-checked`, and the presence/absence of an `svg` inside `[data-slot="radio-group-indicator"]` — none inspects a CSS class or any appearance treatment. The validator independently reproduced the exact regression: it swapped `radio-group-item.svelte`'s disc classes (`aspect-square size-4 rounded-full border …`) back to the old pill classes (`rounded-sm px-2.5 py-1 data-[state=checked]:bg-background …`), leaving the role/aria/indicator markup intact, ran the suite, and **both tests still passed** (then restored the file, `git diff` clean). The test's role/aria/indicator assertions are genuine and would catch a contract break — only the *appearance/pill-skin* half of the docblock claim is unbacked.

**Fix:** Either soften the docblock to describe only what is locked (role / aria-checked / indicator contract), or add an appearance assertion, e.g.:
```ts
expect(a.className).toMatch(/rounded-full/);
expect(a.className).not.toMatch(/rounded-sm|px-2\.5/);
```

---

### #3 🟡 Medium: Canonical `ui/radio-group` has zero production consumers after this change, with no in-code note it is deliberate groundwork

| | |
|---|---|
| **File** | `web/src/lib/components/ui/radio-group/radio-group-item.svelte`, `radio-group.svelte`, `index.ts` |
| **Category** | knowledge-preservation / evolution-readiness |
| **Confidence** | 100 (3 finders + validator confirmed) |
| **Found by** | broad-reviewer (Medium), knowledge-reviewer (Low), design-reviewer (Low) — validator: **confirmed** |

**Issue:** `SettingsSheet.svelte` now imports `SegmentedControl` (which talks to bits-ui `RadioGroupPrimitive` directly), so the vendored `ui/radio-group` primitive's only remaining importer is the test harness — zero production consumers (validator-confirmed via grep). The primitive is deliberately kept in canonical form as groundwork for a future vanilla radio list (nibs-vmaq, `blocked_by: nibs-qj7m`), but nothing in `radio-group-item.svelte` / `radio-group.svelte` / `index.ts` records that. The reuse-rationale comments in `SegmentedControl.svelte` and `radio-group.test.ts` speak to the *split*, but not to the current zero-consumer status. A future dead-code sweep (or a maintainer editing this folder in isolation) could reasonably remove it as orphaned scaffold. This project uses inline nib-ID breadcrumbs for exactly this kind of "why does this exist / don't delete" context (see `SettingsSheet.svelte:32-37` citing nibs-8fj2/nibs-vmaq/nibs-p07b).

**Mitigations (why Medium, not higher):** nib nibs-qj7m captures the deliberate-groundwork narrative and is a legitimate source of truth in this workflow; `ui/*` is a vendored library where unconsumed primitives are normal; no dead-code tool (knip/ts-prune) is configured; and the primitive *is* exercised by `radio-group.test.ts` (note: design-reviewer's separate "no test exercises it" premise was inaccurate and is not carried into this finding). Design and knowledge both rated Low on these grounds; broad rated Medium.

**Fix:** Add a short breadcrumb at the top of `radio-group-item.svelte` (or `index.ts`), e.g.:
```svelte
<!-- Canonical shadcn radio, generalized in nibs-qj7m from the SettingsSheet
     segmented-pill skin (now SegmentedControl.svelte). No production consumer
     yet — reserved for nibs-vmaq's theme selector. Do not delete as "unused". -->
```

---

## Minor Findings

### Consistency

- `web/src/lib/components/ui/radio-group/radio-group-item.svelte:4` — icon imported via the barrel `import { Circle as CircleIcon } from "@lucide/svelte"` instead of the per-icon subpath used unanimously by all 10 other `ui/*` components (`import CheckIcon from '@lucide/svelte/icons/check'`, etc.) and by upstream shadcn-svelte (`@lucide/svelte/icons/circle`). Quotable-fact convention drift, conf 100 (quick-reviewer, consistency-reviewer). Resolved by the finding #1 fix snippet.
- `web/src/lib/components/ui/radio-group/radio-group-item.svelte:24` — indicator uses `data-slot="radio-group-indicator"`, breaking the `<root-slot>-indicator` naming pattern every sibling follows (`checkbox` → `checkbox-indicator`, `dropdown-menu-radio-item` → `dropdown-menu-radio-item-indicator`); expected `radio-group-item-indicator`. If renamed, also update the selector in `radio-group.test.ts:29,32`. Quotable-fact, conf 100 (consistency-reviewer).

### Testing Gaps

- `web/src/lib/components/SegmentedControl.test.ts:58-68` — the arrow-key nav test asserts only that `onchange` fired with `"comfortable"`, not that DOM focus actually moved (`toHaveFocus()`). The test is sound and does exercise real roving-tabindex behavior; adding a focus assertion would tie it more tightly to the mechanism it names (test-reviewer, Low).

### Residual Risks

- `web/src/lib/components/SegmentedControl.svelte:12-15` — the public API erases the option value type (`value: string`, `onchange: (value: string) => void`), forcing the `v as RowDensity` cast in `SettingsSheet.svelte:64`. A generic `<script lang="ts" generics="T extends string">` would drop the cast and preserve type safety across the boundary. The cast is pre-existing/relocated (it existed on the old inline `RadioGroup.Root`), the `{#each options}` loop keeps it inert today, and sibling closed-option pickers (`StatusSelect`, `PrioritySelect`, `EstimateSelect`) are all stringly-typed — so this is deferrable polish, not a defect (design-reviewer, typescript-reviewer, Low).

---

## Pre-existing Issues

### P1 🟢 Low: No `svelte-check`/`tsc` script — `vite build` does not type-check `.svelte` files

| | |
|---|---|
| **File** | `web/package.json:8` (scripts) |
| **Category** | tooling / type-safety |
| **Found by** | typescript-reviewer (Low, pre_existing) |

**Issue:** No `svelte-check`/`tsc` script exists in `package.json`, the Taskfile, or CI; `build` is plain `vite build`, which transpiles but does not type-check `<script lang="ts">` blocks. Consequently the `v as RowDensity` cast, a mistyped generic, or the `ItemProps`/`children` mismatch in finding #1 would never fail a build or `task test` — they are only visible to a human reading the diff. Pre-existing and out of this change's scope; noted because it is why several of the above findings would otherwise go uncaught mechanically. Consider adding `"check": "svelte-check --tsconfig ./tsconfig.json"` wired into `task test`/`task build`.

---

## Agent Summary

| Agent | Issues Found | Unique Issues |
|-------|:------------:|:-------------:|
| quick-reviewer | 1 | 0 |
| broad-reviewer | 2 | 0 |
| knowledge-reviewer | 1 | 0 |
| consistency-reviewer | 3 | 1 |
| design-reviewer | 3 | 0 |
| typescript-reviewer | 3 | 1 |
| test-reviewer | 2 | 2 |
| **Total** | **8** | |

Notes:
- **Issues Found**: total consolidated findings (primary + minor + pre-existing) attributed to this agent, including shared ones.
- **Unique Issues**: findings reported ONLY by this agent — consistency-reviewer (data-slot naming), typescript-reviewer (no svelte-check script), test-reviewer (test docblock overclaim + arrow-key focus gap).

---

## Specialist Notes

### Considered But Not Flagged (all agents)

- **`{...restProps}` + inline `{#snippet children}` structural shape** (quick, broad) — matches `ui/checkbox/checkbox.svelte` and upstream shadcn-svelte; the type-honesty gap is captured as finding #1, but the structural composition itself is idiomatic.
- **Outer Item lacks `flex items-center justify-center` for centering** (quick) — verified against upstream shadcn-svelte radio-group source, which is identical; native buttons center content by default. Not a bug.
- **`left-1/2`/`-translate-x-1/2` (physical) vs upstream `start-1/2` (logical/RTL)** (quick) — the codebase already mixes physical/logical positioning and has no RTL support; not meaningful drift.
- **`SegmentedControl` bypassing the vendored Item to talk to `RadioGroupPrimitive` directly** (broad, design, knowledge) — intentional and documented in the in-file header comment; correct direction of dependency (app presentation depends on the primitive, not vice versa); no duplication risk since the pill class now lives in exactly one place.
- **Harness "never runs as a test itself" claim** (broad, knowledge, test) — verified accurate against `vitest.config.ts` `include: ["src/**/*.test.ts"]`; a `.svelte` file cannot match `*.test.ts`.
- **Removed segmented-control comment in `radio-group-item.svelte`** (knowledge) — the `data-[state=checked]` styling it described moved to `SegmentedControl.svelte`, whose expanded comment restates the role/aria/data-state + roving-tabindex knowledge where it is now load-bearing; removal is correct, not knowledge loss.
- **`onValueChange={(v) => v && onchange(v)}` empty-value guard** (knowledge, typescript) — `v` is soundly typed `string` (bits-ui `OnChangeFn<string>`), and the guard correctly suppresses only the empty-string deselect (does not misfire on `"0"`); defensive, not load-bearing.
- **`{#each options as option}` without a keyed block** (broad, design) — pre-existing pattern; static two-item list; no practical risk.
- **`interface Props` vs inline type / `class: className = undefined` default** (consistency, typescript) — competing local conventions exist (SettingsSheet, Toolbar inline their types); redundant `= undefined` is type-identical under `cn()`; anchor 25, not flagged.
- **`ariaLabel` / `onchange` prop naming** (consistency) — `onchange` matches sibling selects; `ariaLabel` is first-of-kind with no convention to violate.
- **Appearance/a11y preservation of the density control** (broad, test) — `SegmentedControl`'s Item class string is character-for-character identical to the pre-refactor inline class; Root class, `orientation`, and the guard are preserved; the unchanged `SettingsSheet.test.ts` still passes (17 tests green), confirming parity.
- **Suppressed by confidence gate**: none (all surviving findings at anchor ≥75 or re-anchored quotable facts).

## Recurring Findings

| File | Category | Occurrences | First Seen |
|------|----------|-------------|------------|
| `web/src/lib/components/ui/radio-group/radio-group-item.svelte` | design / evolution-readiness | 2 | 2026-07-04 |

The prior review (`CODE_REVIEW_2026-07-04_17-03-07.md`, finding #1, Medium) flagged that `radio-group-item.svelte` baked a SettingsSheet-specific segmented-pill skin into the canonical `ui/` location and recommended "introduce an app-layer `SegmentedControl.svelte` composed over a canonical `ui/radio-group`." **This changeset is the fix for that finding.** The recurrence is on the same file's evolution-readiness dimension (now finding #3: the canonical primitive's post-fix zero-consumer status is undocumented) — a residual of the same generalization work, not a re-introduction of the original skin problem.

## Session Metrics (--report)

Review wave dispatched 2026-07-04 ~17:33; validation wave ~17:44; all 10 subagents synchronous (`run_in_background: false`).

| Agent | Kind | Model tier | Tokens | Tool calls | Duration | Findings submitted |
|-------|------|-----------|-------:|-----------:|---------:|-------------------:|
| quick-reviewer | reviewer | mid (sonnet) | 98,858 | 38 | 401.5s | 1 |
| broad-reviewer | reviewer | mid (sonnet) | 103,362 | 35 | 459.9s | 2 |
| knowledge-reviewer | reviewer | session (opus) | 60,911 | 10 | 135.8s | 1 |
| consistency-reviewer | reviewer | mid (sonnet) | 89,744 | 39 | 279.4s | 3 |
| design-reviewer | reviewer | session (opus) | 57,031 | 11 | 160.5s | 3 |
| typescript-reviewer | reviewer | mid (sonnet) | 90,256 | 38 | 334.5s | 3 |
| test-reviewer | reviewer | mid (sonnet) | 84,401 | 27 | 350.8s | 2 |
| validator (finding #1) | validator | mid (sonnet) | 59,646 | 19 | 124.4s | confirmed |
| validator (finding #2) | validator | mid (sonnet) | 49,777 | 8 | 63.3s | confirmed |
| validator (finding #3) | validator | mid (sonnet) | 55,651 | 13 | 85.8s | confirmed |

- **Pre-flight gates**: test PASS (17 tests, 3 relevant files); build/lint not separately run (no dedicated lint/typecheck script — `build` is `vite build`).
- **Validation**: 3 selected (all primary — 1 dissenting-severity, 1 single-finder, 1 dissenting-severity), 0 waived, 0 over budget; 3 confirmed / 0 refuted / 0 uncertain.
- **Anomalies**: none. All figures are harness-reported verbatim; durations rounded to 0.1s.
