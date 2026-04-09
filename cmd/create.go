package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/alphaleonis/nibs/internal/bodytemplate"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var (
	createStatus    string
	createType      string
	createPriority  string
	createBody      string
	createBodyFile  string
	createTag       []string
	createParent    string
	createBlocking  []string
	createBlockedBy []string
	createDocument  []string
	createEstimate  string
	createPrefix    string
	createAfter     string
	createBefore    string
	createFirst     bool
	createJSON      bool
)

var createCmd = &cobra.Command{
	Use:     "create [title]",
	Aliases: []string{"c", "new"},
	Short:   "Create a new nib",
	Long:    `Creates a new nib (issue) with a generated ID and optional title.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		title := strings.Join(args, " ")
		if title == "" {
			title = "Untitled"
		}

		// Validate inputs
		if createStatus != "" && !app.Config().IsValidStatus(createStatus) {
			return cmdError(createJSON, output.ErrInvalidStatus, "invalid status: %s (must be %s)", createStatus, app.Config().StatusList())
		}
		if createType != "" && !app.Config().IsValidType(createType) {
			return cmdError(createJSON, output.ErrValidation, "invalid type: %s (must be %s)", createType, app.Config().TypeList())
		}
		if createPriority != "" && !app.Config().IsValidPriority(createPriority) {
			return cmdError(createJSON, output.ErrValidation, "invalid priority: %s (must be %s)", createPriority, app.Config().PriorityList())
		}
		if createEstimate != "" && !app.Config().IsValidEstimate(createEstimate) {
			return cmdError(createJSON, output.ErrValidation, "invalid estimate: %s (must be %s)", createEstimate, app.Config().EstimateList())
		}

		body, err := resolveContent(createBody, createBodyFile)
		if err != nil {
			return cmdError(createJSON, output.ErrFileError, "%s", err)
		}

		// Resolve the effective type before template lookup
		effectiveType := createType
		if effectiveType == "" {
			effectiveType = app.Config().GetDefaultType()
		}

		// Use template body when no body provided and type has a template
		if body == "" {
			tmplBody := bodytemplate.BodyTemplate(effectiveType)
			// Open editor if available and interactive (or EDITOR is set)
			if tmplBody != "" && getEditor() != "" {
				edited, editErr := editContent(tmplBody)
				if editErr != nil {
					return cmdError(createJSON, output.ErrFileError, "editor failed: %v", editErr)
				}
				body = edited
			} else {
				body = tmplBody
			}
		}

		// Build GraphQL input
		input := model.CreateNibInput{Title: title}
		if createStatus != "" {
			input.Status = &createStatus
		} else {
			defaultStatus := app.Config().GetDefaultStatus()
			input.Status = &defaultStatus
		}
		input.Type = &effectiveType
		if createPriority != "" {
			input.Priority = &createPriority
		}
		if createEstimate != "" {
			input.Estimate = &createEstimate
		}
		if body != "" {
			input.Body = &body
		}
		if len(createTag) > 0 {
			input.Tags = createTag
		}

		// Add parent
		if createParent != "" {
			input.Parent = &createParent
		}

		// Add blocking
		if len(createBlocking) > 0 {
			input.Blocking = createBlocking
		}

		// Add blocked_by
		if len(createBlockedBy) > 0 {
			input.BlockedBy = createBlockedBy
		}

		// Add documents
		if len(createDocument) > 0 {
			input.Documents = createDocument
		}

		// Add custom prefix
		if createPrefix != "" {
			input.Prefix = &createPrefix
		}

		// Add positioning
		if createAfter != "" {
			input.AfterID = &createAfter
		}
		if createBefore != "" {
			input.BeforeID = &createBefore
		}
		if createFirst {
			input.First = &createFirst
		}

		// Create via GraphQL mutation
		resolver := app.newResolver()
		b, err := resolver.Mutation().CreateNib(context.Background(), input)
		if err != nil {
			return cmdError(createJSON, output.ErrFileError, "failed to create nib: %v", err)
		}

		if createJSON {
			return output.Success(filterResolvedBlockersOne(b, app.Core), "Nib created")
		}

		fmt.Println(ui.Success.Render("Created ") + ui.ID.Render(b.ID) + " " + ui.Muted.Render(b.Path))
		return nil
	},
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

	createCmd.Flags().StringVarP(&createStatus, "status", "s", "", "Initial status ("+strings.Join(statusNames, ", ")+")")
	createCmd.Flags().StringVarP(&createType, "type", "t", "", "Nib type ("+strings.Join(typeNames, ", ")+")")
	createCmd.Flags().StringVarP(&createPriority, "priority", "p", "", "Priority level ("+strings.Join(priorityNames, ", ")+")")
	estimateNames := make([]string, len(config.DefaultEstimates))
	for i, e := range config.DefaultEstimates {
		estimateNames[i] = e.Name
	}
	createCmd.Flags().StringVarP(&createEstimate, "estimate", "e", "", "Estimate size ("+strings.Join(estimateNames, ", ")+")")
	createCmd.Flags().StringVarP(&createBody, "body", "d", "", "Body content (use '-' to read from stdin)")
	createCmd.Flags().StringVar(&createBodyFile, "body-file", "", "Read body from file")
	createCmd.Flags().StringArrayVar(&createTag, "tag", nil, "Add tag (can be repeated)")
	createCmd.Flags().StringVar(&createParent, "parent", "", "Parent nib ID")
	createCmd.Flags().StringArrayVar(&createBlocking, "blocking", nil, "ID of nib this blocks (can be repeated)")
	createCmd.Flags().StringArrayVar(&createBlockedBy, "blocked-by", nil, "ID of nib that blocks this one (can be repeated)")
	createCmd.Flags().StringArrayVar(&createDocument, "document", nil, "Linked document path (can be repeated)")
	createCmd.Flags().StringVar(&createPrefix, "prefix", "", "Custom ID prefix (overrides config prefix)")
	createCmd.Flags().StringVar(&createAfter, "after", "", "Insert after this sibling nib ID")
	createCmd.Flags().StringVar(&createBefore, "before", "", "Insert before this sibling nib ID")
	createCmd.Flags().BoolVar(&createFirst, "first", false, "Insert before all siblings")
	createCmd.Flags().BoolVar(&createJSON, "json", false, "Output as JSON")
	createCmd.MarkFlagsMutuallyExclusive("body", "body-file")
	createCmd.MarkFlagsMutuallyExclusive("after", "before", "first")
	rootCmd.AddCommand(createCmd)
}
