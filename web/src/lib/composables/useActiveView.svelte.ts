/**
 * Reactive shell for the active-nib view. Owns one `ViewState` and the
 * form/live/detail buffer around it. Transitions go through the pure kernel
 * (`activeView.ts`) via `apply` (reduce + reconcile) or `guarded`, the only
 * place the dirty-guard lives.
 *
 * The buffer is keyed on `edit:<id>` / `create:<nonce>`, so it survives
 * expand/collapse and saving the same nib, and is rebuilt only when the target
 * changes.
 */

import { untrack } from "svelte";
import { isSyntheticRowId } from "../tree";
import { getValidChildTypes } from "../typeHierarchy";
import {
  reduce,
  abandonsBuffer,
  canSaveState,
  type ViewState,
  type Action,
  type Presentation,
} from "./activeView";
import type {
  CreateForm,
  EditForm,
  CreateDefaults,
  CreateOutcome,
  EditOutcome,
  NibSnapshot,
} from "../nibForm.svelte";
import type { LiveNib } from "../liveNib.svelte";
import type { HistoryNav } from "./useHistoryNav.svelte";
import type { NibDetailQuery } from "../gql/graphql";

/** The dirty-nav guard's answer: save then proceed, discard then proceed, or stay. */
export type ConfirmChoice = "save" | "discard" | "cancel";

/**
 * What `noteMissing` left on screen:
 *   - "closed" — no buffer remains; the caller heals the URL and reports the deletion.
 *   - "kept"   — the buffer is held in `gone` behind its deleted notice, and the
 *     `?nib=` URL still describes it.
 *   - "stale"  — the view was not on this nib; do not close or heal.
 */
export type MissingNibOutcome = "closed" | "kept" | "stale";

/** Relation entry shape, matching `NIB_DETAIL_QUERY`'s relation selections. */
export interface DetailNibRef {
  id: string;
  title: string;
  type: string;
  status: string;
}

/**
 * The `NIB_DETAIL_QUERY` nib the view rail reads. Derived from the generated
 * result type, so dropping a selected field breaks its readers at compile time.
 */
export type DetailNib = NonNullable<NibDetailQuery["nib"]>;

/** The reactive detail-query wrapper injected by the app. */
export interface DetailView {
  readonly nib: DetailNib | null;
  readonly fetching: boolean;
}

export interface ActiveViewDeps {
  nav: Pick<HistoryNav, "navigateToNib" | "closePanel" | "replaceClosed">;
  /** Build an edit form. A create→edit hand-off passes the created snapshot as
   *  `seed`, since the new nib's detail query has not run yet. */
  editForm: (nibId: string, seed?: NibSnapshot) => EditForm;
  createForm: (defaults: CreateDefaults) => CreateForm;
  liveNib: (nibId: string) => LiveNib;
  /** The `NIB_DETAIL_QUERY` wrapper for a nib. */
  detail: (nibId: string) => DetailView;
  /** Network-only fetch of a nib's current snapshot, for the null-remote
   *  conflict fallback. Resolve null when the nib does not exist; REJECT on a
   *  transport or GraphQL error. The fallback tells the two apart. */
  fetchSnapshot: (nibId: string) => Promise<NibSnapshot | null>;
  /** Show a transient error message (a toast). */
  notifyError: (message: string) => void;
  /** Prompt the dirty-nav guard. `canSave: false` means the buffer's nib was
   *  DELETED: offer Discard/Cancel only. An archived nib arrives with
   *  `canSave: true`. TypeScript accepts an implementation that ignores the
   *  parameter, so honor it in review. */
  confirm: (opts: { canSave: boolean }) => Promise<ConfirmChoice>;
}

/** A viewport rectangle (from getBoundingClientRect) the type picker anchors to. */
export interface AnchorRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

/**
 * The add-child type picker. Outside the ViewState machine: it overlays the view
 * and changes no buffer until a type is chosen.
 */
export interface TypePickerState {
  parentId: string;
  parentType: string;
  validTypes: readonly string[];
  anchor: AnchorRect;
}

export interface ActiveView {
  readonly state: ViewState;
  readonly form: CreateForm | EditForm | null;
  readonly detail: DetailView | null;
  readonly isOpen: boolean;
  readonly presentation: Presentation;
  readonly typePicker: TypePickerState | null;
  /** True while Back/Forward must be frozen: dirty buffer or an open type picker. */
  readonly blocksHistoryNav: boolean;
  /**
   * Re-arm the one-shot detail seed so the next detail result rebaselines a
   * pristine buffer. Call after a gap in the live subscription. A dirty buffer
   * is still left alone.
   */
  invalidateDetailSeed(): void;
  /** True while the active form's null-remote conflict fallback runs.
   *  `form.saving` is already false by then, so OR this into the Save control's
   *  `disabled`. */
  readonly savePending: boolean;
  /** Bumped each time a clean buffer is silently rebaselined onto an incoming
   *  change. Only deltas matter. */
  readonly externalApplied: number;

