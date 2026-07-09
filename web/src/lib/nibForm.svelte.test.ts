import { describe, it, expect, vi } from "vitest";
import { createNibForm, editNibForm } from "./nibForm.svelte";
import type { FormDeps, NibSnapshot } from "./nibForm.svelte";
import { getBodyTemplate } from "./bodyTemplates";
import type { CommandResult } from "./mutations/types";

// --- Test helpers ---------------------------------------------------------

interface RecordedCall {
  kind: string;
  id?: string;
  input?: any;
  ifMatch?: string;
}

/**
 * Fake MutationStore double. Records every dispatched command and returns a
 * canned CommandResult. No DOM, no urql — pure boundary testing.
 */
function makeMutations(responder?: (cmd: any) => CommandResult) {
  const calls: RecordedCall[] = [];
  const execute = vi.fn(async (cmd: any): Promise<CommandResult> => {
    calls.push({ kind: cmd.kind, id: cmd.id, input: cmd.input, ifMatch: cmd.ifMatch });
    return responder ? responder(cmd) : { ok: true, data: {} };
  });
  const deps: FormDeps = { mutations: { execute: execute as any } };
  return { deps, calls, execute };
}

function updateResponder(etag = "etag-new"): (cmd: any) => CommandResult {
  return () => ({ ok: true, data: { updateNib: { etag } } });
}

function createResponder(
  overrides: Record<string, unknown> = {},
): (cmd: any) => CommandResult {
  return (cmd) => ({
    ok: true,
    data: {
      createNib: {
        id: "nibs-new1",
        title: cmd.input.title,
        status: cmd.input.status,
        type: cmd.input.type,
        priority: cmd.input.priority ?? null,
        estimate: cmd.input.estimate ?? null,
        tags: cmd.input.tags ?? [],
        body: cmd.input.body ?? "",
        etag: "etag-created",
        ...overrides,
      },
    },
  });
}

function seed(overrides: Partial<NibSnapshot> = {}): NibSnapshot {
  return {
    id: "nibs-abc1",
    title: "Original title",
    status: "todo",
    type: "task",
    priority: "high",
    estimate: "M",
    tags: ["alpha", "beta"],
    body: "Original body",
    etag: "etag-1",
    ...overrides,
  };
}

// --- Edit form ------------------------------------------------------------

describe("editNibForm — buffering & dirty", () => {
  it("seeds fields from the snapshot and starts non-dirty", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed());

    expect(form.mode).toBe("edit");
    expect(form.id).toBe("nibs-abc1");
    expect(form.etag).toBe("etag-1");
    expect(form.title).toBe("Original title");
    expect(form.status).toBe("todo");
    expect(form.type).toBe("task");
    expect(form.priority).toBe("high");
    expect(form.estimate).toBe("M");
    expect([...form.tags]).toEqual(["alpha", "beta"]);
    expect(form.body).toBe("Original body");
    expect(form.dirty).toBe(false);
  });

  it("mutating any field flips dirty", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed());

    form.title = "Changed";
    expect(form.dirty).toBe(true);
  });

  it("mutating status/priority/body/tags each flips dirty", () => {
    const { deps } = makeMutations();

    const f1 = editNibForm(deps, seed());
    f1.status = "in-progress";
    expect(f1.dirty).toBe(true);

    const f2 = editNibForm(deps, seed());
    f2.priority = "low";
    expect(f2.dirty).toBe(true);

    const f3 = editNibForm(deps, seed());
    f3.body = "new body";
    expect(f3.dirty).toBe(true);

    const f4 = editNibForm(deps, seed());
    f4.addTag("gamma");
    expect(f4.dirty).toBe(true);
  });

  it("discard() reverts fields to baseline and clears dirty", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed());

    form.title = "Changed";
    form.body = "Changed body";
    form.addTag("gamma");
    form.removeTag("alpha");
    expect(form.dirty).toBe(true);

    form.discard();
    expect(form.title).toBe("Original title");
    expect(form.body).toBe("Original body");
    expect([...form.tags]).toEqual(["alpha", "beta"]);
    expect(form.dirty).toBe(false);
  });

  it("adding then removing a tag returns to non-dirty", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed());

    form.addTag("gamma");
    expect(form.dirty).toBe(true);
    form.removeTag("gamma");
    expect(form.dirty).toBe(false);
  });

  it("the edit type setter never swaps the body", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ type: "task", body: "Original body" }));

    form.type = "bug";
    expect(form.body).toBe("Original body");
    expect(form.type).toBe("bug");
  });
});

