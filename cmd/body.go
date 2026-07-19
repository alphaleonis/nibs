package cmd

import (
	"context"
	"errors"
	"fmt"
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
	bodyCreate     bool
)

var bodyCmd = &cobra.Command{
	Use:   "body <id>",
	Short: "Edit a nib's body content",
	Long: `Edits an existing nib's body. Content is never passed inline; it always
comes from the input channel — "-" for stdin or "@FILE" for a file — so
multi-line Markdown never has to ride on a shell argument.

Operations (choose one):
  --set <chan>                 Replace the entire body.
  --section "## H" --set <chan>  Replace the content under an EXISTING heading "## H".
  --section "## H" --set <chan> --create  Upsert: replace if present, else create "## H".
  --append <chan>              Append a block to the body (its block carries its own heading).
  --replace-old T --replace-new U  Replace exactly one occurrence of T with U.

--section --set targets an existing heading and errors if it is absent; add
--create to create the heading in place (upsert), or use --append, whose block
carries its own heading. --create is valid only with --section --set.

--section matches at the level you SPELL: "### H" matches only a level-3 heading
and will NOT match (or clobber) a level-2 "## H"; a bare "H" (no #) matches a
heading at any level. When --create finds no match, the new heading is created at
the spelled level (a bare heading defaults to "##").

--section matches a heading by EXACT text first; only if no exact heading exists
does it fall back to a heading whose text is "<name> (…)" (a parenthetical
suffix), so "Key Decisions" targets an exact "## Key Decisions" over a
"## Key Decisions (Phase 1)".

The surgical --replace-old/--replace-new form matches exactly once: zero
matches fail with TEXT_NOT_FOUND and more than one with TEXT_AMBIGUOUS (both
exit 2), each reporting the occurrence count.`,
	Args: bodyArgs,
	RunE: runBody,
}

// bodyArgs validates the body command's positional arguments. Before the
// ordinary arg-count check it catches a common footgun: --set and --append are
// string flags, so a following flag token — long like "--create" or short like
// cobra's auto-registered "-h" — is silently bound as the flag's value (pflag
// consumes the next token), pushing the real input channel out as a stray
// positional. Both failing shapes reach this validator — the trailing-"-" shape
// short-circuits here (2 positionals) before RunE, and the no-trailing-"-" shape
// (1 positional) would otherwise trip an unrelated inline-prose error in RunE —
// so this single placement names the swallowed token and steers to the correct
// order for both. When neither flag swallowed a flag token, it falls through to
// the standard exactly-one-argument check.
func bodyArgs(cmd *cobra.Command, args []string) error {
	if swallowed, ok := swallowedFlagValue(bodySet); ok {
		return cmdError(bodyJSON, output.ErrValidation,
			"--set expects an input channel ('-' or '@FILE'); %q was consumed as its value — write it as `--set - %s`",
			swallowed, swallowed)
	}
	if swallowed, ok := swallowedFlagValue(bodyAppend); ok {
		return cmdError(bodyJSON, output.ErrValidation,
			"--append expects an input channel ('-' or '@FILE'); %q was consumed as its value — write it as `--append - %s`",
			swallowed, swallowed)
	}
	return codedExactArgs(&bodyJSON, 1)(cmd, args)
}

