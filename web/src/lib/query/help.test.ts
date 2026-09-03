import { describe, it, expect } from "vitest";
import { queryHelpSections } from "./help";
import { FIELD_SPECS } from "./fields";
import { REL_TOKEN_ORDER } from "./relations";
import { AREA_DESCRIPTION, AREA_FIELD } from "./area";

const sections = () => queryHelpSections();
const section = (title: string) => sections().find((s) => s.title === title)!;

describe("queryHelpSections — generated from the parser's own vocabulary", () => {
  // The point of generating rather than hand-listing: the help cannot document a
  // token the parser rejects, and cannot miss one it accepts. These assert
  // coverage against the source arrays, so adding a token to the vocabulary
  // without touching help.ts still lands in the panel.
  it("lists every metadata field", () => {
    const tokens = section("Fields").rows.map((r) => r.token);
    expect(tokens).toHaveLength(FIELD_SPECS.length);
    for (const spec of FIELD_SPECS) expect(tokens).toContain(`${spec.name}:<value>`);
  });

  it("lists every relationship-id token", () => {
    const tokens = section("Relationships").rows.map((r) => r.token);
    const idSpecs = REL_TOKEN_ORDER.filter((t) => t.kind === "id");
    expect(tokens).toHaveLength(idSpecs.length);
    for (const t of idSpecs) expect(tokens).toContain(`${t.name}:<id>`);
  });

  it("lists every existence token", () => {
    const tokens = section("Presence").rows.map((r) => r.token);
    const boolSpecs = REL_TOKEN_ORDER.filter((t) => t.kind === "bool");
    expect(tokens).toHaveLength(boolSpecs.length);
    for (const t of boolSpecs) expect(tokens).toContain(t.token);
  });

  it("documents the area token, spelled the way the parser reads it", () => {
    const rows = section("Areas").rows;
    expect(rows).toHaveLength(1);
    expect(rows[0].token).toBe(`${AREA_FIELD}:<path>`);
    expect(rows[0].description).toBe(AREA_DESCRIPTION);
  });

  it("says what the area token does that no other token does", () => {
    // Downward closure has no expression in the grammar — the token carries one
    // path — so the panel is the only place it is stated.
    expect(section("Areas").note).toMatch(/webhooks is not within web/);
  });

  // Coverage counts alone would pass on an array of blanks.
  it("gives every generated row a non-empty description", () => {
    for (const title of ["Fields", "Relationships", "Presence", "Areas"]) {
      for (const row of section(title).rows) {
        expect(row.description.trim(), `${title} / ${row.token}`).not.toBe("");
      }
    }
  });

  // Status carries the `open` / `closed` group shorthands; the help must surface
  // them, since they are the spellings the box canonicalizes TO.
  it("shows a field's group names alongside its concrete values", () => {
    const status = section("Fields").rows.find((r) => r.token === "status:<value>")!;
    expect(status.description).toContain("open");
    expect(status.description).toContain("closed");
    expect(status.description).toContain("in-progress");
  });

  // Tags are pattern-checked, not enumerated, so listing "values" for them would
  // either be empty or wrong.
  it("describes tags as free-form rather than enumerating values", () => {
    const tags = section("Fields").rows.find((r) => r.token === "tags:<value>")!;
    expect(tags.description).toMatch(/any tag/i);
  });

  it("authors the operator and example sections", () => {
    expect(section("Operators").rows.length).toBeGreaterThan(3);
    expect(section("Examples").rows.length).toBeGreaterThan(3);
  });

  // Every example must be something the box would actually accept. A help panel
  // that demonstrates a rejected query is worse than none.
  it("only shows examples the parser accepts", async () => {
    const { parseQuery } = await import("./index");
    for (const row of section("Examples").rows) {
      expect(parseQuery(row.token).invalidTokens, row.token).toEqual([]);
    }
  });
});
