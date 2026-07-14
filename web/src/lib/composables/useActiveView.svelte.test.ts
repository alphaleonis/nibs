import { describe, it, expect, vi } from "vitest";
import { flushSync } from "svelte";
import {
  createActiveView,
  type ActiveViewDeps,
  type ActiveView,
  type ConfirmChoice,
} from "./useActiveView.svelte";
import type { CreateForm, EditForm, CreateDefaults, NibSnapshot } from "../nibForm.svelte";
import type { LiveNib } from "../liveNib.svelte";

function snap(overrides: Partial<NibSnapshot> = {}): NibSnapshot {
  return {
    id: "n1",
    title: "T",
    status: "todo",
    type: "task",
    priority: "",
    estimate: "",
    tags: [],
    body: "",
    etag: "e0",
    ...overrides,
  };
}

interface FakeEdit {
  mode: "edit";
  id: string;
  dirty: boolean;
  etag: string;
  title: string;
  body: string;
  externalChange: NibSnapshot | null;
  noteExternalChange: ReturnType<typeof vi.fn>;
  applyExternal: ReturnType<typeof vi.fn>;
  save: ReturnType<typeof vi.fn>;
}

interface FakeCreate {
  mode: "create";
  dirty: boolean;
  save: ReturnType<typeof vi.fn>;
}

interface LiveInst {
  deleted: boolean;
  external: NibSnapshot | null;
  error: unknown;
}

/** Build a fully-stubbed dependency set with observable spies/handles. */
function makeDeps() {
  const nav = { navigateToNib: vi.fn(), closePanel: vi.fn(), replaceClosed: vi.fn() };
  // Tri-state dirty-guard confirm: "save" | "discard" | "cancel".
  // Defaults to "discard" — the pre-Save behavior (proceed, abandoning edits).
  const confirm = vi.fn<() => Promise<ConfirmChoice>>(async () => "discard");

  const editForms = new Map<string, FakeEdit>();
  const createForms: FakeCreate[] = [];
  const liveInsts = new Map<string, LiveInst>();
  const detailInsts = new Map<string, { nib: unknown; fetching: boolean }>();
  const created: string[] = [];
  const disposed: string[] = [];

  // Honors a create→edit `seed` (Fix 2): the edit form adopts the seed's
  // title/body/etag so the create hand-off carries the new nib's content.
  // A `$state` object so `dirty`/`externalChange` are reactive — the presenter's
  // conflict-adopt effect (F1) tracks them. The spies faithfully mirror the real
  // EditForm's state transitions (noteExternalChange records, applyExternal clears).
  const editForm = (nibId: string, seed?: NibSnapshot): EditForm => {
    const f = $state<FakeEdit>({
      mode: "edit",
      id: nibId,
      dirty: false,
      etag: seed?.etag ?? "e0",
      title: seed?.title ?? "",
      body: seed?.body ?? "",
      externalChange: null,
      noteExternalChange: vi.fn((remote: NibSnapshot) => {
        f.externalChange = remote;
      }),
      applyExternal: vi.fn((remote: NibSnapshot) => {
        f.externalChange = null;
        f.etag = remote.etag;
        f.dirty = false;
      }),
      save: vi.fn(async () => ({ kind: "saved", snapshot: snap({ id: nibId }) })),
    });
    editForms.set(nibId, f);
    return f as unknown as EditForm;
  };

  const createForm = (_defaults: CreateDefaults): CreateForm => {
    const f: FakeCreate = {
      mode: "create",
      dirty: false,
      save: vi.fn(async () => ({
        kind: "created",
        id: "nibs-new1",
        snapshot: snap({ id: "nibs-new1", title: "Fresh", body: "Hello body", etag: "e-new" }),
      })),
    };
    createForms.push(f);
    return f as unknown as CreateForm;
  };

  const liveNib = (nibId: string): LiveNib => {
    created.push(nibId);
    const inst = $state<LiveInst>({ deleted: false, external: null, error: undefined });
    liveInsts.set(nibId, inst);
    // Registers a cleanup so the shell's per-target $effect.root disposal is
    // observable (mirrors the real liveNib's internal subscription teardown).
    $effect(() => () => {
      disposed.push(nibId);
    });
    return {
      get deleted() {
        return inst.deleted;
      },
      get external() {
        return inst.external;
      },
      get error() {
        return inst.error;
      },
    };
  };

  const detail = (nibId: string) => {
    const inst = $state<{ nib: unknown; fetching: boolean }>({ nib: null, fetching: true });
    detailInsts.set(nibId, inst);
    return {
      get nib() {
        return inst.nib as never;
      },
      get fetching() {
        return inst.fetching;
      },
    };
  };

  // One-shot "current snapshot" fetch used by the null-remote conflict fallback.
  // Defaults to null (no remote); tests override per-call.
  const fetchSnapshot = vi.fn(async (_nibId: string): Promise<NibSnapshot | null> => null);

  // Last-resort feedback used by the null-remote conflict fallback when its fetch
  // FAILS — no snapshot to resolve against, no deletion to report (wired to a toast).
  const notifyError = vi.fn<(message: string) => void>();

  const deps: ActiveViewDeps = {
    nav,
    editForm,
    createForm,
    liveNib,
    detail,
    fetchSnapshot,
    notifyError,
    confirm,
  };
  return {
    deps,
    nav,
    confirm,
    editForms,
    createForms,
    liveInsts,
    detailInsts,
    fetchSnapshot,
    notifyError,
    created,
    disposed,
  };
}

/** Create the presenter inside a fresh effect root; returns view + disposer. */
function mount(deps: ActiveViewDeps): { view: ActiveView; dispose: () => void } {
  let view!: ActiveView;
  const dispose = $effect.root(() => {
    view = createActiveView(deps);
  });
  flushSync();
  return { view, dispose };
}

