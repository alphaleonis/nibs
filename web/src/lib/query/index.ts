// GitHub-style query language for the web filter box. Pure, dependency-free
// parse/serialize between filter-box text and the structured NibFilter, plus a
// static (context-aware) completion helper for the input.
//
// Grammar (phase 2): the five metadata facets — type, priority, status, estimate,
// tags — each as `field:v1,v2` (OR within the field) with an optional `-` prefix
// for exclusion (`-type:bug` → excludeType). The four enums are value-validated;
// tags are pattern-checked. `status:` additionally accepts the group names `open`
// and `closed`, which expand to their member statuses on parse and collapse back
// on serialize. Known-field tokens with an invalid value are carried in an
// `invalidTokens` sidecar. Everything else (unknown fields, bare words) is
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
// Token/gap segmentation: the pure boundary helper the box's click-affordance layer
// consumes to give each filter token one hit-region (phase 7).
export { tokenSegments } from "./tokens";
export type { TokenSegment } from "./tokens";
// Relationship/existence token support (phase 5). `RelIdKey` is the set of scalar
// relationship-id NibFilter keys — the field the row context menu's "Filter
// related" items compose onto the current filter.
export type { RelIdKey, ExistenceKey } from "./relations";
// The hierarchy subset of that vocabulary: the tokens naming a nib's tree position,
// and the escape hatch that drops them. Used by the table's empty state to explain a
// result emptied by several tree constraints at once.
export { hierarchyTokens, clearHierarchyFilters } from "./relations";
// Async ID/title typeahead for relationship-id token values (phase 6). The pure
// caret-in-value detector + the candidate-row shape; the debounced fetch lives in
// the Toolbar, the search fn in `../searchNibs`.
export { relTokenValueContext } from "./relComplete";
export type { RelValueContext, NibSuggestion } from "./relComplete";
