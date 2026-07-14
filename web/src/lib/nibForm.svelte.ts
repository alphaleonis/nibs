/**
 * Buffered nib-form model — the single, boundary-testable "nib being edited"
 * substance shared by the detail panel and editor (the unified nib view).
 *
 * Two capability types behind separate constructors (a type-safe create/edit
 * split): edit-only concerns (etag, tag-diff, conflict, applyExternal) cannot
 * be invoked in create mode, and the create-only template-swap cannot leak into
 * edit mode. `MutationStore` is injected via `FormDeps`; the model has no DOM,
 * urql, or subscription imports.
 */

import type { MutationStore } from "./mutations/store.svelte";
import { createNib as createNibCmd, updateNib as updateNibCmd } from "./mutations/commands";
import type { CreateNibInput, UpdateNibInput } from "./mutations/types";
import { getBodyTemplate } from "./bodyTemplates";

/** A committed nib. `etag` is ALWAYS present (it identifies a saved revision). */
export interface NibSnapshot {
  readonly id: string;
  readonly title: string;
  readonly status: string;
  readonly type: string;
  readonly priority: string;
  readonly estimate: string;
  readonly tags: readonly string[];
  readonly body: string;
  readonly etag: string;
}

export interface CreateDefaults {
  readonly type?: string;
  readonly status?: string;
  readonly parent?: string;
}

export interface FormDeps {
  readonly mutations: Pick<MutationStore, "execute">;
}

/** The mode-agnostic bindable surface that the form MARKUP touches. */
export interface NibFormFields {
  title: string;
  status: string;
  type: string; // the type setter swaps the body template in create mode only
  priority: string;
  estimate: string;
  body: string;
  /** Replace the body wholesale, marking the buffer dirty exactly like typing does.
   *
   *  Default (opts unset / `reinitEditor: false`) is in-place: an open editor pane
   *  syncs the change via a minimal-diff doc transaction (see MarkdownEditor's
   *  external-value sync), preserving undo/cursor/scroll — the safe choice for an
   *  out-of-band edit like a rendered task-checkbox flip. Pass
   *  `{ reinitEditor: true }` to force a full editor re-init (`{#key}` remount). */
  setBody(value: string, opts?: { reinitEditor?: boolean }): void;
  readonly tags: readonly string[];
  addTag(tag: string): void;
  removeTag(tag: string): void;
  readonly bodyVersion: number; // bump -> {#key} re-inits MarkdownEditor
  readonly saving: boolean;
  readonly dirty: boolean;
}

export type CreateOutcome =
  | { kind: "created"; id: string; snapshot: NibSnapshot }
  | { kind: "error"; message?: string };

export type EditOutcome =
  | { kind: "saved"; snapshot: NibSnapshot }
  // `remote` is the known-conflicting snapshot when we detected the change
  // proactively (via `noteExternalChange`); it is `null` for a server-side
  // if-match rejection that raced the subscription — the live subscription
  // backfills the snapshot into `externalChange` moments later.
  | { kind: "conflict"; remote: NibSnapshot | null }
  | { kind: "error"; message?: string };

// --- internals ------------------------------------------------------------

/** The mutable working-copy field set (mirrors NibSnapshot minus id/etag). */
interface FieldValues {
  title: string;
  status: string;
  type: string;
  priority: string;
  estimate: string;
  tags: string[];
  body: string;
}

function fieldsFromSnapshot(s: NibSnapshot): FieldValues {
  return {
    title: s.title,
    status: s.status,
    type: s.type,
    priority: s.priority,
    estimate: s.estimate,
    tags: [...s.tags],
    body: s.body,
  };
}

