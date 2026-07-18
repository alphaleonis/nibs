package cmd

import (
	"context"
	"errors"
	"strings"

	"github.com/alphaleonis/nibs/internal/bodytemplate"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibtypes"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/projection"
	"github.com/spf13/cobra"
)

var (
	newStatus    string
	newType      string
	newPriority  string
	newBody      string
	newBodyFile  string
	newTag       []string
	newParent    string
	newBlocking  []string
	newBlockedBy []string
	newDocument  []string
	newEstimate  string
	newPrefix    string
	newAfter     string
	newBefore    string
	newFirst     bool
	newJSON      bool
	newNoEdit    bool
)

var newCmd = &cobra.Command{
	Use:     "new [title]",
	Aliases: []string{"create", "c"},
	Short:   "Create a new nib",
	Long: `Creates a new nib (issue) with a generated ID and optional title.

When no body is supplied on an interactive terminal, $EDITOR (or $VISUAL) opens
to author the body from the type's template. The editor is skipped — the template
is used as-is — with --no-edit, with --json, or when stdin/stdout is not a terminal
(pipe, redirect, or agent/subagent shell), keeping --json output clean and parseable.`,
	// At most one positional: the optional title. Zero args is legal (the title
	// defaults to "Untitled"), so this is MaximumNArgs(1), not ExactArgs(1).
	// Extra args used to be silently folded into the title via strings.Join;
	// rejecting them keeps the documented `nibs new "<title>"` contract explicit.
	Args: codedMaximumNArgs(&newJSON, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		title := strings.Join(args, " ")
		if title == "" {
			title = "Untitled"
		}

		// Validate inputs
		if newStatus != "" && !app.Config().IsValidStatus(newStatus) {
			return cmdError(newJSON, output.ErrInvalidStatus, "invalid status: %s (must be %s)", newStatus, app.Config().StatusList())
		}
		if newType != "" && !app.Config().IsValidType(newType) {
			return cmdError(newJSON, output.ErrValidation, "invalid type: %s (must be %s)", newType, app.Config().TypeList())
		}
		if newPriority != "" && !app.Config().IsValidPriority(newPriority) {
			return cmdError(newJSON, output.ErrValidation, "invalid priority: %s (must be %s)", newPriority, app.Config().PriorityList())
		}
		if newEstimate != "" && !app.Config().IsValidEstimate(newEstimate) {
			return cmdError(newJSON, output.ErrValidation, "invalid estimate: %s (must be %s)", newEstimate, app.Config().EstimateList())
		}

		body, err := resolveBodyFlag(newBody, newBodyFile)
		if err != nil {
			return inputError(newJSON, err)
		}

		// Resolve the effective type before template lookup
		effectiveType := newType
		if effectiveType == "" {
			effectiveType = app.Config().GetDefaultType()
		}

		// Use template body when no body provided and type has a template
		if body == "" {
			tmplBody := bodytemplate.BodyTemplate(effectiveType)
			if shouldOpenEditor(tmplBody) {
				edited, editErr := editContent(tmplBody)
				if editErr != nil {
					return cmdError(newJSON, output.ErrFileError, "editor failed: %v", editErr)
				}
				body = edited
			} else {
				body = tmplBody
			}
		}

		// Build GraphQL input
		input := model.CreateNibInput{Title: title}
		if newStatus != "" {
			input.Status = &newStatus
		} else {
			defaultStatus := app.Config().GetDefaultStatus()
			input.Status = &defaultStatus
		}
		input.Type = &effectiveType
		if newPriority != "" {
			input.Priority = &newPriority
		}
		if newEstimate != "" {
			input.Estimate = &newEstimate
		}
		if body != "" {
			input.Body = &body
		}
		if len(newTag) > 0 {
			input.Tags = newTag
		}

		// Add parent
		if newParent != "" {
			input.Parent = &newParent
		}

		// Add blocking
		if len(newBlocking) > 0 {
			input.Blocking = newBlocking
		}

		// Add blocked_by
		if len(newBlockedBy) > 0 {
			input.BlockedBy = newBlockedBy
		}

		// Add documents
		if len(newDocument) > 0 {
			input.Documents = newDocument
		}

		// Add custom prefix
		if newPrefix != "" {
			input.Prefix = &newPrefix
		}

		// Add positioning
		if newAfter != "" {
			input.AfterID = &newAfter
		}
		if newBefore != "" {
			input.BeforeID = &newBefore
		}
		if newFirst {
			input.First = &newFirst
		}

		// Create via GraphQL mutation
		resolver := app.newResolver()
		b, err := resolver.Mutation().CreateNib(context.Background(), input)
		if err != nil {
			// An illegal parent type (e.g. a task under a milestone) is surfaced as
			// a structured HIERARCHY error carrying the allowed parent types, not a
			// generic file error. The rule itself lives in internal/nibtypes.
			var he *nibtypes.HierarchyError
			if errors.As(err, &he) {
				if newJSON {
					return output.ErrorHierarchy(he.Error(), he.Allowed)
				}
				return cmdError(false, output.ErrHierarchy, "%s", he.Error())
			}
			return cmdError(newJSON, output.ErrFileError, "failed to create nib: %v", err)
		}

		// Echo the created nib as a lean card — the same projection + rendering
		// path `nibs get` uses (no body/etag unless explicitly asked). Card is a
		// compile-time-valid view, so ViewFields never errors here.
		card, _ := projection.ViewFields(string(projection.ViewCard))
		return echoCard(newJSON, b, resolver.ProjectionResolver(context.Background()), card)
	},
}

