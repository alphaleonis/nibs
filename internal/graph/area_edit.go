package graph

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/safetext"
)

// AreaEditIOError reports that an area vocabulary edit failed on the
// FILESYSTEM: the store's write lock could not be taken, the store could not be
// re-read under it, a member nib could not be rewritten, or the edited
// vocabulary could not be written back.
//
// It is separate from config.AreaEditRefusal, which is about the file's
// CONTENT, because the two have different audiences and different repairs: a
// refusal is the caller's argument to fix and nothing about it is repaired by
// rerunning, where this one is the machine's and a rerun often is the repair.
// That is the same split `nibs area rename` and `nibs area rm` already report —
// exit 2 for a refusal, exit 5 for this — and cmd/set.go's mutationErrCode maps
// this type so the `nibs query` surface agrees with them.
//
// Like FilterTargetUnreadableError it implements NO Unwrap, and for the same
// reason: the causes it carries are whatever the operating system and the config
// loader handed back, and exposing them to errors.Is would let any classifier
// keyed on a sentinel claim this error and answer for it. Cause stays
// inspectable as a field and is rendered into the message, so nothing is lost
// for diagnosis.
type AreaEditIOError struct {
	// Msg is the whole sentence, built by the site that failed. It is not
	// assembled from parts here because a partial failure has to say what it
	// already wrote and whether rerunning finishes the job, and only the site
	// knows that.
	Msg string
	// Cause is the filesystem failure, kept for diagnosis only. It is
	// deliberately not named Err: that reads as "the thing you unwrap to" and
	// would invite the one-line Unwrap this type must not have.
	Cause error
}

func (e *AreaEditIOError) Error() string { return e.Msg }

// areaEditIOError builds one, rendering cause into the message so a caller
// holding only the text still sees it.
func areaEditIOError(cause error, format string, a ...any) error {
	return &AreaEditIOError{Msg: fmt.Sprintf(format, a...), Cause: cause}
}

// areaEditSession is one area edit's critical section: the store's cross-process
// write lock, held for the WHOLE verb, and the vocabulary re-read under it.
//
// Both halves of an area edit — the member cascade and the `areas:` rewrite —
// are read-modify-writes of shared state, and the second is a rewrite of the
// entire file. Split across two critical sections, two concurrent edits
// interleave: each reads the pre-edit config, each writes the whole file back,
// and the loser's declaration is gone while its cascade sits on disk. Both
// callers report success, so nothing ever says to rerun, and the members it
// moved are write-refused from then on.
//
// It blocks rather than refusing, matching every other store mutation: the other
// holder is another nibs process finishing one operation.
//
// The RELOAD is why blocking is safe. Waiting means another process was
// mid-write while this one held the snapshot it started with. A concurrent
// `nibs config set-prefix` renames every file in the store, so a cascade over
// the old paths would write each member back under its pre-rename name; and a
// concurrent area edit reshapes the very tree the membership question is asked
// over. Reading the store again HERE is what makes both ordinary events.
//
// This is cmd/area.go's beginAreaEdit minus its startup-snapshot refusals
// (areaRetiredWhileWaiting and the vanished-areas.yml case). Those rest on
// App.StartupAreas, a CLI-process concept with no server analogue — a server
// holds no per-command "what the store declared when I started" — and they only
// choose between two WORDINGS for a path that is not declared now. They never
// decide whether the store is written, so their absence costs a sentence and
// never a write.
type areaEditSession struct {
	lock  *nibcore.StoreLock
	areas *config.Areas
}

