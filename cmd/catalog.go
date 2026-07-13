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
)

// catalogJSON toggles the structured JSON form of the topics that support it
// (fields, filters, hierarchy, recipes, and the topic index). examples always
// emits JSON payloads; schema always emits the raw SDL.
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
// in runCatalog's dispatch (pinned by TestCatalogTopicsMatchDispatch).
var catalogTopics = []catalogTopic{
	{"fields", "Every projectable field with its kind and the JSON key it serializes to"},
	{"filters", "The list/rel filter flags and their status/type/priority/estimate enum values"},
	{"hierarchy", "The legal parent (and child) types per nib type"},
	{"examples", "A real {nib} and {nibs,count,truncated} JSON payload"},
	{"recipes", "The composite/agent views (context, plan, roadmap, list --ready)"},
	{"schema", "The GraphQL SDL"},
}

var catalogCmd = &cobra.Command{
	Use:   "catalog [topic]",
	Short: "Print generated agent-onboarding vocabulary (fields, filters, hierarchy, ...)",
	Long: `Print the nibs vocabulary an agent needs, generated from the live definitions
so it can never drift from what the CLI actually accepts.

Every topic is derived at runtime from the code that enforces it: the field
menu from the projection engine, the enum values from config, the parent/child
matrix from the same source the HIERARCHY error uses, and the schema from the
GraphQL executable. Nothing here is hand-transcribed.

Topics:
  fields      Every projectable field (` + "`-f`/--fields" + `) with its kind and JSON key.
  filters     The list/rel filter flags and their enum values.
  hierarchy   The legal parent (and child) types per nib type.
  examples    A real {nib} and {nibs,count,truncated} JSON payload.
  recipes     The composite/agent views and their one-line purpose.
  schema      The GraphQL SDL (same as 'nibs graphql --schema').

Run 'nibs catalog' with no topic for this index. --json emits structured data
for fields, filters, hierarchy, recipes, and the index; examples is always JSON;
schema is always SDL.`,
	Args:              cobra.MaximumNArgs(1),
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

// catalogFilters renders the enum-valued filter flags and their values,
// generated from config (the single source of truth for the enums).
func catalogFilters() error {
	cfg := config.Default()
	filters := []filterInfo{
		{"status", "--status/-s", "--no-status", cfg.StatusNames()},
		{"type", "--type/-t", "--no-type", cfg.TypeNames()},
		{"priority", "--priority/-p", "--no-priority", cfg.PriorityNames()},
		{"estimate", "--estimate/-e", "--no-estimate", cfg.EstimateNames()},
	}
	if catalogJSON {
		return output.JSONRaw(map[string]any{"filters": filters})
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

// catalogExamples emits a real {nib} single-read payload and a
// {nibs,count,truncated} list envelope, projected from a synthetic nib through
// the card view. The shapes are byte-generated by the projection engine — the
// same code 'nibs get --json' and 'nibs list --json' use — so an agent reads the
// exact shape instead of probing. A nil Resolver is safe here: the card view is
// scalars plus bare blocked-by, none of which need the store.
func catalogExamples() error {
	card, err := projection.ViewFields(string(projection.ViewCard))
	if err != nil {
		return err // card is a compile-time-valid view; unreachable in practice
	}

	primary := catalogSampleNib()
	single, err := projection.Project(primary, card, nil)
	if err != nil {
		return err
	}
	list, err := projection.ProjectList([]*nib.Nib{primary, catalogSampleParent()}, card, nil, 0)
	if err != nil {
		return err
	}

	if catalogJSON {
		return output.JSONRaw(map[string]any{
			"nib":  map[string]any{"nib": single},
			"list": list,
		})
	}

	fmt.Println("# Single read — the {nib} contract ('nibs get --json <id>', card view)")
	if err := output.JSONRaw(map[string]any{"nib": single}); err != nil {
		return err
	}
	fmt.Println()
	fmt.Println("# List — the {nibs,count,truncated} envelope ('nibs list --json', card view)")
	return output.JSONRaw(list)
}

// catalogSampleNib is a representative, deterministic nib used to generate the
// examples payloads. It exercises every card-view field (tags, parent, a
// blocker) so the rendered shape shows populated values, not just empties.
func catalogSampleNib() *nib.Nib {
	ts := time.Date(2026, 1, 15, 9, 30, 0, 0, time.UTC)
	return &nib.Nib{
		ID:        "tnib-a1b2",
		Slug:      "wire-up-oauth-login",
		Path:      ".nibs/tnib-a1b2-wire-up-oauth-login.md",
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

// catalogSampleParent is the second element of the examples list envelope, so
// the payload shows count > 1.
func catalogSampleParent() *nib.Nib {
	ts := time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC)
	return &nib.Nib{
		ID:        "tnib-9f3d",
		Slug:      "authentication",
		Path:      ".nibs/tnib-9f3d-authentication.md",
		Title:     "Authentication",
		Status:    "todo",
		Type:      "epic",
		Priority:  "normal",
		Order:     "9z",
		CreatedAt: &ts,
		UpdatedAt: &ts,
	}
}

// recipeInfo names a composite/agent view and its one-line purpose. Command
// purposes are pulled from the live cobra Short strings (and the --ready flag's
// usage) so they cannot drift from the commands themselves.
type recipeInfo struct {
	Command string `json:"command"`
	Purpose string `json:"purpose"`
}

// catalogRecipes renders the composite views agents reach for, generated from
// the live command Short strings and the list --ready flag usage.
func catalogRecipes() error {
	recipes := []recipeInfo{
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

// commandShort returns the Short description of a top-level subcommand by name,
// or a placeholder if it is ever renamed (guarded by a test).
func commandShort(name string) string {
	for _, c := range rootCmd.Commands() {
		if c.Name() == name {
			return c.Short
		}
	}
	return "(command not found: " + name + ")"
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
		"Emit structured JSON (fields, filters, hierarchy, recipes, index; examples is always JSON)")
	rootCmd.AddCommand(catalogCmd)
}
