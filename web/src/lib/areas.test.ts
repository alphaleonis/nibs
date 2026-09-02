import { describe, it, expect } from "vitest";
import { createAreaVocabulary, EMPTY_AREAS, LOADING_AREAS } from "./areas";
import type { AreaNode, AreaVocabulary } from "./areas";

function area(path: string, depth: number, extra: Partial<AreaNode> = {}): AreaNode {
  const segments = path.split("/");
  return {
    path,
    name: segments[segments.length - 1],
    description: "",
    color: "",
    depth,
    ...extra,
  };
}

/**
 * Declaration order, a parent immediately before the subtree it heads — the
 * order `Config.areas` promises.
 *
 * `webhooks` is a ROOT declared beside `web`, and that is the point of the
 * fixture: it is a string prefix of neither more nor less than `web` is of it,
 * so a `startsWith` implementation of `subtreeOf` would sweep it into `web`
 * while the ordering rule cannot.
 */
const DECLARED: readonly AreaNode[] = [
  area("auth", 0, { description: "Login and sessions", color: "#c084fc" }),
  area("web", 0),
  area("web/dashboard", 1),
  area("web/dashboard/charts", 2),
  area("webhooks", 0),
  area("infra", 0),
];

function paths(nodes: readonly AreaNode[]): string[] {
  return nodes.map((n) => n.path);
}

describe("createAreaVocabulary", () => {
  const vocab = createAreaVocabulary(DECLARED);

  it("reports sections in declaration order", () => {
    expect(paths(vocab.sections())).toEqual([
      "auth",
      "web",
      "web/dashboard",
      "web/dashboard/charts",
      "webhooks",
      "infra",
    ]);
  });

  it("copies the input, so a later mutation of the caller's array cannot reach it", () => {
    const source: AreaNode[] = [area("auth", 0)];
    const built = createAreaVocabulary(source);
    source.push(area("smuggled", 0));
    expect(paths(built.sections())).toEqual(["auth"]);
  });

  describe("subtreeOf", () => {
    const cases: { name: string; path: string; want: string[] }[] = [
      {
        name: "a root with a subtree yields itself and everything declared beneath it",
        path: "web",
        want: ["web", "web/dashboard", "web/dashboard/charts"],
      },
      {
        name: "an interior node yields its own run",
        path: "web/dashboard",
        want: ["web/dashboard", "web/dashboard/charts"],
      },
      { name: "a leaf yields itself alone", path: "web/dashboard/charts", want: ["web/dashboard/charts"] },
      { name: "a childless root yields itself alone", path: "auth", want: ["auth"] },
      { name: "the last entry yields itself alone", path: "infra", want: ["infra"] },
      { name: "an undeclared path yields nothing", path: "retired", want: [] },
      { name: "the empty path yields nothing", path: "", want: [] },
    ];

    for (const c of cases) {
      it(c.name, () => {
        expect(paths(vocab.subtreeOf(c.path))).toEqual(c.want);
      });
    }

    // The guard the whole flat-list contract rests on: closure runs over the
    // DECLARED TREE, not over the strings (config.IsAreaWithin says the same on
    // the Go side), and here it runs over the ORDER — so a sibling root that
    // happens to start with the same characters is outside it, and a list
    // shuffled out of declaration order fails rather than quietly answering
    // about a different tree.
    it("never sweeps in a sibling root sharing a string prefix", () => {
      expect(paths(vocab.subtreeOf("web"))).not.toContain("webhooks");
      expect(paths(vocab.subtreeOf("webhooks"))).toEqual(["webhooks"]);
    });
  });

  describe("resolve", () => {
    it("returns the declared node, carrying its description and color", () => {
      expect(vocab.resolve("auth")).toEqual(
        expect.objectContaining({ name: "auth", description: "Login and sessions", color: "#c084fc" }),
      );
    });

    it("resolves a nested path to the node at that path, not to a same-named root", () => {
      expect(vocab.resolve("web/dashboard")?.name).toBe("dashboard");
      expect(vocab.resolve("dashboard")).toBeNull();
    });

    it("returns null for an undeclared value and for the unset one", () => {
      expect(vocab.resolve("retired")).toBeNull();
      expect(vocab.resolve("")).toBeNull();
    });
  });

  describe("completions", () => {
    it("offers matching paths in declaration order", () => {
      expect(vocab.completions("web")).toEqual([
        "web",
        "web/dashboard",
        "web/dashboard/charts",
        "webhooks",
      ]);
    });

    it("matches anywhere in the path, not just at the front", () => {
      expect(vocab.completions("dash")).toEqual(["web/dashboard", "web/dashboard/charts"]);
    });

    it("ignores case", () => {
      expect(vocab.completions("WEB/DASH")).toEqual(["web/dashboard", "web/dashboard/charts"]);
    });

    it("offers everything for an empty partial", () => {
      expect(vocab.completions("")).toEqual(paths(DECLARED));
    });
  });
});

describe("status", () => {
  // Three answers, not two. "none" is a project that declares no areas — normal
  // and permanent — and it must not be reachable from the pre-load window, or
  // the UI would announce a settled fact while still waiting.
  const cases: { name: string; vocab: AreaVocabulary; want: string }[] = [
    { name: "before the config resolves", vocab: LOADING_AREAS, want: "loading" },
    { name: "when the project declares none", vocab: EMPTY_AREAS, want: "none" },
    { name: "when areas are declared", vocab: createAreaVocabulary(DECLARED), want: "ready" },
  ];
  for (const c of cases) {
    it(`is "${c.want}" ${c.name}`, () => {
      expect(c.vocab.status).toBe(c.want);
    });
  }
});

describe("validity", () => {
  // The distinction a filter round-trip depends on: the server refuses an
  // undeclared `area:` outright — the whole query fails, not just the predicate
  // — so reading "not yet loaded" as "undeclared" would either drop a valid
  // token or send one that fails.
  it("answers \"unknown\" before load and \"undeclared\" after, for the same input", () => {
    expect(LOADING_AREAS.validity("retired")).toBe("unknown");
    expect(createAreaVocabulary(DECLARED).validity("retired")).toBe("undeclared");
  });

  it("answers \"unknown\" before load and \"declared\" after, for a declared input", () => {
    expect(LOADING_AREAS.validity("web/dashboard")).toBe("unknown");
    expect(createAreaVocabulary(DECLARED).validity("web/dashboard")).toBe("declared");
  });

  it("answers \"undeclared\" for the unset value, matching config.IsValidArea", () => {
    expect(createAreaVocabulary(DECLARED).validity("")).toBe("undeclared");
    expect(EMPTY_AREAS.validity("")).toBe("undeclared");
  });
});

describe("the degenerate vocabularies", () => {
  it("answer emptily without throwing", () => {
    for (const vocab of [LOADING_AREAS, EMPTY_AREAS]) {
      expect(vocab.sections()).toEqual([]);
      expect(vocab.resolve("web")).toBeNull();
      expect(vocab.subtreeOf("web")).toEqual([]);
      expect(vocab.completions("")).toEqual([]);
    }
  });

  // Module singletons, shared by every test file a vitest worker runs, so one
  // reassigned method would follow the worker into unrelated suites.
  it("are frozen", () => {
    for (const vocab of [LOADING_AREAS, EMPTY_AREAS]) {
      expect(Object.isFrozen(vocab)).toBe(true);
      expect(() => {
        (vocab as unknown as Record<string, unknown>).validity = () => "declared";
      }).toThrow();
    }
  });
});
