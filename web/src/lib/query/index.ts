// GitHub-style query language for the web filter box: parsing and canonical
// serialization between box text and NibFilter, completion, and highlighting.
//
// Every function is pure. Runtime vocabularies (area paths, tags) are passed in;
// `../areas` is imported for types only.
//
// Token kinds: metadata facets `field:v1,v2` with `-` for exclusion (fields.ts),
// relationship and existence tokens (relations.ts), and `area:<path>` (area.ts).
// Everything else is free-text `search`.
export { parseQuery } from "./parse";
export type { ParsedQuery } from "./parse";
export { serializeQuery } from "./serialize";
export type { QueryFilter } from "./fields";
export { getCompletion } from "./complete";
export type { Completion, CompletionKind } from "./complete";
// Syntax-highlight spans for the box's backdrop.
export { tokenizeSpans } from "./spans";
export type { Span, SpanKind } from "./spans";
// The in-UI syntax reference, generated from the vocabulary.
export { queryHelpSections } from "./help";
export type { HelpSection, HelpRow } from "./help";
// Token/gap groupings for the box's backdrop and click layer.
export { tokenSegments, tokenGroups } from "./tokens";
export type { TokenSegment, TokenGroup } from "./tokens";
// `RelIdKey` is the field the row menu's "Filter related" items set.
export type { RelIdKey, ExistenceKey } from "./relations";
// For the table's empty state: the hierarchy tokens in a filter and the filter
// without them, and the contradictory pairs the server refuses.
export { hierarchyTokens, clearHierarchyFilters } from "./relations";
export { contradictionTokens } from "./relations";
// Caret detection for the relationship-id typeahead; the Toolbar runs the search.
export { relTokenValueContext } from "./relComplete";
export type { RelValueContext, NibSuggestion } from "./relComplete";
// The box asks `isRefusedArea` again when the vocabulary changes; `parseQuery`
// asked once, with whatever vocabulary its caller had.
export { AREA_FIELD, isRefusedArea } from "./area";
