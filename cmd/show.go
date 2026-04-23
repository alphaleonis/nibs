package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	showJSON     bool
	showRaw      bool
	showBodyOnly bool
	showETagOnly bool
)

var showCmd = &cobra.Command{
	Use:   "show <id> [id...]",
	Short: "Show a nib's contents",
	Long:  `Displays the full contents of one or more nibs, including front matter and body.`,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		resolver := app.newResolver()

		// Collect all nibs
		var nibs []*nib.Nib
		for _, id := range args {
			b, err := resolver.Query().Nib(context.Background(), id)
			if err != nil {
				return reportErr(showJSON, output.ErrNotFound,
					fmt.Errorf("failed to find nib: %w", err))
			}
			if b == nil {
				return reportErr(showJSON, output.ErrNotFound,
					fmt.Errorf("nib not found: %s", id))
			}
			nibs = append(nibs, b)
		}

		// JSON output — extend the standard nib shape with resolved
		// outbound/inbound mentions so `show --json` is a one-shot
		// "tell me everything about this nib" call (matching the
		// parent/blocking surfacing philosophy).
		if showJSON {
			filtered := filterResolvedBlockers(nibs, app.Core)
			envelopes := make([]showJSONEnvelope, len(filtered))
			for i, b := range filtered {
				envelopes[i] = buildShowJSONEnvelope(b, app.Core)
			}
			if len(envelopes) == 1 {
				return output.JSONRaw(envelopes[0])
			}
			return output.JSONRaw(envelopes)
		}

		// Raw markdown output (frontmatter + body)
		if showRaw {
			for i, b := range nibs {
				if i > 0 {
					fmt.Print("\n---\n\n")
				}
				content, err := b.Render()
				if err != nil {
					return fmt.Errorf("failed to render nib: %w", err)
				}
				fmt.Print(string(content))
			}
			return nil
		}

		// Body only (no header, no styling)
		if showBodyOnly {
			for i, b := range nibs {
				if i > 0 {
					fmt.Print("\n---\n\n")
				}
				fmt.Print(b.Body)
			}
			return nil
		}

		// ETag only (for easy extraction in scripts)
		if showETagOnly {
			for i, b := range nibs {
				if i > 0 {
					fmt.Println()
				}
				fmt.Print(b.ETag())
			}
			return nil
		}

		// Default: styled human-friendly output
		for i, b := range nibs {
			if i > 0 {
				fmt.Println()
				fmt.Println(ui.Muted.Render(strings.Repeat("═", 60)))
				fmt.Println()
			}
			showStyledNib(b,
				computeBlockingIDs(b, app.Core),
				mentionIDs(b, app.Core),
				mentionedByIDs(b, app.Core),
				app.Config())
		}

		return nil
	},
}

// showJSONEnvelope wraps a Nib with its resolved mention ID lists for --json
// output. We want agents to get the mention graph in one call rather than
// chasing a separate `nibs refs` query — same philosophy as parent/blocking
// already being carried on the nib JSON.
//
// We cannot simply embed `*nib.Nib` because it carries a custom MarshalJSON
// method; that method would be promoted to the envelope and drop our
// mentions fields. Instead we marshal the Nib to raw JSON first, then
// re-decode into a map so we can inject the mention arrays. A dedicated
// MarshalJSON handles the merge.
//
// JSON keys use snake_case (`mentioned_by`, not `mentionedBy`) to match the
// existing nib JSON convention — see internal/nib/nib.go (`blocked_by`,
// `created_at`, `updated_at`). If this envelope is ever refactored to use
// struct tags, use `json:"mentioned_by,omitempty"`.
//
// The mention arrays hold IDs (parallel to `blocked_by`), not full nib
// objects, so agents get a uniform shape for relationship fields. Use
// `nibs refs` if the full resolved objects are needed.
type showJSONEnvelope struct {
	Nib         *nib.Nib
	Mentions    []string
	MentionedBy []string
}

