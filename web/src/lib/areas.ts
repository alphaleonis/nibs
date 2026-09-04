/**
 * The areas vocabulary, as the client asks about it.
 *
 * Areas are the one genuinely per-project vocabulary — statuses, types,
 * priorities, estimates and the hierarchy rules are generated at build time into
 * `generated/vocabulary.ts`, and a per-store `areas:` block cannot be. So this
 * one arrives at runtime, over `Config.areas`.
 *
 * A PORT OF QUESTIONS, not a getter over a list. Each method mirrors a decision
 * the Go side already makes (`config.GetArea`, `config.IsValidArea`,
 * `config.IsAreaWithin`, `config.AreasDeclared`), so a consumer asks rather than
 * re-derives — which is what keeps the two sides from drifting.
 *
 * Pure: no Svelte, no urql. The production adapter builds one from the config
 * query; a test builds one from a literal.
 */

/** One declared area. Mirrors the `Area` type on the wire. */
export interface AreaNode {
  /** Full path from a root, segments joined with "/" — the value a nib's `area:` carries. */
  readonly path: string;
  /** This node's own segment. */
  readonly name: string;
  readonly description: string;
  /** Display color — hex code or bare color name; empty when unset. */
  readonly color: string;
  /** Depth from a root; 0 at the top level. */
  readonly depth: number;
}

/**
 * Whether a value is one the server's `area:` filter will accept.
 *
 * THREE-valued on purpose. The server refuses an undeclared `area:` filter
 * outright — the whole `nibs` query fails, not just that predicate — and a
 * filter round-trips through localStorage and `?q=`, so it can be held before
 * the vocabulary has arrived. Reading "not yet loaded" as "undeclared" would
 * either drop a valid token or send one that fails the query.
 */
export type AreaValidity = "declared" | "undeclared" | "unknown";

export interface AreaVocabulary {
  /**
   * "none" means the project declares no areas — a normal and permanent state
   * (`config.AreasDeclared` is the same question), distinct from "loading".
   * Never conflated with `sections().length`, because those are different
   * answers to different questions.
   *
   * "unavailable" means the config query FAILED, and is neither of the other
   * two: "loading" would promise an answer shortly that is never coming, and
   * "none" would assert a fact about the project we could not ask about. Both
   * empty states are "no sections to show", but only one of them is a healthy
   * project, and they earn different remedies.
   */
  readonly status: "loading" | "none" | "ready" | "unavailable";
  /** Every declared area in DECLARATION order. */
  sections(): readonly AreaNode[];
  /** What a nib's stored `area:` resolves to, or null when it names no declared
   *  area (`config.GetArea`). Stored values arrive verbatim. */
  resolve(stored: string): AreaNode | null;
  /** `config.IsValidArea`, plus the pre-load third answer. */
  validity(path: string): AreaValidity;
  /** The downward closure, `path` included — `config.IsAreaWithin` read forwards.
   *  Empty when `path` names no declared area. */
  subtreeOf(path: string): readonly AreaNode[];
  /** What completes `area:<partial>` — declaration order, case-insensitive substring. */
  completions(partial: string): readonly string[];
}

/**
 * A declared color narrowed to what may be handed to CSS, or null for one that
 * may not.
 *
 * `AreaConfig.Color` is free text out of a store's areas.yml and reaches an
 * inline style, so it is narrowed here rather than trusted at the sink: a bare
 * CSS color name or a hex code — the two shapes that field documents — and
 * nothing else. Narrowing loses a color a project wrote some other legal way
 * (`rgb(...)`, `oklch(...)`), which is the price of not passing a `;` into a
 * style declaration.
 *
 * The narrowing is the boundary, and it has to be: an inline style is a
 * declaration LIST, so a value carrying its own `;` can end its declaration and
 * open another rather than being rejected as one malformed value. Executed
 * against the sink rather than reasoned about — the single-property form drops
 * the same string, so the sink cannot be judged by the call it resembles.
 */
export function cssColor(color: string): string | null {
  return /^[a-zA-Z]+$|^#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/.test(color) ? color : null;
}

const EMPTY_NODES: readonly AreaNode[] = Object.freeze([]);
const EMPTY_PATHS: readonly string[] = Object.freeze([]);

/**
 * Build a vocabulary from the flat list the server sends.
 *
 * The list is in DECLARATION order with a parent immediately before the subtree
 * it heads, and that ordering is the contract `subtreeOf` reads: a node's
 * subtree is the maximal run of following entries with a greater `depth`. The
 * client therefore never restates `IsAreaWithin`'s segment descent, and
 * `webhooks ⊄ web` falls out of the ordering rather than out of a string test
 * one side could tighten alone.
 */
export function createAreaVocabulary(flat: readonly AreaNode[]): AreaVocabulary {
  const nodes: readonly AreaNode[] = Object.freeze([...flat]);
  const indexByPath = new Map<string, number>();
  for (let i = 0; i < nodes.length; i++) indexByPath.set(nodes[i].path, i);

  function subtreeOf(path: string): readonly AreaNode[] {
    const start = indexByPath.get(path);
    if (start === undefined) return EMPTY_NODES;
    const depth = nodes[start].depth;
    let end = start + 1;
    while (end < nodes.length && nodes[end].depth > depth) end++;
    return Object.freeze(nodes.slice(start, end));
  }

  // Frozen because a vocabulary is routinely a module singleton shared by every
  // test file in a vitest worker, where one reassigned method would follow the
  // worker into unrelated suites.
  return Object.freeze({
    status: nodes.length === 0 ? "none" : "ready",
    sections: () => nodes,
    resolve: (stored: string) => {
      const i = indexByPath.get(stored);
      return i === undefined ? null : nodes[i];
    },
    validity: (path: string): AreaValidity => (indexByPath.has(path) ? "declared" : "undeclared"),
    subtreeOf,
    completions: (partial: string) => {
      const needle = partial.toLowerCase();
      return Object.freeze(
        nodes.flatMap((n) => (n.path.toLowerCase().includes(needle) ? [n.path] : [])),
      );
    },
  } satisfies AreaVocabulary);
}

/**
 * The vocabulary before the config query resolves.
 *
 * Not `createAreaVocabulary([])`: that answers "undeclared" for every path,
 * which is the one wrong answer during this window. Everything else is empty
 * either way.
 */
export const LOADING_AREAS: AreaVocabulary = Object.freeze({
  status: "loading",
  sections: () => EMPTY_NODES,
  resolve: () => null,
  validity: () => "unknown",
  subtreeOf: () => EMPTY_NODES,
  completions: () => EMPTY_PATHS,
} satisfies AreaVocabulary);

/** The vocabulary of a project that declares no areas. */
export const EMPTY_AREAS: AreaVocabulary = createAreaVocabulary([]);

/**
 * The vocabulary when the config query failed.
 *
 * `validity()` still answers "unknown", for the same reason `LOADING_AREAS` does
 * — a stored `area:` token must not be judged undeclared on the strength of an
 * answer that never arrived. `status` is the only member that differs from
 * `LOADING_AREAS`, and it is what lets a consumer that would wait on "loading"
 * stop instead and say why.
 */
export const UNAVAILABLE_AREAS: AreaVocabulary = Object.freeze({
  status: "unavailable",
  sections: () => EMPTY_NODES,
  resolve: () => null,
  validity: () => "unknown",
  subtreeOf: () => EMPTY_NODES,
  completions: () => EMPTY_PATHS,
} satisfies AreaVocabulary);
