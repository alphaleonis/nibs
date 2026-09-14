/**
 * The buffered "nib being edited" model shared by the detail panel and editor.
 *
 * Create and edit are separate classes: etag, tag diffing and conflict handling
 * exist only on `EditForm`, template swapping only on `CreateForm`. Mutations are
 * injected through `FormDeps`; no DOM, urql or subscription imports.
 */

import type { MutationStore } from "./mutations/store.svelte";
import { createNib as createNibCmd, updateNib as updateNibCmd } from "./mutations/commands";
import type { CreateNibInput, UpdateNibInput } from "./mutations/types";
import { getBodyTemplate } from "./bodyTemplates";
import { takesAssignmentAxes } from "./membership";

/** A committed nib. `etag` is ALWAYS present (it identifies a saved revision). */
export interface NibSnapshot {
  readonly id: string;
  readonly title: string;
  readonly status: string;
  readonly type: string;
  readonly priority: string;
  readonly estimate: string;
  /** The DIRECT milestone assignment, verbatim: "" even for a nib scheduled
   *  through an assigned ancestor. */
  readonly milestone: string;
  /** The area assignment (a declared path), verbatim; "" for none. */
  readonly area: string;
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
  /** Edit mode only: `CreateNibInput` has no milestone, so a create form never sends it. */
  milestone: string;
  area: string;
  body: string;
  /** Replace the body, dirtying the buffer like typing does. By default an open
   *  editor syncs in place, keeping undo, cursor and scroll; `reinitEditor: true`
   *  remounts it instead. */
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
  // `remote` is null for a server-side if-match rejection that raced the
  // subscription, which delivers the snapshot to `externalChange` afterwards.
  | { kind: "conflict"; remote: NibSnapshot | null }
  // The edited nib no longer exists on the server; see `isNotFound`.
  | { kind: "missing" }
  | { kind: "error"; message?: string };

// --- internals ------------------------------------------------------------

/** The mutable working-copy field set (mirrors NibSnapshot minus id/etag). */
interface FieldValues {
  title: string;
  status: string;
  type: string;
  priority: string;
  estimate: string;
  milestone: string;
  area: string;
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
    milestone: s.milestone,
    area: s.area,
    tags: [...s.tags],
    body: s.body,
  };
}

/**
 * Whether a mutation failed on a stale if-match.
 *
 * Primary: `extensions.code === "ETAG_MISMATCH"`, which cmd/serve.go's
 * `etagErrorPresenter` attaches only to `*nibcore.ETagMismatchError`. Fallback:
 * the message substring "etag mismatch", in case the code goes missing. The
 * message format is pinned by Go's `TestETagMismatchErrorFormat` and the
 * "substring fallback" cases in nibForm.svelte.test.ts.
 */
function isEtagConflict(message: string | undefined, code?: string): boolean {
  if (code === "ETAG_MISMATCH") return true;
  return !!message && /etag mismatch/i.test(message);
}

/**
 * Whether a mutation failed because its target nib no longer exists:
 * `extensions.code === "NOT_FOUND"`, which `etagErrorPresenter` attaches to any
 * error carrying `nib.ErrNotFound`. No message fallback, because many unrelated
 * errors say "not found" and this routes the whole view to the gone notice.
 *
 * Reading it as the edited nib's own deletion holds only while `EditForm.save()`
 * sends no blocking fields: a deleted blocking target also yields NOT_FOUND
 * (updateTargetClone wraps with %w).
 */
function isNotFound(code?: string): boolean {
  return code === "NOT_FOUND";
}

/** Multiset tag equality: order-insensitive, duplicate-sensitive. */
function sameTags(a: readonly string[], b: readonly string[]): boolean {
  if (a.length !== b.length) return false;
  const sa = [...a].sort();
  const sb = [...b].sort();
  return sa.every((t, i) => t === sb[i]);
}

/**
 * Body equality ignoring line endings. A body enters the form with the file's
 * endings, but MarkdownEditor emits CodeMirror's LF doc, so a byte compare would
 * keep a CRLF nib dirty after one keystroke.
 *
 * Only comparisons normalize; `body` keeps its CRLF until edited. A buffer that
 * differs from the remote only in endings therefore converges, and saving it
 * writes the LF body.
 */
function sameBody(a: string, b: string): boolean {
  return a.replace(/\r\n?/g, "\n") === b.replace(/\r\n?/g, "\n");
}