// MarshalJSON merges the nib's own JSON shape with the mention arrays.
// `mentions` and `mentioned_by` are ALWAYS emitted — empty arrays (not
// null, not absent) when the nib has no outbound/inbound references.
func (e showJSONEnvelope) MarshalJSON() ([]byte, error) {
	// Serialize the nib (uses the Nib's own MarshalJSON which injects etag).
	nibBytes, err := json.Marshal(e.Nib)
	if err != nil {
		return nil, err
	}
	// Decode into an ordered map so we can add mention fields to the envelope.
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(nibBytes, &merged); err != nil {
		return nil, fmt.Errorf("decoding nib JSON for envelope merge (expected object): %w", err)
	}
	// Guard against a future Nib field colliding with our injected keys.
	for _, reserved := range []string{"mentions", "mentioned_by"} {
		if _, exists := merged[reserved]; exists {
			return nil, fmt.Errorf("nib JSON unexpectedly contains reserved key %q — field name collision", reserved)
		}
	}
	// Always emit both keys as arrays (never null, never absent) so `jq
	// '.mentions | length'` works uniformly for agent consumers.
	mentions := e.Mentions
	if mentions == nil {
		mentions = []string{}
	}
	mentionedBy := e.MentionedBy
	if mentionedBy == nil {
		mentionedBy = []string{}
	}
	mentionsRaw, err := json.Marshal(mentions)
	if err != nil {
		return nil, err
	}
	merged["mentions"] = mentionsRaw
	mentionedByRaw, err := json.Marshal(mentionedBy)
	if err != nil {
		return nil, err
	}
	merged["mentioned_by"] = mentionedByRaw
	return json.Marshal(merged)
}

// buildShowJSONEnvelope wraps a Nib with its outbound/inbound mention ID
// lists. IDs mirror the `blocked_by` shape — callers wanting the full
// resolved objects should use `nibs refs`.
func buildShowJSONEnvelope(b *nib.Nib, reader graph.NibReader) showJSONEnvelope {
	return showJSONEnvelope{
		Nib:         b,
		Mentions:    mentionIDs(b, reader),
		MentionedBy: mentionedByIDs(b, reader),
	}
}

// mentionIDs returns the IDs of nibs that this nib's body mentions.
//
// Convention: lightweight read-only relationship rendering in `show` goes
// through graph.NibReader (Core-direct). Filterable relationship listing
// (e.g. `nibs refs <id> --status todo`) goes through the GraphQL resolver
// path (`resolver.Nib().Mentions(ctx, b, filter)`). See cmd/refs.go for
// the resolver-path pattern.
//
// The ID-extraction step is delegated to graph.MentionIDList so this layer
// and the MentionIds/MentionedByIds GraphQL resolvers share a single
// implementation — if one ever gains a filtering step (e.g. dropping
// archived mentions), the other picks it up for free.
func mentionIDs(b *nib.Nib, reader graph.NibReader) []string {
	return graph.MentionIDList(reader.FindMentions(b.ID))
}

// mentionedByIDs returns the IDs of nibs whose bodies mention this nib.
// See the convention note on mentionIDs — this uses the Core-direct path
// (graph.NibReader.FindMentionedBy) rather than the resolver, and shares
// the graph.MentionIDList extraction helper with the resolver siblings.
func mentionedByIDs(b *nib.Nib, reader graph.NibReader) []string {
	return graph.MentionIDList(reader.FindMentionedBy(b.ID))
}

// computeBlockingIDs returns the IDs of active nibs that this nib is blocking,
// computed from other nibs' blockedBy fields via FindIncomingLinks.
// Filters out resolved (completed/scrapped) nibs — a resolved nib is not
// considered to be blocking anything, and resolved blockees are not shown.
func computeBlockingIDs(b *nib.Nib, reader graph.NibReader) []string {
	if nib.IsResolvedStatus(b.Status) {
		return nil
	}
	incoming := reader.FindIncomingLinks(b.ID)
	var ids []string
	for _, link := range incoming {
		if link.LinkType == "blocked_by" && !nib.IsResolvedStatus(link.FromNib.Status) {
			ids = append(ids, link.FromNib.ID)
		}
	}
	return ids
}

