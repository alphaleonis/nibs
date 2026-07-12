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
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var (
	updateStatus          string
	updateType            string
	updatePriority        string
	updateEstimate        string
	updateTitle           string
	updateBody            string
	updateBodyFile        string
	updateBodyReplaceOld  []string
	updateBodyReplaceNew  []string
	updateBodyAppend      string
	updateParent          string
	updateRemoveParent    bool
	updateBlocking        []string
	updateRemoveBlocking  []string
	updateBlockedBy       []string
	updateRemoveBlockedBy []string
	updateTag             []string
	updateRemoveTag       []string
	updateDocument        []string
	updateRemoveDocument  []string
	updateAfter           string
	updateBefore          string
	updateFirst           bool
	updateIfMatch         string
	updateJSON            bool
)

var updateCmd = &cobra.Command{
	Use:     "update <id>",
	Aliases: []string{"u"},
	Short:   "Update a nib's properties",
	Long:    `Updates one or more properties of an existing nib.`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		ctx := context.Background()
		resolver := app.newResolver()

		// Find the nib
		b, err := resolver.Query().Nib(ctx, args[0])
		if err != nil {
			return cmdError(updateJSON, output.ErrNotFound, "failed to find nib: %v", err)
		}

		// If not found, check the archive and unarchive if present
		wasArchived := false
		if b == nil {
			unarchived, unarchiveErr := app.Core.LoadAndUnarchive(args[0])
			if unarchiveErr != nil {
				return cmdError(updateJSON, output.ErrNotFound, "nib not found: %s", args[0])
			}
			// Re-query to get the model.Nib
			b, err = resolver.Query().Nib(ctx, unarchived.ID)
			if err != nil || b == nil {
				return cmdError(updateJSON, output.ErrNotFound, "nib not found: %s", args[0])
			}
			wasArchived = true
		}

		// Track changes for output
		var changes []string

		// Prepare ifMatch for GraphQL mutations
		var ifMatch *string
		if updateIfMatch != "" {
			ifMatch = &updateIfMatch
		}

		// Build and validate field updates
		input, fieldChanges, err := buildUpdateInput(cmd, b.Tags, b.Body, app.Config())
		if err != nil {
			return cmdError(updateJSON, output.ErrValidation, "%s", err)
		}
		changes = append(changes, fieldChanges...)

		// Add ifMatch to input if provided
		if ifMatch != nil {
			input.IfMatch = ifMatch
		}

		// Apply all updates atomically via single UpdateNib mutation
		// This includes field updates, body modifications, and relationship changes
		if hasFieldUpdates(input) {
			b, err = resolver.Mutation().UpdateNib(ctx, b.ID, input)
			if err != nil {
				return mutationError(updateJSON, err)
			}
			// Refresh ifMatch to the new etag so subsequent mutations
			// (e.g. ReorderNib) don't fail with an etag mismatch
			if ifMatch != nil {
				newETag := b.ETag()
				ifMatch = &newETag
			}
		}

		// Handle positioning flags (--after, --before, --first)
		hasPosition := updateAfter != "" || updateBefore != "" || updateFirst
		if hasPosition {
			var afterID, beforeID *string
			var first *bool

			if updateAfter != "" {
				afterID = &updateAfter
			}
			if updateBefore != "" {
				beforeID = &updateBefore
			}
			if updateFirst {
				f := true
				first = &f
			}

			b, err = resolver.Mutation().ReorderNib(ctx, b.ID, afterID, beforeID, first, nil, ifMatch)
			if err != nil {
				return mutationError(updateJSON, err)
			}
			changes = append(changes, "position")
		}

		// Require at least one change
		if len(changes) == 0 {
			return cmdError(updateJSON, output.ErrValidation,
				"no changes specified (use --status, --type, --priority, --estimate, --title, --body, --parent, --blocking, --blocked-by, --tag, --after, --before, --first, or their --remove-* variants)")
		}

		// Output result
		if updateJSON {
			msg := "Nib updated"
			if wasArchived {
				msg = "Nib unarchived and updated"
			}
			return output.Success(filterResolvedBlockersOne(b, app.Core), msg)
		}

		if wasArchived {
			fmt.Println(ui.Success.Render("Unarchived and updated ") + ui.ID.Render(b.ID) + " " + ui.Muted.Render(b.Path))
		} else {
			fmt.Println(ui.Success.Render("Updated ") + ui.ID.Render(b.ID) + " " + ui.Muted.Render(b.Path))
		}
		return nil
	},
}

