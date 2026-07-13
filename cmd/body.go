package cmd

import (
	"context"
	"errors"
	"strings"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/mdsection"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/projection"
	"github.com/spf13/cobra"
)

var (
	bodySet        string
	bodyAppend     string
	bodySection    string
	bodyReplaceOld string
	bodyReplaceNew string
	bodyIfMatch    string
	bodyJSON       bool
)

var bodyCmd = &cobra.Command{
	Use:   "body <id>",
	Short: "Edit a nib's body content",
	Long: `Edits an existing nib's body. Content is never passed inline; it always
comes from the input channel — "-" for stdin or "@FILE" for a file — so
multi-line Markdown never has to ride on a shell argument.

Operations (choose one):
  --set <chan>                 Replace the entire body.
  --section "## H" --set <chan>  Replace only the content under heading "## H".
  --append <chan>              Append a block to the body.
  --replace-old T --replace-new U  Replace exactly one occurrence of T with U.

The surgical --replace-old/--replace-new form matches exactly once: zero
matches fail with TEXT_NOT_FOUND and more than one with TEXT_AMBIGUOUS (both
exit 2), each reporting the occurrence count.`,
	Args: cobra.ExactArgs(1),
	RunE: runBody,
}

func runBody(cmd *cobra.Command, args []string) error {
	app := getApp(cmd)
	ctx := context.Background()
	resolver := app.newResolver()

	setChanged := cmd.Flags().Changed("set")
	appendChanged := cmd.Flags().Changed("append")
	sectionChanged := cmd.Flags().Changed("section")
	replaceChanged := cmd.Flags().Changed("replace-old")

	// --section names the target heading; the replacement content still rides on
	// --set, so --section without --set has no content channel. Cobra's
	// mutual-exclusion groups already keep --section away from --append and
	// --replace-old, so the only invalid combination left to catch is bare
	// --section. Check it before the none-set case for a more specific message.
	if sectionChanged && !setChanged {
		return cmdError(bodyJSON, output.ErrValidation,
			"--section requires --set to supply the replacement content channel")
	}

	// Require exactly one primary operation. Cobra's mutual-exclusion groups
	// reject the "more than one" case; this catches "none at all".
	if !setChanged && !appendChanged && !replaceChanged {
		return cmdError(bodyJSON, output.ErrValidation,
			"no body operation specified (use --set, --append, --section with --set, or --replace-old/--replace-new)")
	}

	// Find the nib; fall back to the archive and unarchive it (mirrors set).
	b, err := resolver.Query().Nib(ctx, args[0])
	if err != nil {
		return cmdError(bodyJSON, output.ErrNotFound, "failed to find nib: %v", err)
	}
	if b == nil {
		unarchived, unarchiveErr := app.Core.LoadAndUnarchive(args[0])
		if unarchiveErr != nil {
			return cmdError(bodyJSON, output.ErrNotFound, "nib not found: %s", args[0])
		}
		b, err = resolver.Query().Nib(ctx, unarchived.ID)
		if err != nil || b == nil {
			return cmdError(bodyJSON, output.ErrNotFound, "nib not found: %s", args[0])
		}
	}

	input, err := buildBodyInput(b, setChanged, appendChanged, sectionChanged, replaceChanged)
	if err != nil {
		// buildBodyInput only returns input-channel resolution errors and section
		// validation errors; inputError maps a failed read to FILE_ERROR and a
		// usage/validation error to VALIDATION.
		return inputError(bodyJSON, err)
	}
	if bodyIfMatch != "" {
		input.IfMatch = &bodyIfMatch
	}

	b, err = resolver.Mutation().UpdateNib(ctx, b.ID, input)
	if err != nil {
		return bodyMutationError(bodyJSON, err)
	}

	// Lean card echo — the same projection path `nibs get` uses (no body/etag).
	card, _ := projection.ViewFields(string(projection.ViewCard))
	return echoCard(bodyJSON, b, resolver.ProjectionResolver(ctx), card)
}