describe("createActiveView · buffer lifecycle", () => {
  it("keeps the same form instance across expand/collapse (identity survives)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f1 = view.form;
    expect(f1).toBe(h.editForms.get("n1"));
    expect(h.created).toEqual(["n1"]);

    view.expand();
    flushSync();
    expect(view.form).toBe(f1);
    expect(view.presentation).toBe("expanded");

    view.collapse();
    flushSync();
    expect(view.form).toBe(f1);
    expect(view.presentation).toBe("docked");
    // No new live subscription was opened for the expand/collapse churn.
    expect(h.created).toEqual(["n1"]);

    dispose();
  });

  it("swaps the buffer and disposes the old live on a real target change", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f1 = view.form;

    await view.open("n2");
    flushSync();

    expect(view.form).toBe(h.editForms.get("n2"));
    expect(view.form).not.toBe(f1);
    expect(h.created).toEqual(["n1", "n2"]);
    expect(h.disposed).toContain("n1");

    dispose();
  });

  it("exposes the injected detail wrapper for the open nib", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    expect(view.detail?.fetching).toBe(true);
    expect(view.detail?.nib).toBeNull();

    h.detailInsts.get("n1")!.nib = { id: "n1", title: "Loaded" };
    h.detailInsts.get("n1")!.fetching = false;
    flushSync();
    expect(view.detail?.fetching).toBe(false);
    expect((view.detail?.nib as { title: string }).title).toBe("Loaded");

    dispose();
  });

  it("gives two separate create episodes distinct buffers", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.startCreate({ type: "task" });
    flushSync();
    const c1 = view.form;
    expect(c1).toBe(h.createForms[0]);

    await view.startCreate({ type: "bug" });
    flushSync();
    expect(view.form).toBe(h.createForms[1]);
    expect(view.form).not.toBe(c1);

    dispose();
  });
});

describe("createActiveView · guard funnel", () => {
  it("refuses close when the form is dirty and confirm resolves cancel", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    h.editForms.get("n1")!.dirty = true;
    h.confirm.mockResolvedValueOnce("cancel");

    await view.requestClose();
    expect(h.confirm).toHaveBeenCalledTimes(1);
    expect(view.state.kind).toBe("viewing");
    expect(h.nav.closePanel).not.toHaveBeenCalled();

    dispose();
  });

  it("refuses opening another nib when dirty and confirm resolves cancel", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    h.editForms.get("n1")!.dirty = true;
    h.confirm.mockResolvedValueOnce("cancel");

    await view.open("n2");
    expect(h.confirm).toHaveBeenCalledTimes(1);
    expect(view.state).toEqual({ kind: "viewing", nibId: "n1", presentation: "docked" });
    expect(h.nav.navigateToNib).not.toHaveBeenCalledWith("n2");

    dispose();
  });

  it("refuses startCreate when dirty and confirm resolves cancel", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    h.editForms.get("n1")!.dirty = true;
    h.confirm.mockResolvedValueOnce("cancel");

    await view.startCreate({ type: "bug" });
    expect(h.confirm).toHaveBeenCalledTimes(1);
    expect(view.state.kind).toBe("viewing");

    dispose();
  });

  it("proceeds through the guard when confirm resolves discard", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    h.editForms.get("n1")!.dirty = true;
    h.confirm.mockResolvedValueOnce("discard");

    await view.requestClose();
    flushSync();
    expect(view.state.kind).toBe("closed");
    expect(h.nav.closePanel).toHaveBeenCalledTimes(1);
    // Discard drops the buffer without saving.
    expect(h.editForms.get("n1")!.save).not.toHaveBeenCalled();

    dispose();
  });

  it("does not prompt when opening the same nib (resync, not abandon)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    h.editForms.get("n1")!.dirty = true;

    await view.open("n1");
    expect(h.confirm).not.toHaveBeenCalled();
    expect(h.nav.navigateToNib).toHaveBeenLastCalledWith("n1");
    expect(view.state.kind).toBe("viewing");

    dispose();
  });

  it("syncTo bypasses the guard entirely (no confirm, no nav push)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    h.editForms.get("n1")!.dirty = true;
    h.nav.navigateToNib.mockClear();

    view.syncTo("n2");
    flushSync();
    expect(h.confirm).not.toHaveBeenCalled();
    expect(h.nav.navigateToNib).not.toHaveBeenCalled();
    expect(view.state).toEqual({ kind: "viewing", nibId: "n2", presentation: "docked" });

    view.syncTo(null);
    flushSync();
    expect(view.state.kind).toBe("closed");

    dispose();
  });
});

