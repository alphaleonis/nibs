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
	"github.com/spf13/pflag"
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
	bodyClear      bool
)

var bodyCmd = &cobra.Command{
	Use:   "body <id>",
	Short: "Edit a nib's body content",
	Long: `Edits an existing nib's body. Content is never passed inline; it always
comes from the input channel — "-" for stdin or "@FILE" for a file — so
multi-line Markdown never has to ride on a shell argument.

Operations (choose one):
  --set <chan>                 Replace the entire body.
  --append <chan>              Append a block to the body (its block carries its own heading).
  --replace-old T --replace-new U  Replace exactly one occurrence of T with U.
  --section "## H" --set <chan>     Replace the content under an EXISTING heading "## H".
  --section "## H" --append <chan>  Add to the end of "## H", after its subsections.
  --section "## H" --clear          Empty "## H", deleting its subsections; keeps the heading.

--section targets an existing heading and errors if it is absent; add --create
to --section --set or --section --append to create the heading in place (--set
then upserts), or use --append without --section, whose block carries its own
heading. --section --append separates the new content from the section's
existing content with a blank line.

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

// bodyOperation is the one edit a `nibs body` call makes.
type bodyOperation int

const (
	bodyOpSet bodyOperation = iota + 1
	bodyOpAppend
	bodyOpReplace
	bodyOpClear
)

// sectionRule is whether an operation takes --section.
type sectionRule int

const (
	sectionOptional sectionRule = iota
	sectionRequired
	sectionForbidden
)

// bodyOperationRule describes one operation: the flag that selects it, whether
// it takes --section, and whether --section --create applies to it.
type bodyOperationRule struct {
	flag      string
	operation bodyOperation
	section   sectionRule
	creates   bool
}

// bodyOperationRules lists every body operation. classifyBodyOperation derives
// its operation checks and messages from these rows.
var bodyOperationRules = []bodyOperationRule{
	{flag: "set", operation: bodyOpSet, section: sectionOptional, creates: true},
	{flag: "append", operation: bodyOpAppend, section: sectionOptional, creates: true},
	{flag: "replace-old", operation: bodyOpReplace, section: sectionForbidden},
	{flag: "clear", operation: bodyOpClear, section: sectionRequired},
}

// bodyRequest is a classified `nibs body` call.
type bodyRequest struct {
	operation bodyOperation
	// section is whether --section targets one heading.
	section bool
	// create is whether --create creates that heading when it is absent.
	create bool
}

// classifyBodyOperation returns the one operation the given flags request. It
// returns an error naming the problem when the flags request no operation, more
// than one, or a modifier the operation does not take. The caller reports that
// error as a validation error (exit 2).
func classifyBodyOperation(flags *pflag.FlagSet) (bodyRequest, error) {
	if flags.Changed("replace-old") != flags.Changed("replace-new") {
		return bodyRequest{}, errors.New("--replace-old and --replace-new must be used together")
	}

	var chosen []bodyOperationRule
	for _, rule := range bodyOperationRules {
		if bodyFlagGiven(flags, rule.flag) {
			chosen = append(chosen, rule)
		}
	}
	section := bodyFlagGiven(flags, "section")
	create := bodyFlagGiven(flags, "create")

	if len(chosen) > 1 {
		return bodyRequest{}, fmt.Errorf("choose one body operation; got %s",
			joinRuleFlags(chosen, "and", func(bodyOperationRule) bool { return true }))
	}
	if len(chosen) == 0 {
		takesSection := func(r bodyOperationRule) bool { return r.section != sectionForbidden }
		if section {
			return bodyRequest{}, fmt.Errorf("--section needs an operation: %s", joinRuleFlags(bodyOperationRules, "or", takesSection))
		}
		return bodyRequest{}, fmt.Errorf("no body operation specified (use %s, or --section with %s)",
			joinRuleFlags(bodyOperationRules, "or", func(r bodyOperationRule) bool { return r.section != sectionRequired }),
			joinRuleFlags(bodyOperationRules, "or", takesSection))
	}

	rule := chosen[0]
	switch {
	case section && rule.section == sectionForbidden:
		return bodyRequest{}, fmt.Errorf("--section does not combine with --%s", rule.flag)
	case !section && rule.section == sectionRequired:
		return bodyRequest{}, fmt.Errorf("--%s requires --section to name the heading", rule.flag)
	case create && (!section || !rule.creates):
		return bodyRequest{}, fmt.Errorf("--create is only valid with --section and %s",
			joinRuleFlags(bodyOperationRules, "or", func(r bodyOperationRule) bool { return r.creates }))
	}
	return bodyRequest{operation: rule.operation, section: section, create: create}, nil
}

// bodyRuleFor returns the row of bodyOperationRules for operation, and whether
// one exists.
func bodyRuleFor(operation bodyOperation) (bodyOperationRule, bool) {
	for _, rule := range bodyOperationRules {
		if rule.operation == operation {
			return rule, true
		}
	}
	return bodyOperationRule{}, false
}

// bodyFlagGiven returns whether the named flag requests its behavior. A bool flag
// counts only when true, so "--create=false" means the same as no --create.
func bodyFlagGiven(flags *pflag.FlagSet, name string) bool {
	f := flags.Lookup(name)
	if f.Value.Type() == "bool" {
		return f.Value.String() == "true"
	}
	return f.Changed
}

// unhandledBodyOperationError reports an operation that a per-operation switch
// has no case for. It is a defect in nibs, not in the caller's request.
type unhandledBodyOperationError struct {
	operation bodyOperation
	in        string
}

func (e *unhandledBodyOperationError) Error() string {
	return fmt.Sprintf("internal error: %s has no case for body operation %d", e.in, e.operation)
}

// joinRuleFlags returns the flags of the rules that keep accepts, spelled with
// their "--" prefix and joined by commas and a final conjunction.
func joinRuleFlags(rules []bodyOperationRule, conjunction string, keep func(bodyOperationRule) bool) string {
	var names []string
	for _, r := range rules {
		if keep(r) {
			names = append(names, "--"+r.flag)
		}
	}
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " " + conjunction + " " + names[len(names)-1]
}

func runBody(cmd *cobra.Command, args []string) error {
	app := getApp(cmd)
	ctx := context.Background()
	resolver := app.newResolver()

	req, err := classifyBodyOperation(cmd.Flags())
	if err != nil {
		return cmdError(bodyJSON, output.ErrValidation, "%s", err)
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

	// Snapshot the body before UpdateNib (below) reassigns b; the shadow-warning
	// check reads this original for its pre-write EXACT other-level lookup.
	// buildBodyInput itself never mutates b.Body — it only reads it to build a
	// separate newBody — so this capture is a safety margin against future reorder.
	originalBody := b.Body

	input, sectionAppended, err := buildBodyInput(b, req)
	var unhandled *unhandledBodyOperationError
	if errors.As(err, &unhandled) {
		return cmdError(bodyJSON, output.ErrUncategorized, "%s", err)
	}
	if err != nil {
		// inputError maps a failed read to FILE_ERROR and any other error to VALIDATION.
		return inputError(bodyJSON, err)
	}

	// Compute the shadowing-append warning against the ORIGINAL body, before the
	// write reassigns b. Its append-vs-replace input is the write's own signal
	// (sectionAppended), not a re-derivation. It rides alongside the card echo below
	// (never blocking).
	warning := sectionShadowWarning(originalBody, req, sectionAppended)

	if bodyIfMatch != "" {
		input.IfMatch = &bodyIfMatch
	}

	b, err = resolver.Mutation().UpdateNib(ctx, b.ID, input)
	if err != nil {
		return bodyMutationError(bodyJSON, err)
	}

	// Lean card echo — the same projection path `nibs get` uses (no body/etag).
	// A non-blocking warning rides alongside: text mode writes it to stderr, --json
	// mode adds a sibling "warnings" array, mirroring `nibs new`'s convention.
	card, _ := projection.ViewFields(string(projection.ViewCard))
	return echoCardWithWarning(cmd, bodyJSON, b, resolver.ProjectionResolver(ctx), card, warning)
}

// buildBodyInput resolves the classified body operation into an UpdateNibInput.
//
// The returned bool is sectionAppended: true only when a --section --create
// request created its heading, because only a created heading can be the
// written-but-shadowed heading that sectionShadowWarning reports.
func buildBodyInput(b *nib.Nib, req bodyRequest) (model.UpdateNibInput, bool, error) {
	var input model.UpdateNibInput

	if req.section {
		return buildSectionInput(b, req)
	}

	switch req.operation {
	case bodyOpSet:
		// Replace the whole body verbatim.
		content, err := resolveBodyFlag(bodySet, "")
		if err != nil {
			return input, false, err
		}
		input.Body = &content

	case bodyOpAppend:
		appendText, err := resolveAppendFlag(bodyAppend)
		if err != nil {
			return input, false, err
		}
		input.BodyMod = &model.BodyModification{Append: &appendText}

	case bodyOpReplace:
		// Surgical exact-once replace. The exactly-once check (and its
		// TEXT_NOT_FOUND / TEXT_AMBIGUOUS outcomes) lives in the graph/nib layer.
		input.BodyMod = &model.BodyModification{
			Replace: []*model.ReplaceOperation{{Old: bodyReplaceOld, New: bodyReplaceNew}},
		}

	default:
		return input, false, &unhandledBodyOperationError{operation: req.operation, in: "buildBodyInput"}
	}

	return input, false, nil
}

// buildSectionInput applies a --section operation to the one heading the flag
// names. The heading matches as sectionHeadingText and sectionMatchLevel derive
// it, the same derivation sectionShadowWarning reads.
//
// Without --create, a heading absent at the match level is a sectionNotFoundError
// and nothing is written. mdsection.Replace would otherwise return the body
// unchanged and report success.
func buildSectionInput(b *nib.Nib, req bodyRequest) (model.UpdateNibInput, bool, error) {
	var input model.UpdateNibInput
	heading := sectionHeadingText(bodySection)
	matchLevel := sectionMatchLevel(bodySection)
	appendLevel := sectionHeadingLevel(bodySection)

	var content string
	switch req.operation {
	case bodyOpSet:
		text, err := resolveBodyFlag(bodySet, "")
		if err != nil {
			return input, false, err
		}
		content = strings.TrimRight(text, "\n")
	case bodyOpAppend:
		text, err := resolveAppendFlag(bodyAppend)
		if err != nil {
			return input, false, err
		}
		content = text
	case bodyOpClear:
		content = ""
	default:
		return input, false, &unhandledBodyOperationError{operation: req.operation, in: "buildSectionInput"}
	}

	if !req.create {
		if _, found := mdsection.Find(b.Body, heading, matchLevel); !found {
			// FindExact, so a lone parenthetical "H (…)" does not count as a
			// heading at another level: an exact heading created by --create would
			// win the wildcard read over it, so the ordinary --create advice holds.
			_, otherLevel := mdsection.FindExact(b.Body, heading, mdsection.AnyLevel)
			return input, false, &sectionNotFoundError{heading: bodySection, operation: req.operation, otherLevelExists: otherLevel}
		}
	}

	// Without req.create the heading exists here, so created is true only for a create.
	var newBody string
	var created bool
	switch req.operation {
	case bodyOpAppend:
		newBody, created = mdsection.AppendAtLevel(b.Body, matchLevel, appendLevel, heading, content)
	case bodyOpSet, bodyOpClear:
		newBody, created = mdsection.SetAtLevel(b.Body, matchLevel, appendLevel, heading, content)
	default:
		return input, false, &unhandledBodyOperationError{operation: req.operation, in: "buildSectionInput"}
	}
	input.Body = &newBody
	return input, created, nil
}

// sectionHeadingText derives the heading TEXT a --section flag targets: the value
// with its "#" markers stripped. Surrounding whitespace is trimmed BEFORE the "#"
// are stripped so a leading space (" ## H") cannot defeat TrimLeft and leave the
// markers in the text. This is the single source of truth for the derivation so
// the write (buildBodyInput) and the shadow-warning check (sectionShadowWarning)
// cannot desync.
func sectionHeadingText(flag string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(flag), "#"))
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
// body at the requested match level. It is a usage/validation error (not an I/O
// failure), so inputError maps it to VALIDATION (exit 2). Its advice depends on
// the operation, so it never suggests a flag that operation cannot take.
//
// otherLevelExists is true when a same-text heading DOES exist at a different
// level than the spelled one. The message then teaches spelling that level
// before --create, which would add a duplicate that wildcard readers may shadow
// (see sectionShadowWarning).
type sectionNotFoundError struct {
	heading          string
	operation        bodyOperation
	otherLevelExists bool
}

func (e *sectionNotFoundError) Error() string {
	rule, known := bodyRuleFor(e.operation)
	if !known {
		return fmt.Sprintf("section %q not found", e.heading)
	}
	if e.otherLevelExists {
		if !rule.creates {
			return fmt.Sprintf("no %q section — a same-named heading exists at another level; spell that level to target it", e.heading)
		}
		return fmt.Sprintf("no %q section — a same-named heading exists at another level; spell that level to target it, or add --create to add a new %q", e.heading, e.heading)
	}
	switch e.operation {
	case bodyOpSet:
		return fmt.Sprintf("section %q not found — --section --set replaces an existing section; use --append (its block carries the heading) to create one, or add --create to create it in place", e.heading)
	case bodyOpAppend:
		return fmt.Sprintf("section %q not found — --section --append adds to an existing section; add --create to create it", e.heading)
	case bodyOpClear:
		return fmt.Sprintf("section %q not found — --section --clear empties an existing section, so there is nothing to clear", e.heading)
	default:
		return fmt.Sprintf("section %q not found", e.heading)
	}
}

// sectionShadowWarning returns a non-blocking warning when a `--section --create`
// request, with --set or --append, APPENDED a new heading at the spelled level
// while an EXACT same-text heading already exists at a DIFFERENT level. The
// tool's own wildcard readers — decision extraction (`nibs context`, via
// nibcontext.ExtractDecisions) and the parent-nib "Key Decisions" merge (`nibs
// close`) — call mdsection.Find at AnyLevel and surface only the FIRST matching
// heading, so the appended section would be written-but-shadowed. Two same-text
// headings at different levels are legitimate Markdown, so this warns rather than
// blocks.
//
// The append-vs-replace decision is the write's own: sectionAppended is the bool
// mdsection.SetAtLevel or mdsection.AppendAtLevel returned. A write into an
// existing section creates nothing and so can never shadow; only a created
// heading can. A second Find here would agree with the write only while both used
// the same matchLevel.
//
// The other-level check uses FindExact (exact only): the appended heading is
// EXACT, and an exact heading WINS a wildcard read over a parenthetical one — so a
// lone "H (…)" is NOT a shadow. Warn iff sectionAppended && an EXACT same-text
// heading exists; in the append branch no match existed at the spelled level, so
// any exact heading found is necessarily at a different level (a genuine shadow).
//
// Returns "" when the write replaced in place (sectionAppended == false), when no
// EXACT same-text heading exists at another level, or when this is not a
// `--section --create` request. A bare --section ("X", matchLevel AnyLevel)
// replaces an existing heading at any level rather than appending, so it never
// trips this.
func sectionShadowWarning(originalBody string, req bodyRequest, sectionAppended bool) string {
	if !req.section || !req.create || !sectionAppended {
		return ""
	}
	heading := sectionHeadingText(bodySection)
	if _, exact := mdsection.FindExact(originalBody, heading, mdsection.AnyLevel); !exact {
		return ""
	}
	return fmt.Sprintf("warning: created a new %q section, but a same-named heading already exists at another level; wildcard readers — decision extraction (`nibs context`) and the parent-nib Key Decisions merge (`nibs close`) — may surface only the first. Spell the existing level to target it instead.", bodySection)
}

// echoCardWithWarning renders the body-mutation card and surfaces a non-blocking
// warning alongside it, mirroring echoCardWithDuplicates' channel discipline
// (`nibs new`): text mode prints the card to stdout and the warning to stderr;
// --json mode adds a sibling "warnings" array to the {nib} contract (the nib
// object is unchanged) so the stdout echo stays valid JSON. An empty warning
// makes this exactly echoCard.
func echoCardWithWarning(cmd *cobra.Command, jsonMode bool, b *nib.Nib, r projection.Resolver, sel projection.Selection, warning string) error {
	if warning == "" {
		return echoCard(jsonMode, b, r, sel)
	}
	if jsonMode {
		p, err := projection.Project(b, sel, r)
		if err != nil {
			return cmdError(true, output.ErrValidation, "failed to project nib: %v", err)
		}
		return output.JSONRaw(map[string]any{
			"nib":      p,
			"warnings": []string{warning},
		})
	}
	if err := echoCard(false, b, r, sel); err != nil {
		return err
	}
	// Writes to stderr; a write error here is not actionable and must not derail
	// the (already successful) mutation, so the result is explicitly ignored.
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), warning)
	return nil
}