/**
 * Recognize a server-side optimistic-concurrency rejection (stale if-match).
 *
 * PRIMARY signal: the structured GraphQL `extensions.code === "ETAG_MISMATCH"`,
 * attached by the backend error presenter (cmd/serve.go, `etagErrorPresenter`)
 * to ONLY the typed `*nibcore.ETagMismatchError`. It is wrapping-proof (survives
 * message rewording and urql's "[GraphQL] " prefix) and cannot be confused with
 * a validation/generic error that merely mentions "etag".
 *
 * FALLBACK signal: a substring match on the human-readable message
 * "etag mismatch: provided <x>, current is <y>" (see internal/nibcore/core.go,
 * `ETagMismatchError.Error`). This is defense-in-depth on the one path that
 * reaches this classifier — `EditForm.save()` over HTTP — so conflict routing
 * still works if the structured code ever goes missing (an `etagErrorPresenter`
 * regression, or a future non-HTTP error path that skips the presenter). The
 * format is pinned on both sides: Go `TestETagMismatchErrorFormat`
 * (internal/nibcore) and the "substring fallback" cases in nibForm.svelte.test.ts
 * — keep them in lockstep.
 */
function isEtagConflict(message: string | undefined, code?: string): boolean {
  if (code === "ETAG_MISMATCH") return true;
  return !!message && /etag mismatch/i.test(message);
}

/**
 * Order-insensitive but duplicate-SENSITIVE tag equality (a multiset compare via
 * a sorted element-wise comparison). Add-then-remove returns to baseline, while
 * ["x","x"] and ["x","y"] are correctly unequal — the latter mattered once this
 * fed #matchesFields (the convergence decision that gates a real write path).
 */
function sameTags(a: readonly string[], b: readonly string[]): boolean {
  if (a.length !== b.length) return false;
  const sa = [...a].sort();
  const sb = [...b].sort();
  return sa.every((t, i) => t === sb[i]);
}

/**
 * Shared working-copy-vs-baseline machinery. Owns the buffered fields, the
 * baseline used for `dirty`/tag-diffing, the `saving` flag, and the body
 * version counter. The `type` setter delegates to `afterTypeChange`, which the
 * create subclass overrides for template swapping.
 */
abstract class BaseForm implements NibFormFields {
  protected readonly deps: FormDeps;