describe("createActiveView · missing nib", () => {
  it("closes a CLEAN buffer and reports it closed (stale deep link)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();

    // Nothing unsaved to lose: drop the view so the caller heals the URL.
    expect(view.noteMissing("n1")).toBe("closed");
    flushSync();
    expect(view.state.kind).toBe("closed");
    expect(view.form).toBeNull();
    expect(h.confirm).not.toHaveBeenCalled();

    dispose();
  });

  it("routes a DIRTY buffer to gone, preserving the form and its edits", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = view.form;
    h.editForms.get("n1")!.dirty = true;

    // Unsaved edits must survive: same outcome as the live-subscription deletion
    // path (gone), never a silent close.
    expect(view.noteMissing("n1")).toBe("kept");
    flushSync();
    expect(view.state).toEqual({ kind: "gone", nibId: "n1", presentation: "docked" });
    expect(view.form).toBe(f);
    expect(view.blocksHistoryNav).toBe(true);
    // The buffer is preserved outright — no discard prompt is involved.
    expect(h.confirm).not.toHaveBeenCalled();

    dispose();
  });

  it("ignores a stale report for a nib the view has already moved off", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    await view.open("n2");
    flushSync();

    // A report queued for n1 must not disturb the n2 buffer. "stale" — distinct
    // from "kept": nothing was preserved, the view simply is not on n1.
    expect(view.noteMissing("n1")).toBe("stale");
    flushSync();
    expect(view.state).toEqual({ kind: "viewing", nibId: "n2", presentation: "docked" });
    expect(view.form).toBe(h.editForms.get("n2"));

    dispose();
  });

  it("reports 'kept' for a repeat report while already gone on the same id, changing nothing", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = view.form;
    h.editForms.get("n1")!.dirty = true;

    expect(view.noteMissing("n1")).toBe("kept");
    flushSync();
    const goneState = view.state;

    // A second report for the SAME id while already `gone` is not stale: the
    // buffer for n1 is still on screen behind the deleted notice. "kept" is what
    // tells the caller the ?nib= URL still describes something and must survive.
    expect(view.noteMissing("n1")).toBe("kept");
    flushSync();
    expect(view.state).toEqual(goneState);
    expect(view.form).toBe(f);
    expect(h.editForms.get("n1")!.dirty).toBe(true);

    dispose();
  });

  it("reports 'kept' from gone even when the buffer is PRISTINE (live-bridge deletion)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = view.form;

    // The live bridge applies DELETED with no dirty gate, so a pristine nib
    // reaches `gone` with no report ever made. A detail-query report arriving
    // afterward must not close it out from under the notice.
    h.liveInsts.get("n1")!.deleted = true;
    flushSync();
    expect(view.state.kind).toBe("gone");

    expect(view.noteMissing("n1")).toBe("kept");
    flushSync();
    expect(view.state).toEqual({ kind: "gone", nibId: "n1", presentation: "docked" });
    expect(view.form).toBe(f);

    dispose();
  });

  it("survives a same-id resync while gone: keeps the buffer and re-converges", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = view.form;
    h.editForms.get("n1")!.dirty = true;

    expect(view.noteMissing("n1")).toBe("kept");
    flushSync();
    expect(view.state.kind).toBe("gone");

    // Back/Forward while a dirty `gone` buffer blocks history nav: handlePopState
    // re-anchors on the SAME id, and the unconditional syncTo that follows bounces
    // gone -> viewing. The buffer key (`edit:n1`) is identical across both states,
    // so reconcileBuffer must leave the form — and the user's edits — untouched.
    view.syncTo("n1");
    flushSync();
    expect(view.state).toEqual({ kind: "viewing", nibId: "n1", presentation: "docked" });
    expect(view.form).toBe(f);
    expect(h.editForms.get("n1")!.dirty).toBe(true);

    // The nib is still missing, so the re-armed report converges straight back to
    // `gone` on the same buffer — no loop, no rebuild, no lost edits.
    expect(view.noteMissing("n1")).toBe("kept");
    flushSync();
    expect(view.state).toEqual({ kind: "gone", nibId: "n1", presentation: "docked" });
    expect(view.form).toBe(f);
    expect(h.confirm).not.toHaveBeenCalled();

    dispose();
  });
});