describe("editNibForm — save", () => {
  it("builds UpdateNibInput with priority||null, estimate||null and tag diff, using baseline etag", async () => {
    const { deps, calls } = makeMutations(updateResponder("etag-2"));
    const form = editNibForm(deps, seed());

    form.priority = ""; // cleared -> null
    form.estimate = ""; // cleared -> null
    form.addTag("gamma");
    form.removeTag("alpha");
    form.title = "New title";

    const outcome = await form.save();

    expect(calls).toHaveLength(1);
    expect(calls[0].kind).toBe("update-nib");
    expect(calls[0].id).toBe("nibs-abc1");
    expect(calls[0].ifMatch).toBe("etag-1");
    expect(calls[0].input.title).toBe("New title");
    expect(calls[0].input.status).toBe("todo");
    expect(calls[0].input.type).toBe("task");
    expect(calls[0].input.priority).toBeNull();
    expect(calls[0].input.estimate).toBeNull();
    expect(calls[0].input.body).toBe("Original body");
    expect(calls[0].input.addTags).toEqual(["gamma"]);
    expect(calls[0].input.removeTags).toEqual(["alpha"]);

    expect(outcome.kind).toBe("saved");
  });

  it("omits addTags/removeTags when tags are unchanged", async () => {
    const { deps, calls } = makeMutations(updateResponder());
    const form = editNibForm(deps, seed());

    form.title = "New title";
    await form.save();

    expect(calls[0].input).not.toHaveProperty("addTags");
    expect(calls[0].input).not.toHaveProperty("removeTags");
  });

  it("re-baselines and adopts the returned etag on success", async () => {
    const { deps } = makeMutations(updateResponder("etag-2"));
    const form = editNibForm(deps, seed());

    form.title = "New title";
    const outcome = await form.save();

    expect(form.etag).toBe("etag-2");
    expect(form.dirty).toBe(false);
    // Re-baselined: mutating back to the just-saved value is not dirty; a fresh
    // change flips dirty again against the NEW baseline.
    expect(outcome.kind).toBe("saved");
    if (outcome.kind === "saved") {
      expect(outcome.snapshot.etag).toBe("etag-2");
      expect(outcome.snapshot.title).toBe("New title");
    }
    form.title = "New title";
    expect(form.dirty).toBe(false);
  });

  it("returns error and does not dispatch when the title is empty", async () => {
    const { deps, calls } = makeMutations(updateResponder());
    const form = editNibForm(deps, seed());

    form.title = "   ";
    const outcome = await form.save();

    expect(outcome.kind).toBe("error");
    expect(calls).toHaveLength(0);
  });

  it("returns error (no re-baseline) when the mutation fails", async () => {
    const { deps } = makeMutations(() => ({ ok: false, error: "boom" }));
    const form = editNibForm(deps, seed());

    form.title = "New title";
    const outcome = await form.save();

    expect(outcome.kind).toBe("error");
    expect(form.dirty).toBe(true);
    expect(form.etag).toBe("etag-1");
  });
});

describe("editNibForm — conflict & overwrite", () => {
  it("noteExternalChange skips self-echo (remote etag equals baseline etag)", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ etag: "etag-1" }));

    form.noteExternalChange(seed({ etag: "etag-1", title: "Echo" }));
    expect(form.externalChange).toBeNull();
  });

  it("noteExternalChange records a genuine external change", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ etag: "etag-1" }));

    const remote = seed({ etag: "etag-remote", title: "Theirs" });
    form.noteExternalChange(remote);
    expect(form.externalChange).toEqual(remote);
  });

  it("save() returns conflict WITHOUT dispatching when an external change is unresolved", async () => {
    const { deps, calls } = makeMutations(updateResponder());
    const form = editNibForm(deps, seed({ etag: "etag-1" }));

    const remote = seed({ etag: "etag-remote", title: "Theirs" });
    form.noteExternalChange(remote);
    form.title = "Mine";

    const outcome = await form.save();

    expect(outcome.kind).toBe("conflict");
    if (outcome.kind === "conflict") {
      expect(outcome.remote).toEqual(remote);
    }
    expect(calls).toHaveLength(0);
  });

  it("save({overwrite:true}) dispatches using the REMOTE etag, not the stale baseline", async () => {
    const { deps, calls } = makeMutations(updateResponder("etag-after"));
    const form = editNibForm(deps, seed({ etag: "etag-1" }));

    form.noteExternalChange(seed({ etag: "etag-remote", title: "Theirs" }));
    form.title = "Mine";

    const outcome = await form.save({ overwrite: true });

    expect(calls).toHaveLength(1);
    expect(calls[0].ifMatch).toBe("etag-remote");
    expect(outcome.kind).toBe("saved");
    expect(form.externalChange).toBeNull();
    expect(form.etag).toBe("etag-after");
  });

  it("applyExternal rebases baseline and fields to the remote and clears the external change", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ etag: "etag-1" }));

    form.title = "Local edit";
    const remote = seed({
      etag: "etag-remote",
      title: "Remote title",
      body: "Remote body",
      tags: ["x"],
    });
    form.noteExternalChange(remote);

    form.applyExternal(remote);

    expect(form.title).toBe("Remote title");
    expect(form.body).toBe("Remote body");
    expect([...form.tags]).toEqual(["x"]);
    expect(form.etag).toBe("etag-remote");
    expect(form.externalChange).toBeNull();
    expect(form.dirty).toBe(false);
  });
});

