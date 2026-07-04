import { describe, it, expect } from "vitest";
import { Flag, Layers, Bug, Sparkles, SquareCheck, FlaskConical } from "@lucide/svelte";
import { typeIcons } from "./icons";

// The web type icons mirror the canonical config/TUI types (internal/config/config.go
// DefaultTypes) so every nib type renders an icon coloured from its --type-* token.
// nibs-159v: research must be present (previously rendered iconless) and the domain
// colour tokens must stay wired to the TUI hues.
describe("typeIcons", () => {
  it("covers every canonical config type, including research", () => {
    for (const type of ["milestone", "epic", "bug", "feature", "task", "research"]) {
      expect(typeIcons[type], `missing icon for type "${type}"`).toBeDefined();
    }
  });

  it("maps each type's colour to its --type-* domain token", () => {
    expect(typeIcons.milestone.color).toBe("var(--type-milestone)");
    expect(typeIcons.epic.color).toBe("var(--type-epic)");
    expect(typeIcons.bug.color).toBe("var(--type-bug)");
    expect(typeIcons.feature.color).toBe("var(--type-feature)");
    expect(typeIcons.task.color).toBe("var(--type-task)");
    expect(typeIcons.research.color).toBe("var(--type-research)");
  });

  it("uses the expected lucide icon component per type", () => {
    expect(typeIcons.milestone.icon).toBe(Flag);
    expect(typeIcons.epic.icon).toBe(Layers);
    expect(typeIcons.bug.icon).toBe(Bug);
    expect(typeIcons.feature.icon).toBe(Sparkles);
    expect(typeIcons.task.icon).toBe(SquareCheck);
    expect(typeIcons.research.icon).toBe(FlaskConical);
  });
});