describe("createActiveView · guard funnel · Save option", () => {
  it("Save → saved: persists the edit buffer, then PROCEEDS with the pending navigation", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    f.dirty = true;
    // Default edit save() resolves { kind: "saved" }.
    h.confirm.mockResolvedValueOnce("save");
    h.nav.navigateToNib.mockClear();

    await view.open("n2");
    flushSync();

    // The buffer was saved through the normal path...
    expect(f.save).toHaveBeenCalledTimes(1);
    // ...and the pending navigation then proceeded in the same step.
    expect(h.nav.navigateToNib).toHaveBeenCalledTimes(1);
    expect(h.nav.navigateToNib).toHaveBeenCalledWith("n2");
    expect(view.state).toEqual({ kind: "viewing", nibId: "n2", presentation: "docked" });

    dispose();
  });

  it("Save → 409 conflict: ABORTS the navigation, leaves the buffer intact, and surfaces the resolver via save()", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    f.dirty = true;

    // The save rejects on a stale if-match with an unknown (null) remote; the
    // presenter's null-remote fallback fetches the current snapshot and feeds it
    // into the inline Load-theirs / Overwrite resolver (save() owns this).
    const remote = snap({ id: "n1", etag: "e9", title: "Server copy" });
    h.fetchSnapshot.mockResolvedValueOnce(remote);
    f.save.mockResolvedValueOnce({ kind: "conflict", remote: null });
    h.confirm.mockResolvedValueOnce("save");
    h.nav.navigateToNib.mockClear();

    await view.open("n2");
    flushSync();

    // The save was attempted and the resolver surfaced (buffer preserved)...
    expect(f.save).toHaveBeenCalledTimes(1);
    expect(h.fetchSnapshot).toHaveBeenCalledWith("n1");
    expect(f.noteExternalChange).toHaveBeenCalledWith(remote);
    // ...but the navigation was ABORTED — the user stays on the dirty n1 buffer
    // to resolve the conflict and re-navigate manually. No data lost.
    expect(h.nav.navigateToNib).not.toHaveBeenCalled();
    expect(view.state).toEqual({ kind: "viewing", nibId: "n1", presentation: "docked" });
    expect(f.dirty).toBe(true);

    dispose();
  });

  it("Save → plain error: ABORTS the navigation and surfaces the error via the toast channel", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    f.dirty = true;

    f.save.mockResolvedValueOnce({ kind: "error", message: "server exploded" });
    h.confirm.mockResolvedValueOnce("save");
    h.nav.navigateToNib.mockClear();

    await view.open("n2");
    flushSync();

    // The error is surfaced (the edit path suppresses the dispatcher toast, so
    // the guard is the only feedback) and the navigation is aborted.
    expect(h.notifyError).toHaveBeenCalledTimes(1);
    expect(h.notifyError).toHaveBeenCalledWith("server exploded");
    expect(h.nav.navigateToNib).not.toHaveBeenCalled();
    expect(view.state).toEqual({ kind: "viewing", nibId: "n1", presentation: "docked" });
    expect(f.dirty).toBe(true);

    dispose();
  });

  it("Save on a CREATE buffer: the create's own post-save navigation wins; the guard's pending nav is NOT double-applied", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.startCreate({ type: "task" });
    flushSync();
    const c = h.createForms[0];
    c.dirty = true;
    // Default create save() resolves { kind: "created", id: "nibs-new1", ... },
    // and the presenter internally SAVED-transitions + navigates to the new id.
    h.confirm.mockResolvedValueOnce("save");
    h.nav.navigateToNib.mockClear();

    // Guarded action targets n2, but the create's own navigation must win.
    await view.open("n2");
    flushSync();

    expect(c.save).toHaveBeenCalledTimes(1);
    // Exactly ONE navigation happened — to the freshly-created nib, not n2.
    expect(h.nav.navigateToNib).toHaveBeenCalledTimes(1);
    expect(h.nav.navigateToNib).toHaveBeenCalledWith("nibs-new1");
    expect(h.nav.navigateToNib).not.toHaveBeenCalledWith("n2");
    expect(view.state).toEqual({ kind: "viewing", nibId: "nibs-new1", presentation: "docked" });

    dispose();
  });

  it("Discard still proceeds with the navigation without saving (unchanged)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    f.dirty = true;
    h.confirm.mockResolvedValueOnce("discard");
    h.nav.navigateToNib.mockClear();

    await view.open("n2");
    flushSync();

    expect(f.save).not.toHaveBeenCalled();
    expect(h.nav.navigateToNib).toHaveBeenCalledWith("n2");
    expect(view.state).toEqual({ kind: "viewing", nibId: "n2", presentation: "docked" });

    dispose();
  });

  it("Cancel stays put without saving (unchanged)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    f.dirty = true;
    h.confirm.mockResolvedValueOnce("cancel");
    h.nav.navigateToNib.mockClear();

    await view.open("n2");
    flushSync();

    expect(f.save).not.toHaveBeenCalled();
    expect(h.nav.navigateToNib).not.toHaveBeenCalled();
    expect(view.state).toEqual({ kind: "viewing", nibId: "n1", presentation: "docked" });

    dispose();
  });

  it("Save in flight: a competing navigation that swaps the form mid-await does NOT apply the stale pending action (no silent discard) (HIGH)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const a = h.editForms.get("n1")!;
    a.dirty = true;

    // Gate A's save so we can interleave a navigation while it is in flight.
    let releaseSave!: (v: { kind: "saved"; snapshot: NibSnapshot }) => void;
    a.save.mockImplementationOnce(
      () => new Promise((resolve) => (releaseSave = resolve)),
    );
    h.confirm.mockResolvedValueOnce("save");
    h.nav.navigateToNib.mockClear();

    // Navigate away → guard → "save" → parks on the gated save. Do NOT await yet.
    const pending = view.open("n2");
    await new Promise((r) => setTimeout(r));
    flushSync();
    expect(a.save).toHaveBeenCalledTimes(1);

    // While the save is in flight (dialog already closed, UI interactive), a
    // competing navigation (Back/Forward guard-bypass) swaps the active form to n3.
    view.syncTo("n3");
    flushSync();
    expect(view.state).toEqual({ kind: "viewing", nibId: "n3", presentation: "docked" });

    // A's save now resolves. The guard must NOT apply the stale OPEN n2 over n3
    // (that would swap the buffer and silently discard n3's edits).
    releaseSave({ kind: "saved", snapshot: snap({ id: "n1", etag: "e1" }) });
    await pending;
    flushSync();

    expect(h.nav.navigateToNib).not.toHaveBeenCalledWith("n2");
    expect(view.state).toEqual({ kind: "viewing", nibId: "n3", presentation: "docked" });

    dispose();
  });

  it("Save → CREATE error (incl. client-side, dispatcher-bypassing): guard surfaces exactly one notifyError — no silent no-op (HIGH)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.startCreate({ type: "task" });
    flushSync();
    const c = h.createForms[0];
    c.dirty = true;
    // A client-side create error (e.g. empty title) never reaches the dispatcher,
    // and create save() now suppresses the dispatcher toast anyway — so the guard
    // is the SOLE owner and MUST surface it, else Save is a silent no-op.
    c.save.mockResolvedValueOnce({ kind: "error", message: "Title is required" });
    h.confirm.mockResolvedValueOnce("save");
    h.nav.navigateToNib.mockClear();

    await view.open("n2");
    flushSync();

    expect(c.save).toHaveBeenCalledTimes(1);
    expect(h.notifyError).toHaveBeenCalledTimes(1);
    expect(h.notifyError).toHaveBeenCalledWith("Title is required");
    expect(h.nav.navigateToNib).not.toHaveBeenCalled();

    dispose();
  });

  it("Save → benign 'Save already in progress' result: does NOT notifyError (internal state, not user-actionable) (L2)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    f.dirty = true;
    // A concurrent save is already in flight → the form's save() short-circuits.
    f.save.mockResolvedValueOnce({ kind: "error", message: "Save already in progress" });
    h.confirm.mockResolvedValueOnce("save");
    h.nav.navigateToNib.mockClear();

    await view.open("n2");
    flushSync();

    // The benign in-progress result is not surfaced; navigation still aborted.
    expect(h.notifyError).not.toHaveBeenCalled();
    expect(h.nav.navigateToNib).not.toHaveBeenCalled();
    expect(view.state).toEqual({ kind: "viewing", nibId: "n1", presentation: "docked" });

    dispose();
  });
});