// textMatchError reports a surgical replace that did not match exactly once as
// TEXT_NOT_FOUND (zero matches) or TEXT_AMBIGUOUS (more than one), enriched with
// the number of times the search text was found, so an agent can pick its next
// move — different text for a zero-match, more surrounding context for an
// ambiguous one — without scraping the message: in --json the envelope carries
// occurrences. It reports ok=false for every other error, leaving the caller's
// own classification in charge.
//
// This is the ONE place a text-match envelope is built. Every CLI surface that
// builds a --json error envelope reaches it: `nibs body --replace-old` through
// bodyMutationError, and `nibs query`'s batched bodyMod.replace through its
// coded boundary (cmd/graphql.go). One surface deliberately does not:
// cmd/serve.go's presenter stamps only the codes it enumerates, so a
// bodyMod.replace refusal reaches a web client uncoded and without a count. The
// TUI is not a second exception — it writes no bodyMod at all.
//
// A count of 0 is emitted, not omitted, which is what output.ErrorText's *int
// buys: a bare int would be dropped by the field's omitempty, and an absent
// occurrences already means something else — that no single count speaks for the
// response, the batched case cmd/graphql.go withholds it for.
//
// The 0-vs-everything-else split matches mutationErrCode's, so one refusal
// classifies the same whichever surface raises it. Neither splits on N>1
// specifically because a count of 1 cannot be constructed — nib.ReplaceOnce
// raises the error only for a count that is not 1.
//
// err is rendered as-is rather than the ReplaceMatchError's own message, so
// whatever context the caller's text carries survives. Two cases need it: the
// "replacement N failed" prefix nib.ApplyBodyMod adds, which names WHICH
// operation of a multi-replace modification was refused, and the transport
// prefix `nibs query` puts on every failure.
//
// PRECONDITION: err must represent exactly ONE refused replace — the caller owns
// establishing that, exactly as for hierarchyError and etagConflictError.
// errors.As stops at the first *ReplaceMatchError it reaches, so a failure
// covering several would hand back one count as though it answered all of them.
// The direct command satisfies this trivially (one mutation, one error, and
// ApplyBodyMod returns at its first refused replacement); `nibs query`
// establishes it through formatGraphQLErrors, which sets Err only for a response
// with a single classified failure of the response's own class.
func textMatchError(jsonOutput bool, err error) (error, bool) {
	var matchErr *nib.ReplaceMatchError
	if !errors.As(err, &matchErr) {
		return nil, false
	}
	code := output.ErrTextAmbiguous
	if matchErr.Count == 0 {
		code = output.ErrTextNotFound
	}
	if jsonOutput {
		return output.ErrorText(code, err.Error(), matchErr.Count), true
	}
	return &output.CodedError{Code: code, Msg: err.Error()}, true
}