  open(nibId: string): Promise<void>;
  expand(): void;
  collapse(): void;
  startCreate(defaults: { type: string; parent?: string }): Promise<void>;
  /** Add a child of `parentId`: 1 valid type → create directly; ≥2 → open the
   *  type picker anchored to `anchor` (the clicked control's viewport rect). */
  startCreateChild(parentId: string, parentType: string, anchor: AnchorRect): Promise<void>;
  chooseType(nibType: string): Promise<void>;
  cancelType(): void;
  /** Persist the active buffer (create hand-off and conflict routing included).
   *  Resolves `undefined` without dispatching when there is no buffer or its nib
   *  was deleted; check `canSaveState(state)` first to explain that to the user. */
  save(): Promise<CreateOutcome | EditOutcome | undefined>;
  requestClose(): Promise<void>;
  /** Follow a move history already made (popstate, multi-select desync).
   *  Bypasses the dirty-guard. */
  syncTo(nibId: string | null): void;
  /** Report that `nibId` no longer resolves on the server. Viewing it: a pristine
   *  buffer closes ("closed"), a dirty one moves to `gone`/"deleted" ("kept").
   *  Already `gone` on it: "kept", upgrading an archived reason to deleted.
   *  Otherwise "stale". Bypasses the dirty-guard. */
  noteMissing(nibId: string): MissingNibOutcome;
  /** Tear down the live subscription (call on host teardown). */
  dispose(): void;
}