// --- Create form ----------------------------------------------------------

describe("createNibForm — seeding & template", () => {
  it("seeds the per-type body template and default status/type", () => {
    const { deps } = makeMutations();
    const form = createNibForm(deps, { type: "bug" });

    expect(form.mode).toBe("create");
    expect(form.type).toBe("bug");
    expect(form.status).toBe("draft");
    expect(form.body).toBe(getBodyTemplate("bug"));
    expect(form.dirty).toBe(false);
  });

  it("defaults to type task and status draft when no defaults given", () => {
    const { deps } = makeMutations();
    const form = createNibForm(deps);

    expect(form.type).toBe("task");
    expect(form.status).toBe("draft");
    expect(form.body).toBe(getBodyTemplate("task"));
  });

  it("honours an explicit status default", () => {
    const { deps } = makeMutations();
    const form = createNibForm(deps, { type: "task", status: "todo" });
    expect(form.status).toBe("todo");
  });

  it("does not expose edit-only members", () => {
    const { deps } = makeMutations();
    const form = createNibForm(deps, { type: "task" });
    expect("applyExternal" in form).toBe(false);
    expect("etag" in form).toBe(false);
    expect("id" in form).toBe(false);
  });

  it("typing a title flips dirty", () => {
    const { deps } = makeMutations();
    const form = createNibForm(deps, { type: "task" });

    form.title = "My new nib";
    expect(form.dirty).toBe(true);
  });
});

describe("createNibForm — template swap by equality", () => {
  it("swaps body to the new template while body is untouched, bumps bodyVersion, stays non-dirty", () => {
    const { deps } = makeMutations();
    const form = createNibForm(deps, { type: "task" });

    expect(form.body).toBe(getBodyTemplate("task"));
    const v0 = form.bodyVersion;

    form.type = "bug";

    expect(form.body).toBe(getBodyTemplate("bug"));
    expect(form.bodyVersion).toBe(v0 + 1);
    expect(form.dirty).toBe(false);
  });

  it("leaves an edited body alone when the type changes", () => {
    const { deps } = makeMutations();
    const form = createNibForm(deps, { type: "task" });

    form.body = "Hand-written body";
    expect(form.dirty).toBe(true);

    form.type = "bug";
    expect(form.body).toBe("Hand-written body");
    expect(form.type).toBe("bug");
  });

  it("re-enables the swap after the body is deleted back to the template", () => {
    const { deps } = makeMutations();
    const form = createNibForm(deps, { type: "task" });

    // Edit away, then restore to the current template.
    form.body = "scratch";
    form.body = getBodyTemplate("task");

    form.type = "bug";
    expect(form.body).toBe(getBodyTemplate("bug"));
  });
});

describe("createNibForm — save", () => {
  it("builds an omit-empty CreateNibInput and returns created", async () => {
    const { deps, calls } = makeMutations(createResponder());
    // "feature" has no template -> empty body, so body is omitted too.
    const form = createNibForm(deps, { type: "feature" });
    form.title = "My task";

    const outcome = await form.save();

    expect(calls).toHaveLength(1);
    expect(calls[0].kind).toBe("create-nib");
    expect(calls[0].input).toEqual({
      title: "My task",
      type: "feature",
      status: "draft",
    });

    expect(outcome.kind).toBe("created");
    if (outcome.kind === "created") {
      expect(outcome.id).toBe("nibs-new1");
      expect(outcome.snapshot.id).toBe("nibs-new1");
    }
  });

  it("includes priority, estimate, tags, body and parent when present", async () => {
    const { deps, calls } = makeMutations(createResponder());
    const form = createNibForm(deps, { type: "task", parent: "nibs-parent" });
    form.title = "Full nib";
    form.priority = "high";
    form.estimate = "L";
    form.addTag("x");
    form.body = "Custom body";

    await form.save();

    expect(calls[0].input).toEqual({
      title: "Full nib",
      type: "task",
      status: "draft",
      priority: "high",
      estimate: "L",
      tags: ["x"],
      body: "Custom body",
      parent: "nibs-parent",
    });
  });

  it("returns error and does not dispatch when the title is empty", async () => {
    const { deps, calls } = makeMutations(createResponder());
    const form = createNibForm(deps, { type: "task" });

    const outcome = await form.save();

    expect(outcome.kind).toBe("error");
    expect(calls).toHaveLength(0);
  });

  it("returns error when the create mutation fails", async () => {
    const { deps } = makeMutations(() => ({ ok: false, error: "nope" }));
    const form = createNibForm(deps, { type: "task" });
    form.title = "X";

    const outcome = await form.save();
    expect(outcome.kind).toBe("error");
  });
});