describe("createActiveView · create -> edit hand-off", () => {
  it("save() on a create commits, dispatches SAVED, and navigates to the new id", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.startCreate({ type: "task" });
    flushSync();
    expect(view.state.kind).toBe("creating");

    const outcome = await view.save();
    flushSync();

    expect(outcome).toEqual({ kind: "created", id: "nibs-new1", snapshot: expect.any(Object) });
    expect(view.state).toEqual({ kind: "viewing", nibId: "nibs-new1", presentation: "docked" });
    expect(view.form?.mode).toBe("edit");
    expect(h.nav.navigateToNib).toHaveBeenCalledWith("nibs-new1");

    // Fix 2: the edit form the SAVED transition builds is seeded from the created
    // snapshot (no blank flash), so it carries the created title/body/etag before
    // the new nib's detail query has run.
    const edited = h.editForms.get("nibs-new1")!;
    expect(edited.title).toBe("Fresh");
    expect(edited.body).toBe("Hello body");
    expect(edited.etag).toBe("e-new");

    dispose();
  });

  it("save() on an edit delegates to the form without a state transition", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const outcome = await view.save();

    expect(h.editForms.get("n1")!.save).toHaveBeenCalledTimes(1);
    expect(outcome).toEqual({ kind: "saved", snapshot: expect.any(Object) });
    expect(view.state).toEqual({ kind: "viewing", nibId: "n1", presentation: "docked" });

    dispose();
  });
});

describe("createActiveView · async edit-form seed", () => {
  it("does NOT overwrite the buffer when it is dirty before the detail lands", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    // The user types into the buffer before the (async) detail query resolves.
    f.dirty = true;

    // Detail resolves with a real, etagged snapshot.
    h.detailInsts.get("n1")!.nib = { id: "n1", title: "Loaded", etag: "e1" };
    h.detailInsts.get("n1")!.fetching = false;
    flushSync();

    // The seed must NOT rebaseline over the user's in-progress edits.
    expect(f.applyExternal).not.toHaveBeenCalled();

    dispose();
  });

  it("adopts the detail snapshot exactly once when the buffer is pristine", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    expect(f.dirty).toBe(false);

    const loaded = { id: "n1", title: "Loaded", etag: "e1" };
    h.detailInsts.get("n1")!.nib = loaded;
    h.detailInsts.get("n1")!.fetching = false;
    flushSync();

    expect(f.applyExternal).toHaveBeenCalledTimes(1);
    expect(f.applyExternal).toHaveBeenCalledWith(
      expect.objectContaining({ id: "n1", title: "Loaded", etag: "e1" }),
    );

    // A background re-emit of the same detail must not re-seed the buffer.
    h.detailInsts.get("n1")!.nib = { ...loaded };
    flushSync();
    expect(f.applyExternal).toHaveBeenCalledTimes(1);

    dispose();
  });
});

describe("createActiveView · live bridge", () => {
  it("maps live.deleted -> gone and keeps history blocked while dirty", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    h.editForms.get("n1")!.dirty = true;

    h.liveInsts.get("n1")!.deleted = true;
    flushSync();

    expect(view.state).toEqual({ kind: "gone", nibId: "n1", presentation: "docked" });
    expect(view.blocksHistoryNav).toBe(true);

    dispose();
  });

  it("feeds live.external into a DIRTY edit form via noteExternalChange (persistent warning path)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    // Unsaved edits present -> the incoming change must not clobber the buffer;
    // it surfaces as a persistent warning region instead.
    h.editForms.get("n1")!.dirty = true;

    const remote = snap({ id: "n1", etag: "e9", title: "Theirs" });
    h.liveInsts.get("n1")!.external = remote;
    flushSync();

    expect(h.editForms.get("n1")!.noteExternalChange).toHaveBeenCalledWith(remote);
    expect(h.editForms.get("n1")!.applyExternal).not.toHaveBeenCalled();
    // No auto-apply happened, so the "updated" toast counter must not advance.
    expect(view.externalApplied).toBe(0);

    dispose();
  });

  it("silently rebaselines a CLEAN edit form via applyExternal and advances externalApplied (toast path)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    expect(f.dirty).toBe(false);
    expect(view.externalApplied).toBe(0);

    const remote = snap({ id: "n1", etag: "e9", title: "Theirs" });
    h.liveInsts.get("n1")!.external = remote;
    flushSync();

    // Not dirty -> rebaseline onto the incoming version (no warning region),
    // and advance the counter the view watches to fire the minor toast.
    expect(f.applyExternal).toHaveBeenCalledWith(remote);
    expect(f.noteExternalChange).not.toHaveBeenCalled();
    expect(view.externalApplied).toBe(1);

    dispose();
  });

  it("adopts the pending remote via the CLEAN path when a dirty buffer goes not-dirty again (Discard/undo) — F1", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    // Unsaved edits present when a genuine external change lands -> recorded as a
    // warning (the persistent Load-theirs / Overwrite resolver).
    f.dirty = true;
    const remote = snap({ id: "n1", etag: "e9", title: "Theirs" });
    h.liveInsts.get("n1")!.external = remote;
    flushSync();
    expect(f.noteExternalChange).toHaveBeenCalledWith(remote);
    expect(f.externalChange).toEqual(remote);
    expect(f.applyExternal).not.toHaveBeenCalled();
    expect(view.externalApplied).toBe(0);

    // User hits Discard (or edits back to baseline): the buffer converges to
    // not-dirty while the external change is still pending.
    f.dirty = false;
    flushSync();

    // The presenter adopts the remote through the CLEAN path (applyExternal
    // rebaselines + clears the resolver) and advances the toast counter — so a
    // stale Overwrite over the remote's newer change is structurally impossible.
    expect(f.applyExternal).toHaveBeenCalledWith(remote);
    expect(f.externalChange).toBeNull();
    expect(view.externalApplied).toBe(1);

    dispose();
  });

  it("does not regress the buffer when an older detail seed resolves after the live bridge advanced it — F4", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    expect(f.dirty).toBe(false);

    // A fresh external write lands via the live bridge BEFORE the (slower) detail
    // query resolves — the bridge silently rebaselines the clean buffer onto it.
    const fresh = snap({ id: "n1", etag: "e9", title: "Fresh remote" });
    h.liveInsts.get("n1")!.external = fresh;
    flushSync();
    expect(f.applyExternal).toHaveBeenCalledTimes(1);
    expect(f.applyExternal).toHaveBeenCalledWith(fresh);

    // Now the detail query resolves with an OLDER server-read snapshot. The
    // one-shot seed must NOT re-apply it over the fresher bridge state (that would
    // regress fields + etag backward, surfacing as a spurious 409 on next save).
    h.detailInsts.get("n1")!.nib = { id: "n1", title: "Stale", etag: "e0" };
    h.detailInsts.get("n1")!.fetching = false;
    flushSync();

    expect(f.applyExternal).toHaveBeenCalledTimes(1);
    expect(f.applyExternal).toHaveBeenLastCalledWith(fresh);

    dispose();
  });
});