  title = $state("");
  status = $state("");
  #type = $state("");
  priority = $state("");
  estimate = $state("");
  body = $state("");
  #tags = $state<string[]>([]);
  #saving = $state(false);
  #bodyVersion = $state(0);
  #baseline = $state.raw<FieldValues>({
    title: "",
    status: "",
    type: "",
    priority: "",
    estimate: "",
    tags: [],
    body: "",
  });

  constructor(deps: FormDeps) {
    this.deps = deps;
  }

  get type(): string {
    return this.#type;
  }
  set type(value: string) {
    const changed = value !== this.#type;
    this.#type = value;
    if (changed) this.afterTypeChange(value);
  }

  get tags(): readonly string[] {
    return this.#tags;
  }

  get saving(): boolean {
    return this.#saving;
  }

  get bodyVersion(): number {
    return this.#bodyVersion;
  }

  get dirty(): boolean {
    const b = this.#baseline;
    return (
      this.title !== b.title ||
      this.status !== b.status ||
      this.#type !== b.type ||
      this.priority !== b.priority ||
      this.estimate !== b.estimate ||
      this.body !== b.body ||
      !sameTags(this.#tags, b.tags)
    );
  }

  setBody(value: string, opts?: { reinitEditor?: boolean }): void {
    this.body = value;
    // A body change alone marks the buffer dirty via the derived `dirty` getter
    // (body !== baseline). DEFAULT is in-place / non-remounting: an open editor
    // pane syncs the new body via a minimal-diff doc transaction that preserves
    // undo history / cursor / scroll (nibs-fva8) — the safe choice for an
    // out-of-band edit like the task-checkbox flip. Pass `{ reinitEditor: true }`
    // to force a full editor re-init (the `{#key bodyVersion}` remount) instead.
    // (Genuine baseline resets — discard / applyExternal / afterTypeChange —
    // call bumpBodyVersion() directly, not through here.)
    if (opts?.reinitEditor === true) this.bumpBodyVersion();
  }

  addTag(tag: string): void {
    if (!this.#tags.includes(tag)) this.#tags = [...this.#tags, tag];
  }

  removeTag(tag: string): void {
    this.#tags = this.#tags.filter((t) => t !== tag);
  }

  /** Revert the working copy to the baseline. */
  discard(): void {
    this.setFields(this.#baseline);
    this.bumpBodyVersion();
  }

  // --- protected helpers for subclasses ---

  /** Hook fired when the public `type` setter changes value (no-op by default). */
  protected afterTypeChange(_newType: string): void {}

  protected get baseline(): FieldValues {
    return this.#baseline;
  }

  /** Seed working-copy fields WITHOUT triggering the type-change hook. */
  protected setFields(v: FieldValues): void {
    this.title = v.title;
    this.status = v.status;
    this.#type = v.type;
    this.priority = v.priority;
    this.estimate = v.estimate;
    this.body = v.body;
    this.#tags = [...v.tags];
  }

  /** Replace the baseline wholesale (a full re-baseline). */
  protected rebaseline(v: FieldValues): void {
    this.#baseline = {
      title: v.title,
      status: v.status,
      type: v.type,
      priority: v.priority,
      estimate: v.estimate,
      tags: [...v.tags],
      body: v.body,
    };
  }

  /** Patch selected baseline fields, leaving the rest of the baseline intact. */
  protected rebaselineFields(patch: Partial<FieldValues>): void {
    this.#baseline = {
      ...this.#baseline,
      ...patch,
      tags: patch.tags ? [...patch.tags] : this.#baseline.tags,
    };
  }

  protected currentFields(): FieldValues {
    return {
      title: this.title,
      status: this.status,
      type: this.#type,
      priority: this.priority,
      estimate: this.estimate,
      tags: [...this.#tags],
      body: this.body,
    };
  }

  protected setSaving(value: boolean): void {
    this.#saving = value;
  }

  protected bumpBodyVersion(): void {
    this.#bodyVersion++;
  }
}

/**
 * Create form. Seeds a per-type body template and swaps it when the type
 * changes while the body is still untouched (body === last template).
 */
export class CreateForm extends BaseForm implements NibFormFields {
  readonly mode = "create" as const;
  readonly #parent?: string;
  #lastTemplate = "";

  constructor(deps: FormDeps, defaults?: CreateDefaults) {
    super(deps);
    const type = defaults?.type || "task";
    const status = defaults?.status || "draft";
    const template = getBodyTemplate(type);
    this.#parent = defaults?.parent || undefined;
    this.#lastTemplate = template;

    const init: FieldValues = {
      title: "",
      status,
      type,
      priority: "",
      estimate: "",
      tags: [],
      body: template,
    };
    this.setFields(init);
    this.rebaseline(init);
  }

  protected override afterTypeChange(newType: string): void {
    // Template policy by equality: only swap while the body is "untouched"
    // (equal to the last template). Deleting the body back to the template
    // re-enables the swap; an edited body is left alone.
    if (this.body !== this.#lastTemplate) return;

    const template = getBodyTemplate(newType);
    this.body = template;
    this.#lastTemplate = template;
    this.bumpBodyVersion();
    // Re-baseline the swapped fields so a pristine create form stays non-dirty.
    this.rebaselineFields({ type: newType, body: template });
  }

  async save(): Promise<CreateOutcome> {
    if (this.saving) return { kind: "error", message: "Save already in progress" };

    const title = this.title.trim();
    if (!title) return { kind: "error", message: "Title is required" };

    const input: CreateNibInput = {
      title,
      type: this.type,
      status: this.status,
      ...(this.priority ? { priority: this.priority } : {}),
      ...(this.estimate ? { estimate: this.estimate } : {}),
      ...(this.tags.length > 0 ? { tags: [...this.tags] } : {}),
      ...(this.body ? { body: this.body } : {}),
      ...(this.#parent ? { parent: this.#parent } : {}),
    };

    this.setSaving(true);
    try {
      // suppressToast: save() OWNS the messaging for this call (mirrors the edit
      // path). The direct Save button (ActiveNibView.handleSave) and the dirty-nav
      // guard each surface a create error exactly once — so the dispatcher must not
      // also toast it (would double up), and client-side early-returns above (empty
      // title) that never reach the dispatcher still get that single feedback.
      const result = await this.deps.mutations.execute(createNibCmd(input), {
        suppressToast: true,
      });
      if (!result.ok) return { kind: "error", message: result.error };

      const created = result.data?.createNib;
      const id: string | undefined = created?.id;
      if (!id) return { kind: "error", message: "Create returned no id" };

      const snapshot: NibSnapshot = {
        id,
        title: created?.title ?? title,
        status: created?.status ?? this.status,
        type: created?.type ?? this.type,
        priority: created?.priority ?? this.priority,
        estimate: created?.estimate ?? this.estimate,
        tags: created?.tags ?? [...this.tags],
        body: created?.body ?? this.body,
        etag: created?.etag ?? "",
      };
      return { kind: "created", id, snapshot };
    } finally {
      this.setSaving(false);
    }
  }
}

/**
 * Edit form. Owns optimistic-concurrency etag threading, tag diffing against
 * the baseline, self-echo-filtered external-change tracking, and conflict /
 * overwrite handling.
 */
export class EditForm extends BaseForm implements NibFormFields {
  readonly mode = "edit" as const;
  readonly id: string;
  #etag = $state("");
  #externalChange = $state<NibSnapshot | null>(null);

  constructor(deps: FormDeps, seed: NibSnapshot) {
    super(deps);
    this.id = seed.id;
    this.#etag = seed.etag;
    const init = fieldsFromSnapshot(seed);
    this.setFields(init);
    this.rebaseline(init);
  }

  get etag(): string {
    return this.#etag;
  }

  get externalChange(): NibSnapshot | null {
    const remote = this.#externalChange;
    // AC3: once the working copy has converged back to the recorded remote's
    // field values there is nothing left to resolve — clear the warning even
    // though the etags still differ. This is DERIVED (not one-shot): diverging
    // again re-surfaces it. `save()` applies the SAME convergence check, so the
    // two accessors never disagree: on a converged buffer save() performs a real
    // write against the remote etag rather than a silent no-op (HIGH #1).
    if (!remote || this.#matchesFields(remote)) return null;
    return remote;
  }

  /** Whether the working copy already equals the remote snapshot's field values.
   *  Title is compared trimmed — save() writes `title.trim()`, so convergence must
   *  be judged against what would actually be persisted, not the raw buffer. */
  #matchesFields(remote: NibSnapshot): boolean {
    return (
      this.title.trim() === remote.title &&
      this.status === remote.status &&
      this.type === remote.type &&
      this.priority === remote.priority &&
      this.estimate === remote.estimate &&
      this.body === remote.body &&
      sameTags(this.tags, remote.tags)
    );
  }

  /** Feed a subscription event. Self-echoes (remote etag === ours) are dropped. */
  noteExternalChange(remote: NibSnapshot): void {
    if (remote.etag === this.#etag) return;
    this.#externalChange = remote;
  }

  /** "Reload theirs": rebase baseline + working copy onto the remote snapshot. */
  applyExternal(remote: NibSnapshot): void {
    this.#etag = remote.etag;
    const init = fieldsFromSnapshot(remote);
    this.setFields(init);
    this.rebaseline(init);
    this.#externalChange = null;
    this.bumpBodyVersion();
  }

  async save(opts?: { overwrite?: boolean }): Promise<EditOutcome> {
    if (this.saving) return { kind: "error", message: "Save already in progress" };

    const title = this.title.trim();
    if (!title) return { kind: "error", message: "Title is required" };

    const external = this.#externalChange;
    if (external && !opts?.overwrite) {
      // Only a buffer that STILL diverges from the recorded remote is a genuine,
      // unresolved conflict — surface the resolver without dispatching. If the
      // working copy has converged to the remote's field values (the same check
      // the `externalChange` getter uses, so the two never disagree — HIGH #1),
      // there is nothing to resolve: the content we'd send already equals the
      // server's current revision. Fall through to a REAL write below, using the
      // remote's etag as if-match, so Save legitimately succeeds and rebaselines
      // instead of a silent, feedback-less no-op.
      if (!this.#matchesFields(external)) {
        return { kind: "conflict", remote: external };
      }
    }

    // if-match selection:
    // - overwrite → last-write-wins: re-adopt the known REMOTE etag, or omit
    //   if-match entirely when no remote is known. Omitting lets the write land
    //   ONLY under the default `require_if_match: false`; under `true` the
    //   backend returns ETagRequiredError and the write does NOT land. The
    //   no-remote overwrite branch is currently unreachable from the UI anyway
    //   (Overwrite renders only while `externalChange` is set).
    // - converged proactive change (external set, non-overwrite) → the REMOTE
    //   etag: the content equals the remote and that etag is the server's
    //   current state, so the write lands, advances the etag, and rebaselines.
    // - normal save → our baseline etag.
    const ifMatch = opts?.overwrite
      ? (external?.etag ?? undefined)
      : external
        ? external.etag
        : this.#etag;

    const baselineTags = this.baseline.tags;
    const input: UpdateNibInput = {
      title,
      status: this.status,
      type: this.type,
      priority: this.priority || null,
      estimate: this.estimate || null,
      body: this.body,
    };
    const addedTags = this.tags.filter((t) => !baselineTags.includes(t));
    const removedTags = baselineTags.filter((t) => !this.tags.includes(t));
    if (addedTags.length > 0) input.addTags = addedTags;
    if (removedTags.length > 0) input.removeTags = removedTags;

    this.setSaving(true);
    try {
      // `suppressToast: true` — save() OWNS the messaging for this call. A
      // server-side 409 is routed into the inline conflict resolver (below), and
      // a plain error is surfaced by the caller via the returned `error` outcome;
      // either way the raw `toast.error(<GraphQL message>)` the dispatcher would
      // otherwise fire must NOT race ahead of that (the whole point of Item 2).
      const result = await this.deps.mutations.execute(
        updateNibCmd(this.id, input, ifMatch),
        { suppressToast: true },
      );
      if (!result.ok) {
        // Route a server-side if-match rejection into the SAME conflict resolver
        // as the proactive path — the buffer stays dirty, so nothing is clobbered.
        // Classified on the structured `errorCode` first (extensions.code), with
        // the message substring as a fallback (isEtagConflict). The remote
        // snapshot isn't in the error, so `remote` is `this.#externalChange`: the
        // proactively-recorded remote if the subscription already delivered one,
        // else null — in which case the presenter (useActiveView) one-shot-fetches
        // the current snapshot so the resolver isn't stuck waiting on the sub.
        if (!opts?.overwrite && isEtagConflict(result.error, result.errorCode)) {
          return { kind: "conflict", remote: this.#externalChange };
        }
        return { kind: "error", message: result.error };
      }

      const newEtag: string = result.data?.updateNib?.etag ?? this.#etag;
      this.#etag = newEtag;
      this.title = title; // adopt the trimmed title so the field matches baseline
      this.rebaseline(this.currentFields());
      this.#externalChange = null;

      const snapshot: NibSnapshot = {
        id: this.id,
        title,
        status: this.status,
        type: this.type,
        priority: this.priority,
        estimate: this.estimate,
        tags: [...this.tags],
        body: this.body,
        etag: newEtag,
      };
      return { kind: "saved", snapshot };
    } finally {
      this.setSaving(false);
    }
  }
}

export function createNibForm(deps: FormDeps, defaults?: CreateDefaults): CreateForm {
  return new CreateForm(deps, defaults);
}

export function editNibForm(deps: FormDeps, seed: NibSnapshot): EditForm {
  return new EditForm(deps, seed);
}
