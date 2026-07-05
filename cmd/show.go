package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// truncateBody cuts body to at most maxRunes runes, appending "…" (U+2026)
// when a cut occurred. maxRunes <= 0 returns (body, false) unchanged. An
// empty body short-circuits. Counts RUNES (not bytes) so multi-byte UTF-8
// characters are preserved intact. Equality (len==maxRunes) does NOT
// truncate — the ellipsis is only added when something was actually cut.
func truncateBody(body string, maxRunes int) (string, bool) {
	if body == "" || maxRunes <= 0 {
		return body, false
	}
	if utf8.RuneCountInString(body) <= maxRunes {
		return body, false
	}
	runes := []rune(body)
	return string(runes[:maxRunes]) + "…", true
}

var (
	showJSON       bool
	showRaw        bool
	showBodyOnly   bool
	showETagOnly   bool
	showActive     bool
	showNoMentions bool
	// showBodyChars truncates body previews at N runes when > 0. Applied
	// to default styled, --json, and --body-only output paths; --raw and
	// --etag-only are untouched (byte-faithful / metadata-only).
	showBodyChars int
	// showSummary is shorthand for --body-chars showSummaryDefaultChars.
	// Mutually exclusive with --body-chars.
	showSummary bool
)

// showSummaryDefaultChars is the rune budget applied when --summary is set.
// 300 runes was chosen as a balance between "enough to tell what a nib is
// about" and "cheap enough that agents can survey 20+ nibs without
// flooding context" — the motivating use case in nibs-lmwm.
const showSummaryDefaultChars = 300