describe("createActiveView · null-remote conflict fallback", () => {
  it("one-shot-fetches the current snapshot and feeds the resolver when save() returns a null-remote conflict", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    f.dirty = true;

    // The server rejected the save on a stale if-match, but the live
    // subscription is silent — no external event backfilled the remote, so
    // save() reports a conflict with an unknown (null) remote.
    const remote = snap({ id: "n1", etag: "e9", title: "Server copy" });
    h.fetchSnapshot.mockResolvedValueOnce(remote);
    f.save.mockResolvedValueOnce({ kind: "conflict", remote: null });

    const outcome = await view.save();
    flushSync();

    // The outcome is passed through unchanged...
    expect(outcome).toEqual({ kind: "conflict", remote: null });
    // ...and the fallback fetched the current snapshot for the open nib and fed
    // it into the inline resolver directly (not waiting on the dead subscription).
    expect(h.fetchSnapshot).toHaveBeenCalledTimes(1);
    expect(h.fetchSnapshot).toHaveBeenCalledWith("n1");
    // The resolver is fed exactly once (no re-surface): pins the fallback ran once.
    expect(f.noteExternalChange).toHaveBeenCalledTimes(1);
    expect(f.noteExternalChange).toHaveBeenCalledWith(remote);
    expect(f.externalChange).toEqual(remote);
    // A snapshot WAS surfaced, so the toast fallback must stay quiet (no double signal).
    expect(h.notifyError).not.toHaveBeenCalled();

    dispose();
  });

  it("does not fetch or clobber when the live subscription already recorded a change (fresher wins)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    f.dirty = true;

    // The live bridge records a fresh external change first.
    const fromSub = snap({ id: "n1", etag: "e-sub", title: "Sub copy" });
    h.liveInsts.get("n1")!.external = fromSub;
    flushSync();
    expect(f.externalChange).toEqual(fromSub);

    // Now a save resolves as a null-remote conflict; the fallback must NOT run,
    // because the subscription's (authoritative) record is already up.
    const stale = snap({ id: "n1", etag: "e-old", title: "Stale copy" });
    h.fetchSnapshot.mockResolvedValueOnce(stale);
    f.save.mockResolvedValueOnce({ kind: "conflict", remote: null });

    await view.save();
    flushSync();

    expect(h.fetchSnapshot).not.toHaveBeenCalled();
    expect(f.externalChange).toEqual(fromSub); // untouched
    // Only the live bridge's single call stands — the fallback never re-noted,
    // which `not.toHaveBeenCalledWith(stale)` alone wouldn't pin (it was already
    // called once with fromSub).
    expect(f.noteExternalChange).toHaveBeenCalledTimes(1);
    expect(f.noteExternalChange).toHaveBeenCalledWith(fromSub);
    // The subscription's banner is already up — never toast over it.
    expect(h.notifyError).not.toHaveBeenCalled();

    dispose();
  });

  it("does not surface the fetched snapshot if the buffer went clean during the fetch (Discard race)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    f.dirty = true;

    const remote = snap({ id: "n1", etag: "e9", title: "Server copy" });
    // Flip the buffer clean from INSIDE the fetch mock so the pre-fetch guard
    // genuinely passes (dirty at dispatch) and the fetch runs, then the Discard
    // lands WHILE the one-shot query is in flight. This exercises the post-await
    // re-check specifically — flipping dirty in the test body instead would let
    // the pre-fetch guard short-circuit before fetchSnapshot is ever called.
    h.fetchSnapshot.mockImplementationOnce(async () => {
      f.dirty = false; // user hit Discard mid-fetch
      return remote;
    });
    f.save.mockResolvedValueOnce({ kind: "conflict", remote: null });

    await view.save();
    flushSync();

    // The pre-fetch guard passed and the one-shot fetch actually ran...
    expect(h.fetchSnapshot).toHaveBeenCalledWith("n1");
    // ...but the POST-await re-check saw the now-clean buffer and suppressed the
    // surfacing. A not-dirty buffer has no conflict to resolve — never record one.
    expect(f.noteExternalChange).not.toHaveBeenCalled();

    dispose();
  });

  it("routes the buffer to gone when the fallback fetch resolves null (nib gone), keeping the edits", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    const formBefore = view.form;
    f.dirty = true;

    // The save was rejected on a stale if-match, but the current snapshot can't be
    // loaded because the nib was deleted/archived in the race window — fetch
    // resolves null (NOT a throw; a throw is a transport failure, tested below).
    h.fetchSnapshot.mockResolvedValueOnce(null);
    f.save.mockResolvedValueOnce({ kind: "conflict", remote: null });

    await view.save();
    flushSync();

    expect(h.fetchSnapshot).toHaveBeenCalledWith("n1");
    // The fetch is network-authoritative, so it has PROVEN the deletion. It must
    // act on that itself: nothing else is guaranteed to arrive (this fallback runs
    // precisely when the live subscription may be lagging). `gone` renders the
    // deleted notice, so the rejected save is not silent.
    expect(view.state).toEqual({ kind: "gone", nibId: "n1", presentation: "docked" });
    // The dirty buffer survives the transition — same buffer key (`edit:n1`).
    expect(view.form).toBe(formBefore);
    expect(h.editForms.get("n1")!.dirty).toBe(true);
    // No resolver (nothing to reconcile against) and no "please retry" toast —
    // that is wrong advice for a nib that no longer exists.
    expect(f.noteExternalChange).not.toHaveBeenCalled();
    expect(h.notifyError).not.toHaveBeenCalled();

    dispose();
  });

  it("toasts when the fallback fetch rejects (transport error) instead of throwing out of save()", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    f.dirty = true;

    // The one-shot fetch rejects (a genuine load failure — fetchSnapshot throws on
    // transport/GraphQL error). save() must swallow it and give the user feedback
    // rather than surfacing an unhandled rejection. A load FAILURE (unlike a
    // deleted nib) legitimately toasts "please retry".
    h.fetchSnapshot.mockRejectedValueOnce(new Error("network down"));
    f.save.mockResolvedValueOnce({ kind: "conflict", remote: null });

    const outcome = await view.save();
    flushSync();

    expect(outcome).toEqual({ kind: "conflict", remote: null });
    expect(h.fetchSnapshot).toHaveBeenCalledWith("n1");
    expect(f.noteExternalChange).not.toHaveBeenCalled();
    expect(h.notifyError).toHaveBeenCalledTimes(1);
    expect(h.notifyError).toHaveBeenCalledWith(
      "This nib changed on the server and the latest version couldn't be loaded. Please retry.",
    );

    dispose();
  });

  it("a re-entrant save() during the in-flight fallback does not double-fetch, double-surface, or double-toast", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    const f = h.editForms.get("n1")!;
    f.dirty = true;

    // Gate the fallback fetch so a second save() can fire WHILE it is in flight.
    let releaseFetch!: (v: NibSnapshot | null) => void;
    h.fetchSnapshot.mockImplementationOnce(
      () => new Promise<NibSnapshot | null>((resolve) => (releaseFetch = resolve)),
    );
    // Every save resolves as a null-remote conflict (persistent, both calls).
    f.save.mockResolvedValue({ kind: "conflict", remote: null });

    // First save() enters the fallback and parks on the gated fetch. Drain to a
    // macrotask boundary so save()'s continuation reaches (and calls) fetchSnapshot.
    const first = view.save();
    await new Promise((r) => setTimeout(r));
    flushSync();
    expect(h.fetchSnapshot).toHaveBeenCalledTimes(1);
    // While the fallback is in flight the presenter reports savePending so the UI
    // keeps the Save control disabled (prevents the re-click at its source).
    expect(view.savePending).toBe(true);

    // A second save() (impatient re-trigger) must NOT start a second fallback: the
    // in-flight guard skips the fetch/surface/toast even though f.saving is false.
    const second = await view.save();
    flushSync();
    expect(second).toEqual({ kind: "conflict", remote: null });
    expect(h.fetchSnapshot).toHaveBeenCalledTimes(1); // still exactly one fetch

    // Release the original fetch; it surfaces the snapshot exactly once, then clears.
    const remote = snap({ id: "n1", etag: "e9", title: "Server copy" });
    releaseFetch(remote);
    await first;
    flushSync();

    expect(f.noteExternalChange).toHaveBeenCalledTimes(1);
    expect(f.noteExternalChange).toHaveBeenCalledWith(remote);
    expect(h.notifyError).not.toHaveBeenCalled();
    expect(view.savePending).toBe(false);

    dispose();
  });

  it("form A's settling fallback does not clear form B's in-flight marker after a syncTo swap (cross-form regression)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    // Two gated fetches: [0] parks A's fallback, [1] parks B's fallback.
    let releaseA!: (v: NibSnapshot | null) => void;
    let releaseB!: (v: NibSnapshot | null) => void;
    h.fetchSnapshot
      .mockImplementationOnce(() => new Promise<NibSnapshot | null>((r) => (releaseA = r)))
      .mockImplementationOnce(() => new Promise<NibSnapshot | null>((r) => (releaseB = r)));

    // --- Form A ("n1"): start a null-remote conflict fallback, PARK it in flight.
    await view.open("n1");
    flushSync();
    const a = h.editForms.get("n1")!;
    a.dirty = true;
    a.save.mockResolvedValueOnce({ kind: "conflict", remote: null });

    const firstA = view.save();
    await new Promise((r) => setTimeout(r));
    flushSync();
    expect(h.fetchSnapshot).toHaveBeenCalledTimes(1);
    expect(view.savePending).toBe(true); // A guarded

    // --- Swap the active form to B ("n2") via the guard-bypass (Back/Forward),
    //     WHILE A's fallback is still in flight.
    view.syncTo("n2");
    flushSync();
    const b = h.editForms.get("n2")!;
    b.dirty = true;

    // --- Form B hits its OWN null-remote conflict and parks on its own fetch.
    b.save.mockResolvedValueOnce({ kind: "conflict", remote: null });
    const firstB = view.save();
    await new Promise((r) => setTimeout(r));
    flushSync();
    expect(h.fetchSnapshot).toHaveBeenCalledTimes(2);
    expect(view.savePending).toBe(true); // B is now the guarded form

    // --- Release A's (now-orphaned) fetch. A's finally MUST NOT clear B's still-
    //     pending marker — with the unconditional clear it would flip savePending
    //     false and reopen B's re-entrancy window (cross-form regression).
    releaseA(snap({ id: "n1", etag: "e9" }));
    await firstA;
    flushSync();
    expect(view.savePending).toBe(true); // B STILL guarded (false under the bug)
    expect(b.noteExternalChange).not.toHaveBeenCalled(); // B's fetch not yet resolved
    expect(a.noteExternalChange).not.toHaveBeenCalled(); // A is no longer active — surfaced nothing

    // --- Release B's fetch: it surfaces once and clears; savePending falls.
    const remoteB = snap({ id: "n2", etag: "e9b", title: "B server copy" });
    releaseB(remoteB);
    await firstB;
    flushSync();
    expect(b.noteExternalChange).toHaveBeenCalledTimes(1);
    expect(b.noteExternalChange).toHaveBeenCalledWith(remoteB);
    expect(view.savePending).toBe(false);

    dispose();
  });
});

