import { describe, it, expect, vi } from "vitest";
import { flushSync } from "svelte";
import { createActiveView, type ActiveViewDeps, type ActiveView } from "./useActiveView.svelte";
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
  const confirm = vi.fn<() => Promise<boolean>>(async () => true);

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

  const deps: ActiveViewDeps = { nav, editForm, createForm, liveNib, detail, confirm };
  return { deps, nav, confirm, editForms, createForms, liveInsts, detailInsts, created, disposed };
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
  it("refuses close when the form is dirty and confirm resolves false", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    h.editForms.get("n1")!.dirty = true;
    h.confirm.mockResolvedValueOnce(false);

    await view.requestClose();
    expect(h.confirm).toHaveBeenCalledTimes(1);
    expect(view.state.kind).toBe("viewing");
    expect(h.nav.closePanel).not.toHaveBeenCalled();

    dispose();
  });

  it("refuses opening another nib when dirty and confirm resolves false", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    h.editForms.get("n1")!.dirty = true;
    h.confirm.mockResolvedValueOnce(false);

    await view.open("n2");
    expect(h.confirm).toHaveBeenCalledTimes(1);
    expect(view.state).toEqual({ kind: "viewing", nibId: "n1", presentation: "docked" });
    expect(h.nav.navigateToNib).not.toHaveBeenCalledWith("n2");

    dispose();
  });

  it("refuses startCreate when dirty and confirm resolves false", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    h.editForms.get("n1")!.dirty = true;
    h.confirm.mockResolvedValueOnce(false);

    await view.startCreate({ type: "bug" });
    expect(h.confirm).toHaveBeenCalledTimes(1);
    expect(view.state.kind).toBe("viewing");

    dispose();
  });

  it("proceeds through the guard when confirm resolves true", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    h.editForms.get("n1")!.dirty = true;
    h.confirm.mockResolvedValueOnce(true);

    await view.requestClose();
    flushSync();
    expect(view.state.kind).toBe("closed");
    expect(h.nav.closePanel).toHaveBeenCalledTimes(1);

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

    h.confirm.mockResolvedValueOnce(false);
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
