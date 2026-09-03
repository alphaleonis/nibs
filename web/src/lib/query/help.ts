import { FIELD_SPECS, completionValues } from "./fields";
import { REL_TOKEN_ORDER } from "./relations";
import { AREA_DESCRIPTION, AREA_FIELD } from "./area";

// The in-UI syntax reference for the filter box.
//
// The token rows are DERIVED from the same structures the parser reads —
// `FIELD_SPECS` (plus its group names, via `completionValues`), `REL_TOKEN_ORDER`,
// and `area.ts`. Hand-listing them here would create one more parallel copy of
// the vocabulary, free to document a token the parser rejects or to miss one it
// accepts; `nibs-l1j3` closed exactly that hole for the vocabulary itself, and
// re-opening it in the help would be worse, because help is what a user trusts
// when the box surprises them.
//
// What IS authored here is the prose: the operators section, and the worked
// examples. Those explain meaning rather than enumerate vocabulary, so they cannot
// be generated — but they also cannot go stale in the same silent way, because
// every token they mention is one the generated sections list too.
//
// The CLI documents the same surface (`nibs cheat`, `nibs catalog`), but that
// content lives Go-side with no wire path to this client, so consuming it would
// need codegen or a new GraphQL field. This module reads the TypeScript vocabulary
// this box actually parses, which is the closer source of truth for this UI.

export interface HelpRow {
  /** The token pattern, rendered in code style (`type:<value>`, `has:parent`). */
  token: string;
  /** One line of prose. Empty for rows whose token is self-describing. */
  description: string;
}

export interface HelpSection {
  title: string;
  /** Optional lead-in shown above the rows. */
  note?: string;
  rows: HelpRow[];
}

/** The whole reference, in display order. */
export function queryHelpSections(): HelpSection[] {
  return [
    {
      title: "Fields",
      note: "Filter on a nib's own metadata. Values are listed after each field.",
      rows: FIELD_SPECS.map((spec) => ({
        token: `${spec.name}:<value>`,
        // Tags have no fixed vocabulary (they are pattern-checked), so
        // `completionValues` is empty for them and the row says so instead of
        // rendering an empty list.
        description: spec.values === null ? "any tag on a nib" : completionValues(spec).join(" · "),
      })),
    },
    {
      title: "Relationships",
      note: "Take a nib id. Each names the relationship the MATCHED nib holds toward it.",
      rows: REL_TOKEN_ORDER.filter((t) => t.kind === "id").map((t) => ({
        token: `${t.name}:<id>`,
        description: t.description,
      })),
    },
    {
      title: "Presence",
      note: "No value to supply — these ask whether a relationship exists at all.",
      rows: REL_TOKEN_ORDER.filter((t) => t.kind === "bool").map((t) => ({
        token: t.token,
        description: t.description,
      })),
    },
    {
      title: "Areas",
      // The closure is worth spelling out because the grammar cannot show it: the
      // token carries one path, and what it sweeps in is decided server-side.
      note: "Takes a declared area path. Closure is over the declared tree, so webhooks is not within web.",
      rows: [{ token: `${AREA_FIELD}:<path>`, description: AREA_DESCRIPTION }],
    },
    {
      title: "Operators",
      rows: [
        { token: "a b", description: "Space is AND — every condition must hold" },
        { token: "-type:bug", description: "A leading minus excludes (metadata fields only)" },
        { token: "status:todo,in-progress", description: "A comma is OR within one field" },
        { token: "login flow", description: "Bare words search id, title and body" },
      ],
    },
    {
      title: "Examples",
      rows: [
        { token: "type:bug status:open", description: "Open bugs" },
        { token: "-status:closed -tags:wip", description: "Everything unfinished that is not parked as work-in-progress" },
        { token: "ancestor:nibs-a1b2 type:task", description: "Tasks anywhere under one epic" },
        { token: "is:blocked priority:high", description: "High-priority work that is stuck" },
        { token: "no:parent type:milestone", description: "Top-level milestones" },
        { token: "is:backlog type:epic", description: "Epics no milestone plan covers" },
        { token: "area:web status:open", description: "Open work owned by web or anything declared under it" },
      ],
    },
  ];
}
