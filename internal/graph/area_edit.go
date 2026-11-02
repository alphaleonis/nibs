package graph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/safetext"
)

// The area vocabulary mutations: message formatters over nibcore.Core's area
// verbs. The verb itself lives in nibcore whole — there is one lock order, and a
// surface that stepped through Core could not obey it (see nibcore.editArea).
// What stays here is argument-shape questions and the wording of every refusal.
//
// Name no filesystem path in a message worded here — these messages travel to an
// HTTP client, where an absolute path discloses the operating-system username
// and the project layout. A path can still reach a client underneath the
// wording: an IO failure's Cause, or a read failure the planner passed through
// raw. `nibs serve` scrubs the rendered message at its own boundary
// (servedErrorPresenter in cmd/serve_pathscrub.go); `nibs query` keeps it for an
// operator repairing a broken store.

// addAreaImpl declares a new area at input.Path, the FULL path of the new node.
//
// It rewrites no nib, so unlike the two below there is no cascade to sequence
// against the vocabulary write — the store's verb still owes the lock, since
// areas.yml is rewritten whole and two concurrent declarations without one lose
// a declaration each way.
func (r *mutationResolver) addAreaImpl(ctx context.Context, input model.AddAreaInput) (*model.AreaEditPayload, error) {
	// Answered before the call, for the reason updateAreaImpl gives. Unlike the
	// rename's, neither of these speaks about a node the store declares — they
	// judge the shape of the path and of the color — so no vocabulary could make
	// one of them the wrong thing to say. The planner asks them again under the
	// lock regardless.
	if err := area.ValidateNewPath(input.Path); err != nil {
		return nil, err
	}
	color := areaText(input.Color)
	if err := area.ValidateColor(color); err != nil {
		return nil, err
	}

	res, err := r.AreaWriter.AddArea(ctx, input.Path, areaText(input.Description), color)
	if err != nil {
		return nil, wordAreaAddFailure(err)
	}
	return r.areaEditResult(res), nil
}