// swallowedFlagValue reports whether a string flag's value looks like a flag
// token pflag bound by mistake. A legitimate input channel is only ever "-" or
// "@FILE", so any value beginning with "-" other than the lone "-" stdin marker
// can only be a following flag the string flag ate — long ("--create") or short
// ("-h", cobra's auto --help shorthand) alike. The lone "-" is excluded so the
// valid stdin channel never false-positives.
func swallowedFlagValue(value string) (string, bool) {
	if value != "-" && strings.HasPrefix(value, "-") {
		return value, true
	}
	return "", false
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

	// --create is an upsert modifier for --section --set only; it is meaningless
	// on any other operation, so reject it loudly rather than ignore it.
	if cmd.Flags().Changed("create") && (!sectionChanged || !setChanged) {
		return cmdError(bodyJSON, output.ErrValidation,
			"--create is only valid with --section --set")
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

	input, err := buildBodyInput(b, setChanged, appendChanged, sectionChanged, replaceChanged, bodyCreate)
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
// --set to target one heading's content; create makes that pairing an upsert.
func buildBodyInput(b *nib.Nib, setChanged, appendChanged, sectionChanged, replaceChanged, create bool) (model.UpdateNibInput, error) {
	var input model.UpdateNibInput

	switch {
	case sectionChanged:
		// Replace the content under a heading. mdsection matches on the heading
		// text without its "#" markers, so accept both "## Notes" and "Notes".
		// Trim surrounding whitespace BEFORE stripping "#" so a leading space
		// (" ## H") cannot defeat TrimLeft and leave the markers in the text.
		heading := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(bodySection), "#"))
		// matchLevel: a spelled level ("### H") matches only an existing heading at
		// that exact level; a bare heading ("H") is a wildcard (0) matching any level.
		// This keeps a "### Sub" request from silently clobbering a level-2 "## Sub".
		matchLevel := sectionMatchLevel(bodySection)
		content, err := resolveBodyFlag(bodySet, "")
		if err != nil {
			return input, err
		}
		if create {
			// Upsert: replace a section matching at matchLevel if present, else append
			// a new heading at the level the flag spells (bare defaults to level 2).
			// SetAtLevel (not Set) because a spelled "### H" must gate the match to
			// its exact level; the two clearly-named level vars can't be transposed.
			newBody := mdsection.SetAtLevel(b.Body, matchLevel, sectionHeadingLevel(bodySection), heading, strings.TrimRight(content, "\n"))
			input.Body = &newBody
			break
		}
		// Strict default: --set replaces only; a heading absent at the requested
		// level is a no-op in mdsection, so check first and fail loudly rather than
		// silently doing nothing (or clobbering a same-named heading at another level).
		if _, found := mdsection.Find(b.Body, heading, matchLevel); !found {
			return input, &sectionNotFoundError{heading: bodySection}
		}
		newBody := mdsection.Replace(b.Body, heading, strings.TrimRight(content, "\n"), matchLevel)
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

// sectionHeadingLevel derives the Markdown heading level from a --section flag
// value by counting its leading '#' characters ("## H" → 2, "### H" → 3). A bare
// heading with no markers ("H") defaults to level 2, the "##" section convention
// nibs bodies use. Whitespace is trimmed first so " ## H" still reads as level 2.
// This is the APPEND level — the level of a new heading created by --create when
// no match exists — distinct from the match level (see sectionMatchLevel).
func sectionHeadingLevel(flag string) int {
	level := mdsection.HeadingLevel(strings.TrimSpace(flag))
	if level == 0 {
		return 2
	}
	return level
}

// sectionMatchLevel derives the heading level a --section flag REQUIRES an
// existing heading to match. A spelled level ("### H" → 3) matches only a heading
// at that exact level; a bare heading ("H" → 0) is a wildcard matching any level.
// Whitespace is trimmed first so " ### H" still reads as level 3. Unlike
// sectionHeadingLevel, a bare heading yields 0 (wildcard), not the append default
// of 2 — so a bare --section keeps its historic level-agnostic matching.
func sectionMatchLevel(flag string) int {
	return mdsection.HeadingLevel(strings.TrimSpace(flag))
}

// sectionNotFoundError signals that --section named a heading absent from the
// body. It is a usage/validation error (not an I/O failure), so inputError maps
// it to VALIDATION (exit 2).
type sectionNotFoundError struct{ heading string }

func (e *sectionNotFoundError) Error() string {
	return fmt.Sprintf("section %q not found — --section --set replaces an existing section; use --append (its block carries the heading) to create one, or add --create to create it in place", e.heading)
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
	bodyCmd.Flags().StringVar(&bodySection, "section", "", "Heading whose content --set replaces (e.g. \"## Notes\"); matches exact text first, else a \"<name> (…)\" suffix; a spelled level matches only that level, a bare heading matches any level")
	bodyCmd.Flags().BoolVar(&bodyCreate, "create", false, "With --section --set, create the heading if absent (upsert); default errors on a missing heading")
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
