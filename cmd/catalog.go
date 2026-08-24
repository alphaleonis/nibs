package cmd

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibtypes"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/projection"
	"github.com/alphaleonis/nibs/internal/store"
)

// catalogJSON toggles the structured JSON form of the topics that support it
// (fields, filters, hierarchy, areas, recipes, and the topic index). examples
// always emits JSON payloads; schema always emits the raw SDL.
var catalogJSON bool

// catalogTopic is one entry in the catalog's self-describing topic index. The
// index is the single source of truth for the valid topic set — both the
// no-argument listing and the "unknown topic" error name it, so they cannot
// drift from the dispatch switch.
type catalogTopic struct {
	Name        string `json:"topic"`
	Description string `json:"description"`
}

// catalogTopics is the ordered topic index. Each Name must have a matching case
// in runCatalog's dispatch — TestCatalogTopicsRunClean drives every advertised
// topic and fails on one the dispatch cannot render. Only that direction is
// guarded: a dispatch case missing from the index still runs when typed, it is
// simply advertised nowhere, which costs discoverability rather than an error.
var catalogTopics = []catalogTopic{
	{"fields", "Every projectable field with its kind and the JSON key it serializes to"},
	{"filters", "The list/rel filter flags and their status/type/priority/estimate enum values"},
	{"hierarchy", "The legal parent (and child) types per nib type"},
	{"areas", "The areas vocabulary: how a project declares one, and the verbs that assign, filter and edit it"},
	{"examples", "A real {nib} and {nibs,count,truncated} JSON payload"},
	{"recipes", "The composite/agent views (next, context, plan, roadmap, list --ready)"},
	{"schema", "The GraphQL SDL"},
}

var catalogCmd = &cobra.Command{
	Use:   "catalog [topic]",
	Short: "Print generated agent-onboarding vocabulary (fields, filters, hierarchy, ...)",
	Long: `Print the nibs vocabulary an agent needs, generated from the live definitions
so it can never drift from what the CLI actually accepts.

Every topic is derived at runtime from the code that enforces it: the field
menu from the projection engine, the enum values from config, the parent/child
matrix from the same source the HIERARCHY error uses, the area verbs and flags
from their own help strings, and the schema from the GraphQL executable. Nothing
here is hand-transcribed.

Topics:
  fields      Every projectable field (` + "`-f`/--fields" + `) with its kind and JSON key.
  filters     The list/rel filter flags and their enum values.
  hierarchy   The legal parent (and child) types per nib type.
  areas       The areas vocabulary and the verbs that assign, filter and edit it.
  examples    A real {nib} and {nibs,count,truncated} JSON payload.
  recipes     The composite/agent views and their one-line purpose.
  schema      The GraphQL SDL (same as 'nibs graphql --schema').

Run 'nibs catalog' with no topic for this index. --json emits structured data
for fields, filters, hierarchy, areas, recipes, and the index; examples is
always JSON; schema is always SDL.`,
	Args:              codedMaximumNArgs(&catalogJSON, 1),
	ValidArgs:         catalogTopicNames(),
	DisableAutoGenTag: true,
	RunE:              runCatalog,
}

// runCatalog dispatches to a topic renderer, or prints the index when called
// with no topic. An unknown topic is a VALIDATION error naming the valid set.
func runCatalog(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return catalogIndex()
	}
	switch args[0] {
	case "fields":
		return catalogFields()
	case "filters":
		return catalogFilters()
	case "hierarchy":
		return catalogHierarchy()
	case "areas":
		return catalogAreas()
	case "examples":
		return catalogExamples()
	case "recipes":
		return catalogRecipes()
	case "schema":
		return catalogSchema()
	default:
		return reportErr(catalogJSON, output.ErrValidation,
			fmt.Errorf("unknown catalog topic %q; valid topics: %s", args[0], strings.Join(catalogTopicNames(), ", ")))
	}
}

// catalogTopicNames returns the topic names in index order.
func catalogTopicNames() []string {
	names := make([]string, len(catalogTopics))
	for i, t := range catalogTopics {
		names[i] = t.Name
	}
	return names
}

// catalogIndex prints the topic index (the no-argument form).
func catalogIndex() error {
	if catalogJSON {
		return output.JSONRaw(map[string]any{"topics": catalogTopics})
	}
	var b strings.Builder
	b.WriteString("nibs catalog <topic> — generated introspection for agents\n\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, t := range catalogTopics {
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", t.Name, t.Description)
	}
	_ = tw.Flush()
	fmt.Print(b.String())
	return nil
}

