/**
 * Reactive shell for the active-nib view presenter (#nibs-wvl2).
 *
 * Owns a single `ViewState` and the form/live/detail lifecycle around it. Every
 * public transition routes through the pure kernel (`activeView.ts`) via one of
 * two funnels:
 *   - `apply(action)`   — reduce + reconcile the working-copy buffer
 *   - `guarded(action)` — the SINGLE place the dirty-guard lives; buffer-abandoning
 *                         actions await a discard confirm when the form is dirty
 *
 * The buffer (form + live subscription + detail query) is keyed on
 * content-identity (`edit:<id>` / `create:<nonce>`), so it survives
 * expand/collapse/save-same-nib and is recreated only on a real target change.
 * A single `$effect` bridges the live subscription into the machine:
 * `live.deleted -> DELETED` and `live.external -> form.noteExternalChange`.
 *
 * Everything is dependency-injected (nav, form/live/detail factories, confirm)
 * so the shell is boundary-testable with stubs under `$effect.root`.
 */

import { getValidChildTypes } from "../typeHierarchy";
import {
  reduce,
  abandonsBuffer,
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

/** Minimal nib reference shape used by relation lists. */
export interface DetailNibRef {
  id: string;
  title: string;
  type: string;
  status: string;
}

/** The `NIB_DETAIL_QUERY` nib (relations + documents) the view rail consumes. */
export interface DetailNib {
  id: string;
  title: string;
  status: string;
  type: string;
  priority?: string | null;
  estimate?: string | null;
  tags?: readonly string[];
  body?: string;
  documents?: readonly string[];
  etag: string;
  parent?: DetailNibRef | null;
  children?: readonly DetailNibRef[];
  blocking?: readonly DetailNibRef[];
  blockedBy?: readonly DetailNibRef[];
  mentions?: readonly DetailNibRef[];
  mentionedBy?: readonly DetailNibRef[];
}

/** The reactive detail-query wrapper injected by the app (a single live query). */
export interface DetailView {
  readonly nib: DetailNib | null;
  readonly fetching: boolean;
}

export interface ActiveViewDeps {
  /** History/URL navigation (delegated, never owned). */
  nav: Pick<HistoryNav, "navigateToNib" | "closePanel" | "replaceClosed">;
  /** Build an edit form for a nib. Seeds from the shared detail query (no
   *  re-fetch); a create→edit hand-off may pass the freshly-created snapshot so
   *  the first edit form renders immediately (its detail query hasn't run yet). */
  editForm: (nibId: string, seed?: NibSnapshot) => EditForm;
  /** Build a create form for the given defaults. */
  createForm: (defaults: CreateDefaults) => CreateForm;
  /** Build the live-change subscription binder for a nib. */
  liveNib: (nibId: string) => LiveNib;
  /** The single `NIB_DETAIL_QUERY` wrapper for a nib (relations/documents). */
  detail: (nibId: string) => DetailView;
  /** Ask the user to discard unsaved changes; resolve false to keep them. */
  confirm: () => Promise<boolean>;
}

/** A viewport-space rectangle (from getBoundingClientRect) the type picker anchors to. */
export interface AnchorRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

/**
 * Transient state for the add-child type picker. Kept OUTSIDE the ViewState
 * machine so it overlays the current view (docked nib, table, …) as an anchored
 * popover instead of replacing it — picking a type never swaps the detail buffer
 * until the user commits to a type (which then runs a normal guarded START_CREATE).
 */
export interface TypePickerState {
  parentId: string;
  parentType: string;
  validTypes: string[];
  anchor: AnchorRect;
}

export interface ActiveView {
  readonly state: ViewState;
  readonly form: CreateForm | EditForm | null;
  readonly detail: DetailView | null;
  readonly isOpen: boolean;
  readonly presentation: Presentation;
  /** The open add-child type picker, or null. Overlays the view (never replaces it). */
  readonly typePicker: TypePickerState | null;
  /** True while Back/Forward must be frozen: dirty buffer or an open type picker. */
  readonly blocksHistoryNav: boolean;

  open(nibId: string): Promise<void>;
  expand(): void;
  collapse(): void;
  startCreate(defaults: { type: string; parent?: string }): Promise<void>;
  /** Add a child of `parentId`: 1 valid type → create directly; ≥2 → open the
   *  type picker anchored to `anchor` (the clicked control's viewport rect). */
  startCreateChild(parentId: string, parentType: string, anchor: AnchorRect): Promise<void>;
  chooseType(nibType: string): Promise<void>;
  cancelType(): void;
  save(): Promise<CreateOutcome | EditOutcome | undefined>;
  requestClose(): Promise<void>;
  /** The SOLE guard-bypass: popstate / multi-select desync (history already moved). */
  syncTo(nibId: string | null): void;
  /** Tear down the live subscription (call on host teardown). */
  dispose(): void;
}

export function createActiveView(deps: ActiveViewDeps): ActiveView {
  // All four hold references we swap WHOLESALE (never mutate through) — the
  // reduced state is immutable, and the form/live/detail objects own their own
  // internal reactivity. `$state.raw` keeps their identity intact (no proxy) so
  // `view.form === theInstance`, while reference swaps still fire reactivity.
  let viewState = $state.raw<ViewState>({ kind: "closed" });
  let form = $state.raw<CreateForm | EditForm | null>(null);
  let detailView = $state.raw<DetailView | null>(null);
  let live = $state.raw<LiveNib | null>(null);
  // The add-child type picker overlays the view; it is deliberately NOT part of
  // the ViewState machine (see TypePickerState) so it never disturbs the buffer.
  let typePicker = $state.raw<TypePickerState | null>(null);

  // Non-reactive bookkeeping.
  let currentKey: string | null = null;
  let createNonce = 0;
  let liveDispose: (() => void) | null = null;
  let lastExternal: NibSnapshot | null = null;
  // A create→edit hand-off stashes the created snapshot here so reconcileBuffer
  // can seed the first edit form for the brand-new id (whose detail query hasn't
  // run yet), avoiding a blank flash. Consumed exactly once, then cleared.
  let pendingCreateSeed: NibSnapshot | null = null;

  /** Content-identity of the current buffer, or null when there is none. */
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

  /** Only this action (re)enters a fresh create episode → bump the nonce. */
  function initiatesCreate(action: Action): boolean {
    return action.type === "START_CREATE";
  }

  /** Reconcile the form/live/detail to match the current buffer identity. */
  function reconcileBuffer() {
    const s = viewState;
    const key = bufferKey(s);
    // Same buffer target: survive expand/collapse/save-same-nib untouched.
    if (key === currentKey) return;
    currentKey = key;

    // Tear down the previous live subscription before swapping targets.
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
      // Own the live subscription in its own root: disposing it on the next
      // target change tears down the internal $effect the real binder registers.
      liveDispose = $effect.root(() => {
        live = deps.liveNib(nibId);
      });
      return;
    }

    // closed: no buffer.
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
      const ok = await deps.confirm();
      if (!ok) return false;
    }
    apply(action);
    return true;
  }

  // Bridge the live subscription into the machine + form. Reads `live`/`form`
  // reactively so it re-runs both on a target swap and on a new event.
  $effect(() => {
    const l = live;
    const f = form;
    if (!l) {
      lastExternal = null;
      return;
    }
    if (l.deleted) apply({ type: "DELETED" });
    const ext = l.external;
    if (ext && ext !== lastExternal && f && f.mode === "edit") {
      f.noteExternalChange(ext);
    }
    lastExternal = ext;
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
      tags: n.tags ? [...n.tags] : [],
      body: n.body ?? "",
      etag: n.etag,
    };
  }

  // Async edit-form seed. `editForm(id)` is built eagerly with a placeholder so
  // the view has a form to render immediately; the real snapshot only arrives
  // once the (async) detail query resolves. When it does, adopt it via
  // `applyExternal` — which rebaselines the working copy and clears dirt, so a
  // freshly-opened nib is pristine and its editor re-inits with the real body.
  // Guards: only edit forms, only a fully-loaded nib (etag present), exactly
  // once per buffer identity (`currentKey`) so a background refetch never
  // clobbers in-progress edits, and only while the buffer is pristine — a dirty
  // buffer is left untouched (the one-shot still arms so it won't re-seed later).
  let seededKey: string | null = null;
  $effect(() => {
    const f = form;
    const d = detailView;
    if (!f || f.mode !== "edit" || !d) return;
    const n = d.nib;
    if (!n || !n.etag) return;
    if (seededKey === currentKey) return;
    // Mark the key seeded even when we skip below, so this one-shot never
    // re-fires for the same buffer: once seeded, a later genuine external change
    // arrives via the live bridge's noteExternalChange, not applyExternal.
    seededKey = currentKey;
    // Don't rebaseline over in-progress edits: applyExternal wipes the working
    // copy. If the user already typed before the detail landed, keep their buffer.
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
    get blocksHistoryNav() {
      return Boolean(form?.dirty) || typePicker !== null;
    },

    async open(nibId) {
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
        // Unambiguous: create directly (guarded, so a dirty buffer still prompts).
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
      // Committing to a type is a normal guarded create off the picked parent.
      await guarded({ type: "START_CREATE", defaults: { type: nibType, parent: tp.parentId } });
    },
    cancelType() {
      typePicker = null;
    },
    async save() {
      const f = form;
      if (!f) return undefined;
      if (f.mode === "create") {
        const outcome = await f.save();
        // Re-validate the buffer is still THIS create episode before handing off:
        // the user may have closed / opened another nib / started a new create while
        // save() was in flight (create forms don't rebaseline mid-save, so dirty stays
        // true and those transitions aren't blocked). Firing nav unconditionally would
        // yank the URL to a nib the presenter no longer reflects.
        if (outcome.kind === "created" && form === f) {
          // Hand the created snapshot to the edit form the SAVED transition
          // builds so it renders the new nib immediately (its detail query
          // hasn't run yet). reconcileBuffer consumes + clears it.
          pendingCreateSeed = outcome.snapshot;
          apply({ type: "SAVED", nibId: outcome.id });
          deps.nav.navigateToNib(outcome.id);
        }
        return outcome;
      }
      return await f.save();
    },
    async requestClose() {
      if (await guarded({ type: "CLOSE" })) deps.nav.closePanel();
    },
    syncTo(nibId) {
      apply(nibId === null ? { type: "CLOSE" } : { type: "OPEN", nibId });
    },
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
