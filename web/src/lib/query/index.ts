// GitHub-style query language for the web filter box. Pure, dependency-free
// parse/serialize between filter-box text and the structured NibFilter, plus a
// static (context-aware) completion helper for the input.
//
// Grammar (phase 2): the five metadata facets — type, priority, status, estimate,
// tags — each as `field:v1,v2` (OR within the field) with an optional `-` prefix
// for exclusion (`-type:bug` → excludeType). The four enums are value-validated;
// tags are pattern-checked. Known-field tokens with an invalid value are carried
// in an `invalidTokens` sidecar. Everything else (unknown fields, bare words) is
// free-text `search`. Serialization is canonical for stable round-trips.
export { parseQuery } from "./parse";
export type { ParsedQuery } from "./parse";
export { serializeQuery } from "./serialize";
export type { QueryFilter } from "./fields";
export { getCompletion } from "./complete";
export type { Completion, CompletionKind } from "./complete";
// Syntax-highlight spans: a pure, read-only view over the same tokens for the
// filter box's backdrop highlight layer.
export { tokenizeSpans } from "./spans";
export type { Span, SpanKind } from "./spans";