/** The working copy and its baseline, `saving`, and the body version counter. */
abstract class BaseForm implements NibFormFields {
  protected readonly deps: FormDeps;

  title = $state("");
  status = $state("");
  #type = $state("");
  priority = $state("");
  estimate = $state("");
  milestone = $state("");
  area = $state("");
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
    milestone: "",
    area: "",
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
      this.milestone !== b.milestone ||
      this.area !== b.area ||
      !sameBody(this.body, b.body) ||
      !sameTags(this.#tags, b.tags)
    );
  }

  setBody(value: string, opts?: { reinitEditor?: boolean }): void {
    this.body = value;
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
    this.milestone = v.milestone;
    this.area = v.area;
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
      milestone: v.milestone,
      area: v.area,
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
      milestone: this.milestone,
      area: this.area,
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
 * Create form. Seeds a per-type body template and swaps it on a type change
 * while the body still equals the last template.
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
      milestone: "",
      area: "",
      tags: [],
      body: template,
    };
    this.setFields(init);
    this.rebaseline(init);
  }

  protected override afterTypeChange(newType: string): void {
    // A milestone takes no area and hides the Area control, so clear the value
    // rather than send one the user cannot see. Keep this above the template
    // early-return. No re-baseline: a create form's baseline area is always "".
    if (!takesAssignmentAxes(newType)) this.area = "";

    // Swap only while the body is untouched.
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
      ...(this.area ? { area: this.area } : {}),
      ...(this.tags.length > 0 ? { tags: [...this.tags] } : {}),
      ...(this.body ? { body: this.body } : {}),
      ...(this.#parent ? { parent: this.#parent } : {}),
    };

    this.setSaving(true);
    try {
      // The callers (ActiveNibView.handleSave, the dirty-nav guard) show the error.
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
        milestone: "",
        // CREATE_NIB_MUTATION selects no `area`; a refused one returned above.
        area: this.area,
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
    // Hidden while the working copy matches the remote, shown again on divergence.
    // save() uses the same check.
    if (!remote || this.#matchesFields(remote)) return null;
    return remote;
  }

  /** Whether the working copy equals the remote's fields as save() would write
   *  them: title trimmed, body compared with `sameBody`. */
  #matchesFields(remote: NibSnapshot): boolean {
    return (
      this.title.trim() === remote.title &&
      this.status === remote.status &&
      this.type === remote.type &&
      this.priority === remote.priority &&
      this.estimate === remote.estimate &&
      this.milestone === remote.milestone &&
      this.area === remote.area &&
      sameBody(this.body, remote.body) &&
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
      // A buffer that has converged to the remote is not a conflict: it falls
      // through and writes with the remote's etag.
      if (!this.#matchesFields(external)) {
        return { kind: "conflict", remote: external };
      }
    }

    // if-match:
    // - overwrite: the remote etag, or none when no remote is known, which lands
    //   only while `require_if_match` is false. The UI offers Overwrite only with
    //   a remote.
    // - converged external change: the remote etag.
    // - otherwise: the baseline etag.
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
    // Send milestone only when it changed: validateAndSetMilestone runs the
    // assignment door on every assignment sent, so re-sending one to a
    // since-completed milestone would refuse an unrelated save.
    if (this.milestone !== this.baseline.milestone) {
      input.milestone = this.milestone || null;
    }

    // Area only when changed too, though re-sending it is validated the same as
    // omitting it.
    if (this.area !== this.baseline.area) {
      input.area = this.area || null;
    }

    const addedTags = this.tags.filter((t) => !baselineTags.includes(t));
    const removedTags = baselineTags.filter((t) => !this.tags.includes(t));
    if (addedTags.length > 0) input.addTags = addedTags;
    if (removedTags.length > 0) input.removeTags = removedTags;

    this.setSaving(true);
    try {
      // A 409 goes to the inline conflict resolver and other errors to the caller.
      const result = await this.deps.mutations.execute(
        updateNibCmd(this.id, input, ifMatch),
        { suppressToast: true },
      );
      if (!result.ok) {
        // `remote` is null when the subscription has not delivered the change
        // yet; useActiveView then fetches the current snapshot.
        if (!opts?.overwrite && isEtagConflict(result.error, result.errorCode)) {
          return { kind: "conflict", remote: this.#externalChange };
        }
        // A deleted nib fails GetForUpdate before any if-match check, with or
        // without overwrite.
        if (isNotFound(result.errorCode)) {
          return { kind: "missing" };
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
        milestone: this.milestone,
        area: this.area,
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