// buildUpdateInput constructs the GraphQL input from flags and returns which fields changed.
func buildUpdateInput(cmd *cobra.Command, existingTags []string, currentBody string, cfg *config.Config) (model.UpdateNibInput, []string, error) {
	var input model.UpdateNibInput
	var changes []string

	if cmd.Flags().Changed("status") {
		if !cfg.IsValidStatus(updateStatus) {
			return input, nil, fmt.Errorf("invalid status: %s (must be %s)", updateStatus, cfg.StatusList())
		}
		input.Status = &updateStatus
		changes = append(changes, "status")
	}

	if cmd.Flags().Changed("type") {
		if !cfg.IsValidType(updateType) {
			return input, nil, fmt.Errorf("invalid type: %s (must be %s)", updateType, cfg.TypeList())
		}
		input.Type = &updateType
		changes = append(changes, "type")
	}

	if cmd.Flags().Changed("priority") {
		if !cfg.IsValidPriority(updatePriority) {
			return input, nil, fmt.Errorf("invalid priority: %s (must be %s)", updatePriority, cfg.PriorityList())
		}
		input.Priority = graphql.OmittableOf(&updatePriority)
		changes = append(changes, "priority")
	}

	if cmd.Flags().Changed("estimate") {
		if !cfg.IsValidEstimate(updateEstimate) {
			return input, nil, fmt.Errorf("invalid estimate: %s (must be %s)", updateEstimate, cfg.EstimateList())
		}
		input.Estimate = graphql.OmittableOf(&updateEstimate)
		changes = append(changes, "estimate")
	}

	if cmd.Flags().Changed("title") {
		input.Title = &updateTitle
		changes = append(changes, "title")
	}

	// Handle body modifications
	if cmd.Flags().Changed("body") || cmd.Flags().Changed("body-file") {
		// Full body replacement
		body, err := resolveContent(updateBody, updateBodyFile)
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
			if len(updateBodyReplaceOld) > 1 || len(updateBodyReplaceNew) > 1 {
				return input, nil, fmt.Errorf("--body-replace-old/--body-replace-new cannot be specified multiple times; use GraphQL for multiple replacements")
			}
			bodyMod.Replace = []*model.ReplaceOperation{
				{
					Old: updateBodyReplaceOld[0],
					New: updateBodyReplaceNew[0],
				},
			}
		}

		if cmd.Flags().Changed("body-append") {
			appendText, err := resolveAppendContent(updateBodyAppend)
			if err != nil {
				return input, nil, err
			}
			bodyMod.Append = &appendText
		}

		input.BodyMod = bodyMod
		changes = append(changes, "body")
	}

	// Handle tags using granular add/remove (consistent with relationships)
	if len(updateTag) > 0 {
		input.AddTags = updateTag
		changes = append(changes, "tags")
	}
	if len(updateRemoveTag) > 0 {
		input.RemoveTags = updateRemoveTag
		changes = append(changes, "tags")
	}

	// Handle parent relationship. Parent is Omittable[*string]: an explicit
	// value sets it, empty string clears it. Only set when a parent flag was
	// given so the field stays omitted (unchanged) otherwise.
	if cmd.Flags().Changed("parent") {
		input.Parent = graphql.OmittableOf(&updateParent)
		changes = append(changes, "parent")
	} else if updateRemoveParent {
		emptyParent := ""
		input.Parent = graphql.OmittableOf(&emptyParent)
		changes = append(changes, "parent")
	}

	// Handle blocking relationships
	if len(updateBlocking) > 0 {
		input.AddBlocking = updateBlocking
		changes = append(changes, "blocking")
	}
	if len(updateRemoveBlocking) > 0 {
		input.RemoveBlocking = updateRemoveBlocking
		changes = append(changes, "blocking")
	}

	// Handle blocked-by relationships
	if len(updateBlockedBy) > 0 {
		input.AddBlockedBy = updateBlockedBy
		changes = append(changes, "blocked-by")
	}
	if len(updateRemoveBlockedBy) > 0 {
		input.RemoveBlockedBy = updateRemoveBlockedBy
		changes = append(changes, "blocked-by")
	}
	if len(updateDocument) > 0 {
		input.AddDocuments = updateDocument
		changes = append(changes, "documents")
	}
	if len(updateRemoveDocument) > 0 {
		input.RemoveDocuments = updateRemoveDocument
		changes = append(changes, "documents")
	}

	return input, changes, nil
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

	updateCmd.Flags().StringVarP(&updateStatus, "status", "s", "", "New status ("+strings.Join(statusNames, ", ")+")")
	updateCmd.Flags().StringVarP(&updateType, "type", "t", "", "New type ("+strings.Join(typeNames, ", ")+")")
	updateCmd.Flags().StringVarP(&updatePriority, "priority", "p", "", "New priority ("+strings.Join(priorityNames, ", ")+", or empty to clear)")
	estimateNames := make([]string, len(config.DefaultEstimates))
	for i, e := range config.DefaultEstimates {
		estimateNames[i] = e.Name
	}
	updateCmd.Flags().StringVarP(&updateEstimate, "estimate", "e", "", "New estimate ("+strings.Join(estimateNames, ", ")+", or empty to clear)")
	updateCmd.Flags().StringVar(&updateTitle, "title", "", "New title")
	updateCmd.Flags().StringVarP(&updateBody, "body", "d", "", "New body (use '-' to read from stdin)")
	updateCmd.Flags().StringVar(&updateBodyFile, "body-file", "", "Read body from file")
	updateCmd.Flags().StringArrayVar(&updateBodyReplaceOld, "body-replace-old", nil, "Text to find and replace (requires --body-replace-new)")
	updateCmd.Flags().StringArrayVar(&updateBodyReplaceNew, "body-replace-new", nil, "Replacement text (requires --body-replace-old)")
	updateCmd.Flags().StringVar(&updateBodyAppend, "body-append", "", "Text to append to body (use '-' for stdin)")
	updateCmd.Flags().StringVar(&updateParent, "parent", "", "Set parent nib ID")
	updateCmd.Flags().BoolVar(&updateRemoveParent, "remove-parent", false, "Remove parent")
	updateCmd.Flags().StringArrayVar(&updateBlocking, "blocking", nil, "ID of nib this blocks (can be repeated)")
	updateCmd.Flags().StringArrayVar(&updateRemoveBlocking, "remove-blocking", nil, "ID of nib to unblock (can be repeated)")
	updateCmd.Flags().StringArrayVar(&updateBlockedBy, "blocked-by", nil, "ID of nib that blocks this one (can be repeated)")
	updateCmd.Flags().StringArrayVar(&updateRemoveBlockedBy, "remove-blocked-by", nil, "ID of blocker nib to remove (can be repeated)")
	updateCmd.Flags().StringArrayVar(&updateTag, "tag", nil, "Add tag (can be repeated)")
	updateCmd.Flags().StringArrayVar(&updateRemoveTag, "remove-tag", nil, "Remove tag (can be repeated)")
	updateCmd.Flags().StringArrayVar(&updateDocument, "document", nil, "Add document path (can be repeated)")
	updateCmd.Flags().StringArrayVar(&updateRemoveDocument, "remove-document", nil, "Remove document path (can be repeated)")
	updateCmd.Flags().StringVar(&updateAfter, "after", "", "Move after this sibling nib ID")
	updateCmd.Flags().StringVar(&updateBefore, "before", "", "Move before this sibling nib ID")
	updateCmd.Flags().BoolVar(&updateFirst, "first", false, "Move to first position")
	updateCmd.Flags().StringVar(&updateIfMatch, "if-match", "", "Only update if etag matches (optimistic locking)")
	updateCmd.MarkFlagsMutuallyExclusive("after", "before", "first")
	updateCmd.MarkFlagsMutuallyExclusive("parent", "remove-parent")
	updateCmd.Flags().BoolVar(&updateJSON, "json", false, "Output as JSON")
	// body and body-file are mutually exclusive with body modifications
	updateCmd.MarkFlagsMutuallyExclusive("body", "body-file", "body-replace-old")
	updateCmd.MarkFlagsMutuallyExclusive("body", "body-file", "body-append")
	// body-replace-old and body-append can now be used together!
	updateCmd.MarkFlagsRequiredTogether("body-replace-old", "body-replace-new")
	rootCmd.AddCommand(updateCmd)
}