// catalogFields renders every projectable field with its kind and JSON key,
// generated from projection.FieldCatalog (the same registry that drives `-f`).
func catalogFields() error {
	infos := projection.FieldCatalog()
	if catalogJSON {
		type fieldOut struct {
			Name    string `json:"name"`
			Kind    string `json:"kind"`
			JSONKey string `json:"json_key"`
		}
		out := make([]fieldOut, len(infos))
		for i, f := range infos {
			out[i] = fieldOut{Name: string(f.Name), Kind: f.Kind, JSONKey: f.JSONKey}
		}
		return output.JSONRaw(map[string]any{"fields": out})
	}

	var b strings.Builder
	b.WriteString("Projectable fields (-f/--fields, --view). Kind is scalar, computed, or relation.\n\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "FIELD\tKIND\tJSON KEY")
	for _, f := range infos {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", f.Name, f.Kind, f.JSONKey)
	}
	_ = tw.Flush()
	fmt.Print(b.String())
	return nil
}

// filterInfo describes one enum-valued filter: its list/rel include and exclude
// flags and the legal values, all sourced from config so they cannot drift.
type filterInfo struct {
	Field       string   `json:"field"`
	IncludeFlag string   `json:"include_flag"`
	ExcludeFlag string   `json:"exclude_flag"`
	Values      []string `json:"values"`
}

// scopeFilterInfo describes one scope filter on `nibs list` — the flags that
// select a population by where work sits rather than by an enum value. These
// are published apart from filterInfo above because they answer to neither of
// that table's two claims: their values are nib ids, declared area paths or
// nothing at all rather than a fixed enum, and they are `nibs list` flags
// only — `nibs rel` accepts none of them, so listing them in a table headed
// "list and rel" would state something false about rel.
type scopeFilterInfo struct {
	Flag        string `json:"flag"`
	Description string `json:"description"`
}

// scopeFilterCatalogEntries returns the scope filters, described in the flags'
// own usage strings so the catalog cannot drift from what `nibs list` accepts —
// the same discipline the enum table follows against config.
func scopeFilterCatalogEntries() []scopeFilterInfo {
	return []scopeFilterInfo{
		{"--milestone <id>", flagUsage("list", "milestone")},
		{"--backlog", flagUsage("list", "backlog")},
		{"--area <path>", flagUsage("list", "area")},
	}
}

// statusGroupCatalog describes one status group accepted by -s/--status and
// --no-status and the concrete statuses it expands to, all derived from config
// so the group members cannot drift from what resolveStatusFilter expands.
type statusGroupCatalog struct {
	Group   string   `json:"group"`
	Members []string `json:"members"`
}

// statusGroupCatalogEntries returns the accepted status groups and their
// concrete members, sourced from config (the same sets statusGroupMembers
// expands to).
func statusGroupCatalogEntries(cfg *config.Config) []statusGroupCatalog {
	return []statusGroupCatalog{
		{statusGroupOpen, cfg.OpenStatusNames()},
		{statusGroupClosed, cfg.ClosedStatusNames()},
	}
}

// catalogFilters renders the enum-valued filter flags and their values,
// generated from config (the single source of truth for the enums). The status
// groups (open/closed) are documented alongside the concrete statuses because
// -s/--status and --no-status accept them anywhere a concrete status is
// accepted, and list/rel apply an open-by-default status filter. The
// scope flags get a block of their own — see scopeFilterInfo for why they
// cannot join the enum table.
func catalogFilters() error {
	cfg := config.Default()
	filters := []filterInfo{
		{"status", "--status/-s", "--no-status", cfg.StatusNames()},
		{"type", "--type/-t", "--no-type", cfg.TypeNames()},
		{"priority", "--priority/-p", "--no-priority", cfg.PriorityNames()},
		{"estimate", "--estimate/-e", "--no-estimate", cfg.EstimateNames()},
	}
	statusGroups := statusGroupCatalogEntries(cfg)
	scopeFilters := scopeFilterCatalogEntries()
	if catalogJSON {
		return output.JSONRaw(map[string]any{
			"filters":         filters,
			"scope_filters":   scopeFilters,
			"status_groups":   statusGroups,
			"open_by_default": true,
		})
	}

	var b strings.Builder
	b.WriteString("Enum filters for 'nibs list' and 'nibs rel'. Repeat a flag to OR values;\n")
	b.WriteString("use the exclude flag to filter out. Values are fixed (not configurable).\n\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "FIELD\tINCLUDE\tEXCLUDE\tVALUES")
	for _, f := range filters {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", f.Field, f.IncludeFlag, f.ExcludeFlag, strings.Join(f.Values, ", "))
	}
	_ = tw.Flush()

	b.WriteString("\nScope filters — 'nibs list' only ('nibs rel' takes none of them). The two\n")
	b.WriteString("milestone flags are mutually exclusive with each other:\n\n")
	stw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(stw, "FLAG\tSELECTS")
	for _, q := range scopeFilters {
		_, _ = fmt.Fprintf(stw, "%s\t%s\n", q.Flag, q.Description)
	}
	_ = stw.Flush()
	b.WriteString("\nAssign a milestone with 'nibs set <id> --milestone <ms>' and reposition\n")
	b.WriteString("within its queue with 'nibs mv <id> --queue --after|--before <anchor>' (or\n")
	b.WriteString("--first, which takes no anchor). Assign an area with 'nibs set <id> --area\n")
	b.WriteString("<path>'. Areas are declared per project, so --area's values are not listed\n")
	b.WriteString("here: run 'nibs catalog areas' for the grammar and 'nibs area list' for what\n")
	b.WriteString("this store declares.\n")

	b.WriteString("\nStatus groups — accepted by -s/--status and --no-status anywhere a concrete\n")
	b.WriteString("status is (they expand to their member statuses):\n\n")
	gtw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(gtw, "GROUP\tMEMBERS")
	for _, g := range statusGroups {
		_, _ = fmt.Fprintf(gtw, "%s\t%s\n", g.Group, strings.Join(g.Members, ", "))
	}
	_ = gtw.Flush()

	b.WriteString("\n'nibs list' and 'nibs rel' show only open nibs by default (the closed\n")
	b.WriteString("statuses are hidden). An explicit -s overrides that (-s closed shows only\n")
	b.WriteString("closed nibs), --all includes every status, and --open is shorthand for\n")
	b.WriteString("-s open.\n")
	b.WriteString("\nThe groups partition the declared statuses: open is a workflow position,\n")
	b.WriteString("closed a close reason. A nib carrying a status outside that vocabulary (a\n")
	b.WriteString("hand-edited file with no 'status:' holds \"\") is in neither group — the open\n")
	b.WriteString("default keeps it, but -s open and -s closed both drop it.\n")
	b.WriteString("\nNote that deferred is closed but still blocks its dependents (unlike\n")
	b.WriteString("completed and scrapped), because the work is coming back.\n")
	fmt.Print(b.String())
	return nil
}

// hierarchyInfo describes the legal parent and child types for one nib type,
// both sourced from internal/nibtypes — the same source the HIERARCHY error
// uses — so the catalog and the validation error can never disagree.
type hierarchyInfo struct {
	Type          string   `json:"type"`
	ValidParents  []string `json:"valid_parents"`
	ValidChildren []string `json:"valid_children"`
}

// catalogHierarchy renders the parent/child type matrix, generated from
// nibtypes.ValidParentTypes / ValidChildTypes.
func catalogHierarchy() error {
	rows := make([]hierarchyInfo, 0, len(config.DefaultTypes))
	for _, name := range (config.Default()).TypeNames() {
		rows = append(rows, hierarchyInfo{
			Type:          name,
			ValidParents:  normalizeEmpty(nibtypes.ValidParentTypes(name)),
			ValidChildren: normalizeEmpty(nibtypes.ValidChildTypes(name)),
		})
	}
	if catalogJSON {
		return output.JSONRaw(map[string]any{"types": rows})
	}

	var b strings.Builder
	b.WriteString("Legal nib hierarchy. A nib may only be parented under one of its valid parents\n")
	b.WriteString("(the same rule the HIERARCHY error enforces). '(none)' means top-level only.\n\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "TYPE\tVALID PARENTS\tVALID CHILDREN")
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Type, joinOrNone(r.ValidParents), joinOrNone(r.ValidChildren))
	}
	_ = tw.Flush()
	fmt.Print(b.String())
	return nil
}

// areaCommandCatalogEntries returns the area surface: the three verbs that read
// and edit the vocabulary, and the three flags that assign to it and filter by
// it. Every purpose is the command's own Short or the flag's own usage string,
// so this table cannot drift from what the CLI accepts.
func areaCommandCatalogEntries() []recipeInfo {
	return []recipeInfo{
		{"nibs area list", commandShort("area", "list")},
		{"nibs area rename <path> <new-name>", commandShort("area", "rename")},
		{"nibs area rm <path>", commandShort("area", "rm")},
		{`nibs new "<title>" -t <type> --area <path>`, flagUsage("new", "area")},
		{"nibs set <id> --area <path>", flagUsage("set", "area")},
		{"nibs list --area <path>", flagUsage("list", "area")},
	}
}

// catalogAreas renders the areas grammar: how a project declares the vocabulary
// and the verbs that assign to, filter by and edit it.
//
// It renders the GRAMMAR and not the vocabulary, which is the one thing that
// separates this topic from every other one. Areas are the only vocabulary a
// project authors itself, and `catalog` resolves no store (it sits in
// commandNeedsNoStore, so it answers the same outside a project as inside one) —
// so the declared set is one command away rather than inlined here, and the text
// says which command that is.
func catalogAreas() error {
	commands := areaCommandCatalogEntries()
	if catalogJSON {
		return output.JSONRaw(map[string]any{
			"commands":           commands,
			"path_separator":     config.AreaPathSeparator,
			"declared_in":        store.ConfigFileName,
			"downward_closed":    true,
			"vocabulary_command": areaVocabularyCommand,
		})
	}

	var b strings.Builder
	b.WriteString("Areas are the one vocabulary a project authors itself — statuses, types,\n")
	b.WriteString("priorities and estimates are fixed. A store declares them as a nested\n")
	b.WriteString("`areas:` block in its " + store.ConfigFileName + ", and a nib is placed in one by the node's\n")
	b.WriteString("FULL path: `web" + config.AreaPathSeparator + "dashboard` is the `dashboard` node declared under `web`.\n")
	b.WriteString("\nThe declared set is per project, so it is not printed here — this command\n")
	b.WriteString("resolves no store. Run '" + areaVocabularyCommand + "' for this project's tree, each node\n")
	b.WriteString("with the description that says what belongs in it (--json for the same tree\n")
	b.WriteString("as data).\n\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "COMMAND\tPURPOSE")
	for _, c := range commands {
		_, _ = fmt.Fprintf(tw, "%s\t%s\n", c.Command, c.Purpose)
	}
	_ = tw.Flush()
	b.WriteString("\n--area takes a declared PATH, never a nib id, and a path the store does not\n")
	b.WriteString("declare is a validation error (exit 2) naming the declared set — on the\n")
	b.WriteString("filter as much as on the two assignment flags, so a typo is an error rather\n")
	b.WriteString("than a silently empty listing; a store that declares no areas says that\n")
	b.WriteString("instead. 'nibs set <id> --clear area' unassigns.\n")
	b.WriteString("\n'nibs list --area' is DOWNWARD-CLOSED over the declared tree: --area web\n")
	b.WriteString("selects the nibs in web and those in every area declared beneath it. It is a\n")
	b.WriteString("'nibs list' flag only ('nibs rel' takes no --area), and a milestone takes no\n")
	b.WriteString("area at all.\n")
	b.WriteString("\nEditing the vocabulary rewrites the nibs assigned at or below what the edit\n")
	b.WriteString("touches, because an area is a PATH: 'nibs area rename' moves every path\n")
	b.WriteString("under the node it renames, and 'nibs area rm' is refused while nibs are\n")
	b.WriteString("assigned at or below the node unless a disposition says where they go\n")
	b.WriteString("(--move-to <area> reassigns them, --unassign drops their assignment).\n")
	b.WriteString("'nibs check' reports a nib whose area the vocabulary does not declare.\n")
	fmt.Print(b.String())
	return nil
}

// areaVocabularyCommand is the command that prints the project's own declared
// areas. Named once because several surfaces point at it and they must send the
// reader to the same place.
const areaVocabularyCommand = "nibs area list"

// catalogExamples emits a real {nib} single-read payload and a
// {nibs,count,truncated} list envelope, projected from a synthetic nib through
// the card view. The shapes are byte-generated by the projection engine — the
// same code 'nibs get --json' and 'nibs list --json' use — so an agent reads the
// exact shape instead of probing.
//
// There is no store here, so the sample nibs are their own: catalogSampleStore
// answers the one computed field the card view carries (parent, which resolves
// its stored link) over the two synthetic nibs, and nothing else.
func catalogExamples() error {
	card, err := projection.ViewFields(string(projection.ViewCard))
	if err != nil {
		return err // card is a compile-time-valid view; unreachable in practice
	}

	primary := catalogSampleNib()
	store := catalogSampleStore{primary, catalogSampleParent()}
	single, err := projection.Project(primary, card, store)
	if err != nil {
		return err
	}
	list, err := projection.ProjectList([]*nib.Nib{primary, catalogSampleParent()}, card, store, 0)
	if err != nil {
		return err
	}

	recipes := catalogOpenWorkRecipes()
	if catalogJSON {
		return output.JSONRaw(map[string]any{
			"nib":     map[string]any{"nib": single},
			"list":    list,
			"recipes": recipes,
		})
	}

	fmt.Println("# Single read — the {nib} contract ('nibs get --json <id>', card view)")
	if err := output.JSONRaw(map[string]any{"nib": single}); err != nil {
		return err
	}
	fmt.Println()
	fmt.Println("# List — the {nibs,count,truncated} envelope ('nibs list --json', card view)")
	if err := output.JSONRaw(list); err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("\n# Open-work recipes — list/rel are open by default, so no post-filtering\n")
	b.WriteString("# (no '--json | python status filter') is needed:\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, r := range recipes {
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", r.Command, r.Purpose)
	}
	_ = tw.Flush()
	fmt.Print(b.String())
	return nil
}

// catalogOpenWorkRecipes returns the copy-paste command lines for the most
// common open-work questions. They lean on the open-by-default status filter so
// an agent never hand-rolls a '--json | python' post-filter to hide closed
// nibs — the incident that motivated the open default.
func catalogOpenWorkRecipes() []recipeInfo {
	return []recipeInfo{
		{"nibs list", "open work everywhere (closed statuses hidden by default)"},
		{"nibs rel <id> --rel descendants", "open work under a parent (add -t bug for open bugs only)"},
		{"nibs list --milestone <id>", "open work in a milestone's queue, in queue order (--backlog: what no milestone holds)"},
		{"nibs list -s closed", "only closed nibs (an explicit -s overrides the open default)"},
		{"nibs list --all", "every status, including the closed ones"},
		{"nibs list --ready", flagUsage("list", "ready")},
	}
}

// catalogSampleNib is a representative, deterministic nib used to generate the
// examples payloads. It exercises every card-view field (tags, parent, a
// blocker) so the rendered shape shows populated values, not just empties.
func catalogSampleNib() *nib.Nib {
	ts := time.Date(2026, 1, 15, 9, 30, 0, 0, time.UTC)
	return &nib.Nib{
		ID:        "tnib-a1b2",
		Slug:      "wire-up-oauth-login",
		Path:      "data/tnib-a1b2-wire-up-oauth-login.md",
		Title:     "Wire up OAuth login",
		Status:    "in-progress",
		Type:      "feature",
		Priority:  "high",
		Estimate:  "m",
		Tags:      []string{"auth", "web"},
		Parent:    "tnib-9f3d",
		BlockedBy: []string{"tnib-77aa"},
		Order:     "a0",
		CreatedAt: &ts,
		UpdatedAt: &ts,
		Body:      "## Context\n\nUsers want to sign in with their existing accounts.\n",
	}
}

// catalogSampleStore is the projection.Resolver over the two synthetic example
// nibs. Only the fields the card view carries are answered for real — parent,
// which resolves a stored link and so cannot be read off a lone nib. The rollups
// and relation expansions are not in that view, so they answer with zero values
// rather than pretending to a store that does not exist here.
type catalogSampleStore []*nib.Nib

func (s catalogSampleStore) NibByID(id string) (*nib.Nib, bool) {
	for _, b := range s {
		if b.ID == id {
			return b, true
		}
	}
	return nil, false
}

// ParentID mirrors the resolved-parent rule the real store applies: a stored
// link naming no nib in this set is not a parent. Both sample nibs are present,
// so the primary's link resolves and the example shows a real parent id.
func (s catalogSampleStore) ParentID(id string) string {
	b, ok := s.NibByID(id)
	if !ok || b.Parent == "" {
		return ""
	}
	parent, ok := s.NibByID(b.Parent)
	if !ok {
		return ""
	}
	return parent.ID
}

func (s catalogSampleStore) ChildCount(string) int       { return 0 }
func (s catalogSampleStore) Progress(string) any         { return nil }
func (s catalogSampleStore) Ready(string) bool           { return false }
func (s catalogSampleStore) Blocking(string) []string    { return nil }
func (s catalogSampleStore) Mentions(string) []string    { return nil }
func (s catalogSampleStore) MentionedBy(string) []string { return nil }

// catalogSampleParent is the second element of the examples list envelope, so
// the payload shows count > 1.
func catalogSampleParent() *nib.Nib {
	ts := time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC)
	return &nib.Nib{
		ID:        "tnib-9f3d",
		Slug:      "authentication",
		Path:      "data/tnib-9f3d-authentication.md",
		Title:     "Authentication",
		Status:    "todo",
		Type:      "epic",
		Priority:  "normal",
		Order:     "9z",
		CreatedAt: &ts,
		UpdatedAt: &ts,
	}
}

// recipeInfo pairs a runnable command line with its one-line purpose. Purposes
// are pulled from the live cobra Short strings and flag usage strings so they
// cannot drift from the commands themselves.
type recipeInfo struct {
	Command string `json:"command"`
	Purpose string `json:"purpose"`
}

// catalogRecipes renders the composite views agents reach for, generated from
// the live command Short strings and the list --ready flag usage.
func catalogRecipes() error {
	recipes := []recipeInfo{
		{"nibs next", commandShort("next")},
		{"nibs context", commandShort("context")},
		{"nibs plan <id>", commandShort("plan")},
		{"nibs roadmap", commandShort("roadmap")},
		{"nibs list --ready", flagUsage("list", "ready")},
	}
	if catalogJSON {
		return output.JSONRaw(map[string]any{"recipes": recipes})
	}

	var b strings.Builder
	b.WriteString("Composite views for common agent questions.\n\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, r := range recipes {
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", r.Command, r.Purpose)
	}
	_ = tw.Flush()
	fmt.Print(b.String())
	return nil
}

// catalogSchema prints the GraphQL SDL, reusing the exact source
// 'nibs graphql --schema' prints (GetGraphQLSchema). --json does not apply: the
// SDL is not JSON.
func catalogSchema() error {
	fmt.Print(GetGraphQLSchema())
	return nil
}

// commandShort returns the Short description of a command addressed by its path
// from the root — commandShort("next") for a top-level verb, commandShort("area",
// "list") for a subcommand — or a placeholder if any step is ever renamed
// (guarded by a test).
//
// Each step matches on c.Name(), so an ALIAS finds nothing — but unlike a
// skip-list lookup, a miss here is visible in the output rather than silent,
// which is what the placeholder and its guard are for. flagUsage below has the
// same shape and the same answer.
func commandShort(path ...string) string {
	// A caller that names no command is asking for nothing; answering with the
	// root's own Short would put nibs' one-line description where a subcommand's
	// purpose belongs, and read as a real answer.
	if len(path) == 0 {
		return "(command not found: <no command named>)"
	}
	cmd := rootCmd
	for _, name := range path {
		var found *cobra.Command
		for _, c := range cmd.Commands() {
			if c.Name() == name {
				found = c
				break
			}
		}
		if found == nil {
			return "(command not found: " + strings.Join(path, " ") + ")"
		}
		cmd = found
	}
	return cmd.Short
}

// flagUsage returns the usage string of a flag on a top-level subcommand, or a
// placeholder if the command or flag is ever renamed (guarded by a test).
func flagUsage(cmdName, flagName string) string {
	for _, c := range rootCmd.Commands() {
		if c.Name() != cmdName {
			continue
		}
		if f := c.Flags().Lookup(flagName); f != nil {
			return f.Usage
		}
	}
	return "(flag not found: " + cmdName + " --" + flagName + ")"
}

// normalizeEmpty maps a nil slice to a non-nil empty slice so it serializes to
// a JSON [] (a type that cannot have a parent/child) rather than null.
func normalizeEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// joinOrNone renders a slice as a comma-separated list, or "(none)" when empty.
func joinOrNone(s []string) string {
	if len(s) == 0 {
		return "(none)"
	}
	return strings.Join(s, ", ")
}

func init() {
	catalogCmd.Flags().BoolVar(&catalogJSON, "json", false,
		"Emit structured JSON (fields, filters, hierarchy, areas, recipes, index; examples is always JSON)")
	rootCmd.AddCommand(catalogCmd)
}