describe("createActiveView · type picker + blocksHistoryNav", () => {
  const anchor = { x: 10, y: 20, width: 14, height: 14 };

  it("startCreateChild with several valid types opens the picker overlay (buffer untouched) and blocks history", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.startCreateChild("p1", "epic", anchor);
    flushSync();

    // The picker overlays — it is NOT a ViewState, so the buffer stays put.
    expect(view.state.kind).toBe("closed");
    expect(view.form).toBeNull();
    expect(view.typePicker).toEqual({
      parentId: "p1",
      parentType: "epic",
      validTypes: ["bug", "feature", "task", "research"],
      anchor,
    });
    expect(view.blocksHistoryNav).toBe(true);

    dispose();
  });

  it("keeps the open detail view visible while the picker is up (issue #4)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    await view.startCreateChild("p1", "epic", anchor);
    flushSync();

    // The n1 detail buffer is still present behind the overlay.
    expect(view.state).toEqual({ kind: "viewing", nibId: "n1", presentation: "docked" });
    expect(view.form?.mode).toBe("edit");
    expect(view.typePicker).not.toBeNull();

    dispose();
  });

  it("startCreateChild on a milestone offers every non-milestone child type", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.startCreateChild("p1", "milestone", anchor);
    flushSync();

    // Backend-mirrored hierarchy: milestone parents epic/bug/feature/task/research.
    expect(view.typePicker?.validTypes).toEqual(["epic", "bug", "feature", "task", "research"]);
    expect(view.state.kind).toBe("closed");
    expect(view.form).toBeNull();

    dispose();
  });

  it("startCreateChild with a leaf parent is a no-op", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.startCreateChild("p1", "task", anchor);
    flushSync();

    expect(view.typePicker).toBeNull();
    expect(view.state.kind).toBe("closed");

    dispose();
  });

  it("chooseType closes the picker and opens a create buffer for the picked type", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.startCreateChild("p1", "epic", anchor);
    flushSync();
    await view.chooseType("feature");
    flushSync();

    expect(view.typePicker).toBeNull();
    expect(view.state).toEqual({
      kind: "creating",
      defaults: { type: "feature", parent: "p1" },
      presentation: "docked",
    });
    expect(view.form?.mode).toBe("create");

    dispose();
  });

  it("choosing a type over a dirty buffer runs the discard guard; refusal keeps the buffer", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    h.editForms.get("n1")!.dirty = true;
    await view.startCreateChild("p1", "epic", anchor);
    flushSync();
    // Opening the picker never prompts (no buffer change yet).
    expect(h.confirm).not.toHaveBeenCalled();

    h.confirm.mockResolvedValueOnce("cancel");
    await view.chooseType("feature");
    flushSync();

    // Refused: the create is abandoned and the dirty viewing buffer survives.
    expect(h.confirm).toHaveBeenCalledTimes(1);
    expect(view.typePicker).toBeNull();
    expect(view.state).toEqual({ kind: "viewing", nibId: "n1", presentation: "docked" });

    dispose();
  });

  it("cancelType closes the picker and leaves the underlying view untouched", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    await view.startCreateChild("p1", "epic", anchor);
    flushSync();
    expect(view.typePicker).not.toBeNull();

    view.cancelType();
    flushSync();
    expect(view.typePicker).toBeNull();
    expect(view.state).toEqual({ kind: "viewing", nibId: "n1", presentation: "docked" });
    expect(view.blocksHistoryNav).toBe(false);

    dispose();
  });

  it("a pristine viewing state does not block history nav", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    expect(view.blocksHistoryNav).toBe(false);
    expect(view.isOpen).toBe(true);

    dispose();
  });
});

describe("createActiveView · dispose", () => {
  it("dispose() tears down the active live subscription (host teardown)", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    expect(h.created).toContain("n1");

    view.dispose();
    flushSync();
    expect(h.disposed).toContain("n1");

    dispose();
  });
});
