package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/nibtypes"
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
	setParent          string
	setMilestone       string
	setArea            string
	setBlocking        []string
	setRemoveBlocking  []string
	setBlockedBy       []string
	setRemoveBlockedBy []string
	setTag             []string
	setRemoveTag       []string
	setDocument        []string
	setRemoveDocument  []string
	setIfMatch         string
	setJSON            bool
)

// clearableFields are the field names --clear accepts. These are exactly the
// fields the graph layer can clear via an explicit-null update (an Omittable set
// to a nil inner pointer): the priority/estimate scalars, the two links that
// place a nib — parent (decomposition) and milestone (scheduling) — and the area
// that says who owns it.
var clearableFields = []string{"priority", "estimate", "parent", "milestone", "area"}

var setCmd = &cobra.Command{
	Use:     "set <id>",
	Aliases: []string{"update", "u"},
	Short:   "Set a nib's metadata, links, or clear fields",
	// The clearable set is interpolated, not listed: this paragraph and the
	// --clear flag's own usage string print on the same --help screen, and a
	// hand-listed set there contradicted the derived one below it.
	Long: `Sets one or more properties of an existing nib: metadata (status, type,
priority, estimate, title), links (parent, milestone, blocking, blocked-by,
tags, documents), or clears a clearable field
(--clear ` + strings.Join(clearableFields, "|") + `).

--milestone <id> assigns the nib to a milestone's queue (appended last; the
target must be a milestone, and a nib and one of its ancestors are never both
assigned). --clear milestone unassigns it and drops its queue position. Use
'nibs mv <id> --queue' to reposition within the queue.

--area <path> assigns the nib to one of the areas the store's config declares
(a nested path is spelled 'web/dashboard'); an undeclared path is refused
naming the declared set. --clear area unassigns it.`,
	Args: codedExactArgs(&setJSON, 1),
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
		input, fieldChanges, err := buildSetInput(cmd, app.Config(), b.ID)
		if err != nil {
			return inputError(setJSON, err)
		}
		changes = append(changes, fieldChanges...)

		// Add ifMatch to input if provided
		if ifMatch != nil {
			input.IfMatch = ifMatch
		}

		// The writes that can put a NEW pair into a milestone queue are linted
		// for order-vs-dependency inversions (decision 2.3): an assignment
		// (the subject enters a queue) and a new dependency edge in either
		// spelling (a pair already in one queue gains its blocking half). The
		// lint is a before/after comparison so it fires once, at the creating
		// write — a reassignment to the same queue re-reports nothing — which
		// is why the snapshot is taken here, ahead of the mutation. A clear
		// and the --remove-* forms can only take pairs away; a status change
		// is not linted either — reopening a released blocker re-arms a pair
		// whose order and dependency both pre-existed.
		lintQueue := cmd.Flags().Changed("milestone") || len(setBlockedBy) > 0 || len(setBlocking) > 0
		var inversionsBefore map[inversionKey]bool
		if lintQueue {
			inversionsBefore = queueInversionKeys(resolver.Reader, b.ID)
		}

		// Apply all field/link/clear updates atomically via a single UpdateNib
		// mutation.
		if hasFieldUpdates(input) {
			b, err = resolver.Mutation().UpdateNib(ctx, b.ID, input)
			if err != nil {
				return setMutationError(setJSON, err)
			}
		}

		// Require at least one change
		if len(changes) == 0 {
			return cmdError(setJSON, output.ErrValidation,
				"no changes specified (use --status, --type, --priority, --estimate, --title, --parent, --milestone, --area, --blocking, --blocked-by, --tag, --document, --clear, or their --remove-* variants; use `nibs mv` to reposition or reparent)")
		}

		warning := ""
		if lintQueue {
			warning = queueInversionWarning(resolver.Reader, b.ID, inversionsBefore)
		}

		// Echo the updated nib as a lean card — the same projection + rendering
		// path `nibs get` uses (no body/etag unless explicitly asked). Card is a
		// compile-time-valid view, so ViewFields never errors here.
		card, _ := projection.ViewFields(string(projection.ViewCard))
		return echoCardWithWarning(cmd, setJSON, b, resolver.ProjectionResolver(ctx), card, warning)
	},
}

