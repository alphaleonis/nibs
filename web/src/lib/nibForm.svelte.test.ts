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
    milestone: "",
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

  it("setBody() replaces the body, marks dirty, and does NOT bump bodyVersion by default (in-place sync)", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ body: "- [ ] a" }));
    const v0 = form.bodyVersion;

    form.setBody("- [x] a");
    expect(form.body).toBe("- [x] a");
    expect(form.dirty).toBe(true);
    // Default is in-place: no remount. MarkdownEditor syncs the out-of-band change
    // via a minimal-diff doc transaction, preserving undo/cursor/scroll.
    // The checkbox-flip path uses this default.
    expect(form.bodyVersion).toBe(v0);
  });

  it("setBody(value, { reinitEditor: true }) bumps bodyVersion (forces a full editor re-init)", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ body: "- [ ] a" }));
    const v0 = form.bodyVersion;

    form.setBody("- [x] a", { reinitEditor: true });
    expect(form.body).toBe("- [x] a");
    expect(form.dirty).toBe(true);
    // Explicit opt-in: bump bodyVersion so the `{#key}` fully re-keys the editor.
    expect(form.bodyVersion).toBe(v0 + 1);
  });

  it("discard() bumps bodyVersion so a reset fully re-inits the editor (no stale doc)", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ body: "- [ ] a" }));
    // A non-remounting edit first, so the only bump under test is discard's.
    form.setBody("- [x] a", { reinitEditor: false });
    const v0 = form.bodyVersion;

    form.discard();
    expect(form.body).toBe("- [ ] a");
    expect(form.bodyVersion).toBe(v0 + 1);
  });

  it("applyExternal() bumps bodyVersion so load-theirs fully re-inits the editor", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ etag: "etag-1", body: "mine" }));
    const v0 = form.bodyVersion;

    form.applyExternal(seed({ etag: "etag-2", body: "theirs" }));
    expect(form.body).toBe("theirs");
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

// --- Line-ending-insensitive body comparison ------------------------------

/**
 * The buffer and its baseline can legitimately hold the SAME content in
 * different line encodings: a nib read from disk keeps whatever endings it has
 * (the backend does not normalize), while `onchange` emits CodeMirror's
 * always-LF doc the moment the user types. Both `dirty` and `#matchesFields`
 * must therefore judge bodies line-ending-insensitively, or a CRLF nib is
 * permanently dirty / never converges after a single keystroke.
 *
 * These cases pin the predicate on BOTH sides — semantically-equal bodies must
 * compare equal, and genuinely different content must still compare different,
 * including differences a too-eager normalization would erase (a `\r\n` that
 * collapses to two newlines instead of one, a trailing terminator).
 */