// areaText reads an optional input field, where an omitted one and an empty one
// both declare none.
func areaText(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// updateAreaImpl edits the declared node at path — its name, its description,
// its color, or any combination. A rename cascades to every nib assigned at or
// below the node; an edit that changes no name rewrites none.
//
// It takes the store's own NodeUpdate rather than the wire input, so the
// wire-to-engine mapping stays in the generated resolver's thin body and the
// argument rules below read against the type the store actually acts on.
func (r *mutationResolver) updateAreaImpl(ctx context.Context, path string, u area.NodeUpdate) (*model.AreaEditPayload, error) {
	// Answered before the call: the arguments alone decide these, and waiting for
	// the store's write lock has no deadline but this request's own end. The
	// store refuses them again under the lock, in wording about the file.
	if u.NewName != nil {
		if err := validateAreaRenameArgument(*u.NewName); err != nil {
			return nil, err
		}
	}
	if u.Color != nil {
		if err := area.ValidateColor(*u.Color); err != nil {
			return nil, err
		}
	}

	res, err := r.AreaWriter.UpdateArea(ctx, path, u)
	if err != nil {
		return nil, wordAreaUpdateFailure(err)
	}
	return r.areaEditResult(res), nil
}

// removeAreaImpl retires the declared node at input.Path together with the
// subtree it heads, disposing of every nib assigned at or below it as the input
// says.
func (r *mutationResolver) removeAreaImpl(ctx context.Context, input model.RemoveAreaInput) (*model.AreaEditPayload, error) {
	disposition, err := areaDisposition(input)
	if err != nil {
		return nil, err
	}

	res, err := r.AreaWriter.RemoveArea(ctx, input.Path, disposition)
	if err != nil {
		return nil, wordAreaRetireFailure(err)
	}
	return r.areaEditResult(res), nil
}

// validateAreaRenameArgument refuses a new name the argument alone rules out.
func validateAreaRenameArgument(newName string) error {
	if err := area.ValidateName(newName); err != nil {
		return err
	}
	if strings.Contains(newName, area.PathSeparator) {
		return fmt.Errorf("%q contains \"/\", which separates the areas in a path. A rename keeps an area under its parent, so enter only the new name",
			area.RenderPath(newName))
	}
	return nil
}

// areaDisposition reads which disposition a retire was given. `unassign: false`
// is no disposition rather than a contradiction with moveTo: a client sends it
// for an unchecked box.
func areaDisposition(input model.RemoveAreaInput) (nibcore.AreaDisposition, error) {
	switch {
	case input.MoveTo != nil && input.Unassign != nil && *input.Unassign:
		return nibcore.AreaDisposition{}, fmt.Errorf(
			"moveTo and unassign: true ask for two different things to happen to the same nibs. Use only one of them")
	case input.MoveTo != nil:
		return nibcore.MoveAreaMembersTo(*input.MoveTo), nil
	case input.Unassign != nil && *input.Unassign:
		return nibcore.UnassignAreaMembers(), nil
	default:
		return nibcore.AreaDisposition{}, nil
	}
}

// areaEditResult is what both verbs answer with: the vocabulary this edit left
// behind, and whatever notes it owes the caller.
func (r *mutationResolver) areaEditResult(res nibcore.AreaEditResult) *model.AreaEditPayload {
	return &model.AreaEditPayload{
		Config: configResultWithAreas(r.Reader, res.Areas),
		Notes:  r.areaEditNotes(res),
	}
}

// areaEditNotes renders the notes an edit owes its caller, and sends each to the
// store's warning sink as well. The two readers are different people: the payload
// answers the API client, the sink is where the operator running `nibs serve`
// reads.
//
// There is one note today — the areas.yml this edit replaced was a SYMLINK, and
// the atomic write left a regular file in its place. Restoring that link brings
// back the pre-edit vocabulary while the nibs this edit rewrote stay as it left
// them, after a rename on a path that vocabulary does not declare.
//
// THE NOTE NAMES NO PATH AND THE WARNING DOES, per this file's rule: the note
// travels to an HTTP client, and `nibs serve` scrubs rendered ERROR messages
// rather than the data of a successful answer, so a path here would reach that
// client unscrubbed.
func (r *mutationResolver) areaEditNotes(res nibcore.AreaEditResult) []string {
	if res.StaleLinkTarget == "" {
		return nil
	}
	r.AreaWriter.Warn("this store's areas.yml was a symlink to %s and is now a regular file; %s still declares the old vocabulary, so update or remove it",
		res.StaleLinkTarget, res.StaleLinkTarget)
	return []string{"this store's areas.yml was a symlink and is now a regular file; the link's old target still declares the vocabulary as it was before this edit, so update or remove it — restoring that link would undo this edit while the nibs it rewrote stay as they are"}
}

// wordAreaAddFailure words what declaring an area refuses. What it does not word
// falls through to the refusals every area verb shares.
func wordAreaAddFailure(err error) error {
	var declared *nibcore.AreaAlreadyDeclaredError
	if errors.As(err, &declared) {
		return wordAreaRefusal(err, "cannot create area %q, because it already exists. Choose a different name",
			area.RenderPath(declared.Path))
	}
	// wordAreaEditFailure's undeclared-path wording speaks about an area to act
	// on, not a parent to create under. The remedy names no CLI command: a caller
	// of this surface has no shell.
	var parent *nibcore.AreaParentUndeclaredError
	if errors.As(err, &parent) {
		return wordAreaRefusal(err, "cannot create area %q, because area %q does not exist. Create %q first, then try again",
			area.RenderPath(parent.Path), area.RenderPath(parent.Parent), area.RenderPath(parent.Parent))
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) && ioErr.Phase == nibcore.AreaEditPhaseWrite {
		// The one write failure that strands nothing: no nib is rewritten ahead of
		// it, so there is no cascade to report and no disposition to drop on the
		// rerun. The other phases this verb reaches are worded by the shared arm,
		// and its cascade and confirm phases are unreachable — the plan carries
		// neither a rewrite nor an emptied path.
		return wordAreaRefusal(ioErr,
			"area %q was not created, because the store's areas.yml could not be written: %v. Nothing was changed. Fix the problem, then try again",
			area.RenderPath(ioErr.Path), ioErr.Cause)
	}
	return wordAreaEditFailure(err, "create")
}

func wordAreaUpdateFailure(err error) error {
	// An update given nothing to set. Its repair is the three fields a caller may
	// set, which the shared arm does not know.
	var empty *nibcore.AreaUpdateEmptyError
	if errors.As(err, &empty) {
		return wordAreaRefusal(err, "nothing to change on area %q. Give a new name (newName), a description or a color",
			area.RenderPath(empty.Path))
	}
	var unchanged *nibcore.AreaNameUnchangedError
	if errors.As(err, &unchanged) {
		return wordAreaRefusal(err, "area %q is already named %q, so the rename would change nothing",
			area.RenderPath(unchanged.Path), area.RenderPath(unchanged.Name))
	}
	var taken *nibcore.AreaNameTakenError
	if errors.As(err, &taken) {
		return wordAreaRefusal(err, "cannot rename area %q to %q, because area %q already exists. Choose a different name",
			area.RenderPath(taken.Path), area.RenderPath(taken.NewName), area.RenderPath(taken.Sibling))
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		switch ioErr.Phase {
		case nibcore.AreaEditPhaseCascade:
			return wordAreaRefusal(ioErr,
				"renaming area %q to %q stopped part way: it moved %d of the %s assigned to it or to an area beneath it, then failed: %v. The area is still named %q, and the moved nibs are saved.%s Rename it again to finish; the retry starts where this one stopped",
				area.RenderPath(ioErr.Path), area.RenderPath(ioErr.NewPath), len(ioErr.Written),
				areaNibCount(len(ioErr.Members)), ioErr.Cause, area.RenderPath(ioErr.Path),
				areaCascadeStranded(ioErr.NewPath, len(ioErr.Written)))
		case nibcore.AreaEditPhaseWrite:
			return areaUpdateWriteFailure(ioErr)
		}
	}
	return wordAreaEditFailure(err, "change")
}

func wordAreaRetireFailure(err error) error {
	var members *nibcore.AreaMembersPresentError
	if errors.As(err, &members) {
		return wordAreaRefusal(err,
			"cannot retire area %q: %s assigned to it or to an area beneath it (%s). Move them to another area (moveTo), clear their area (unassign: true), or keep the area",
			area.RenderPath(members.Path), areaNibsAre(len(members.Members)), namedMembers(members.Members))
	}
	var empty *nibcore.AreaDispositionEmptyError
	if errors.As(err, &empty) {
		return wordAreaRefusal(err, "no nib is assigned to area %q or to an area beneath it, so there are none to %s. Retire it without %s",
			area.RenderPath(empty.Path), areaDispositionAction(empty.Disposition),
			areaDispositionField(empty.Disposition))
	}
	var within *nibcore.AreaMoveTargetWithinError
	if errors.As(err, &within) {
		return wordAreaRefusal(err,
			"cannot move the nibs to %q, because retiring %q retires it too. Choose an area outside %q, or clear their area (unassign: true)",
			area.RenderPath(within.Target), area.RenderPath(within.Path), area.RenderPath(within.Path))
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		switch ioErr.Phase {
		case nibcore.AreaEditPhaseCascade:
			return wordAreaRefusal(ioErr,
				"retiring area %q stopped part way: it %s %d of the %s assigned to it or to an area beneath it, then failed: %v. Area %q still exists, and those changes are saved. Retire it again the same way to finish; the retry starts where this one stopped",
				area.RenderPath(ioErr.Path), areaDispositionVerb(ioErr.Disposition), len(ioErr.Written),
				areaNibCount(len(ioErr.Members)), ioErr.Cause, area.RenderPath(ioErr.Path))
		case nibcore.AreaEditPhaseConfirm:
			// A retire that named a disposition is past its cascade here and
			// reports that; one that named none rewrote nothing, and the shared
			// arm below words it.
			if ioErr.Disposition.Kind != nibcore.AreaDispositionNone {
				return areaRetireConfirmFailure(ioErr)
			}
		case nibcore.AreaEditPhaseWrite:
			return areaRetireWriteFailure(ioErr)
		}
	}
	return wordAreaEditFailure(err, "retire")
}

// wordAreaEditFailure words the failures both verbs share. verb names what the
// caller asked for; one refusal needs it to say what there is none of.
func wordAreaEditFailure(err error, verb string) error {
	var undeclared *nibcore.AreaUndeclaredError
	if errors.As(err, &undeclared) {
		if undeclared.Areas.IsEmpty() {
			return wordAreaRefusal(err, "this store has no areas, so there is none to %s. Create an area first",
				areaPathVerb(undeclared.Role, verb))
		}
		return wordAreaRefusal(err, "this store has no area %q%s. Its areas are %s",
			area.RenderPath(undeclared.Path), areaPathPurpose(undeclared.Role), undeclared.Areas.List())
	}
	var retired *nibcore.AreaRetiredWhileWaitingError
	if errors.As(err, &retired) {
		return areaRetiredWhileWaiting(err, retired)
	}
	var vanished *nibcore.AreaVocabularyVanishedError
	if errors.As(err, &vanished) {
		return wordAreaRefusal(err, "nothing was changed: the store's areas.yml was deleted. Restore the file before you change its areas")
	}
	var arrived *nibcore.AreaMembersArrivedError
	if errors.As(err, &arrived) {
		return wordAreaRefusal(err,
			"area %q was left as it was: %s assigned to it or to an area beneath it (%s) appeared after this edit read the store, usually from a git pull in the store's folder.%s Try again; the retry uses the store as it is now.%s",
			area.RenderPath(arrived.Path), areaNibCount(len(arrived.Members)),
			namedMembers(arrived.Members), areaCascadePersisted(len(arrived.Written)),
			areaCascadeStranded(arrived.NewPath, len(arrived.Written)))
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		switch ioErr.Phase {
		case nibcore.AreaEditPhaseLock:
			var waitEnded *nibcore.StoreLockWaitEndedError
			if errors.As(ioErr.Cause, &waitEnded) {
				return wordAreaRefusal(ioErr,
					"nothing was changed: this request ended after %s, while it waited for another edit to the store to finish. Try again",
					waitEnded.Waited.Round(time.Millisecond))
			}
			return wordAreaRefusal(ioErr,
				"nothing was changed: the store could not be locked for this edit: %v", ioErr.Cause)
		case nibcore.AreaEditPhaseLoadVocabulary:
			return wordAreaRefusal(ioErr,
				"nothing was changed: the store's areas.yml could not be read: %v", ioErr.Cause)
		case nibcore.AreaEditPhaseLoadNibs:
			return wordAreaRefusal(ioErr,
				"nothing was changed: the store's nibs could not be read: %v", ioErr.Cause)
		case nibcore.AreaEditPhaseConfirm:
			return wordAreaRefusal(ioErr,
				"area %q was left as it was: the check that no nib is still assigned to it or to an area beneath it failed: %v.%s Fix the problem, then try again.%s",
				area.RenderPath(ioErr.Path), ioErr.Cause, areaCascadePersisted(len(ioErr.Written)),
				areaCascadeStranded(ioErr.NewPath, len(ioErr.Written)))
		case nibcore.AreaEditPhaseReload:
			// Both writes landed, so there is nothing to rerun — but answering
			// with the stale vocabulary and calling the mutation a success
			// renders in a client as the edit not having happened.
			return wordAreaRefusal(ioErr,
				"the edit was saved, but the store's areas.yml could not be read back afterwards: %v. Do not repeat the edit. Until the file can be read again, the areas show as they were before it",
				ioErr.Cause)
		}
	}
	// What is left is a area.EditRefusal, already worded for a reader who
	// may not be told where the store is, or a read failure the planner passed
	// through raw.
	return err
}

func areaRetiredWhileWaiting(err error, e *nibcore.AreaRetiredWhileWaitingError) error {
	return wordAreaRefusal(err, "nothing was changed: area %q was retired or renamed by another edit. Check the current areas, then try again",
		area.RenderPath(e.Path))
}

// areaUpdateWriteFailure reports an update whose areas.yml write failed. Only
// a rename can have moved nibs before that write; an edit to the description or
// color moved none, so nothing was changed.
func areaUpdateWriteFailure(e *nibcore.AreaEditIOError) error {
	if e.NewPath == "" || len(e.Written) == 0 {
		return wordAreaRefusal(e,
			"area %q was not %s, because the store's areas.yml could not be written: %v. Nothing was changed. Fix the problem, then try again",
			area.RenderPath(e.Path), areaUpdateVerb(e.NewPath), e.Cause)
	}
	return wordAreaRefusal(e,
		"renaming area %q to %q stopped part way: it moved %s, then the store's areas.yml could not be written: %v. The area is still named %q, and the moved nibs are saved.%s Fix the problem, then rename it again to finish",
		area.RenderPath(e.Path), area.RenderPath(e.NewPath), areaNibCount(len(e.Written)), e.Cause,
		area.RenderPath(e.Path), areaCascadeStranded(e.NewPath, len(e.Written)))
}

// areaUpdateVerb is the past participle for an update that changed nothing.
func areaUpdateVerb(newPath string) string {
	if newPath == "" {
		return "updated"
	}
	return "renamed"
}

// areaRetireWriteFailure reports a retire whose members are disposed of and
// whose areas.yml write then failed. It branches on whether a disposition was
// NAMED, not on which one: with none there is nothing to report as done and no
// argument to drop.
func areaRetireWriteFailure(e *nibcore.AreaEditIOError) error {
	if e.Disposition.Kind == nibcore.AreaDispositionNone {
		return wordAreaRefusal(e,
			"area %q was not retired, because the store's areas.yml could not be written: %v. No nib is assigned to it, so nothing was changed. Fix the problem, then try again",
			area.RenderPath(e.Path), e.Cause)
	}
	return wordAreaRefusal(e,
		"retiring area %q stopped part way: it %s %s, then the store's areas.yml could not be written: %v. Area %q still exists, and those changes are saved. Fix the problem, then retire it again without %s to finish",
		area.RenderPath(e.Path), areaDispositionVerb(e.Disposition), areaNibCount(len(e.Written)), e.Cause,
		area.RenderPath(e.Path), areaDispositionField(e.Disposition))
}

// areaRetireConfirmFailure reports a retire whose disposition completed and
// whose confirming re-read then failed. It prescribes the same rerun as
// areaRetireWriteFailure but promises no outcome — a nib that arrived in that
// window refuses it.
func areaRetireConfirmFailure(e *nibcore.AreaEditIOError) error {
	return wordAreaRefusal(e,
		"retiring area %q stopped part way: it %s %s, then the check that no nib is still assigned to it failed: %v. Area %q still exists, and those changes are saved. Fix the problem, then retire it again without %s",
		area.RenderPath(e.Path), areaDispositionVerb(e.Disposition), areaNibCount(len(e.Written)), e.Cause,
		area.RenderPath(e.Path), areaDispositionField(e.Disposition))
}

// areaRefusal is one of the store's typed refusals worded for this surface.
//
// Wrap the refusal, never replace it: cmd/set.go's mutationErrCode classifies
// `nibs query`'s exit on the concrete type the store raised, and an error that
// dropped it arrives as a bare validation fallback.
type areaRefusal struct {
	msg   string
	cause error
}

func (e *areaRefusal) Error() string { return e.msg }

func (e *areaRefusal) Unwrap() error { return e.cause }

func wordAreaRefusal(cause error, format string, a ...any) error {
	return &areaRefusal{msg: fmt.Sprintf(format, a...), cause: cause}
}

// areaPathVerb names what the caller asked to do with the path a refusal is
// about.
func areaPathVerb(role nibcore.AreaPathRole, verb string) string {
	if role == nibcore.AreaPathMoveTarget {
		return "move nibs to"
	}
	return verb
}

// areaPathPurpose names what a move target was for, and nothing for the path
// the verb acts on.
func areaPathPurpose(role nibcore.AreaPathRole) string {
	if role == nibcore.AreaPathMoveTarget {
		return " to move the nibs to"
	}
	return ""
}

// areaDispositionField is the input field that asked for a disposition, for a
// message telling the caller to drop it. Only ever reached for a named one.
func areaDispositionField(d nibcore.AreaDisposition) string {
	if d.Kind == nibcore.AreaDispositionMove {
		return "moveTo"
	}
	return "unassign"
}

// areaDispositionVerb is the past tense a completed disposition reports in.
func areaDispositionVerb(d nibcore.AreaDisposition) string {
	if d.Kind == nibcore.AreaDispositionMove {
		return "moved"
	}
	return "unassigned"
}

// areaDispositionAction is the bare verb a refusal says there is nothing to do.
func areaDispositionAction(d nibcore.AreaDisposition) string {
	if d.Kind == nibcore.AreaDispositionMove {
		return "move"
	}
	return "unassign"
}

// areaMemberNameLimit is QueueNameLimit applied to an area's members.
const areaMemberNameLimit = QueueNameLimit

// namedMembers renders the member ids a refusal quotes, capped, with the number
// it elided stated. The ids are filename-derived, so they go through
// safetext.Strip.
func namedMembers(ids []string) string {
	named := ids
	if len(named) > areaMemberNameLimit {
		named = named[:areaMemberNameLimit]
	}
	rendered := make([]string, len(named))
	for i, id := range named {
		rendered[i] = safetext.Strip(id)
	}
	out := strings.Join(rendered, ", ")
	if len(ids) > len(named) {
		out += fmt.Sprintf(", and %d more", len(ids)-len(named))
	}
	return out
}

func areaNibsAre(n int) string {
	if n == 1 {
		return "1 nib is"
	}
	return fmt.Sprintf("%d nibs are", n)
}

// areaCascadePersisted names what an edit had already written, empty when the
// cascade wrote nothing. It is a whole sentence with its own leading space, so
// a format string places it straight after the previous sentence's period.
func areaCascadePersisted(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return " The nib this edit had already updated stays updated."
	}
	return fmt.Sprintf(" The %d nibs this edit had already updated stay updated.", n)
}

// areaCascadeStranded says what the persisted clause otherwise reads as
// reassurance about: a rename's cascaded members sit on the new path while the
// vocabulary still declares the old one. A retire's land on a declared path or
// on none, so it renders nothing.
func areaCascadeStranded(newPath string, written int) string {
	if newPath == "" || written == 0 {
		return ""
	}
	return " Until the rename finishes, those nibs carry an area that does not exist yet, so every change to them is refused."
}

func areaNibCount(n int) string {
	if n == 1 {
		return "1 nib"
	}
	return fmt.Sprintf("%d nibs", n)
}