// shouldOpenEditor decides whether `nibs new` (given no body) should launch
// $EDITOR to author the template body interactively. It stays out of the editor
// for machine or non-interactive invocations: --json (output must remain clean and
// parseable), --no-edit (explicit opt-out), or any context without a controlling
// terminal on both stdin and stdout (agent Bash tool, subagent, pipe/redirect).
// In a non-tty context the editor cannot open /dev/tty; launching it there errors
// and its stderr noise corrupts --json capture, which previously produced a
// duplicate nib on the parse-failure retry.
func shouldOpenEditor(tmplBody string) bool {
	if tmplBody == "" || newJSON || newNoEdit {
		return false
	}
	if !isInteractiveTerminal() {
		return false
	}
	// editContent independently no-ops when no editor is configured (it early-returns
	// the input unchanged before any side effect), so this final check is a readability
	// short-circuit, not the sole enforcement. The two together are defense-in-depth for
	// spawning an external process — which is why mutating this line alone changes no
	// observable behavior and no behavioral test can isolate it.
	return getEditor() != ""
}

// echoCard renders a freshly-created nib as a card: the {nib} JSON contract in
// --json mode, or the "key: value" text card otherwise. Both reuse get's
// projection rendering so the echo matches `nibs get`.
func echoCard(jsonMode bool, b *nib.Nib, r projection.Resolver, sel projection.Selection) error {
	if jsonMode {
		return renderGetJSON([]*nib.Nib{b}, sel, r)
	}
	p, err := projection.Project(b, sel, r)
	if err != nil {
		return cmdError(false, output.ErrValidation, "failed to project nib: %v", err)
	}
	printProjectedText(p)
	return nil
}

func init() {
	// Build help text with allowed values from hardcoded config
	statusNames := make([]string, len(config.DefaultStatuses))
	for i, s := range config.DefaultStatuses {
		statusNames[i] = s.Name
	}
	typeNames := make([]string, len(config.DefaultTypes))
	for i, t := range config.DefaultTypes {
		typeNames[i] = t.Name
	}
	priorityNames := make([]string, len(config.DefaultPriorities))
	for i, p := range config.DefaultPriorities {
		priorityNames[i] = p.Name
	}

	newCmd.Flags().StringVarP(&newStatus, "status", "s", "", "Initial status ("+strings.Join(statusNames, ", ")+")")
	newCmd.Flags().StringVarP(&newType, "type", "t", "", "Nib type ("+strings.Join(typeNames, ", ")+")")
	newCmd.Flags().StringVarP(&newPriority, "priority", "p", "", "Priority level ("+strings.Join(priorityNames, ", ")+")")
	estimateNames := make([]string, len(config.DefaultEstimates))
	for i, e := range config.DefaultEstimates {
		estimateNames[i] = e.Name
	}
	newCmd.Flags().StringVarP(&newEstimate, "estimate", "e", "", "Estimate size ("+strings.Join(estimateNames, ", ")+")")
	newCmd.Flags().StringVarP(&newBody, "body", "d", "", "Body input channel: '-' for stdin or '@FILE' for a file (no inline text)")
	newCmd.Flags().StringVar(&newBodyFile, "body-file", "", "Read body from file")
	newCmd.Flags().StringArrayVar(&newTag, "tag", nil, "Add tag (can be repeated)")
	newCmd.Flags().StringVar(&newParent, "parent", "", "Parent nib ID")
	newCmd.Flags().StringArrayVar(&newBlocking, "blocking", nil, "ID of nib this blocks (can be repeated)")
	newCmd.Flags().StringArrayVar(&newBlockedBy, "blocked-by", nil, "ID of nib that blocks this one (can be repeated)")
	newCmd.Flags().StringArrayVar(&newDocument, "document", nil, "Linked document path (can be repeated)")
	newCmd.Flags().StringVar(&newPrefix, "prefix", "", "Custom ID prefix (overrides config prefix)")
	newCmd.Flags().StringVar(&newAfter, "after", "", "Insert after this sibling nib ID")
	newCmd.Flags().StringVar(&newBefore, "before", "", "Insert before this sibling nib ID")
	newCmd.Flags().BoolVar(&newFirst, "first", false, "Insert before all siblings")
	newCmd.Flags().BoolVar(&newJSON, "json", false, "Output as JSON (implies --no-edit)")
	newCmd.Flags().BoolVar(&newNoEdit, "no-edit", false, "Never open $EDITOR; use the template body as-is")
	newCmd.MarkFlagsMutuallyExclusive("body", "body-file")
	newCmd.MarkFlagsMutuallyExclusive("after", "before", "first")
	rootCmd.AddCommand(newCmd)
}
