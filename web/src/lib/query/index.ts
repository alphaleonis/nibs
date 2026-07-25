// GitHub-style query language for the web filter box. Pure, dependency-free
// parse/serialize between filter-box text and the structured NibFilter. Phase 1
// handles the `status:` field + free-text `search`; later phases extend the token
// grammar here without touching call sites.
export { parseQuery } from "./parse";
export type { ParsedQuery } from "./parse";
export { serializeQuery } from "./serialize";
