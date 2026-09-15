/**
 * The areas vocabulary, as the client asks about it. Unlike the vocabularies
 * generated into `generated/vocabulary.ts`, areas are per-store, so this arrives
 * at runtime over `Config.areas`.
 *
 * The methods mirror `area.Vocabulary`'s `Get`, `Exists`, `IsWithin` and
 * `IsEmpty`; ask them rather than re-deriving. Pure: no Svelte, no urql.
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
 * Whether the server's `area:` filter will accept a value. "unknown" means the
 * vocabulary has not arrived: the server fails the whole query on an undeclared
 * area, so such a token is neither dropped nor sent yet.
 */
export type AreaValidity = "declared" | "undeclared" | "unknown";

export interface AreaVocabulary {
  /**
   * "none": the project declares no areas (`Vocabulary.IsEmpty`), a permanent state
   * distinct from "loading". "unavailable": the config query failed, so neither
   * an answer nor "none" is coming.
   */
  readonly status: "loading" | "none" | "ready" | "unavailable";
  /** Every declared area in the server's order: siblings by name, parents first. */
  sections(): readonly AreaNode[];
  /** The declared area a stored `area:` names, or null (`Vocabulary.Get`). */
  resolve(stored: string): AreaNode | null;
  /** `Vocabulary.Exists`, plus "unknown" before the vocabulary loads. */
  validity(path: string): AreaValidity;
  /** `path` and every area declared beneath it (`Vocabulary.IsWithin`). Empty when
   *  `path` names no declared area. */
  subtreeOf(path: string): readonly AreaNode[];
  /** What completes `area:<partial>` — `sections()` order, case-insensitive substring. */
  completions(partial: string): readonly string[];
}

/**
 * A declared color if it is a bare CSS color name or hex code, else null. The
 * value reaches an inline style, where a `;` would open another declaration, so
 * narrow it here even though the server validates the same shapes
 * (`area.ValidateColor`).
 */
export function cssColor(color: string): string | null {
  return /^[a-zA-Z]+$|^#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/.test(color) ? color : null;
}

const EMPTY_NODES: readonly AreaNode[] = Object.freeze([]);
const EMPTY_PATHS: readonly string[] = Object.freeze([]);

/**
 * Build a vocabulary from the server's flat list, which has siblings sorted by
 * name and each parent immediately before its subtree. `subtreeOf` relies on
 * that: a subtree is the run of following entries with a greater `depth`.
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

  // Frozen: a vocabulary may be a shared module singleton.
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

/** The vocabulary before the config query resolves. Not `createAreaVocabulary([])`,
 *  which would answer "undeclared" for every path. */
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

/** The vocabulary when the config query failed. Answers like `LOADING_AREAS`
 *  except for `status`. */
export const UNAVAILABLE_AREAS: AreaVocabulary = Object.freeze({
  status: "unavailable",
  sections: () => EMPTY_NODES,
  resolve: () => null,
  validity: () => "unknown",
  subtreeOf: () => EMPTY_NODES,
  completions: () => EMPTY_PATHS,
} satisfies AreaVocabulary);

/**
 * The `Select` value for "no area", because a Select reads "" as nothing
 * selected; `fromSelectValue` translates back. The leading "/" keeps it distinct
 * from every declared path, since an area name may not be empty or contain "/"
 * (`area.validateNodes`). Same value as viewSpine's `NO_AREA_KEY`.
 */
export const NO_AREA = "/__no_area__";

/** The stored assignment a picker value means: "" for the None sentinel. */
export function fromSelectValue(value: string): string {
  return value === NO_AREA ? "" : value;
}

/** The picker value for a stored assignment: the None sentinel for "". */
export function toSelectValue(area: string): string {
  return area === "" ? NO_AREA : area;
}