// buildBodyInput resolves the requested body operation into an UpdateNibInput.
// Exactly one of the flag-changed booleans is true (cobra enforces mutual
// exclusion; the caller rejects the none-set case). --section combines with
// --set to target one heading's content.
func buildBodyInput(b *nib.Nib, setChanged, appendChanged, sectionChanged, replaceChanged bool) (model.UpdateNibInput, error) {
	var input model.UpdateNibInput

	switch {
	case sectionChanged:
		// Replace the content under a heading. mdsection matches on the heading
		// text without its "#" markers, so accept both "## Notes" and "Notes".
		// It is a no-op when the heading is absent, so check first and fail loudly
		// rather than silently doing nothing.
		heading := strings.TrimSpace(strings.TrimLeft(bodySection, "#"))
		content, err := resolveBodyFlag(bodySet, "")
		if err != nil {
			return input, err
		}
		if _, found := mdsection.Find(b.Body, heading); !found {
			return input, &sectionNotFoundError{heading: bodySection}
		}
		newBody := mdsection.Replace(b.Body, heading, strings.TrimRight(content, "\n"))
		input.Body = &newBody

	case setChanged:
		// Replace the whole body verbatim.
		content, err := resolveBodyFlag(bodySet, "")
		if err != nil {
			return input, err
		}
		input.Body = &content

	case appendChanged:
		appendText, err := resolveAppendFlag(bodyAppend)
		if err != nil {
			return input, err
		}
		input.BodyMod = &model.BodyModification{Append: &appendText}

	case replaceChanged:
		// Surgical exact-once replace. The exactly-once check (and its
		// TEXT_NOT_FOUND / TEXT_AMBIGUOUS outcomes) lives in the graph/nib layer.
		input.BodyMod = &model.BodyModification{
			Replace: []*model.ReplaceOperation{{Old: bodyReplaceOld, New: bodyReplaceNew}},
		}
	}

	return input, nil
}

// sectionNotFoundError signals that --section named a heading absent from the
// body. It is a usage/validation error (not an I/O failure), so inputError maps
// it to VALIDATION (exit 2).
type sectionNotFoundError struct{ heading string }

func (e *sectionNotFoundError) Error() string {
	return "section not found: " + e.heading
}

// bodyMutationError maps a body-mutation error to a CLI error. A surgical
// replace that did not match exactly once surfaces as a typed
// *nib.ReplaceMatchError: 0 occurrences → TEXT_NOT_FOUND, N>1 → TEXT_AMBIGUOUS,
// each carrying the occurrence count. Everything else (etag conflicts,
// corrupt-file, generic validation) delegates to set's shared mapping.
func bodyMutationError(jsonOutput bool, err error) error {
	var matchErr *nib.ReplaceMatchError
	if errors.As(err, &matchErr) {
		code := output.ErrTextAmbiguous
		if matchErr.Count == 0 {
			code = output.ErrTextNotFound
		}
		if jsonOutput {
			return output.ErrorText(code, err.Error(), matchErr.Count)
		}
		return &output.CodedError{Code: code, Msg: err.Error()}
	}
	return setMutationError(jsonOutput, err)
}

func init() {
	bodyCmd.Flags().StringVar(&bodySet, "set", "", "Replace the body from the input channel: '-' for stdin or '@FILE' for a file (no inline text)")
	bodyCmd.Flags().StringVar(&bodyAppend, "append", "", "Append a block from the input channel: '-' for stdin or '@FILE' for a file (no inline text)")
	bodyCmd.Flags().StringVar(&bodySection, "section", "", "Heading whose content --set replaces (e.g. \"## Notes\")")
	bodyCmd.Flags().StringVar(&bodyReplaceOld, "replace-old", "", "Text to find and replace exactly once (requires --replace-new)")
	bodyCmd.Flags().StringVar(&bodyReplaceNew, "replace-new", "", "Replacement text (requires --replace-old)")
	bodyCmd.Flags().StringVar(&bodyIfMatch, "if-match", "", "Only update if etag matches (optimistic locking)")
	bodyCmd.Flags().BoolVar(&bodyJSON, "json", false, "Output as JSON")

	// One primary body operation at a time. --section pairs only with --set.
	bodyCmd.MarkFlagsMutuallyExclusive("set", "append", "replace-old")
	bodyCmd.MarkFlagsMutuallyExclusive("section", "append", "replace-old")
	bodyCmd.MarkFlagsRequiredTogether("replace-old", "replace-new")

	rootCmd.AddCommand(bodyCmd)
}