// showStyledNib displays a single nib with styled output.
func showStyledNib(b *nib.Nib, blockingIDs []string, mentions []string, mentionedBy []string, cfg *config.Config) {
	statusCfg := cfg.GetStatus(b.Status)
	statusColor := "gray"
	if statusCfg != nil {
		statusColor = statusCfg.Color
	}
	isArchive := cfg.IsArchiveStatus(b.Status)

	var header strings.Builder
	header.WriteString(ui.ID.Render(b.ID))
	header.WriteString(" ")
	header.WriteString(ui.RenderStatusWithColor(b.Status, statusColor, isArchive))

	// Display type
	if b.Type != "" {
		typeCfg := cfg.GetType(b.Type)
		typeColor := "gray"
		if typeCfg != nil {
			typeColor = typeCfg.Color
		}
		header.WriteString(" ")
		header.WriteString(ui.RenderTypeWithColor(b.Type, typeColor))
	}

	if b.Priority != "" {
		priorityCfg := cfg.GetPriority(b.Priority)
		priorityColor := "gray"
		if priorityCfg != nil {
			priorityColor = priorityCfg.Color
		}
		header.WriteString(" ")
		header.WriteString(ui.RenderPriorityWithColor(b.Priority, priorityColor))
	}
	if b.Estimate != "" {
		estimateCfg := cfg.GetEstimate(b.Estimate)
		estimateColor := "gray"
		if estimateCfg != nil {
			estimateColor = estimateCfg.Color
		}
		header.WriteString(" ")
		header.WriteString(ui.RenderEstimateWithColor(b.Estimate, estimateColor))
	}
	if len(b.Tags) > 0 {
		header.WriteString("  ")
		header.WriteString(ui.Muted.Render(strings.Join(b.Tags, ", ")))
	}
	header.WriteString("\n")
	header.WriteString(ui.Title.Render(b.Title))

	// Display relationships (parent, blocking, mentions — any combination
	// of these present triggers the relationships block).
	if b.Parent != "" || len(blockingIDs) > 0 || len(mentions) > 0 || len(mentionedBy) > 0 {
		header.WriteString("\n")
		header.WriteString(ui.Muted.Render(strings.Repeat("─", 50)))
		header.WriteString("\n")
		header.WriteString(formatRelationships(b, blockingIDs, mentions, mentionedBy))
	}

	header.WriteString("\n")
	header.WriteString(ui.Muted.Render(strings.Repeat("─", 50)))

	headerBox := lipgloss.NewStyle().
		MarginBottom(1).
		Render(header.String())

	fmt.Println(headerBox)

	// Render the body with Glamour
	if b.Body != "" {
		renderer, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(80),
		)
		if err != nil {
			fmt.Printf("failed to create renderer: %v\n", err)
			return
		}

		rendered, err := renderer.Render(b.Body)
		if err != nil {
			fmt.Printf("failed to render markdown: %v\n", err)
			return
		}

		fmt.Print(rendered)
	}
}

// formatRelationships formats parent, blocking, and mention relationships for
// display. blockingIDs are computed from other nibs' blockedBy fields;
// mentions/mentionedBy are body-derived references via the `#<id>` sigil.
// Empty mention slices are omitted, matching existing blocking/blocked-by
// behaviour.
func formatRelationships(b *nib.Nib, blockingIDs []string, mentions []string, mentionedBy []string) string {
	var parts []string

	// Display parent
	if b.Parent != "" {
		parts = append(parts, fmt.Sprintf("%s %s",
			ui.Muted.Render("parent:"),
			ui.ID.Render(b.Parent)))
	}

	// Display blocking (computed from incoming links)
	for _, target := range blockingIDs {
		parts = append(parts, fmt.Sprintf("%s %s",
			ui.Muted.Render("blocking:"),
			ui.ID.Render(target)))
	}

	// Display outbound mentions as a single joined line; non-empty only.
	if len(mentions) > 0 {
		parts = append(parts, fmt.Sprintf("%s %s",
			ui.Muted.Render("mentions:"),
			renderIDList(mentions)))
	}

	// Display inbound mentions as a single joined line; non-empty only.
	if len(mentionedBy) > 0 {
		parts = append(parts, fmt.Sprintf("%s %s",
			ui.Muted.Render("mentioned by:"),
			renderIDList(mentionedBy)))
	}

	return strings.Join(parts, "\n")
}

// renderIDList renders a slice of nib IDs as a comma-separated list, each id
// wrapped with the ui.ID style.
func renderIDList(ids []string) string {
	rendered := make([]string, len(ids))
	for i, id := range ids {
		rendered[i] = ui.ID.Render(id)
	}
	return strings.Join(rendered, ", ")
}

func init() {
	showCmd.Flags().BoolVar(&showJSON, "json", false, "Output as JSON")
	showCmd.Flags().BoolVar(&showRaw, "raw", false, "Output raw markdown without styling")
	showCmd.Flags().BoolVar(&showBodyOnly, "body-only", false, "Output only the body content")
	showCmd.Flags().BoolVar(&showETagOnly, "etag-only", false, "Output only the etag")
	showCmd.MarkFlagsMutuallyExclusive("json", "raw", "body-only", "etag-only")
	rootCmd.AddCommand(showCmd)
}
