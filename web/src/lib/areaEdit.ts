/**
 * Running one area edit, for the surfaces that own their own messaging.
 *
 * Every area form reports a refusal inline rather than as a toast, so the
 * suppression and the message-reading live here once instead of in each form.
 */

import { toast } from "svelte-sonner";
import { graphqlErrorMessage } from "./graphqlError";
import type {
  AddAreaCommand,
  UpdateAreaCommand,
  RemoveAreaCommand,
  LeafCommand,
  CommandResult,
} from "./mutations";

/** The three verbs that edit the areas vocabulary. */
export type AreaCommand = AddAreaCommand | UpdateAreaCommand | RemoveAreaCommand;

/**
 * The field each verb answers under. Exhaustive over the kinds by type, so a
 * fourth area verb fails to compile here rather than quietly reporting no notes.
 */
const PAYLOAD_FIELD: Record<AreaCommand["kind"], string> = {
  "add-area": "addArea",
  "update-area": "updateArea",
  "remove-area": "removeArea",
};

/**
 * The notes on a successful payload. Narrowed at runtime rather than cast: this
 * is wire data, and a note that is not a string would otherwise reach the toast.
 */
function notesOf(data: unknown, kind: AreaCommand["kind"]): string[] {
  const payload = (data as Record<string, unknown> | null | undefined)?.[PAYLOAD_FIELD[kind]];
  const notes = (payload as { notes?: unknown } | null | undefined)?.notes;
  if (!Array.isArray(notes)) return [];
  return notes.filter((note): note is string => typeof note === "string" && note !== "");
}

/** What a form needs back from one edit. */
export interface AreaEditOutcome {
  ok: boolean;
  /** The server's own sentence, without urql's transport prefix; "" on success. */
  message: string;
}

/** The slice of the mutation store this needs, so a form can be tested without one. */
export interface AreaEditRunner {
  execute(cmd: LeafCommand, opts?: { suppressToast?: boolean }): Promise<CommandResult>;
}

export async function runAreaEdit(
  mutations: AreaEditRunner,
  cmd: AreaCommand,
): Promise<AreaEditOutcome> {
  // Suppressed because the caller renders the refusal inline: without this the
  // dispatcher also toasts it, and the user reads the same sentence twice.
  const res = await mutations.execute(cmd, { suppressToast: true });
  if (!res.ok) return { ok: false, message: graphqlErrorMessage(res.error) };

  // The edit SUCCEEDED, so there is nothing to show inline and the form closes —
  // but a note is something the caller has to act on, so it has to outlive that
  // form and must not time out on its own.
  for (const note of notesOf(res.data, cmd.kind)) {
    toast.warning(note, { duration: Infinity });
  }
  return { ok: true, message: "" };
}