export function createActiveView(deps: ActiveViewDeps): ActiveView {
  // Swapped wholesale, never mutated through. `$state.raw` keeps identity (no
  // proxy), so `view.form === theInstance`.
  let viewState = $state.raw<ViewState>({ kind: "closed" });
  let form = $state.raw<CreateForm | EditForm | null>(null);
  let detailView = $state.raw<DetailView | null>(null);
  let live = $state.raw<LiveNib | null>(null);
  let typePicker = $state.raw<TypePickerState | null>(null);
  let externalApplied = $state(0);
  // The edit form whose null-remote conflict fallback is in flight; also blocks
  // a re-entrant fallback.
  let conflictFallbackFor = $state.raw<EditForm | null>(null);

  // Non-reactive bookkeeping.
  let currentKey: string | null = null;
  let createNonce = 0;
  let liveDispose: (() => void) | null = null;
  let lastExternal: NibSnapshot | null = null;
  // The created snapshot for the create→edit hand-off; reconcileBuffer consumes it.
  let pendingCreateSeed: NibSnapshot | null = null;
  // The buffer key the detail seed last applied to. The live bridge stamps it
  // too, so a slower detail result cannot regress a buffer the bridge advanced.
  let seededKey: string | null = null;

  function bufferKey(s: ViewState): string | null {
    switch (s.kind) {
      case "viewing":
      case "gone":
        return `edit:${s.nibId}`;
      case "creating":
        return `create:${createNonce}`;
      default:
        return null; // closed
    }
  }

  function initiatesCreate(action: Action): boolean {
    return action.type === "START_CREATE";
  }

  /** Rebuild form/live/detail when the buffer key changes. */
  function reconcileBuffer() {
    const s = viewState;
    const key = bufferKey(s);
    if (key === currentKey) return;
    currentKey = key;

    if (liveDispose) {
      liveDispose();
      liveDispose = null;
    }
    live = null;
    detailView = null;
    lastExternal = null;

    if (s.kind === "creating") {
      form = deps.createForm(s.defaults);
      return;
    }

    if (s.kind === "viewing" || s.kind === "gone") {
      const nibId = s.nibId;
      detailView = deps.detail(nibId);
      form = deps.editForm(nibId, pendingCreateSeed ?? undefined);
      pendingCreateSeed = null;
      // A root per binder, so the next target change can dispose its $effect.
      liveDispose = $effect.root(() => {
        live = deps.liveNib(nibId);
      });
      return;
    }

    form = null;
  }

  function apply(action: Action) {
    const next = reduce(viewState, action);
    if (next.kind === "creating" && initiatesCreate(action)) createNonce++;
    viewState = next;
    reconcileBuffer();
  }

  async function guarded(action: Action): Promise<boolean> {
    if (abandonsBuffer(viewState, action) && form?.dirty) {
      // Only a deleted nib withdraws Save. The prompt always fires: proceeding
      // abandons the edits either way.
      const choice = await deps.confirm({ canSave: canSaveState(viewState) });
      if (choice === "cancel") return false;
      if (choice === "save") {
        // The nib may have been deleted while the prompt was open. Abort: the
        // buffer stays in `gone`, and a retry prompts Discard-only.
        if (!canSaveState(viewState)) {
          deps.notifyError("This nib no longer exists, so your changes can't be saved.");
          return false;
        }
        // The dialog is closed, so another navigation can swap `form` during the save.
        const saved = form;
        const outcome = await save();
        // Conflict or missing: save() has surfaced the resolver or deleted notice.
        if (!outcome || outcome.kind === "conflict" || outcome.kind === "missing") return false;
        // Both form saves suppress the dispatcher toast, so this is the only
        // feedback for a failed save. "Save already in progress" is not actionable.
        if (outcome.kind === "error") {
          if (outcome.message !== "Save already in progress") {
            deps.notifyError(outcome.message ?? "Save failed");
          }
          return false;
        }
        // Proceed only on the form we saved. It changes after a create hand-off
        // (already navigated) or when another navigation ran during the save,
        // whose buffer this action would discard.
        if (form !== saved) return false;
      }
    }
    apply(action);
    return true;
  }

  /** See `ActiveView.noteMissing`. The null-remote conflict fallback calls it too. */
  function noteMissing(nibId: string): MissingNibOutcome {
    const s = viewState;
    if (s.kind === "gone" && s.nibId === nibId) {
      if (s.reason === "archived") apply({ type: "DELETED" });
      return "kept";
    }
    if (s.kind !== "viewing" || s.nibId !== nibId) return "stale";
    // DELETED keeps the `edit:<id>` key, so the unsaved form survives.
    if (form?.dirty) {
      apply({ type: "DELETED" });
      return "kept";
    }
    apply({ type: "CLOSE" });
    return "closed";
  }

  /** See `ActiveView.save`. Also the dirty-guard's Save branch. */
  async function save(): Promise<CreateOutcome | EditOutcome | undefined> {
    const f = form;
    if (!f) return undefined;
    // Refuse silently; callers own the user-facing message.
    if (!canSaveState(viewState)) return undefined;
    if (f.mode === "create") {
      const outcome = await f.save();
      // Hand off only if this create episode is still current; the user may have
      // moved on during the save.
      if (outcome.kind === "created" && form === f) {
        pendingCreateSeed = outcome.snapshot;
        apply({ type: "SAVED", nibId: outcome.id });
        deps.nav.navigateToNib(outcome.id);
      }
      return outcome;
    }

    const outcome = await f.save();

    // NOT_FOUND on this path is the edited nib's own deletion (see EditForm.save).
    // Route to gone/deleted rather than wait for a live signal that may not come.
    if (outcome.kind === "missing" && form === f) {
      noteMissing(f.id);
    }

    if (outcome.kind === "conflict" && outcome.remote === null && form === f) {
      await runNullRemoteConflictFallback(f);
    }
    return outcome;
  }

  // A server-side 409 that raced the live subscription carries no remote, and a
  // lagging subscription may never supply one. Fetch it once: a snapshot feeds
  // the resolver, null routes the buffer to `gone`, a failure toasts.
  //
  // `canSurface` is checked before and after the fetch: same form, still dirty,
  // and no change recorded by the live bridge meanwhile.
  async function runNullRemoteConflictFallback(f: EditForm): Promise<void> {
    const canSurface = () => form === f && f.dirty && f.externalChange === null;
    if (!canSurface() || conflictFallbackFor === f) return;
    conflictFallbackFor = f;
    try {
      let snapshot: NibSnapshot | null = null;
      let loadFailed = false;
      try {
        snapshot = await deps.fetchSnapshot(f.id);
      } catch {
        // A rejection is a load failure; a resolved null is a deletion.
        loadFailed = true;
      }
      if (!canSurface()) return;
      if (snapshot) {
        f.noteExternalChange(snapshot);
      } else if (loadFailed) {
        deps.notifyError(
          "This nib changed on the server and the latest version couldn't be loaded. Please retry.",
        );
      } else {
        noteMissing(f.id);
      }
    } finally {
      // Another form may have claimed the slot mid-fetch; clear only our own.
      if (conflictFallbackFor === f) conflictFallbackFor = null;
    }
  }

  // Bridge the live subscription into the machine and form. A clean buffer
  // rebaselines onto an incoming change and bumps `externalApplied`; a dirty one
  // records it for the Load theirs / Overwrite resolver. `dirty` is untracked so
  // keystrokes do not re-run this.
  $effect(() => {
    const l = live;
    const f = form;
    if (!l) {
      lastExternal = null;
      return;
    }
    // UNARCHIVED is a no-op on anything but `gone`/"archived".
    if (l.gone === "archived") apply({ type: "ARCHIVED" });
    else if (l.gone === "deleted") apply({ type: "DELETED" });
    else apply({ type: "UNARCHIVED" });
    const ext = l.external;
    if (ext && ext !== lastExternal && f && f.mode === "edit") {
      if (untrack(() => f.dirty)) {
        f.noteExternalChange(ext);
      } else {
        f.applyExternal(ext);
        externalApplied++;
      }
      // The bridge now owns this buffer; applyExternal checks no freshness, so
      // stop a slower detail seed from regressing it.
      seededKey = currentKey;
    }
    lastExternal = ext;
  });

  // A buffer that turns clean while an external change is pending (Discard, or
  // edited back to baseline) adopts the remote, leaving no resolver to Overwrite
  // it with stale content.
  $effect(() => {
    const f = form;
    if (!f || f.mode !== "edit") return;
    const ext = f.externalChange;
    const dirty = f.dirty;
    if (ext && !dirty) {
      f.applyExternal(ext);
      externalApplied++;
    }
  });

  /** Project a loaded detail nib onto the form's committed-snapshot shape. */
  function snapshotFromDetail(n: DetailNib): NibSnapshot {
    return {
      id: n.id,
      title: n.title,
      status: n.status,
      type: n.type,
      priority: n.priority ?? "",
      estimate: n.estimate ?? "",
      milestone: n.milestone ?? "",
      area: n.area ?? "",
      tags: n.tags ? [...n.tags] : [],
      body: n.body ?? "",
      etag: n.etag,
    };
  }

  // The edit form starts as a placeholder; adopt the detail snapshot once per
  // buffer key when it loads, so a background refetch never clobbers edits. A
  // dirty buffer is left alone.
  $effect(() => {
    const f = form;
    const d = detailView;
    if (!f || f.mode !== "edit" || !d) return;
    const n = d.nib;
    if (!n || !n.etag) return;
    if (seededKey === currentKey) return;
    // Arm even when skipping below; later changes arrive through the live bridge.
    seededKey = currentKey;
    if (!f.dirty) f.applyExternal(snapshotFromDetail(n));
  });

  return {
    get state() {
      return viewState;
    },
    get form() {
      return form;
    },
    get detail() {
      return detailView;
    },
    get isOpen() {
      return viewState.kind !== "closed";
    },
    get presentation() {
      return viewState.kind === "closed" ? "docked" : viewState.presentation;
    },
    get typePicker() {
      return typePicker;
    },
    invalidateDetailSeed() {
      seededKey = null;
    },
    get blocksHistoryNav() {
      return Boolean(form?.dirty) || typePicker !== null;
    },
    get savePending() {
      // A form that swapped in mid-fallback stays editable.
      return conflictFallbackFor !== null && conflictFallbackFor === form;
    },
    get externalApplied() {
      return externalApplied;
    },

    async open(nibId) {
      // A section-container row names no nib; its empty detail result would read
      // as a deletion. Refuse before the guard can prompt.
      if (isSyntheticRowId(nibId)) return;
      if (await guarded({ type: "OPEN", nibId })) deps.nav.navigateToNib(nibId);
    },
    expand() {
      apply({ type: "EXPAND" });
    },
    collapse() {
      apply({ type: "COLLAPSE" });
    },
    async startCreate(defaults) {
      await guarded({ type: "START_CREATE", defaults });
    },
    async startCreateChild(parentId, parentType, anchor) {
      const validTypes = getValidChildTypes(parentType);
      if (validTypes.length === 0) return; // leaf parent — nothing to create
      if (validTypes.length === 1) {
        // Guarded, so a dirty buffer still prompts. No current type has exactly
        // one child type.
        await guarded({
          type: "START_CREATE",
          defaults: { type: validTypes[0], parent: parentId },
        });
        return;
      }
      // Several valid types: overlay the anchored picker (no buffer change yet).
      typePicker = { parentId, parentType, validTypes, anchor };
    },
    async chooseType(nibType) {
      const tp = typePicker;
      if (!tp) return;
      typePicker = null;
      await guarded({ type: "START_CREATE", defaults: { type: nibType, parent: tp.parentId } });
    },
    cancelType() {
      typePicker = null;
    },
    save,
    async requestClose() {
      if (await guarded({ type: "CLOSE" })) deps.nav.closePanel();
    },
    syncTo(nibId) {
      apply(nibId === null ? { type: "CLOSE" } : { type: "OPEN", nibId });
    },
    noteMissing,
    dispose() {
      if (liveDispose) {
        liveDispose();
        liveDispose = null;
      }
      live = null;
      currentKey = null;
    },
  };
}