// beginAreaEdit opens the critical section. The caller owes the returned
// session a release, including on every error path below it.
func (r *mutationResolver) beginAreaEdit() (*areaEditSession, error) {
	lock, err := nibcore.AcquireStoreLock(r.AreaEditor.Root())
	if err != nil {
		return nil, areaEditIOError(err,
			"this store's write lock could not be taken, and an areas edit rewrites both the nibs and the vocabulary so it must hold one: %v", err)
	}
	if err := r.AreaEditor.Load(); err != nil {
		_ = lock.Release()
		// Which half of the re-read failed is worth a message of its own: Load
		// reads the vocabulary and then walks the nibs, and the two are different
		// files with different repairs. Named through nibcore's own marker, since
		// the errors underneath are whatever the loader and the OS handed back and
		// carry nothing to tell them apart by.
		var areasErr *nibcore.AreasLoadError
		if errors.As(err, &areasErr) {
			return nil, areaEditIOError(err,
				"nothing was written: re-reading this store's areas vocabulary under its write lock failed: %v", err)
		}
		return nil, areaEditIOError(err,
			"nothing was written: re-reading this store's nibs under its write lock failed: %v", err)
	}
	return &areaEditSession{lock: lock, areas: r.AreaEditor.Areas()}, nil
}

func (s *areaEditSession) release() { _ = s.lock.Release() }