var showCmd = &cobra.Command{
	Use:   "show <id> [id...]",
	Short: "Show a nib's contents",
	Long: `Displays the full contents of one or more nibs, including front matter and body.

Human and --json output include outbound (mentions) and inbound (mentioned_by)
body-reference lists. Two flags adjust how those lists are computed:

  --active       Drop completed/scrapped entries from both mention sections,
                 matching the resolved-status filter used by
                 'nibs links --rel mentions-out --active' / --rel mentions-in --active.
  --no-mentions  Skip the mention scan entirely. Mention sections are omitted
                 from human output; --json emits empty arrays for both fields.
                 --no-mentions dominates --active when both are set.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Reject --body-chars values <= 0 before any work. 0 as default
		// (unset) is fine; explicit 0 or negative is an error.
		if cmd.Flags().Changed("body-chars") && showBodyChars <= 0 {
			return reportErr(showJSON, output.ErrValidation,
				fmt.Errorf("--body-chars must be > 0"))
		}

		// Resolve effective body-chars budget: --summary implies the
		// default 300, --body-chars overrides. Mutex between the two is
		// enforced by Cobra's MarkFlagsMutuallyExclusive in init().
		bodyChars := showBodyChars
		if showSummary {
			bodyChars = showSummaryDefaultChars
		}

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
				envelopes[i] = buildShowJSONEnvelope(b, app.Core, showActive, !showNoMentions, bodyChars)
			}
			if len(envelopes) == 1 {
				return output.JSONRaw(envelopes[0])
			}
			return output.JSONRaw(envelopes)
		}

		// Raw markdown output (frontmatter + body) — byte-faithful,
		// never truncated.
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

		// Body only (no header, no styling). Truncates when bodyChars > 0.
		if showBodyOnly {
			for i, b := range nibs {
				if i > 0 {
					fmt.Print("\n---\n\n")
				}
				body, _ := truncateBody(b.Body, bodyChars)
				fmt.Print(body)
			}
			return nil
		}

		// ETag only (for easy extraction in scripts) — metadata only,
		// truncation doesn't apply.
		if showETagOnly {
			for i, b := range nibs {
				if i > 0 {
					fmt.Println()
				}
				fmt.Print(b.ETag())
			}
			return nil
		}

		// Default: styled human-friendly output. Pre-truncate the body
		// on a shallow copy so Glamour renders the shortened text —
		// never mutate the shared *nib.Nib from Core.
		for i, b := range nibs {
			if i > 0 {
				fmt.Println()
				fmt.Println(ui.Muted.Render(strings.Repeat("═", 60)))
				fmt.Println()
			}
			mentions, mentionedBy := computeMentionIDs(b, app.Core, showActive, !showNoMentions)
			rendered := b
			if bodyChars > 0 {
				truncated, didTruncate := truncateBody(b.Body, bodyChars)
				if didTruncate {
					shallow := *b
					shallow.Body = truncated
					rendered = &shallow
				}
			}
			showStyledNib(rendered,
				computeBlockingIDs(b, app.Core),
				mentions,
				mentionedBy,
				app.Config())
		}

		return nil
	},
}

// showJSONEnvelope wraps a Nib with its resolved mention ID lists for --json
// output. We want agents to get the mention graph in one call rather than
// chasing a separate `nibs links --rel mentions-out/--rel mentions-in` query
// — same philosophy as parent/blocking already being carried on the nib JSON.
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
// `nibs links --rel mentions-out` / `--rel mentions-in` if the full
// resolved objects are needed.
//
// Shape contract: `mentions` and `mentioned_by` are ALWAYS emitted (as
// `[]` when empty). The Nib struct's own `blocked_by` / `parent` fields,
// by contrast, remain `omitempty` and are absent when empty — aligning
// them would be a broader breaking change out of scope. Agents writing
// jq pipelines against `show --json` output should defensive-check
// potentially-absent fields: `(.blocked_by // []) | length`.
type showJSONEnvelope struct {
	Nib         *nib.Nib
	Mentions    []string
	MentionedBy []string
	// BodyChars truncates the merged "body" key at this many runes when >0.
	// 0 disables truncation. When truncation occurs, MarshalJSON also
	// injects `body_truncated: true`; it never emits the field as false.
	BodyChars int
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
	// Guard against a future Nib field whose JSON tag collides with our
	// injected keys at the TOP LEVEL. Nested collisions at any depth are
	// not detected because json.Unmarshal into map[string]json.RawMessage
	// flattens only the top layer. That's acceptable: the Nib schema is
	// unlikely to use "mentions" or "mentioned_by" except as a top-level
	// field.
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

	// Body truncation: when BodyChars > 0, cut merged["body"] at that rune
	// count and inject `body_truncated: true` only if something was cut.
	// We decode the existing value to a string, run truncateBody, then
	// re-encode. `body_truncated: false` is intentionally never emitted —
	// absence is the default signal.
	if e.BodyChars > 0 {
		if bodyRaw, exists := merged["body"]; exists {
			var bodyStr string
			if err := json.Unmarshal(bodyRaw, &bodyStr); err != nil {
				return nil, fmt.Errorf("decoding body for truncation: %w", err)
			}
			truncated, didTruncate := truncateBody(bodyStr, e.BodyChars)
			if didTruncate {
				newBodyRaw, err := json.Marshal(truncated)
				if err != nil {
					return nil, err
				}
				merged["body"] = newBodyRaw
				merged["body_truncated"] = json.RawMessage("true")
			}
		}
	}

	return json.Marshal(merged)
}

// buildShowJSONEnvelope wraps a Nib with its outbound/inbound mention ID
// lists. IDs mirror the `blocked_by` shape — callers wanting the full
// resolved objects should use `nibs links --rel mentions-out` / `--rel mentions-in`.
//
// Mention-list population is delegated to computeMentionIDs so the JSON
// path and the human path share one gating decision for --no-mentions /
// --active.
func buildShowJSONEnvelope(b *nib.Nib, reader graph.NibReader, activeOnly, includeMentions bool, bodyChars int) showJSONEnvelope {
	outbound, inbound := computeMentionIDs(b, reader, activeOnly, includeMentions)
	return showJSONEnvelope{
		Nib:         b,
		Mentions:    outbound,
		MentionedBy: inbound,
		BodyChars:   bodyChars,
	}
}

// computeMentionIDs resolves outbound and inbound mention ID lists for b.
// When includeMentions is false (e.g. --no-mentions), returns two empty
// non-nil slices without calling Find*, preserving the always-present
// shape contract for agent consumers. When activeOnly is true
// (e.g. --active), completed/scrapped nibs are dropped from both
// directions before ID extraction.
//
// This is the single gating decision point for --no-mentions and
// --active across every show output mode. A future mode (TUI, stream)
// picks up the same semantics by calling this helper.
func computeMentionIDs(b *nib.Nib, reader graph.NibReader, activeOnly, includeMentions bool) (outbound, inbound []string) {
	if !includeMentions {
		return []string{}, []string{}
	}
	return mentionIDs(b, reader, activeOnly), mentionedByIDs(b, reader, activeOnly)
}

// mentionIDs returns the IDs of nibs that this nib's body mentions.
// When activeOnly is true, completed/scrapped targets are filtered out
// (same resolved-status convention used by computeBlockingIDs).
//
// Convention: lightweight read-only relationship rendering in `show` goes
// through graph.NibReader (Core-direct). Filterable relationship listing
// (e.g. `nibs links <id> --rel mentions-out --status todo`) goes through
// the GraphQL resolver path (`resolver.Nib().Mentions(ctx, b, filter)`).
// See cmd/links.go for the resolver-path pattern.
//
// The ID-extraction step is delegated to graph.MentionIDList so this layer
// and the MentionIds/MentionedByIds GraphQL resolvers share a single
// implementation — if one ever gains a filtering step (e.g. dropping
// archived mentions), the other picks it up for free.
func mentionIDs(b *nib.Nib, reader graph.NibReader, activeOnly bool) []string {
	found := reader.FindMentions(b.ID)
	if activeOnly {
		found = filterOutResolvedNibs(found)
	}
	return graph.MentionIDList(found)
}

// mentionedByIDs returns the IDs of nibs whose bodies mention this nib.
// When activeOnly is true, completed/scrapped mentioners are filtered out.
// See the convention note on mentionIDs — this uses the Core-direct path
// (graph.NibReader.FindMentionedBy) rather than the resolver, and shares
// the graph.MentionIDList extraction helper with the resolver siblings.
func mentionedByIDs(b *nib.Nib, reader graph.NibReader, activeOnly bool) []string {
	found := reader.FindMentionedBy(b.ID)
	if activeOnly {
		found = filterOutResolvedNibs(found)
	}
	return graph.MentionIDList(found)
}

// filterOutResolvedNibs drops completed/scrapped nibs (IsResolvedStatus) from
// the slice. Used by mentionIDs / mentionedByIDs when --active is set so the
// mention sections apply the same resolved-filtering already used by
// computeBlockingIDs. Named after what it removes (like filterResolvedBlockers
// in cmd/root.go) rather than what it keeps, so grep for "Resolved" surfaces
// both sibling helpers.
func filterOutResolvedNibs(nibs []*nib.Nib) []*nib.Nib {
	result := make([]*nib.Nib, 0, len(nibs))
	for _, n := range nibs {
		if !nib.IsResolvedStatus(n.Status) {
			result = append(result, n)
		}
	}
	return result
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

	// Display type and priority using the effective (default-applied) values, so a
	// nib whose file omits `type:`/`priority:` shows "task"/"normal" as it did when
	// loadNib synthesized them (the stored Nib now keeps them empty — see
	// nib.DefaultType). EffectiveType/EffectivePriority never return "", so both are
	// always shown.
	effectiveType := b.EffectiveType()
	typeCfg := cfg.GetType(effectiveType)
	typeColor := "gray"
	if typeCfg != nil {
		typeColor = typeCfg.Color
	}
	header.WriteString(" ")
	header.WriteString(ui.RenderTypeWithColor(effectiveType, typeColor))

	effectivePriority := b.EffectivePriority()
	priorityCfg := cfg.GetPriority(effectivePriority)
	priorityColor := "gray"
	if priorityCfg != nil {
		priorityColor = priorityCfg.Color
	}
	header.WriteString(" ")
	header.WriteString(ui.RenderPriorityWithColor(effectivePriority, priorityColor))
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
	showCmd.Flags().BoolVar(&showActive, "active", false,
		"Exclude completed/scrapped nibs from mention sections")
	showCmd.Flags().BoolVar(&showNoMentions, "no-mentions", false,
		"Skip the mention scan entirely; mention sections become empty")
	showCmd.Flags().IntVar(&showBodyChars, "body-chars", 0,
		"Truncate body preview at N runes (appends '…'); 0 disables")
	showCmd.Flags().BoolVar(&showSummary, "summary", false,
		"Shorthand for --body-chars 300; surveys many nibs cheaply")
	showCmd.MarkFlagsMutuallyExclusive("json", "raw", "body-only", "etag-only")
	showCmd.MarkFlagsMutuallyExclusive("body-chars", "summary")
	rootCmd.AddCommand(showCmd)
}
