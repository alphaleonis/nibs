/**
 * Reactive shell for the active-nib view presenter.
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

import { untrack } from "svelte";
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

/**
 * The user's choice at the dirty-nav guard. Tri-state so the guard
 * can offer Save alongside Discard/Cancel:
 *   - "save"    — persist the buffer, then (on success) proceed with the nav.
 *   - "discard" — drop the edits and proceed with the nav.
 *   - "cancel"  — keep the edits and stay put.
 */
export type ConfirmChoice = "save" | "discard" | "cancel";

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
  /** One-shot fetch of a nib's CURRENT committed snapshot (network-authoritative,
   *  bypassing any cached value). Used by the null-remote conflict fallback: when
   *  a server-side 409 races the live subscription, we fetch the current remote
   *  directly rather than waiting on a subscription that may be down/lagging.
   *  Resolves the snapshot, resolves null when the nib no longer exists, and
   *  REJECTS on a transport/GraphQL error — the fallback relies on the reject vs
   *  resolve-null distinction to tell a transient load failure apart from a real
   *  deletion (only the former toasts "please retry"). */
  fetchSnapshot: (nibId: string) => Promise<NibSnapshot | null>;
  /** Surface a transient error message to the user (wired to a toast). The
   *  null-remote conflict fallback uses this as the LAST-resort feedback path:
   *  when it can neither surface the inline resolver (no snapshot to reconcile
   *  against) nor defer to a fresher subscription change, the suppressed
   *  dispatcher toast would otherwise leave a rejected save silent. */
  notifyError: (message: string) => void;
  /** Prompt the dirty-nav guard and resolve the user's tri-state choice:
   *  "save" (persist then proceed), "discard" (drop edits and
   *  proceed), or "cancel" (keep edits and stay put). */
  confirm: () => Promise<ConfirmChoice>;
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
  /** True while the active edit form's null-remote conflict fallback is
   *  in flight. `EditForm.save()` resets `form.saving` to false BEFORE the
   *  presenter's fallback fetch begins, so the Save control (keyed on
   *  `form.saving`) would otherwise re-enable mid-fallback and a re-click could
   *  re-dispatch. The Save `disabled` binding ORs this in so the control stays
   *  visibly disabled through the whole fallback. */
  readonly savePending: boolean;
  /** Monotonic counter bumped each time a CLEAN buffer is silently rebaselined
   *  onto an incoming change (in-app or on-disk). The view watches it to fire a
   *  minor "updated" toast; the value itself is opaque (only deltas matter). */
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
  // Bumped whenever the live bridge silently rebaselines a clean buffer onto an
  // incoming change (see the bridge $effect). The view watches it for the toast.
  let externalApplied = $state(0);
  // The edit form whose null-remote conflict fallback is CURRENTLY in
  // flight, or null. Reactive so `savePending` can keep the Save control disabled
  // through the fallback's round-trip; also the in-flight guard that
  // stops a re-entrant save() from starting a second fetch/dispatch/toast.
  let conflictFallbackFor = $state.raw<EditForm | null>(null);

  // Non-reactive bookkeeping.
  let currentKey: string | null = null;
  let createNonce = 0;
  let liveDispose: (() => void) | null = null;
  let lastExternal: NibSnapshot | null = null;
  // A create→edit hand-off stashes the created snapshot here so reconcileBuffer
  // can seed the first edit form for the brand-new id (whose detail query hasn't
  // run yet), avoiding a blank flash. Consumed exactly once, then cleared.
  let pendingCreateSeed: NibSnapshot | null = null;
  // One-shot guard for the async detail-query seed (further below): the buffer
  // key it last seeded. The live bridge ALSO stamps it once it has taken over
  // syncing (F4), so a slower detail seed that resolves with an older snapshot
  // can't regress a buffer the bridge already advanced.
  let seededKey: string | null = null;

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
      const choice = await deps.confirm();
      // "cancel" — keep the edits, stay put (abort the pending navigation).
      if (choice === "cancel") return false;
      if (choice === "save") {
        // "save" — persist the buffer through the normal save path,
        // then decide whether the pending navigation proceeds. save() already
        // routes a 409 into the inline Load-theirs / Overwrite resolver (and its
        // null-remote fallback), so we do NOT reimplement conflict handling here.
        // Capture the form we are saving BEFORE the await: the dialog has already
        // closed, so the UI is interactive during the in-flight save and a
        // competing navigation can swap `form` while we wait (HIGH).
        const saved = form;
        const outcome = await save();
        // Conflict → ABORT the navigation and leave the buffer intact: save() has
        // surfaced the resolver; the user resolves it and re-navigates manually.
        // Never proceed (that would strand the unresolved edit / lose the intent).
        if (!outcome || outcome.kind === "conflict") return false;
        // Plain (non-conflict) error → abort. Both edit AND create save() now
        // suppress the dispatcher toast, so the guard is the SOLE feedback for a
        // failed save in this flow — including a client-side create error (e.g.
        // empty title) that never reaches the dispatcher at all. Skip only the
        // benign "Save already in progress" concurrency result (internal state, not
        // a user-actionable error, L2). Never navigate on a failed save.
        if (outcome.kind === "error") {
          if (outcome.message !== "Save already in progress") {
            deps.notifyError(outcome.message ?? "Save failed");
          }
          return false;
        }
        // Save succeeded. Apply the pending navigation ONLY if we are still on the
        // form we saved. `form` swaps away from `saved` in two cases, and neither
        // may apply the now-stale captured action:
        //   1. CREATE hand-off — save() SAVED-transitions and navigates to the new
        //      nib, so the user is already there; re-applying would double-navigate.
        //   2. A competing navigation ran during the in-flight save (dialog closed,
        //      UI interactive) — applying the stale OPEN/CLOSE over the newer buffer
        //      would swap it out and silently discard its unsaved edits (HIGH).
        // The single identity check covers both; only an EDIT save that stayed on
        // its own (now rebaselined-clean) buffer falls through to navigate.
        if (form !== saved) return false;
      }
      // "discard" — abandon the buffer and proceed.
    }
    apply(action);
    return true;
  }

  /**
   * Persist the active buffer through the normal save path (the SAME routine the
   * Save control invokes). Extracted so the dirty-nav guard's "Save" branch can
   * reuse it without reimplementing the create hand-off / conflict routing.
   *
   * - create → f.save(); on "created" (still this episode) SAVED-transition and
   *   navigate to the new id.
   * - edit → f.save(); a null-remote 409 runs the conflict fallback.
   */
  async function save(): Promise<CreateOutcome | EditOutcome | undefined> {
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

    const outcome = await f.save();

    // A server-side 409 that raced the live subscription (remote unknown): run
    // the null-remote conflict fallback (see the helper). `form === f`
    // is a cheap early-out; the helper re-checks it (and dirtiness / a fresher
    // sub change) before and after its fetch.
    if (outcome.kind === "conflict" && outcome.remote === null && form === f) {
      await runNullRemoteConflictFallback(f);
    }
    return outcome;
  }

  // Null-remote conflict fallback. A server-side 409 that raced the live
  // subscription returns `remote: null` (no snapshot to reconcile against yet). If
  // the subscription is down/lagging it may NEVER backfill `externalChange`,
  // leaving the user stuck with a dirty buffer and — now that the raw toast is
  // suppressed — no feedback at all. Fetch the current remote snapshot
  // once and feed the resolver directly.
  //
  // Freshness guards (`canSurface`, never regress a fresher subscription update):
  //   - `form === f`: the buffer didn't swap to another nib mid-save.
  //   - `f.dirty`: only a dirty edit buffer surfaces the resolver; a Discard that
  //     landed during the fetch means there is nothing to reconcile.
  //   - `f.externalChange === null`: the live bridge is authoritative — if it
  //     recorded a (possibly fresher) change in the meantime, don't clobber it.
  // Checked BEFORE the fetch (skip a needless round-trip if the sub already won)
  // and AFTER it (guard a change that arrived while the fetch was in flight).
  //
  // In-flight guard: `conflictFallbackFor` blocks a re-entrant save()
  // from starting a second concurrent fallback (second fetch/dispatch/toast); the
  // reactive `savePending` flag keeps the Save control disabled through the whole
  // round-trip so the re-click is prevented at the source.
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
        // fetchSnapshot REJECTS on a transport/GraphQL error (App.svelte); a
        // resolved null instead means the nib is genuinely gone. Only a load
        // FAILURE toasts below — the two must not be conflated.
        loadFailed = true;
      }
      // Post-await re-check: a Discard/swap or a fresher subscription change may
      // have landed while the fetch was in flight — cede to it, surface nothing.
      if (!canSurface()) return;
      if (snapshot) {
        f.noteExternalChange(snapshot);
      } else if (loadFailed) {
        // Couldn't load the current revision: the save WAS rejected and its raw
        // toast is suppressed, so degrade to a visible message rather than fail
        // silently. NOT emitted for a deleted nib (snapshot === null, no throw):
        // "please retry" is wrong advice when the nib is gone — the missing-nib
        // path (App.svelte) owns that message.
        deps.notifyError(
          "This nib changed on the server and the latest version couldn't be loaded. Please retry.",
        );
      }
    } finally {
      // Identity-check the clear (regression): if the active form swapped
      // to another form B mid-fetch and B started its own fallback (overwriting
      // this marker), our later-firing finally must NOT null B's still-pending
      // marker — that would flip `savePending` false and reopen B's re-entrancy
      // window. Only clear the slot if it is still ours.
      if (conflictFallbackFor === f) conflictFallbackFor = null;
    }
  }

  // Bridge the live subscription into the machine + form. Reads `live`/`form`
  // reactively so it re-runs both on a target swap and on a new event.
  //
  // Reconciliation axis is DIRTY vs NOT-DIRTY, not in-app vs external — the
  // subscription can't tell whose mutation it is (a context-menu status change
  // is a distinct etag, so it is NOT self-echo-suppressed), and the right
  // behavior only depends on whether we'd lose unsaved edits:
  //   - not dirty -> silently rebaseline onto the incoming version (applyExternal)
  //     and bump `externalApplied` so the view fires a minor "updated" toast.
  //   - dirty     -> record it (noteExternalChange) so the view shows a persistent
  //     "Load theirs / Overwrite" warning region instead of clobbering edits.
  // `dirty` is read untracked so a later keystroke doesn't re-run this bridge —
  // only a genuinely new `ext` (guarded by `ext !== lastExternal`) acts.
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
      if (untrack(() => f.dirty)) {
        f.noteExternalChange(ext);
      } else {
        f.applyExternal(ext);
        externalApplied++;
      }
      // F4: the live bridge is now the source of truth for this buffer — stamp
      // the one-shot seed guard so a slower detail-query seed that resolves with
      // an OLDER snapshot can't regress the buffer/etag backward (both
      // applyExternal paths apply unconditionally, with no freshness check).
      seededKey = currentKey;
    }
    lastExternal = ext;
  });

  // F1: a NOT-dirty buffer must never be able to force-overwrite the remote's
  // newer change with stale content. When the buffer converges back to not-dirty
  // while an external change is still pending — the user hit Discard, or edited
  // back to the baseline, with the conflict resolver up — adopt the remote
  // through the CLEAN path: applyExternal rebaselines onto it and clears the
  // resolver, and we advance externalApplied for the minor "updated" toast. This
  // makes a stale Overwrite structurally impossible (a not-dirty buffer has no
  // resolver to Overwrite from). Distinct from the bridge above (which reacts to
  // new live events); this fires on the dirty→not-dirty transition itself.
  $effect(() => {
    const f = form;
    if (!f || f.mode !== "edit") return;
    const ext = f.externalChange; // tracked: convergence-derived, null when resolved
    const dirty = f.dirty; // tracked
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
    get savePending() {
      // Only while THIS view's active form is the one in fallback — a buffer that
      // swapped mid-fallback is a different, editable form and must not be frozen.
      return conflictFallbackFor !== null && conflictFallbackFor === form;
    },
    get externalApplied() {
      return externalApplied;
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
        // Defensive fast-path: under the current backend-mirrored hierarchy every
        // non-leaf parent has >=2 child types, so this branch does not fire today —
        // it's kept for a future hierarchy where a type has exactly one child.
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
    save,
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
