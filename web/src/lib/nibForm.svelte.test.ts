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
  opts?: { suppressToast?: boolean };
}

/**
 * Fake MutationStore double. Records every dispatched command (and its execute
 * options) and returns a canned CommandResult. No DOM, no urql — pure boundary
 * testing.
 */
function makeMutations(responder?: (cmd: any) => CommandResult) {
  const calls: RecordedCall[] = [];
  const execute = vi.fn(async (cmd: any, opts?: any): Promise<CommandResult> => {
    calls.push({ kind: cmd.kind, id: cmd.id, input: cmd.input, ifMatch: cmd.ifMatch, opts });
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

  it("setBody() replaces the body, marks dirty, and bumps bodyVersion (editor re-init)", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ body: "- [ ] a" }));
    const v0 = form.bodyVersion;

    form.setBody("- [x] a");
    expect(form.body).toBe("- [x] a");
    expect(form.dirty).toBe(true);
    // Bumping bodyVersion re-keys the CodeMirror editor so an open editor pane
    // stays in sync with an out-of-band body edit (e.g. a task-checkbox flip).
    expect(form.bodyVersion).toBe(v0 + 1);
  });

  it("a body set via setBody() is persisted by save()", async () => {
    const { deps, calls } = makeMutations(updateResponder("etag-2"));
    const form = editNibForm(deps, seed({ body: "- [ ] a" }));

    form.setBody("- [x] a");
    await form.save();

    expect(calls[0].input.body).toBe("- [x] a");
  });

  it("discard() reverts a setBody() change", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ body: "- [ ] a" }));

    form.setBody("- [x] a");
    expect(form.dirty).toBe(true);
    form.discard();
    expect(form.body).toBe("- [ ] a");
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

  it("save() surfaces a server-side etag conflict (stale if-match) as a conflict outcome", async () => {
    // No proactive externalChange was recorded (the subscription raced the save):
    // the write reaches the server, which rejects it on the stale if-match.
    const { deps, calls } = makeMutations(() => ({
      ok: false,
      error: "[GraphQL] etag mismatch: provided etag-1, current is etag-9",
    }));
    const form = editNibForm(deps, seed({ etag: "etag-1" }));
    form.title = "Mine";

    const outcome = await form.save();

    // It reached the server (dispatched) and came back a conflict, not a bare error.
    expect(calls).toHaveLength(1);
    expect(outcome.kind).toBe("conflict");
    if (outcome.kind === "conflict") {
      // No remote snapshot is known from the error alone; the live subscription
      // backfills externalChange shortly after.
      expect(outcome.remote).toBeNull();
    }
  });

  // F3: pin the EXACT Go ETagMismatchError.Error() format string (with and
  // without urql's "[GraphQL] " prefix) through isEtagConflict. The Go side is
  // pinned by TestETagMismatchErrorFormat (internal/nibcore); if a Go maintainer
  // rewords that message, this expectation and the Go test must move together.
  // This is the SUBSTRING FALLBACK safety net — it must stay green even after
  // the structured extensions.code path lands (no errorCode is supplied here).
  it.each([
    "etag mismatch: provided etag-1, current is etag-9",
    "[GraphQL] etag mismatch: provided etag-1, current is etag-9",
  ])("classifies the exact Go etag-mismatch string %j as a conflict (substring fallback)", async (message) => {
    const { deps, calls } = makeMutations(() => ({ ok: false, error: message }));
    const form = editNibForm(deps, seed({ etag: "etag-1" }));
    form.title = "Mine";

    const outcome = await form.save();

    expect(calls).toHaveLength(1);
    expect(outcome.kind).toBe("conflict");
  });

  it("classifies a structured errorCode=ETAG_MISMATCH conflict even when the message text does NOT match", async () => {
    // The server tagged the typed error with extensions.code = "ETAG_MISMATCH",
    // but the human-readable message is (hypothetically) reworded so the
    // substring fallback would MISS it. The structured code must win.
    const { deps, calls } = makeMutations(() => ({
      ok: false,
      error: "[GraphQL] the record was modified by someone else",
      errorCode: "ETAG_MISMATCH",
    }));
    const form = editNibForm(deps, seed({ etag: "etag-1" }));
    form.title = "Mine";

    const outcome = await form.save();

    expect(calls).toHaveLength(1);
    expect(outcome.kind).toBe("conflict");
  });

  it("save()'s update dispatch opts into suppressToast so save() owns the messaging (no racing raw toast)", async () => {
    const { deps, calls } = makeMutations(updateResponder("etag-2"));
    const form = editNibForm(deps, seed({ etag: "etag-1" }));
    form.title = "Mine";

    await form.save();

    expect(calls).toHaveLength(1);
    expect(calls[0].opts?.suppressToast).toBe(true);
  });

  it("save() still returns a plain error for non-conflict failures", async () => {
    const { deps } = makeMutations(() => ({ ok: false, error: "boom: disk full" }));
    const form = editNibForm(deps, seed({ etag: "etag-1" }));
    form.title = "Mine";

    const outcome = await form.save();
    expect(outcome.kind).toBe("error");
    if (outcome.kind === "error") expect(outcome.message).toBe("boom: disk full");
  });

  it("save({overwrite:true}) with no known remote bypasses if-match (last-write-wins)", async () => {
    const { deps, calls } = makeMutations(updateResponder("etag-after"));
    const form = editNibForm(deps, seed({ etag: "etag-1" }));
    form.title = "Mine";

    const outcome = await form.save({ overwrite: true });

    expect(calls).toHaveLength(1);
    // No known remote etag -> omit if-match entirely so the write always lands.
    expect(calls[0].ifMatch).toBeUndefined();
    expect(outcome.kind).toBe("saved");
    expect(form.etag).toBe("etag-after");
  });

  it("clears the external-change warning once the buffer converges to the remote's field values (F7/AC3), and every field participates", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ etag: "etag-1" }));

    // Start editing (dirty), then a genuine external change is recorded. The
    // remote differs from the baseline in ALL SEVEN fields (type included) so
    // each one's participation in #matchesFields can be proven below.
    form.title = "Mine edit";
    const remote = seed({
      etag: "etag-9",
      title: "Theirs",
      status: "in-progress",
      type: "bug",
      body: "Remote body",
      priority: "low",
      estimate: "L",
      tags: ["x", "y"],
    });
    form.noteExternalChange(remote);
    expect(form.externalChange).toEqual(remote);

    // The user manually edits the buffer to exactly the remote's field values
    // (etags still differ). Tags are added in the REVERSE order of the remote's
    // to exercise sameTags order-insensitivity (a position-wise compare would
    // wrongly keep the warning up). There is nothing left to resolve → it clears.
    form.title = "Theirs";
    form.status = "in-progress";
    form.type = "bug";
    form.body = "Remote body";
    form.priority = "low";
    form.estimate = "L";
    form.removeTag("alpha");
    form.removeTag("beta");
    form.addTag("y");
    form.addTag("x"); // buffer tags ["y","x"] vs remote ["x","y"] — reordered but equal
    expect(form.externalChange).toBeNull();

    // Prove each of the 7 fields participates: diverge it in isolation from the
    // fully-converged state and the warning must re-surface; restoring clears it.
    // A #matchesFields that silently dropped any field comparison would stay null.
    const check = (diverge: () => void, restore: () => void) => {
      diverge();
      expect(form.externalChange).toEqual(remote);
      restore();
      expect(form.externalChange).toBeNull();
    };
    check(() => (form.title = "x"), () => (form.title = "Theirs"));
    check(() => (form.status = "todo"), () => (form.status = "in-progress"));
    check(() => (form.type = "task"), () => (form.type = "bug"));
    check(() => (form.priority = "high"), () => (form.priority = "low"));
    check(() => (form.estimate = "M"), () => (form.estimate = "L"));
    check(() => (form.body = "Other body"), () => (form.body = "Remote body"));
    check(() => form.addTag("z"), () => form.removeTag("z"));

    // Diverging again re-surfaces the warning (it is derived, not one-shot).
    form.title = "Diverged";
    expect(form.externalChange).toEqual(remote);
  });

  it("a converged dirty buffer SAVES with the remote etag rather than a silent no-op (HIGH #1)", async () => {
    // AC3 exposed a dead Save: after a dirty buffer's edits converge to the
    // remote's field values the getter reads null (banner hides) but the buffer
    // stays dirty-vs-baseline, so Save stays enabled. The old save() read the raw
    // #externalChange and short-circuited to {conflict} with no dispatch and no
    // feedback. The content already equals the server's current revision, so the
    // fix performs a REAL write using the remote's etag as if-match.
    const { deps, calls } = makeMutations(updateResponder("etag-converged"));
    const form = editNibForm(deps, seed({ etag: "etag-1" }));

    // Dirty edit, then a genuine external change is recorded.
    form.title = "Mine edit";
    const remote = seed({ etag: "etag-9", title: "Theirs", body: "Remote body" });
    form.noteExternalChange(remote);
    expect(form.externalChange).toEqual(remote);

    // Converge the buffer to the remote's field values (etags still differ).
    form.title = "Theirs";
    form.body = "Remote body";
    expect(form.externalChange).toBeNull(); // getter converged → banner hidden
    expect(form.dirty).toBe(true); // but still dirty vs the ORIGINAL baseline

    const outcome = await form.save();

    // A real write happened (not a silent conflict no-op), using the remote etag.
    expect(calls).toHaveLength(1);
    expect(calls[0].ifMatch).toBe("etag-9");
    expect(outcome.kind).toBe("saved");
    // The buffer rebaselines → clean (Save legitimately disables), external cleared.
    expect(form.dirty).toBe(false);
    expect(form.externalChange).toBeNull();
    expect(form.etag).toBe("etag-converged");
  });

  it("sameTags is duplicate-sensitive: a duplicate-tag remote does not falsely converge (#matchesFields)", () => {
    // sameTags feeds #matchesFields (the AC3 convergence decision, which after
    // HIGH #1 gates a write path). A duplicate-INSENSITIVE compare would treat
    // buffer ["x","x"] as equal to remote ["x","y"] (same length, distinct set
    // ⊆), silently hiding a real difference. Every non-tag field is equal here,
    // so the tag comparison alone decides.
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ etag: "etag-1", tags: ["x", "x"] }));

    const remote = seed({ etag: "etag-9", tags: ["x", "y"] });
    form.noteExternalChange(remote);

    // Buffer tags ["x","x"] are NOT equal to remote tags ["x","y"] as multisets.
    expect(form.externalChange).toEqual(remote);
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

  it("save()'s create dispatch opts into suppressToast so the single owner (Save button / dirty-nav guard) owns the messaging (no racing raw toast)", async () => {
    const { deps, calls } = makeMutations(createResponder());
    const form = createNibForm(deps, { type: "feature" });
    form.title = "My task";

    await form.save();

    expect(calls).toHaveLength(1);
    expect(calls[0].opts?.suppressToast).toBe(true);
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
