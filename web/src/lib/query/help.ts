import { FIELD_SPECS, completionValues } from "./fields";
import { REL_TOKEN_ORDER } from "./relations";
import { AREA_TOKEN_ORDER } from "./area";
import { HELP_EXAMPLES, HELP_OPERATORS } from "../generated/filterTokens";

// The in-UI syntax reference for the filter box. Token rows are generated from
// `FIELD_SPECS`, `REL_TOKEN_ORDER` and `AREA_TOKEN_ORDER`, the structures the
// parser reads; do not hand-list tokens. The operator rows and examples come from
// internal/filterlang.

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
      note: "Takes an area path. area: matches that area and the areas nested under it, not every path that starts the same way, so webhooks is not within web.",
      rows: AREA_TOKEN_ORDER.map((spec) => ({
        token: `${spec.name}:<path>`,
        description: spec.description,
      })),
    },
    {
      title: "Operators",
      rows: HELP_OPERATORS.map((row) => ({ ...row })),
    },
    {
      title: "Examples",
      rows: HELP_EXAMPLES.map((row) => ({ ...row })),
    },
  ];
}