// buildSetInput constructs the GraphQL input from flags and returns which fields
// changed. Clearing a clearable field (--clear) sets its Omittable to an
// explicit null (a nil inner pointer), which the UpdateNib resolver reads as
// "clear this field" — distinct from setting a value.
//
// id is the nib being updated. It appears only in the closed-status refusal
// below, to put this nib into the `close` line the message quotes.
func buildSetInput(cmd *cobra.Command, cfg *config.Config, id string) (model.UpdateNibInput, []string, error) {
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
			// Name only the OPEN statuses, for the same reason the -s usage string
			// does: the closed ones are refused by the check below, so listing them
			// as the accepted set would answer one error with a second. The route
			// to a closed status is named instead of its members. Derived from
			// OpenStatusNames so this and the usage string stay the same set.
			return input, nil, fmt.Errorf("invalid status: %s (must be %s; a closed status goes through `nibs close --as`)",
				setStatus, strings.Join(cfg.OpenStatusNames(), ", "))
		}
		// Reaching a closed status belongs to `close`, not to `set`. Both write
		// the same field, but only `close` requires a summary — so leaving this
		// open would make the route that records no reason the shortest one, and
		// the summary requirement decorative. Derived from IsClosedStatus, so a
		// newly declared closed status is refused here without an edit.
		//
		// Two boundaries here are deliberate, not gaps to be closed:
		//
		//   - This is a rule about the `set` verb, not a model invariant. The web
		//     UI and the TUI mutate through the GraphQL resolver and never call
		//     this function, so they can still set a closed status directly — the
		//     web's status dropdown offers every status and depends on that. The
		//     same is true of `nibs graphql` and of `nibs new -s <closed status>`,
		//     which creates a nib already closed. Enforcement is deliberately not
		//     universal; what this closes is the shortcut that skips the summary
		//     on an existing nib.
		//   - Only transitions INTO a closed status are refused. There is no
		//     `reopen` command, so `nibs set <id> -s todo` on a closed nib is how
		//     work returns to the board and must keep working. That is why the
		//     condition below reads the incoming status and never the nib's
		//     current one.
		if cfg.IsClosedStatus(setStatus) {
			// One suggestion covers every nib, closed or not: `close` accepts a nib
			// that is already closed and appends another entry to its ## Summary,
			// so revising a close reason is the same single command as closing for
			// the first time.
			return input, nil, fmt.Errorf(
				"%s is a closed status; `set` cannot close a nib — use `nibs close %s --as %s --summary -` instead (closing requires a summary, `set` does not, so this route would leave no record of why)",
				setStatus, id, setStatus)
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

	// The milestone assignment is the same Omittable shape as parent: the flag
	// sets a value (an empty one clears, like --parent ""), --clear milestone
	// sets the explicit null. Target existence, type and exclusivity are judged
	// by the resolver, which owns those rules for every surface.
	if cmd.Flags().Changed("milestone") {
		input.Milestone = graphql.OmittableOf(&setMilestone)
		changes = append(changes, "milestone")
	}

	// The area assignment takes the same shape, and like the milestone it is NOT
	// pre-checked here: membership in the declared vocabulary is the resolver's
	// rule for every surface, and a copy of it here would be a second place the
	// refusal message is written. The resolver's refusal already reports
	// VALIDATION (exit 2) through mutationError's fallback, which is the class
	// the enum flags above report.
	if cmd.Flags().Changed("area") {
		input.Area = graphql.OmittableOf(&setArea)
		changes = append(changes, "area")
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
	if clears["milestone"] {
		input.Milestone = graphql.OmittableOf[*string](nil)
		changes = append(changes, "milestone")
	}
	if clears["area"] {
		input.Area = graphql.OmittableOf[*string](nil)
		changes = append(changes, "area")
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
		input.Title != nil || input.Tags != nil ||
		input.AddTags != nil || input.RemoveTags != nil ||
		input.Parent.IsSet() || input.Milestone.IsSet() || input.Area.IsSet() ||
		input.AddBlocking != nil || input.RemoveBlocking != nil ||
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
	var idExistsErr *nibcore.IDExistsError
	return errors.As(err, &mismatchErr) || errors.As(err, &requiredErr) || errors.As(err, &idExistsErr)
}

// etagConflictError reports a reconcilable ETag mismatch as CONFLICT enriched
// with the server's current etag, so an agent can retry with it: in --json the
// envelope carries currentEtag (the "409 → retry with the server etag"
// reconcile). It reports ok=false for every other error, leaving the caller's own
// classification in charge — notably an ETagRequiredError, which has no
// comparison etag and so belongs on the generic CONFLICT path.
//
// It is split out from setMutationError for the same reason mutationErrCode is:
// so the direct commands and `nibs query` enrich one conflict identically rather
// than each growing its own version. err is rendered as-is, so the caller's
// message text (a transport prefix, say) survives.
//
// PRECONDITION: err must represent exactly ONE mismatch — the caller owns
// establishing that. errors.As stops at the first *ETagMismatchError it reaches,
// so handing it a failure covering several writes would emit one token as though
// it reconciled all of them. The direct commands satisfy this trivially (one
// mutation, one error); `nibs query` establishes it through formatGraphQLErrors,
// which sets Err only for a response with a single classified failure of the
// response's own class.
func etagConflictError(jsonOutput bool, err error) (error, bool) {
	var mismatch *nibcore.ETagMismatchError
	if !errors.As(err, &mismatch) {
		return nil, false
	}
	if jsonOutput {
		return output.ErrorConflict(err.Error(), mismatch.Current), true
	}
	return &output.CodedError{Code: output.ErrConflict, Msg: err.Error()}, true
}

// hierarchyError reports an illegal parent-type refusal as HIERARCHY enriched
// with the parent types that WOULD be legal, so an agent can pick one instead of
// re-deriving the rule or scraping the message: in --json the envelope carries
// allowedParentTypes. It reports ok=false for every other error, leaving the
// caller's own classification in charge.
//
// This is the ONE place a hierarchy envelope is built. Every CLI surface that
// builds a --json error envelope reaches it: `nibs mv` and `nibs set` through
// setMutationError (which `nibs close` and `nibs body` share, though neither
// writes a parent or a type and so cannot raise this), `nibs query` through its
// coded boundary (cmd/graphql.go), and `nibs new` by calling it directly — new
// cannot share setMutationError because its fallback for an unclassified create
// failure is FILE_ERROR, not validation.
//
// Two surfaces deliberately do not: the TUI issues UpdateNib directly and puts a
// single refusal's own text, uncoded, in its footer — wrapped across as many
// lines as the width needs, up to a third of the screen — while a refused batch
// gets only a count of the nibs that failed, neither reason nor code; and
// cmd/serve.go's presenter stamps only the codes it enumerates, so a hierarchy
// refusal reaches a web client uncoded.
//
// err is rendered as-is rather than the HierarchyError's own message, so
// whatever context the caller's text carries survives. Two cases need it: the
// transport prefix `nibs query` puts on every failure, and the graph layer's own
// "type change would invalidate child <id>: …" wrapper, which names WHICH child
// a `--type` change would orphan — the HierarchyError alone knows only the
// types. The unwrapped case (a refused `--parent`, where Core.ValidateParent
// returns nibtypes.ValidateParentType's error as-is) renders identically either
// way.
//
// PRECONDITION: err must represent exactly ONE refused link — the caller owns
// establishing that, exactly as for etagConflictError. errors.As stops at the
// first *HierarchyError it reaches, so a failure covering several refusals would
// hand back one allowed set as though it answered all of them. The direct
// commands satisfy this trivially (one mutation, one error); `nibs query`
// establishes it through formatGraphQLErrors, which sets Err only for a response
// with a single classified failure of the response's own class.
func hierarchyError(jsonOutput bool, err error) (error, bool) {
	var he *nibtypes.HierarchyError
	if !errors.As(err, &he) {
		return nil, false
	}
	if jsonOutput {
		return output.ErrorHierarchy(err.Error(), he.Allowed), true
	}
	return &output.CodedError{Code: output.ErrHierarchy, Msg: err.Error()}, true
}

// setMutationError maps a mutation error to a CLI error. It mirrors
// mutationError but enriches the two failures that carry a repair hint: an
// illegal parent link with the parent types that would be legal, and a
// reconcilable ETag mismatch with the server's current etag.
//
// The order of the two is inert — *nibtypes.HierarchyError implements no Unwrap
// and neither ETag type wraps one, so no error can satisfy both.
func setMutationError(jsonOutput bool, err error) error {
	if hierarchy, ok := hierarchyError(jsonOutput, err); ok {
		return hierarchy
	}
	if conflict, ok := etagConflictError(jsonOutput, err); ok {
		return conflict
	}
	return mutationError(jsonOutput, err)
}

// mutationErrCode maps a mutation failure onto the CLI's structured error codes,
// reporting ok=false for the failures that carry no class of their own (the
// caller supplies the VALIDATION_ERROR fallback). It is split out from
// mutationError so the direct commands and `nibs query` classify one mutation
// failure identically — the query path reaches it through graphQLErrCode.
//
//   - An on-disk file that cannot be certified (corrupt/unreadable) is a
//     FILE_ERROR, not a retryable CONFLICT: the file must be repaired by hand —
//     retrying cannot resolve it. The error message already spells this out;
//     stopping (non-zero exit) is the correct behavior per the AI-agent
//     "stop on error" contract.
//   - A reconcilable ETag conflict is CONFLICT, the "409 → re-read and retry"
//     class.
//   - An illegal parent link — a child type under a parent type the hierarchy
//     rule forbids — is HIERARCHY. It shares exit 2 with the VALIDATION_ERROR
//     fallback, so what the code buys is not the exit but the allowedParentTypes
//     repair hint the envelope carries with it (see hierarchyError), and the
//     ability to tell a rule violation from a malformed argument. Sharing an
//     exit is safe for a batched `nibs query` response because
//     graphQLResponseCode compares exit statuses, not code strings.
//   - An assignment axis a nib's type refuses — a milestone given a `milestone:`
//     or an `area:` — is VALIDATION_ERROR, the class it already reports on every
//     surface that supplies the fallback. Naming it here is what makes `nibs new`
//     agree with them: create's own fallback is FILE_ERROR, which would send an
//     agent to inspect the filesystem for a bad argument pair. Recognized through
//     the concrete type rather than the message, which is the whole reason
//     nibtypes.AxisError is typed.
//   - A surgical body-replace whose search text did not match exactly once is
//     TEXT_NOT_FOUND (zero matches) or TEXT_AMBIGUOUS (more than one). Both share
//     exit 2 with the VALIDATION_ERROR fallback, so as with HIERARCHY what the
//     codes buy is not the exit but the occurrences count the envelope carries
//     with them (see textMatchError) and the ability to tell "your text was not
//     there" from "your text was ambiguous" — two refusals with different
//     repairs. `nibs body --replace-old` and the batched bodyMod.replace a
//     `nibs query` runs raise the same failure, so both report the same code.
//   - A mutation whose SUBJECT id no nib answers to is NOT_FOUND (exit 3), the
//     class every id-resolving command already reports. `nibs set`, `close` and
//     `body` resolve the id up front and reach here with it only in the delete
//     race between that check and their write; `nibs mv` has no such pre-check,
//     so GetForUpdate's bare nib.ErrNotFound arrives here on any unknown id and
//     would otherwise ride the caller's VALIDATION_ERROR fallback — exit 2, a
//     claim that the caller's INPUT was malformed rather than that the id does
//     not exist. Recognized through the sentinel rather than a concrete type,
//     which is the same channel cmd/serve.go's presenter keys on, so the CLI and
//     the HTTP server classify one missing nib the same way.
//   - A write whose target FILE is not there is a FILE_ERROR, joining the
//     unparseable case above under "the filesystem is not what the store
//     believes". Core.Update replaces a nib's file rather than creating one, so a
//     path that went stale under a loaded process — `nibs config set-prefix`
//     renames every file in the store — refuses with fs.ErrNotExist instead of
//     leaving a second copy of the nib at the retired name. The repair is to
//     re-read the store (after a set-prefix the nib's own id has changed too),
//     which is the opposite of what the VALIDATION_ERROR fallback's "bad input"
//     would send an agent to do.
//
// A SECONDARY id — a parent, a blocking or blocked-by target, a bulk-reorder
// member or anchor — does NOT normally arrive as a sentinel: the graph layer
// refuses an unknown one with a non-wrapping fmt.Errorf formatting the id as %s
// (internal/graph/resolver.go, schema.resolvers.go, bulkreorder.go), so nothing
// errors.Is can see survives and it stays validation-class. The exception is the
// same delete race as above — updateTargetClone re-fetches a blocking target
// after its existence check and wraps that miss with %w — which is exactly the
// case worth calling NOT_FOUND. So "unknown id" splits across two exit statuses
// on that line, and it splits the SAME way for `nibs query` and for the direct
// commands, which is what parity requires.
//
// The two sentinel tests run LAST so a concrete-typed failure that also carries
// a not-found cause keeps its own class. That ordering is load-bearing for
// OnDiskUnparseableError, the one classified type here with an Unwrap; it is
// inert today because both of its construction sites carry an OS read error or a
// YAML parse error. ETagMismatchError, ETagRequiredError, HierarchyError and
// ReplaceMatchError implement no Unwrap at all, so none of the conflict, the
// hierarchy and the text-match branches can be claimed by the sentinel either
// way, and their order among the concrete-type tests is inert.
//
// Between the two sentinels the id-miss goes first: nib.ErrNotFound and
// fs.ErrNotExist are unrelated values, so a chain carrying both is asserting two
// things at once, and "no nib answers to this id" is the one an agent can act
// on. Testing the file miss second means it claims only a chain with no id-miss
// in it.
func mutationErrCode(err error) (string, bool) {
	var unparseableErr *nibcore.OnDiskUnparseableError
	if errors.As(err, &unparseableErr) {
		return output.ErrFileError, true
	}
	if isConflictError(err) {
		return output.ErrConflict, true
	}
	var hierarchyErr *nibtypes.HierarchyError
	if errors.As(err, &hierarchyErr) {
		return output.ErrHierarchy, true
	}
	var axisErr *nibtypes.AxisError
	if errors.As(err, &axisErr) {
		return output.ErrValidation, true
	}
	var matchErr *nib.ReplaceMatchError
	if errors.As(err, &matchErr) {
		if matchErr.Count == 0 {
			return output.ErrTextNotFound, true
		}
		return output.ErrTextAmbiguous, true
	}
	if errors.Is(err, nib.ErrNotFound) {
		return output.ErrNotFound, true
	}
	if errors.Is(err, fs.ErrNotExist) {
		return output.ErrFileError, true
	}
	return "", false
}

// mutationError returns a cmdError with the appropriate error code based on the error type.
func mutationError(jsonOutput bool, err error) error {
	if code, ok := mutationErrCode(err); ok {
		return cmdError(jsonOutput, code, "%s", err)
	}
	return cmdError(jsonOutput, output.ErrValidation, "%s", err)
}

func init() {
	// Build help text with allowed values from hardcoded config. -s lists only
	// the OPEN statuses: the closed ones are refused above, so advertising them
	// here would send an agent straight into that error. The names come from
	// OpenStatusNames rather than a literal list, so the two stay the same set.
	// This is interpolated at package load; the refusal itself re-derives per
	// invocation, and that check is what admits or rejects a value.
	statusNames := config.Default().OpenStatusNames()
	typeNames := make([]string, len(config.DefaultTypes))
	for i, t := range config.DefaultTypes {
		typeNames[i] = t.Name
	}
	priorityNames := make([]string, len(config.DefaultPriorities))
	for i, p := range config.DefaultPriorities {
		priorityNames[i] = p.Name
	}

	// No backticks in this usage string: pflag reads a backticked word as the
	// flag's value placeholder, which would rename the arg in --help.
	setCmd.Flags().StringVarP(&setStatus, "status", "s", "",
		"New status ("+strings.Join(statusNames, ", ")+"; a closed status goes through 'nibs close --as')")
	setCmd.Flags().StringVarP(&setType, "type", "t", "", "New type ("+strings.Join(typeNames, ", ")+")")
	setCmd.Flags().StringVarP(&setPriority, "priority", "p", "", "New priority ("+strings.Join(priorityNames, ", ")+"; use --clear priority to clear)")
	estimateNames := make([]string, len(config.DefaultEstimates))
	for i, e := range config.DefaultEstimates {
		estimateNames[i] = e.Name
	}
	setCmd.Flags().StringVarP(&setEstimate, "estimate", "e", "", "New estimate ("+strings.Join(estimateNames, ", ")+"; use --clear estimate to clear)")
	setCmd.Flags().StringVar(&setTitle, "title", "", "New title")
	setCmd.Flags().StringArrayVar(&setClear, "clear", nil, "Clear a field to its default ("+strings.Join(clearableFields, ", ")+"; can be repeated)")
	setCmd.Flags().StringVar(&setParent, "parent", "", "Set parent nib ID (use --clear parent to remove)")
	setCmd.Flags().StringVar(&setMilestone, "milestone", "", "Assign to this milestone's queue, appended last (use --clear milestone to unassign)")
	// No allowed set in the usage string: areas are declared per store, and this
	// text is built once at package load from no store at all.
	setCmd.Flags().StringVar(&setArea, "area", "", "Assign to a declared area path, e.g. web/dashboard (use --clear area to unassign)")
	setCmd.Flags().StringArrayVar(&setBlocking, "blocking", nil, "ID of nib this blocks (can be repeated)")
	setCmd.Flags().StringArrayVar(&setRemoveBlocking, "remove-blocking", nil, "ID of nib to unblock (can be repeated)")
	setCmd.Flags().StringArrayVar(&setBlockedBy, "blocked-by", nil, "ID of nib that blocks this one (can be repeated)")
	setCmd.Flags().StringArrayVar(&setRemoveBlockedBy, "remove-blocked-by", nil, "ID of blocker nib to remove (can be repeated)")
	setCmd.Flags().StringArrayVar(&setTag, "tag", nil, "Add tag (can be repeated)")
	setCmd.Flags().StringArrayVar(&setRemoveTag, "remove-tag", nil, "Remove tag (can be repeated)")
	setCmd.Flags().StringArrayVar(&setDocument, "document", nil, "Add document path (can be repeated)")
	setCmd.Flags().StringArrayVar(&setRemoveDocument, "remove-document", nil, "Remove document path (can be repeated)")
	setCmd.Flags().StringVar(&setIfMatch, "if-match", "", "Only update if etag matches (optimistic locking)")
	setCmd.Flags().BoolVar(&setJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(setCmd)
}
