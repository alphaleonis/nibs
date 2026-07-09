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
  const editForm = (nibId: string, seed?: NibSnapshot): EditForm => {
    const f: FakeEdit = {
      mode: "edit",
      id: nibId,
      dirty: false,
      etag: seed?.etag ?? "e0",
      title: seed?.title ?? "",
      body: seed?.body ?? "",
      noteExternalChange: vi.fn(),
      applyExternal: vi.fn(),
      save: vi.fn(async () => ({ kind: "saved", snapshot: snap({ id: nibId }) })),
    };
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

  it("feeds live.external into the edit form via noteExternalChange", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();

    const remote = snap({ id: "n1", etag: "e9", title: "Theirs" });
    h.liveInsts.get("n1")!.external = remote;
    flushSync();

    expect(h.editForms.get("n1")!.noteExternalChange).toHaveBeenCalledWith(remote);

    dispose();
  });
});

describe("createActiveView · type picker + blocksHistoryNav", () => {
  it("startCreateChild with several valid types opens the picker and blocks history", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.startCreateChild("p1", "epic");
    flushSync();

    expect(view.state.kind).toBe("pickingType");
    expect(view.blocksHistoryNav).toBe(true);
    expect(view.form).toBeNull();

    dispose();
  });

  it("startCreateChild with a single valid type goes straight to creating", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.startCreateChild("p1", "milestone");
    flushSync();

    expect(view.state).toEqual({
      kind: "creating",
      defaults: { type: "epic", parent: "p1" },
      presentation: "docked",
    });
    expect(view.form?.mode).toBe("create");

    dispose();
  });

  it("chooseType resolves the picker into a create buffer", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.startCreateChild("p1", "epic");
    flushSync();
    view.chooseType("feature");
    flushSync();

    expect(view.state).toEqual({
      kind: "creating",
      defaults: { type: "feature", parent: "p1" },
      presentation: "docked",
    });
    expect(view.form?.mode).toBe("create");

    dispose();
  });

  it("cancelType returns to the resume target when picking from a viewing nib", async () => {
    const h = makeDeps();
    const { view, dispose } = mount(h.deps);

    await view.open("n1");
    flushSync();
    await view.startCreateChild("p1", "epic");
    flushSync();
    expect(view.state.kind).toBe("pickingType");

    view.cancelType();
    flushSync();
    expect(view.state).toEqual({ kind: "viewing", nibId: "n1", presentation: "docked" });

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