// members returns the ids of every nib assigned at or below path, in id order —
// the set a retire refuses over and a rename cascades through, read the same way
// AreaEditor.RewriteAreaAssignments reads it.
//
// The stored pointers All() hands back are read into plain strings immediately,
// per the live-pointer discipline at NibReader.GetSnapshot: nothing here holds
// one across the writes that follow.
func (s *areaEditSession) members(reader NibReader, path string) []string {
	var ids []string
	for _, b := range reader.All() {
		if s.areas.IsWithin(b.Area, path) {
			ids = append(ids, b.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// splitAreaPath separates a path into the path of its parent (empty at the top
// level) and the node's own name — the half a rename replaces.
func splitAreaPath(path string) (parent, name string) {
	i := strings.LastIndex(path, config.AreaPathSeparator)
	if i < 0 {
		return "", path
	}
	return path[:i], path[i+len(config.AreaPathSeparator):]
}

// renamedAreaPath is the path the node at path answers to once it is renamed to
// newName. A rename never re-parents, so the parent segments carry over verbatim
// and only the last one changes.
func renamedAreaPath(path, newName string) string {
	parent, _ := splitAreaPath(path)
	if parent == "" {
		return newName
	}
	return parent + config.AreaPathSeparator + newName
}

// areaMemberNameLimit is QueueNameLimit applied to the other project-sized set
// this package refuses over. See that constant for why an enumeration is capped
// at all; an area's members are the same shape of set as a milestone's queue.
const areaMemberNameLimit = QueueNameLimit

// namedMembers renders the member ids a refusal quotes, capped, with the number
// it elided stated so a shortened list cannot be read as a complete one. The ids
// are filename-derived, so they go through safetext.Strip like every other
// file-sourced scalar reaching a message.
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

// areaNibsAre is areaNibCount as the subject of a refusal sentence ("1 nib is" /
// "3 nibs are").
func areaNibsAre(n int) string {
	if n == 1 {
		return "1 nib is"
	}
	return fmt.Sprintf("%d nibs are", n)
}

// areaNibCount renders the tally these messages carry ("1 nib" / "3 nibs"), so
// none of them has to print a parenthesized plural.
func areaNibCount(n int) string {
	if n == 1 {
		return "1 nib"
	}
	return fmt.Sprintf("%d nibs", n)
}

// requireDeclaredArea refuses a path this store's vocabulary does not declare.
//
// It is a plain error rather than a typed one, which is what puts it in the
// validation class: mutationErrCode recognizes AreaEditIOError and nothing else
// from this file, so every other failure rides the caller's VALIDATION_ERROR
// fallback — the same exit `nibs area rename` and `nibs area rm` give the same
// refusal. config.AreaEditRefusal, which the planners raise, lands there by the
// same route and needs no branch of its own.
//
// The planners refuse an undeclared path too, so this is not what keeps a bad
// edit off disk. Two things make it worth asking first anyway. It names the
// declared set, where the planner's message can only say the file declares no
// such area; and removeArea reads the member set BEFORE it plans, so without it
// a mistyped path is answered with "nothing is assigned at or below it" — a
// factual claim about an area that does not exist.
//
// No filesystem path is named, following FilterAreaError: these messages travel
// to an HTTP client that has no business knowing where the store sits on disk.
func requireDeclaredArea(areas *config.Areas, path, verb string) error {
	if path != "" && areas.IsValid(path) {
		return nil
	}
	if !areas.Declared() {
		return fmt.Errorf("this store declares no areas, so there is none to %s — declare an `areas:` block in the store's areas.yml first", verb)
	}
	return fmt.Errorf("this store declares no area %q: the declared areas are %s",
		config.RenderAreaPath(path), areas.List())
}

// renameAreaImpl renames the declared node at input.Path, cascading to every nib
// assigned at or below it, inside one critical section.
func (r *mutationResolver) renameAreaImpl(input model.RenameAreaInput) (*model.Config, error) {
	session, err := r.beginAreaEdit()
	if err != nil {
		return nil, err
	}
	defer session.release()

	if err := requireDeclaredArea(session.areas, input.Path, "rename"); err != nil {
		return nil, err
	}

	// The planner accepts this one: renaming a node to the name it already has
	// leaves a valid vocabulary, so it hands back an edit and the file is
	// rewritten with nothing changed about what it declares. A caller cannot tell
	// that answer apart from a real rename, so it is refused here instead. It is
	// asked after requireDeclaredArea because it SPEAKS about the node at path —
	// over an undeclared one it would assert something about a node that is not
	// there.
	if _, oldName := splitAreaPath(input.Path); input.NewName == oldName {
		return nil, fmt.Errorf("area %q is already named %q, so the rename would change nothing",
			config.RenderAreaPath(input.Path), config.RenderAreaPath(input.NewName))
	}

	// The config edit is resolved BEFORE the first nib is touched. A member
	// rewrite is durable the moment it lands, so a refusal that could only fire
	// after the cascade would leave the members carrying a path the vocabulary
	// does not declare, and every later write to them refused for it. Planning
	// first moves every refusal the editor can make to before that point, where
	// it leaves the store untouched.
	edit, err := config.PlanRenameStoredArea(session.areas.StoreDir(), input.Path, input.NewName)
	if err != nil {
		return nil, err
	}

	// Read BEFORE the cascade: after a partial failure the set has already
	// shrunk, so a count taken then would understate what the run set out to do.
	members := session.members(r.Reader, input.Path)

	newPath := renamedAreaPath(input.Path, input.NewName)
	// A member assigned BELOW the renamed node keeps the remainder it carried:
	// renaming a parent moves its children's paths without changing their names.
	written, err := r.AreaEditor.RewriteAreaAssignments(session.lock, func(area string) (string, bool) {
		if !session.areas.IsWithin(area, input.Path) {
			return "", false
		}
		return newPath + strings.TrimPrefix(area, input.Path), true
	})
	if err != nil {
		return nil, areaEditIOError(err,
			"rewrote %d of the %s assigned at or below area %q, then %v — the vocabulary still declares %q and those writes are persisted; rerun the same mutation to finish it, since a nib already rewritten is no longer a member and the rerun starts where this stopped",
			len(written), areaNibCount(len(members)), config.RenderAreaPath(input.Path), err, config.RenderAreaPath(input.Path))
	}

	if _, err := edit.Write(); err != nil {
		return nil, areaEditIOError(err,
			"rewrote %s from area %q to %q, then the store's areas.yml could not be updated: %v — the vocabulary still declares %q and those writes are persisted; rerun the same mutation to finish it, since the rewritten nibs are no longer members and the rerun only renames the declaration",
			areaNibCount(len(written)), config.RenderAreaPath(input.Path), config.RenderAreaPath(newPath), err,
			config.RenderAreaPath(input.Path))
	}

	if err := r.AreaEditor.ReloadAreas(); err != nil {
		return nil, areaVocabularyReloadFailure(err)
	}
	return configResult(r.Reader), nil
}

// removeAreaImpl retires the declared node at input.Path together with the
// subtree it heads, disposing of every nib assigned at or below it as the input
// says, inside one critical section.
func (r *mutationResolver) removeAreaImpl(input model.RemoveAreaInput) (*model.Config, error) {
	session, err := r.beginAreaEdit()
	if err != nil {
		return nil, err
	}
	defer session.release()

	if err := requireDeclaredArea(session.areas, input.Path, "retire"); err != nil {
		return nil, err
	}

	disposition, target, err := session.disposition(input)
	if err != nil {
		return nil, err
	}

	members := session.members(r.Reader, input.Path)
	switch {
	case disposition != areaDispositionNone && len(members) == 0:
		return nil, areaEmptyDispositionError(input.Path, disposition)
	case disposition == areaDispositionNone && len(members) > 0:
		return nil, fmt.Errorf(
			"cannot retire area %q: %s assigned at or below it (%s) — reassign them with moveTo, drop their assignment with unassign: true, or leave the declaration in place",
			config.RenderAreaPath(input.Path), areaNibsAre(len(members)), namedMembers(members))
	}

	// Resolved before the first nib is touched, for the reason renameAreaImpl
	// plans first.
	edit, err := config.PlanRemoveStoredArea(session.areas.StoreDir(), input.Path)
	if err != nil {
		return nil, err
	}

	written, err := r.AreaEditor.RewriteAreaAssignments(session.lock, func(area string) (string, bool) {
		// Every member lands ON the target rather than keeping the remainder it
		// carried below the retiring node: the target declares no such child, so
		// preserving it would move each member to another undeclared path. target
		// is "" for an unassign, which is the legal cleared value.
		return target, session.areas.IsWithin(area, input.Path)
	})
	if err != nil {
		return nil, areaEditIOError(err,
			"%s %d of the %s assigned at or below area %q, then %v — %q is still declared and those writes are persisted; rerun the same mutation to finish it, since a nib already disposed of is no longer a member and the rerun starts where this stopped",
			areaDispositionVerb(disposition), len(written), areaNibCount(len(members)),
			config.RenderAreaPath(input.Path), err, config.RenderAreaPath(input.Path))
	}

	if _, err := edit.Write(); err != nil {
		return nil, areaRetireWriteFailure(input.Path, disposition, len(written), err)
	}

	if err := r.AreaEditor.ReloadAreas(); err != nil {
		return nil, areaVocabularyReloadFailure(err)
	}
	return configResult(r.Reader), nil
}

// areaVocabularyReloadFailure reports an edit whose two writes both landed and
// whose re-read of the file it just wrote then failed.
//
// It is the one IO failure here with nothing to rerun: the members are written
// and so is the vocabulary, so disk holds the finished edit. What is wrong is in
// THIS process, which keeps the vocabulary it could still read (see
// nibcore.Core.reloadAreas) — the one the edit replaced. Reporting it is what
// keeps the alternative off the wire: answering with that vocabulary and calling
// the mutation a success, which renders in a client as the edit not having
// happened.
func areaVocabularyReloadFailure(cause error) error {
	return areaEditIOError(cause,
		"both halves of this edit landed on disk, and re-reading the vocabulary it just wrote then failed: %v — there is nothing to rerun; until that file can be read again this store answers from the vocabulary as it was before the edit",
		cause)
}

// areaRetireWriteFailure reports a retire whose members are disposed of and
// whose config write then failed, branching on whether a disposition was
// actually NAMED rather than on which one it was.
//
// With no disposition there is nothing to report as done and no argument to
// drop: that branch is reachable only for an area nothing was assigned to, where
// the refusal above has already established the member set is empty and the
// cascade therefore wrote nothing. Selecting the wording on the move/unassign
// pair instead made the same case in cmd/area.go claim an unassignment had run
// and prescribe dropping an argument the caller never sent.
func areaRetireWriteFailure(path string, disposition areaDisposition, written int, cause error) error {
	if disposition == areaDispositionNone {
		return areaEditIOError(cause,
			"area %q could not be retired: the store's areas.yml could not be updated: %v — nothing is assigned at or below it, so nothing was rewritten and the store is as it was; rerun once that is fixed",
			config.RenderAreaPath(path), cause)
	}
	return areaEditIOError(cause,
		"%s %s from area %q, then the store's areas.yml could not be updated: %v — %q is still declared and those writes are persisted; rerun WITHOUT %s to retire it, which is what finishes the job now that nothing is assigned below it",
		areaDispositionVerb(disposition), areaNibCount(written), config.RenderAreaPath(path), cause,
		config.RenderAreaPath(path), areaDispositionField(disposition))
}

// areaEmptyDispositionError refuses a disposition that has nothing to act on.
// Letting it succeed would report members disposed of when there were none, and
// a silent no-op is the one answer a caller cannot tell apart from a real one.
//
// It is reachable two ways: by naming a disposition for an area nothing is
// assigned to, and by rerunning after a cascade completed and the config write
// did not. Dropping the disposition retires the area from either state, which is
// why that is what the message prescribes.
func areaEmptyDispositionError(path string, disposition areaDisposition) error {
	return fmt.Errorf("nothing to %s: no nib is assigned at or below area %q — drop %s to retire it",
		areaDispositionAction(disposition), config.RenderAreaPath(path), areaDispositionField(disposition))
}

// areaDisposition is what a retire was told to do with the nibs assigned at or
// below the area it is retiring.
//
// None is a legal answer rather than a missing one — an area nothing is assigned
// to needs no disposition — and it is a THIRD case, not the absence of moveTo.
// Deriving the wording from a `move bool` made cmd/area.go's no-disposition
// branch describe an unassignment that never ran.
type areaDisposition int

const (
	areaDispositionNone areaDisposition = iota
	areaDispositionMove
	areaDispositionUnassign
)

// areaDispositionField is the input field that asked for it, for a message
// telling the caller to drop it. It is only ever reached for one that was named.
func areaDispositionField(d areaDisposition) string {
	if d == areaDispositionMove {
		return "moveTo"
	}
	return "unassign"
}

// areaDispositionVerb is the past tense a completed disposition reports in;
// areaDispositionAction is the bare verb a refusal says there is nothing to do.
func areaDispositionVerb(d areaDisposition) string {
	if d == areaDispositionMove {
		return "reassigned"
	}
	return "unassigned"
}

func areaDispositionAction(d areaDisposition) string {
	if d == areaDispositionMove {
		return "reassign"
	}
	return "unassign"
}

// disposition reads which disposition a retire was given and resolves its
// target, returning "" for every case that clears the assignment.
//
// The two are mutually exclusive, and there is no wire equivalent of Cobra's
// MarkFlagsMutuallyExclusive to refuse them for us. `unassign: false` is read as
// NO disposition rather than as a contradiction: it is what a client sends when
// a checkbox is off, and taking it as a conflicting answer would refuse a
// perfectly ordinary `{path, moveTo, unassign: false}`.
//
// BOTH questions about a moveTo target are asked of the vocabulary re-read under
// the lock, because the target is exactly as perishable as the node being
// retired: a concurrent retire of the target finishing while this call waited
// leaves it declared in any earlier snapshot and gone from the store, and the
// reassignment then walks every member into the state the member refusal exists
// to prevent.
func (s *areaEditSession) disposition(input model.RemoveAreaInput) (areaDisposition, string, error) {
	switch {
	case input.MoveTo != nil && input.Unassign != nil && *input.Unassign:
		return areaDispositionNone, "", fmt.Errorf(
			"moveTo and unassign: true are two different dispositions for the same nibs — send one")
	case input.MoveTo != nil:
		target := *input.MoveTo
		if err := requireDeclaredArea(s.areas, target, "move work to"); err != nil {
			return areaDispositionNone, "", err
		}
		if s.areas.IsWithin(target, input.Path) {
			return areaDispositionNone, "", fmt.Errorf(
				"cannot move members to %q: it is declared at or below %q, which this mutation is retiring — name an area outside it, or send unassign: true to drop their assignment",
				config.RenderAreaPath(target), config.RenderAreaPath(input.Path))
		}
		return areaDispositionMove, target, nil
	case input.Unassign != nil && *input.Unassign:
		return areaDispositionUnassign, "", nil
	default:
		return areaDispositionNone, "", nil
	}
}
