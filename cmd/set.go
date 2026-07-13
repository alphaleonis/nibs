package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/projection"
	"github.com/spf13/cobra"
)

var (
	setStatus          string
	setType            string
	setPriority        string
	setEstimate        string
	setTitle           string
	setClear           []string
	setBody            string
	setBodyFile        string
	setBodyReplaceOld  []string
	setBodyReplaceNew  []string
	setBodyAppend      string
	setParent          string
	setBlocking        []string
	setRemoveBlocking  []string
	setBlockedBy       []string
	setRemoveBlockedBy []string
	setTag             []string
	setRemoveTag       []string
	setDocument        []string
	setRemoveDocument  []string
	setAfter           string
	setBefore          string
	setFirst           bool
	setIfMatch         string
	setJSON            bool
)

// clearableFields are the field names --clear accepts. These are exactly the
// fields the graph layer can clear via an explicit-null update (an Omittable set
// to a nil inner pointer): the priority/estimate scalars and the parent link.
var clearableFields = []string{"priority", "estimate", "parent"}

var setCmd = &cobra.Command{
	Use:     "set <id>",
	Aliases: []string{"update", "u"},
	Short:   "Set a nib's metadata, links, or clear fields",
	Long: `Sets one or more properties of an existing nib: metadata (status, type,
priority, estimate, title), links (parent, blocking, blocked-by, tags,
documents), or clears a clearable field (--clear priority|estimate|parent).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		ctx := context.Background()
		resolver := app.newResolver()

		// Find the nib
		b, err := resolver.Query().Nib(ctx, args[0])
		if err != nil {
			return cmdError(setJSON, output.ErrNotFound, "failed to find nib: %v", err)
		}

		// If not found, check the archive and unarchive if present
		if b == nil {
			unarchived, unarchiveErr := app.Core.LoadAndUnarchive(args[0])
			if unarchiveErr != nil {
				return cmdError(setJSON, output.ErrNotFound, "nib not found: %s", args[0])
			}
			// Re-query to get the model.Nib
			b, err = resolver.Query().Nib(ctx, unarchived.ID)
			if err != nil || b == nil {
				return cmdError(setJSON, output.ErrNotFound, "nib not found: %s", args[0])
			}
		}

		// Track changes so a no-op invocation is a usage error.
		var changes []string

		// Prepare ifMatch for GraphQL mutations
		var ifMatch *string
		if setIfMatch != "" {
			ifMatch = &setIfMatch
		}

		// Build and validate field updates. inputError maps a body input-channel
		// I/O failure to FILE_ERROR (exit 5) while validation/usage errors stay
		// VALIDATION (exit 2).
		input, fieldChanges, err := buildSetInput(cmd, app.Config())
		if err != nil {
			return inputError(setJSON, err)
		}
		changes = append(changes, fieldChanges...)

		// Add ifMatch to input if provided
		if ifMatch != nil {
			input.IfMatch = ifMatch
		}

		// Apply all field/link/clear updates atomically via a single UpdateNib
		// mutation.
		if hasFieldUpdates(input) {
			b, err = resolver.Mutation().UpdateNib(ctx, b.ID, input)
			if err != nil {
				return setMutationError(setJSON, err)
			}
			// Refresh ifMatch to the new etag so subsequent mutations
			// (e.g. ReorderNib) don't fail with an etag mismatch
			if ifMatch != nil {
				newETag := b.ETag()
				ifMatch = &newETag
			}
		}

		// Handle positioning flags (--after, --before, --first). Positioning stays
		// on set for now; it moves to `nibs mv` in a later feature.
		hasPosition := setAfter != "" || setBefore != "" || setFirst
		if hasPosition {
			var afterID, beforeID *string
			var first *bool

			if setAfter != "" {
				afterID = &setAfter
			}
			if setBefore != "" {
				beforeID = &setBefore
			}
			if setFirst {
				f := true
				first = &f
			}

			b, err = resolver.Mutation().ReorderNib(ctx, b.ID, afterID, beforeID, first, nil, ifMatch)
			if err != nil {
				return setMutationError(setJSON, err)
			}
			changes = append(changes, "position")
		}

		// Require at least one change
		if len(changes) == 0 {
			return cmdError(setJSON, output.ErrValidation,
				"no changes specified (use --status, --type, --priority, --estimate, --title, --parent, --blocking, --blocked-by, --tag, --document, --clear, --after, --before, --first, or their --remove-* variants)")
		}

		// Echo the updated nib as a lean card — the same projection + rendering
		// path `nibs get` uses (no body/etag unless explicitly asked). Card is a
		// compile-time-valid view, so ViewFields never errors here.
		card, _ := projection.ViewFields(string(projection.ViewCard))
		return echoCard(setJSON, b, resolver.ProjectionResolver(ctx), card)
	},
}

// buildSetInput constructs the GraphQL input from flags and returns which fields
// changed. Clearing a clearable field (--clear) sets its Omittable to an
// explicit null (a nil inner pointer), which the UpdateNib resolver reads as
// "clear this field" — distinct from setting a value.
func buildSetInput(cmd *cobra.Command, cfg *config.Config) (model.UpdateNibInput, []string, error) {
	var input model.UpdateNibInput
	var changes []string

	// Resolve which clearable fields were requested and reject setting and
	// clearing the same field in one invocation (that would be a silent
	// last-writer-wins) before doing any other work.
	clears, err := parseClearFields(setClear)
	if err != nil {
		return input, nil, err
	}
	for _, field := range clearableFields {
		if clears[field] && cmd.Flags().Changed(field) {
			return input, nil, fmt.Errorf("cannot both set and --clear %s", field)
		}
	}

	if cmd.Flags().Changed("status") {
		if !cfg.IsValidStatus(setStatus) {
			return input, nil, fmt.Errorf("invalid status: %s (must be %s)", setStatus, cfg.StatusList())
		}
		input.Status = &setStatus
		changes = append(changes, "status")
	}

	if cmd.Flags().Changed("type") {
		if !cfg.IsValidType(setType) {
			return input, nil, fmt.Errorf("invalid type: %s (must be %s)", setType, cfg.TypeList())
		}
		input.Type = &setType
		changes = append(changes, "type")
	}

	if cmd.Flags().Changed("priority") {
		if !cfg.IsValidPriority(setPriority) {
			return input, nil, fmt.Errorf("invalid priority: %s (must be %s)", setPriority, cfg.PriorityList())
		}
		input.Priority = graphql.OmittableOf(&setPriority)
		changes = append(changes, "priority")
	}

	if cmd.Flags().Changed("estimate") {
		if !cfg.IsValidEstimate(setEstimate) {
			return input, nil, fmt.Errorf("invalid estimate: %s (must be %s)", setEstimate, cfg.EstimateList())
		}
		input.Estimate = graphql.OmittableOf(&setEstimate)
		changes = append(changes, "estimate")
	}

	if cmd.Flags().Changed("title") {
		input.Title = &setTitle
		changes = append(changes, "title")
	}

	// Handle body modifications. These stay on set for now; they move to
	// `nibs body` in a later feature.
	if cmd.Flags().Changed("body") || cmd.Flags().Changed("body-file") {
		// Full body replacement via the input channel ("-"/"@FILE") or --body-file
		body, err := resolveBodyFlag(setBody, setBodyFile)
		if err != nil {
			return input, nil, err
		}
		input.Body = &body
		changes = append(changes, "body")
	} else if cmd.Flags().Changed("body-replace-old") || cmd.Flags().Changed("body-append") {
		// Body modifications via bodyMod
		bodyMod := &model.BodyModification{}

		if cmd.Flags().Changed("body-replace-old") {
			// --body-replace-old requires --body-replace-new (enforced by MarkFlagsRequiredTogether)
			if len(setBodyReplaceOld) > 1 || len(setBodyReplaceNew) > 1 {
				return input, nil, fmt.Errorf("--body-replace-old/--body-replace-new cannot be specified multiple times; use GraphQL for multiple replacements")
			}
			bodyMod.Replace = []*model.ReplaceOperation{
				{
					Old: setBodyReplaceOld[0],
					New: setBodyReplaceNew[0],
				},
			}
		}

		if cmd.Flags().Changed("body-append") {
			appendText, err := resolveAppendFlag(setBodyAppend)
			if err != nil {
				return input, nil, err
			}
			bodyMod.Append = &appendText
		}

		input.BodyMod = bodyMod
		changes = append(changes, "body")
	}

	// Handle tags using granular add/remove (consistent with relationships)
	if len(setTag) > 0 {
		input.AddTags = setTag
		changes = append(changes, "tags")
	}
	if len(setRemoveTag) > 0 {
		input.RemoveTags = setRemoveTag
		changes = append(changes, "tags")
	}

	// Handle parent relationship. Parent is Omittable[*string]: --parent sets it
	// to a value, --clear parent sets it to an explicit null (nil inner pointer),
	// which the resolver treats as clear-to-root. Only touch the field when a
	// parent flag was given so it stays omitted (unchanged) otherwise.
	if cmd.Flags().Changed("parent") {
		input.Parent = graphql.OmittableOf(&setParent)
		changes = append(changes, "parent")
	}

	// Apply --clear (explicit-null) for the requested clearable fields.
	if clears["priority"] {
		input.Priority = graphql.OmittableOf[*string](nil)
		changes = append(changes, "priority")
	}
	if clears["estimate"] {
		input.Estimate = graphql.OmittableOf[*string](nil)
		changes = append(changes, "estimate")
	}
	if clears["parent"] {
		input.Parent = graphql.OmittableOf[*string](nil)
		changes = append(changes, "parent")
	}

	// Handle blocking relationships
	if len(setBlocking) > 0 {
		input.AddBlocking = setBlocking
		changes = append(changes, "blocking")
	}
	if len(setRemoveBlocking) > 0 {
		input.RemoveBlocking = setRemoveBlocking
		changes = append(changes, "blocking")
	}

	// Handle blocked-by relationships
	if len(setBlockedBy) > 0 {
		input.AddBlockedBy = setBlockedBy
		changes = append(changes, "blocked-by")
	}
	if len(setRemoveBlockedBy) > 0 {
		input.RemoveBlockedBy = setRemoveBlockedBy
		changes = append(changes, "blocked-by")
	}
	if len(setDocument) > 0 {
		input.AddDocuments = setDocument
		changes = append(changes, "documents")
	}
	if len(setRemoveDocument) > 0 {
		input.RemoveDocuments = setRemoveDocument
		changes = append(changes, "documents")
	}

	return input, changes, nil
}

// parseClearFields validates the --clear field names and returns a set of the
// requested fields. An unknown field name is a usage error naming the allowed
// set so an agent can correct it structurally.
func parseClearFields(fields []string) (map[string]bool, error) {
	clears := make(map[string]bool, len(fields))
	for _, f := range fields {
		valid := false
		for _, c := range clearableFields {
			if f == c {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("invalid --clear field: %s (must be %s)", f, strings.Join(clearableFields, ", "))
		}
		clears[f] = true
	}
	return clears, nil
}

// hasFieldUpdates returns true if any field in the input is set.
func hasFieldUpdates(input model.UpdateNibInput) bool {
	return input.Status != nil || input.Type != nil || input.Priority.IsSet() || input.Estimate.IsSet() ||
		input.Title != nil || input.Body != nil || input.BodyMod != nil || input.Tags != nil ||
		input.AddTags != nil || input.RemoveTags != nil ||
		input.Parent.IsSet() || input.AddBlocking != nil || input.RemoveBlocking != nil ||
		input.AddBlockedBy != nil || input.RemoveBlockedBy != nil ||
		input.Documents != nil || input.AddDocuments != nil || input.RemoveDocuments != nil
}

// isConflictError returns true if the error is a RECONCILABLE ETag conflict —
// one a client can resolve by re-reading the current etag and retrying. An
// OnDiskUnparseableError is deliberately NOT included: it is non-reconcilable
// (the on-disk file must be repaired), so it must not be presented as a retryable
// CONFLICT lest an agent following the "retry with the server etag" contract be
// steered into clobbering the corrupt file.
func isConflictError(err error) bool {
	var mismatchErr *nibcore.ETagMismatchError
	var requiredErr *nibcore.ETagRequiredError
	return errors.As(err, &mismatchErr) || errors.As(err, &requiredErr)
}

// setMutationError maps a mutation error to a CLI error. It mirrors
// mutationError but enriches a reconcilable ETag mismatch with the server's
// current etag so an agent can retry with it: in --json the envelope carries
// currentEtag (the "409 → retry with the server etag" reconcile). An
// ETagRequiredError has no comparison etag, so it falls through to the generic
// CONFLICT (no currentEtag).
func setMutationError(jsonOutput bool, err error) error {
	var mismatch *nibcore.ETagMismatchError
	if errors.As(err, &mismatch) {
		if jsonOutput {
			return output.ErrorConflict(err.Error(), mismatch.Current)
		}
		return &output.CodedError{Code: output.ErrConflict, Msg: err.Error()}
	}
	return mutationError(jsonOutput, err)
}

// mutationError returns a cmdError with the appropriate error code based on the error type.
func mutationError(jsonOutput bool, err error) error {
	var unparseableErr *nibcore.OnDiskUnparseableError
	if errors.As(err, &unparseableErr) {
		// The current on-disk file cannot be certified (corrupt/unreadable). This
		// is a FILE_ERROR, not a retryable CONFLICT: the file must be repaired by
		// hand — retrying cannot resolve it. The error message already spells this
		// out; stopping (non-zero exit) is the correct behavior per the AI-agent
		// "stop on error" contract.
		return cmdError(jsonOutput, output.ErrFileError, "%s", err)
	}
	if isConflictError(err) {
		return cmdError(jsonOutput, output.ErrConflict, "%s", err)
	}
	return cmdError(jsonOutput, output.ErrValidation, "%s", err)
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

	setCmd.Flags().StringVarP(&setStatus, "status", "s", "", "New status ("+strings.Join(statusNames, ", ")+")")
	setCmd.Flags().StringVarP(&setType, "type", "t", "", "New type ("+strings.Join(typeNames, ", ")+")")
	setCmd.Flags().StringVarP(&setPriority, "priority", "p", "", "New priority ("+strings.Join(priorityNames, ", ")+"; use --clear priority to clear)")
	estimateNames := make([]string, len(config.DefaultEstimates))
	for i, e := range config.DefaultEstimates {
		estimateNames[i] = e.Name
	}
	setCmd.Flags().StringVarP(&setEstimate, "estimate", "e", "", "New estimate ("+strings.Join(estimateNames, ", ")+"; use --clear estimate to clear)")
	setCmd.Flags().StringVar(&setTitle, "title", "", "New title")
	setCmd.Flags().StringArrayVar(&setClear, "clear", nil, "Clear a field to its default ("+strings.Join(clearableFields, ", ")+"; can be repeated)")
	setCmd.Flags().StringVarP(&setBody, "body", "d", "", "New body input channel: '-' for stdin or '@FILE' for a file (no inline text)")
	setCmd.Flags().StringVar(&setBodyFile, "body-file", "", "Read body from file")
	setCmd.Flags().StringArrayVar(&setBodyReplaceOld, "body-replace-old", nil, "Text to find and replace (requires --body-replace-new)")
	setCmd.Flags().StringArrayVar(&setBodyReplaceNew, "body-replace-new", nil, "Replacement text (requires --body-replace-old)")
	setCmd.Flags().StringVar(&setBodyAppend, "body-append", "", "Append to body via input channel: '-' for stdin or '@FILE' for a file (no inline text)")
	setCmd.Flags().StringVar(&setParent, "parent", "", "Set parent nib ID (use --clear parent to remove)")
	setCmd.Flags().StringArrayVar(&setBlocking, "blocking", nil, "ID of nib this blocks (can be repeated)")
	setCmd.Flags().StringArrayVar(&setRemoveBlocking, "remove-blocking", nil, "ID of nib to unblock (can be repeated)")
	setCmd.Flags().StringArrayVar(&setBlockedBy, "blocked-by", nil, "ID of nib that blocks this one (can be repeated)")
	setCmd.Flags().StringArrayVar(&setRemoveBlockedBy, "remove-blocked-by", nil, "ID of blocker nib to remove (can be repeated)")
	setCmd.Flags().StringArrayVar(&setTag, "tag", nil, "Add tag (can be repeated)")
	setCmd.Flags().StringArrayVar(&setRemoveTag, "remove-tag", nil, "Remove tag (can be repeated)")
	setCmd.Flags().StringArrayVar(&setDocument, "document", nil, "Add document path (can be repeated)")
	setCmd.Flags().StringArrayVar(&setRemoveDocument, "remove-document", nil, "Remove document path (can be repeated)")
	setCmd.Flags().StringVar(&setAfter, "after", "", "Move after this sibling nib ID")
	setCmd.Flags().StringVar(&setBefore, "before", "", "Move before this sibling nib ID")
	setCmd.Flags().BoolVar(&setFirst, "first", false, "Move to first position")
	setCmd.Flags().StringVar(&setIfMatch, "if-match", "", "Only update if etag matches (optimistic locking)")
	setCmd.MarkFlagsMutuallyExclusive("after", "before", "first")
	setCmd.Flags().BoolVar(&setJSON, "json", false, "Output as JSON")
	// body and body-file are mutually exclusive with body modifications
	setCmd.MarkFlagsMutuallyExclusive("body", "body-file", "body-replace-old")
	setCmd.MarkFlagsMutuallyExclusive("body", "body-file", "body-append")
	// body-replace-old and body-append can now be used together!
	setCmd.MarkFlagsRequiredTogether("body-replace-old", "body-replace-new")
	rootCmd.AddCommand(setCmd)
}