// bodyMutationError maps a body-mutation error to a CLI error. A surgical
// replace that did not match exactly once goes to textMatchError, the shared
// classifier `nibs query` reaches for the same failure. Everything else (etag
// conflicts, corrupt-file, generic validation) delegates to set's shared
// mapping.
func bodyMutationError(jsonOutput bool, err error) error {
	if textMatch, ok := textMatchError(jsonOutput, err); ok {
		return textMatch
	}
	return setMutationError(jsonOutput, err)
}

func init() {
	bodyCmd.Flags().StringVar(&bodySet, "set", "", "Replace the body from the input channel: '-' for stdin or '@FILE' for a file (no inline text)")
	bodyCmd.Flags().StringVar(&bodyAppend, "append", "", "Append a block from the input channel: '-' for stdin or '@FILE' for a file (no inline text)")
	bodyCmd.Flags().StringVar(&bodySection, "section", "", "Heading that --set, --append or --clear acts on (e.g. \"## Notes\"); matches exact text first, else a \"<name> (…)\" suffix; a spelled level matches only that level, a bare heading matches any level")
	bodyCmd.Flags().BoolVar(&bodyCreate, "create", false, "Create the --section heading if absent; default errors on a missing heading")
	bodyCmd.Flags().BoolVar(&bodyClear, "clear", false, "With --section, empty the section, subsections included, and keep its heading")
	bodyCmd.Flags().StringVar(&bodyReplaceOld, "replace-old", "", "Text to find and replace exactly once (requires --replace-new)")
	bodyCmd.Flags().StringVar(&bodyReplaceNew, "replace-new", "", "Replacement text (requires --replace-old)")
	bodyCmd.Flags().StringVar(&bodyIfMatch, "if-match", "", "Only update if etag matches (optimistic locking)")
	bodyCmd.Flags().BoolVar(&bodyJSON, "json", false, "Output as JSON")

	rootCmd.AddCommand(bodyCmd)
}