describe("editNibForm — body comparison ignores line endings", () => {
  it("a CRLF baseline vs the editor's LF doc is NOT dirty (type a char, delete it)", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ body: "line one\r\nline two" }));
    expect(form.dirty).toBe(false);

    // A genuine keystroke: MarkdownEditor's onchange writes its LF doc straight
    // into `body`. Typing then deleting leaves semantically identical content.
    form.body = "line one\nline twoX";
    expect(form.dirty).toBe(true);
    form.body = "line one\nline two";
    expect(form.dirty).toBe(false);
  });

  it("a CRLF baseline vs genuinely different content IS dirty (the guard is not over-broad)", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ body: "line one\r\nline two" }));

    form.body = "line one\nline three";
    expect(form.dirty).toBe(true);
  });

  it("a lone CR baseline compares equal to the LF doc, and mixed endings normalize on both sides", () => {
    const { deps } = makeMutations();

    const f1 = editNibForm(deps, seed({ body: "a\rb" }));
    f1.body = "a\nb";
    expect(f1.dirty).toBe(false);

    // Mixed CRLF / lone CR / LF in one body, all collapsing to the LF doc.
    const f2 = editNibForm(deps, seed({ body: "a\r\nb\rc\nd" }));
    f2.body = "a\nb\nc\nd";
    expect(f2.dirty).toBe(false);
  });

  it("a CRLF pair collapses to ONE newline, not two (a blank line is a real difference)", () => {
    const { deps } = makeMutations();

    // The boundary a naive /\r/g -> "\n" would get wrong: it would turn the
    // baseline into "a\n\nb" and falsely call this buffer clean.
    const f1 = editNibForm(deps, seed({ body: "a\r\nb" }));
    f1.body = "a\n\nb";
    expect(f1.dirty).toBe(true);

    // Conversely a genuine CRLF blank line DOES equal two LF newlines.
    const f2 = editNibForm(deps, seed({ body: "a\r\n\r\nb" }));
    f2.body = "a\n\nb";
    expect(f2.dirty).toBe(false);
  });

  it("a trailing CRLF terminator equals a lone LF terminator", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ body: "a\r\n" }));

    form.body = "a\n";
    expect(form.dirty).toBe(false);
  });

  it("deleting the trailing terminator entirely IS dirty (not masked by normalization)", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ body: "a\r\n" }));

    form.body = "a";
    expect(form.dirty).toBe(true);
  });

  it("an empty body compares clean", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ body: "" }));

    form.body = "";
    expect(form.dirty).toBe(false);
  });

  it("a terminator-only CRLF body equals a lone LF", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ body: "\r\n" }));

    form.body = "\n";
    expect(form.dirty).toBe(false);
  });

  it("an empty body gaining a newline IS dirty", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ body: "" }));

    form.body = "\n";
    expect(form.dirty).toBe(true);
  });

  it("reversed and doubled CR sequences each count as their own break", () => {
    const { deps } = makeMutations();

    // `/\r\n?/g` binds `?` to the TRAILING \n only, so a \r not followed by \n is
    // its own break: "\r\r" is two breaks and "\n\r" is a \n plus a lone \r.
    const f1 = editNibForm(deps, seed({ body: "a\r\rb" }));
    f1.body = "a\n\nb";
    expect(f1.dirty).toBe(false);

    const f2 = editNibForm(deps, seed({ body: "a\n\rb" }));
    f2.body = "a\n\nb";
    expect(f2.dirty).toBe(false);
  });

  it("a doubled CR is two breaks, not one (the break count stays distinguished)", () => {
    const { deps } = makeMutations();

    const f1 = editNibForm(deps, seed({ body: "a\r\rb" }));
    f1.body = "a\nb";
    expect(f1.dirty).toBe(true);

    const f2 = editNibForm(deps, seed({ body: "a\n\rb" }));
    f2.body = "a\nb";
    expect(f2.dirty).toBe(true);
  });

  it("a body with no line terminator at all is unaffected", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ body: "single line" }));

    expect(form.dirty).toBe(false);
    form.body = "single line!";
    expect(form.dirty).toBe(true);
  });

  it("#matchesFields converges for a CRLF-origin body, so the conflict banner self-clears", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ etag: "etag-1", body: "a\r\nb" }));

    // Dirty edit, then a genuine external change whose body is CRLF (the remote
    // is read from disk, so it carries the file's own endings).
    form.title = "Mine edit";
    const remote = seed({ etag: "etag-9", title: "Theirs", body: "a\r\nb\r\nc" });
    form.noteExternalChange(remote);
    expect(form.externalChange).toEqual(remote);

    // The user converges the buffer by hand. The editor emits LF, so the buffer's
    // body differs from the remote's ONLY in line encoding — there is nothing left
    // to resolve and the banner must clear.
    form.title = "Theirs";
    form.body = "a\nb\nc";
    expect(form.externalChange).toBeNull();

    // Derived, not one-shot: a real body difference re-surfaces it.
    form.body = "a\nb\nd";
    expect(form.externalChange).toEqual(remote);
    form.body = "a\nb\nc";
    expect(form.externalChange).toBeNull();
  });

  it("a body differing from the remote in CONTENT keeps the conflict banner up", () => {
    const { deps } = makeMutations();
    const form = editNibForm(deps, seed({ etag: "etag-1", body: "a\r\nb" }));

    form.title = "Mine edit";
    const remote = seed({ etag: "etag-9", title: "Theirs", body: "a\r\nb\r\nc" });
    form.noteExternalChange(remote);

    // Every other field now matches the remote, so the body alone holds the banner
    // up: a real content difference must keep it there rather than being absorbed
    // as a mere encoding difference.
    form.title = "Theirs";
    form.body = "a\nb\nd";
    expect(form.externalChange).toEqual(remote);
  });

  it("a CRLF-origin converged buffer SAVES with the remote etag", async () => {
    const { deps, calls } = makeMutations(updateResponder("etag-converged"));
    const form = editNibForm(deps, seed({ etag: "etag-1", body: "a\r\nb" }));

    form.body = "a\nb\nc";
    const remote = seed({ etag: "etag-9", body: "a\r\nb\r\nc" });
    form.noteExternalChange(remote);

    // Converged on content, so save() takes the real-write path against the
    // remote etag rather than short-circuiting to {conflict}. The LF buffer is
    // what gets written — the persisted body's endings change from CRLF to LF.
    expect(form.externalChange).toBeNull();
    const outcome = await form.save();

    expect(calls).toHaveLength(1);
    expect(calls[0].ifMatch).toBe("etag-9");
    expect(calls[0].input.body).toBe("a\nb\nc");
    expect(outcome.kind).toBe("saved");
    expect(form.dirty).toBe(false);
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

  it("classifies a structured errorCode=NOT_FOUND as a `missing` outcome (deleted nib), NOT a conflict or plain error", async () => {
    // The target nib was DELETED server-side: GetForUpdate returns nib.ErrNotFound
    // BEFORE any if-match check, so the backend tags it extensions.code = "NOT_FOUND"
    // (never ETAG_MISMATCH). save() must surface this as `missing` so the presenter
    // routes the view to gone/deleted rather than showing the raw "not found" toast.
    const { deps, calls } = makeMutations(() => ({
      ok: false,
      error: "[GraphQL] target nib not found: n1: nib not found",
      errorCode: "NOT_FOUND",
    }));
    const form = editNibForm(deps, seed({ etag: "etag-1" }));
    form.title = "Mine";

    const outcome = await form.save();

    expect(calls).toHaveLength(1);
    expect(outcome.kind).toBe("missing");
  });

  it("classifies NOT_FOUND as `missing` even under overwrite (a deleted nib cannot be force-saved)", async () => {
    const { deps } = makeMutations(() => ({
      ok: false,
      error: "target nib not found: n1: nib not found",
      errorCode: "NOT_FOUND",
    }));
    const form = editNibForm(deps, seed({ etag: "etag-1" }));
    form.noteExternalChange(seed({ etag: "etag-remote", title: "Theirs" }));
    form.title = "Mine";

    const outcome = await form.save({ overwrite: true });

    expect(outcome.kind).toBe("missing");
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

  it("clears the external-change warning once the buffer converges to the remote's field values, and every field participates", () => {
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

  it("a converged dirty buffer SAVES with the remote etag rather than a silent no-op", async () => {
    // Convergence can strand Save: after a dirty buffer's edits converge to the
    // remote's field values the getter reads null (banner hides) but the buffer
    // stays dirty-vs-baseline, so Save stays enabled. Reading the raw
    // #externalChange and short-circuiting to {conflict} would dispatch nothing
    // and give no feedback. The content already equals the server's current
    // revision, so save() must perform a REAL write using the remote's etag as
    // if-match.
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
    // sameTags feeds #matchesFields (the convergence decision, which gates a
    // write path). A duplicate-INSENSITIVE compare would treat
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

describe("editNibForm — milestone", () => {
  it("marks the buffer dirty and sends the new assignment", async () => {
    const { deps, calls } = makeMutations(updateResponder());
    const form = editNibForm(deps, seed({ milestone: "" }));

    expect(form.dirty).toBe(false);
    form.milestone = "nibs-m2";
    expect(form.dirty).toBe(true);

    await form.save();
    expect(calls[0].input.milestone).toBe("nibs-m2");
  });

  it("sends null to clear an assignment, the wire's spelling of 'no queue'", async () => {
    const { deps, calls } = makeMutations(updateResponder());
    const form = editNibForm(deps, seed({ milestone: "nibs-m1" }));

    form.milestone = "";
    await form.save();

    // Null, not "": both clear on the server, and null is what every other
    // clearable field on this input already sends.
    expect(calls[0].input.milestone).toBeNull();
  });

  it("OMITS the field entirely when the assignment did not change", async () => {
    // The load-bearing one. `validateAndSetMilestone` runs the assignment door
    // before it compares old to new, so re-asserting an unchanged assignment to
    // a milestone that has since completed would refuse a save that only
    // touched the title. Omitting is what "unchanged" means on this input.
    const { deps, calls } = makeMutations(updateResponder());
    const form = editNibForm(deps, seed({ milestone: "nibs-m1" }));

    form.title = "A new title";
    await form.save();

    expect(calls[0].input).not.toHaveProperty("milestone");
  });

  it("counts a milestone-only difference as an unresolved external change", () => {
    const { deps } = makeMutations(updateResponder());
    const form = editNibForm(deps, seed({ milestone: "nibs-m1" }));

    // Someone else moved it to another wave. Nothing else differs, so the
    // convergence check is the only thing that can notice.
    form.noteExternalChange(seed({ milestone: "nibs-m2", etag: "etag-remote" }));
    expect(form.externalChange).not.toBeNull();

    // Converging on their value resolves it, exactly as it does for a title.
    form.milestone = "nibs-m2";
    expect(form.externalChange).toBeNull();
  });

  it("carries the saved assignment on the returned snapshot", async () => {
    const { deps } = makeMutations(updateResponder());
    const form = editNibForm(deps, seed({ milestone: "" }));

    form.milestone = "nibs-m2";
    const outcome = await form.save();

    expect(outcome.kind).toBe("saved");
    if (outcome.kind === "saved") expect(outcome.snapshot.milestone).toBe("nibs-m2");
    // And the buffer is clean again: the baseline adopted the new value, so a
    // second save would omit the field.
    expect(form.dirty).toBe(false);
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
