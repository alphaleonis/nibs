package nibcore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
)

// The area-vocabulary verbs: declaring an area, renaming one, and retiring one
// together with the nibs assigned to it.
//
// They live HERE, whole: each is a critical section spanning both of this
// store's locks, and acquireWriteLock states the order canonically — c.mu, then
// the cross-process file lock. A surface that took the file lock first and then
// reached back into Core for the vocabulary, the member set or the cascade —
// each of which takes c.mu — deadlocks against a concurrent Update holding c.mu
// and parked on the file lock, wedging every reader in this process and every
// other nibs process on the machine. No area surface touches either lock; keep
// it that way.
//
// Every refusal below carries FIELDS and no prescription: a sentence naming
// `nibs area rm --unassign` or `unassign: true` belongs to the surface that has
// that reader.

// AreaDispositionKind is what a retire was told to do with the nibs assigned at
// or below the area it is retiring.
//
// None is a THIRD case, not the absence of a move target: an area nothing is
// assigned to needs no disposition.
type AreaDispositionKind int

const (
	AreaDispositionNone AreaDispositionKind = iota
	AreaDispositionMove
	AreaDispositionUnassign
)

// AreaDisposition is one retire's disposition together with the target a move
// names. The zero value is "none", so a caller given no disposition passes
// nothing.
//
// Build one with MoveAreaMembersTo or UnassignAreaMembers: RemoveArea writes
// MoveTo onto every member whatever Kind says, so an unassign carrying a
// non-empty MoveTo silently moves them instead.
type AreaDisposition struct {
	Kind AreaDispositionKind
	// MoveTo is the area a move reassigns members to; "" clears the assignment.
	MoveTo string
}

func MoveAreaMembersTo(target string) AreaDisposition {
	return AreaDisposition{Kind: AreaDispositionMove, MoveTo: target}
}

func UnassignAreaMembers() AreaDisposition {
	return AreaDisposition{Kind: AreaDispositionUnassign}
}

// AreaEditResult is what one completed area edit did, for a surface to report.
type AreaEditResult struct {
	// Areas is the vocabulary the edit WROTE, re-read from disk under the same
	// lock.
	Areas *config.Areas
	// Members is the set the verb acted on, read BEFORE the cascade: after a
	// partial failure the set has already shrunk.
	Members []string
	// Written is the ids the cascade actually rewrote, in id order.
	Written []string
	// NewPath is the path the renamed node answers to now. Empty for the other
	// two verbs.
	NewPath string
	// DeclaredBelow is how many areas were declared BENEATH a retired node and
	// went with it. It counts declarations, not nibs.
	DeclaredBelow int
	// StaleLinkTarget is set when the areas.yml this edit replaced was a
	// SYMLINK: the atomic write leaves a regular file in its place, and the old
	// target still declares the pre-edit vocabulary. Report it — restoring that
	// link undoes this edit.
	StaleLinkTarget string
}

// AreaPathRole says which of an edit's path arguments a refusal is about, for a
// surface to word the refusal from.
type AreaPathRole int

const (
	AreaPathRenamed AreaPathRole = iota
	AreaPathRetired
	AreaPathMoveTarget
	AreaPathParent
)

// AreaUndeclaredError refuses a path this store's vocabulary does not declare.
type AreaUndeclaredError struct {
	Path  string
	Role  AreaPathRole
	Areas *config.Areas
}

func (e *AreaUndeclaredError) Error() string {
	if !e.Areas.Declared() {
		return "this store declares no areas"
	}
	return fmt.Sprintf("this store declares no area %q: the declared areas are %s",
		config.RenderAreaPath(e.Path), e.Areas.List())
}

// AreaRetiredWhileWaitingError reports that path was declared in the vocabulary
// this store had loaded and is not declared in the one it just re-read under the
// write lock — what another nibs process finishing a retire or a rename while
// this edit waited for that lock leaves behind.
//
// Every decision the verb makes comes from the vocabulary the store declares
// NOW; this only words the refusal. How far back "had loaded" reaches differs by
// process — a one-shot CLI read it at startup, a serve process re-reads on every
// areas.yml event — so the sentence is the surface's.
type AreaRetiredWhileWaitingError struct {
	Path string
	Role AreaPathRole
}

func (e *AreaRetiredWhileWaitingError) Error() string {
	return fmt.Sprintf("this store declared area %q when this edit began and does not declare it now",
		config.RenderAreaPath(e.Path))
}

// AreaVocabularyVanishedError reports an areas.yml that existed when this store
// was last read and does not exist now.
//
// Both halves of the check are needed — loaded from a file before, not loaded
// from one now — because config.LoadAreas answers a MISSING file with an empty
// vocabulary and a nil error: a store that never had one looks the same.
type AreaVocabularyVanishedError struct {
	// File is the areas.yml that is gone. Error() never renders it.
	File string
}

func (e *AreaVocabularyVanishedError) Error() string {
	return "this store's areas vocabulary could not be read under its write lock: the file does not exist"
}

// AreaNameUnchangedError refuses renaming a node to the name it already has.
//
// The planner ACCEPTS this one: it hands back an edit and the file is rewritten
// with nothing changed about what it declares.
type AreaNameUnchangedError struct {
	Path string
	Name string
}

func (e *AreaNameUnchangedError) Error() string {
	return fmt.Sprintf("area %q is already named %q", config.RenderAreaPath(e.Path), config.RenderAreaPath(e.Name))
}

// AreaNameTakenError refuses a rename onto a name a sibling already holds.
//
// The planner's revalidation refuses the same edit, but only as "duplicate
// area". Asked here, the refusal names the sibling that is in the way.
type AreaNameTakenError struct {
	Path    string
	NewName string
	// Sibling is the path the new name would collide with: the parent's, with
	// NewName as its last segment.
	Sibling string
}

func (e *AreaNameTakenError) Error() string {
	return fmt.Sprintf("this store already declares area %q", config.RenderAreaPath(e.Sibling))
}

// AreaAlreadyDeclaredError refuses declaring an area at a path the store already
// declares.
type AreaAlreadyDeclaredError struct{ Path string }

func (e *AreaAlreadyDeclaredError) Error() string {
	return fmt.Sprintf("this store already declares area %q", config.RenderAreaPath(e.Path))
}

// AreaParentUndeclaredError refuses a nested declaration whose parent the store
// does not declare. A parent is never created on the way.
type AreaParentUndeclaredError struct {
	Path   string
	Parent string
}

func (e *AreaParentUndeclaredError) Error() string {
	return fmt.Sprintf("this store declares no area %q to nest %q under",
		config.RenderAreaPath(e.Parent), config.RenderAreaPath(e.Path))
}

// AreaMembersPresentError refuses retiring an area work is still assigned to
// with no disposition for that work: the members would be left carrying a path
// the vocabulary no longer declares, and every later write to them refused for
// it.
type AreaMembersPresentError struct {
	Path    string
	Members []string
}

func (e *AreaMembersPresentError) Error() string {
	return fmt.Sprintf("cannot retire area %q: %d nib(s) are assigned at or below it",
		config.RenderAreaPath(e.Path), len(e.Members))
}

// AreaDispositionEmptyError refuses a disposition that has nothing to act on.
//
// It is reachable two ways: naming a disposition for an area nothing is assigned
// to, and rerunning after a cascade completed and the config write did not.
// Dropping the disposition retires the area from either state.
type AreaDispositionEmptyError struct {
	Path        string
	Disposition AreaDisposition
}

func (e *AreaDispositionEmptyError) Error() string {
	return fmt.Sprintf("no nib is assigned at or below area %q, so there is nothing to dispose of",
		config.RenderAreaPath(e.Path))
}

// AreaMoveTargetWithinError refuses reassigning members INTO the subtree being
// retired: the target is about to stop existing, so the move would strand them.
type AreaMoveTargetWithinError struct {
	Target string
	Path   string
}

func (e *AreaMoveTargetWithinError) Error() string {
	return fmt.Sprintf("cannot move members to %q: it is declared at or below %q, which is being retired",
		config.RenderAreaPath(e.Target), config.RenderAreaPath(e.Path))
}

// AreaMembersArrivedError refuses the vocabulary write of an edit that would
// leave work on a path the write stops declaring: a nib assigned at or below
// Path was on disk when the edit read the store for the LAST time, and the
// cascade — which walks the nibs this process holds — never saw it. The route
// is a writer outside this process landing a `.md` file after editArea's
// re-read; a `git pull` in .nibs is the routine one.
//
// The vocabulary still declares Path, so a rerun decides from the store as it
// then stands. That is the remedy both surfaces prescribe.
type AreaMembersArrivedError struct {
	Path string
	// Members is the ids assigned at or below Path when the edit last read the
	// store — the set the vocabulary write would have stranded.
	Members []string
	// Written is what the cascade had already rewritten. Those writes are
	// durable: after a rename the members already carry the new path.
	Written []string
	// NewPath is a rename's new path, empty for a retire. The vocabulary still
	// declares the old one, so every write to Written is refused until the rerun.
	NewPath string
}

func (e *AreaMembersArrivedError) Error() string {
	return fmt.Sprintf("%d nib(s) are assigned at or below area %q and this edit did not see them when it read the store",
		len(e.Members), config.RenderAreaPath(e.Path))
}

// AreaEditPhase is where in one area edit a filesystem failure landed. The
// phases differ in what is already on disk, and so in whether a rerun is the
// repair.
type AreaEditPhase int

const (
	AreaEditPhaseLock AreaEditPhase = iota
	AreaEditPhaseLoadVocabulary
	AreaEditPhaseLoadNibs
	AreaEditPhaseCascade
	// AreaEditPhaseConfirm is the re-read that decides AreaMembersArrivedError.
	AreaEditPhaseConfirm
	AreaEditPhaseWrite
	AreaEditPhaseReload
)

// AreaEditIOError reports that an area vocabulary edit failed on the
// FILESYSTEM, as opposed to config.AreaEditRefusal, which is about the file's
// CONTENT: a rerun often repairs this one and never repairs a refusal. That is
// the exit 5 versus exit 2 split `nibs area rename` and `nibs area rm` report,
// and cmd/set.go's mutationErrCode maps this type so `nibs query` agrees.
//
// It implements no Unwrap: the causes it carries are whatever the operating
// system and the config loader handed back, and errors.Is over them would let a
// classifier keyed on a sentinel answer for this error. Cause stays inspectable
// as a field. A surface that must stay classifiable wraps this error inside its
// own sentence, which works because the wrapper unwraps to this type and this
// type unwraps to nothing.
type AreaEditIOError struct {
	Phase AreaEditPhase
	// Path is the area the verb was acting on, NewPath the one a rename was
	// producing, and Disposition what a retire was told to do with the members.
	Path        string
	NewPath     string
	Disposition AreaDisposition
	// File is the store's areas.yml. Error() never renders it — these sentences
	// reach an HTTP client — so a surface whose reader owns that directory is
	// the one that may name it.
	File string
	// Written is what the cascade had rewritten when the failure landed; Members
	// the set it set out to rewrite.
	Written []string
	Members []string
	Cause   error
}

func (e *AreaEditIOError) Error() string {
	return fmt.Sprintf("this area edit failed while %s: %v", e.Phase.describe(), e.Cause)
}

// describe names a phase for AreaEditIOError's own message.
//
// Every arm is named and there is no default one: a phase added to the iota
// block falls to the "cannot name" string below rather than borrowing the last
// arm's sentence.
func (p AreaEditPhase) describe() string {
	switch p {
	case AreaEditPhaseLock:
		return "taking the store's write lock"
	case AreaEditPhaseLoadVocabulary:
		return "re-reading the store's areas vocabulary under its write lock"
	case AreaEditPhaseLoadNibs:
		return "re-reading the store's nibs under its write lock"
	case AreaEditPhaseCascade:
		return "rewriting the nibs assigned to the area"
	case AreaEditPhaseConfirm:
		return "re-reading the store's nibs to confirm the vocabulary write strands none of them"
	case AreaEditPhaseWrite:
		return "writing the store's areas.yml"
	case AreaEditPhaseReload:
		return "re-reading the areas.yml it had just written"
	}
	return fmt.Sprintf("in a phase of the edit this build cannot name (%d)", int(p))
}

// reloadAreasAfterEdit is Core.loadAreasLocked, indirected so a test can fail
// the edit's re-read of the file it has just written — the one IO phase with no
// real fault to inject.
var reloadAreasAfterEdit = (*Core).loadAreasLocked

// reloadNibsBeforeAreaWrite is Core.loadFromDisk, indirected so a test can land
// a nib file inside the window this re-read exists to close. The seam is the
// re-read ITSELF, so a test cannot demonstrate the refusal against an edit that
// never looks.
var reloadNibsBeforeAreaWrite = (*Core).loadFromDisk

// AddArea declares a new area at path and returns the vocabulary as it then
// stands.
//
// It rewrites no nib, even though a nib may already CARRY the path — left on it
// by a retire or a hand edit, with every write to that nib refused until the
// vocabulary declares the path again. Declaring it is that repair.
//
// Judge the path's SHAPE before calling: config.ValidateNewAreaPath and
// config.ValidateAreaColor answer from the arguments alone, so a typo need not
// wait behind another writer's lock. The planner asks them again regardless.
func (c *Core) AddArea(ctx context.Context, path, description, color string) (AreaEditResult, error) {
	parent, _ := splitAreaPath(path)
	return c.editArea(ctx, path, func(before, now *config.Areas) (areaPlan, error) {
		if now.IsValid(path) {
			return areaPlan{}, &AreaAlreadyDeclaredError{Path: path}
		}
		if parent != "" {
			if before.IsValid(parent) && !now.IsValid(parent) {
				return areaPlan{}, &AreaRetiredWhileWaitingError{Path: parent, Role: AreaPathParent}
			}
			if !now.IsValid(parent) {
				return areaPlan{}, &AreaParentUndeclaredError{Path: path, Parent: parent}
			}
		}
		edit, err := config.PlanCreateStoredArea(now.StoreDir(), path, description, color)
		if err != nil {
			return areaPlan{}, err
		}
		return areaPlan{path: path, edit: edit}, nil
	})
}

// RenameArea renames the declared node at path to newName, cascading to every
// nib assigned at or below it.
//
// newName is a bare name: the parent segments carry over verbatim, and a member
// assigned BELOW the renamed node keeps the remainder it carried.
func (c *Core) RenameArea(ctx context.Context, path, newName string) (AreaEditResult, error) {
	return c.editArea(ctx, path, func(before, now *config.Areas) (areaPlan, error) {
		if err := requireDeclaredArea(before, now, path, AreaPathRenamed); err != nil {
			return areaPlan{}, err
		}
		parent, oldName := splitAreaPath(path)
		// Asked after the path is known to be declared: over an undeclared one,
		// "already named" would describe a node that is not there.
		if newName == oldName {
			return areaPlan{}, &AreaNameUnchangedError{Path: path, Name: newName}
		}
		if sibling := joinAreaPath(parent, newName); now.IsValid(sibling) {
			return areaPlan{}, &AreaNameTakenError{Path: path, NewName: newName, Sibling: sibling}
		}

		// The config edit is resolved BEFORE the first nib is touched: a member
		// rewrite is durable the moment it lands, so a refusal that could only
		// fire after the cascade would strand every member. Planning first moves
		// every refusal the editor can make to before that point.
		edit, err := config.PlanRenameStoredArea(now.StoreDir(), path, newName)
		if err != nil {
			return areaPlan{}, err
		}

		newPath := joinAreaPath(parent, newName)
		return areaPlan{
			path:    path,
			newPath: newPath,
			emptied: path,
			edit:    edit,
			members: c.areaMembersLocked(now, path),
			rewrite: func(area string) (string, bool) {
				if !now.IsWithin(area, path) {
					return "", false
				}
				return newPath + strings.TrimPrefix(area, path), true
			},
		}, nil
	})
}

// RemoveArea retires the declared node at path together with the subtree it
// heads, disposing of every nib assigned at or below it as disposition says.
//
// Every member lands ON the move target; the remainder it carried below the
// retiring node is dropped.
func (c *Core) RemoveArea(ctx context.Context, path string, disposition AreaDisposition) (AreaEditResult, error) {
	return c.editArea(ctx, path, func(before, now *config.Areas) (areaPlan, error) {
		if err := requireDeclaredArea(before, now, path, AreaPathRetired); err != nil {
			return areaPlan{}, err
		}

		// BOTH questions about a move target are asked of the vocabulary re-read
		// under the lock: the target is as perishable as the node being retired,
		// and a concurrent retire of it finishing while this edit waited would
		// leave every member reassigned to a path the store no longer declares.
		if disposition.Kind == AreaDispositionMove {
			if err := requireDeclaredArea(before, now, disposition.MoveTo, AreaPathMoveTarget); err != nil {
				return areaPlan{}, err
			}
			if now.IsWithin(disposition.MoveTo, path) {
				return areaPlan{}, &AreaMoveTargetWithinError{Target: disposition.MoveTo, Path: path}
			}
		}

		members := c.areaMembersLocked(now, path)
		switch {
		case disposition.Kind != AreaDispositionNone && len(members) == 0:
			return areaPlan{}, &AreaDispositionEmptyError{Path: path, Disposition: disposition}
		case disposition.Kind == AreaDispositionNone && len(members) > 0:
			return areaPlan{}, &AreaMembersPresentError{Path: path, Members: members}
		}

		// Resolved before the first nib is touched, for the reason RenameArea
		// plans first.
		edit, err := config.PlanRemoveStoredArea(now.StoreDir(), path)
		if err != nil {
			return areaPlan{}, err
		}

		target := disposition.MoveTo
		return areaPlan{
			path:          path,
			emptied:       path,
			edit:          edit,
			members:       members,
			disposition:   disposition,
			declaredBelow: countDeclaredBelow(now, path),
			rewrite: func(area string) (string, bool) {
				return target, now.IsWithin(area, path)
			},
		}, nil
	})
}

// areaPlan is what one verb decided under the lock: the config edit to write,
// the member cascade to run before it, and the facts the result reports.
type areaPlan struct {
	path    string
	newPath string
	// emptied is the area path this edit stops declaring — a retired node, or a
	// rename's old path. Nothing may be assigned at or below it once the
	// vocabulary write lands. Empty for a verb that declares without retiring.
	emptied       string
	edit          *config.StoredAreaEdit
	members       []string
	disposition   AreaDisposition
	declaredBelow int
	// rewrite claims the members this verb cascades through; nil for a verb that
	// cascades none.
	rewrite func(area string) (string, bool)
}

// editArea is the one critical section every area verb runs in: c.mu, then the
// store's cross-process write lock, then the re-read under both, then plan,
// cascade, confirm, write and reload — with both locks held throughout and
// released only on the way out.
//
// THE LOCK ORDER IS c.mu THEN THE FILE LOCK, as acquireWriteLock states
// canonically and every other Core mutator obeys. Taking them the other way
// round deadlocks against an ordinary concurrent Update.
//
// BOTH HALVES OF THE EDIT ARE ONE CRITICAL SECTION. The member cascade and the
// `areas:` rewrite are read-modify-writes of shared state, and the second
// rewrites the whole file. Split across two critical sections, two concurrent
// edits interleave: each reads the pre-edit config, each writes the whole file
// back, and the loser's declaration is gone while its cascade sits on disk. Both
// callers report success, so nothing ever says to rerun, and the members it
// moved are write-refused from then on.
//
// It WAITS for the file lock rather than refusing, matching every other store
// mutation: the other holder is another nibs process finishing one operation.
// Only ctx ends that wait — acquireWriteLockContext says why no deadline can.
//
// THE COST IS READER AVAILABILITY. c.mu is held from before the file lock is
// asked for until after the reload — a wait for another process, the cascade,
// the config write and two full store loads for a rename or a retire — and Get,
// All and Search all read under it, so under `nibs serve` a read blocks for all
// of it. Do not narrow the span: that is what lets two edits interleave and lose
// one's declaration.
//
// THE RE-READ IS WHY BLOCKING IS SAFE. Waiting means another process was
// mid-write while this one held the state it started with: a concurrent
// `nibs config set-prefix` renames every file in the store, so a cascade over
// the old paths would write each member back under its pre-rename name, and a
// concurrent area edit reshapes the very tree the membership question is asked
// over. Reading the store again HERE is what makes both ordinary events.
//
// The vocabulary is the ONE part of a store's configuration a reload may
// replace, and it is replaced here: it lives behind an atomic pointer so a
// reload cannot race the off-lock readers. Everything in config.yml stays fixed
// at construction.
//
// A failure to re-read is a REFUSAL, never a fallback to the vocabulary already
// loaded.
func (c *Core) editArea(ctx context.Context, path string, plan func(before, now *config.Areas) (areaPlan, error)) (AreaEditResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	before := c.Areas()

	release, err := c.acquireWriteLockContext(ctx)
	if err != nil {
		return AreaEditResult{}, &AreaEditIOError{Phase: AreaEditPhaseLock, Path: path, File: c.layout.AreasPath(), Cause: err}
	}
	defer func() { _ = release() }()

	if err := c.loadLocked(); err != nil {
		phase := AreaEditPhaseLoadNibs
		var areasErr *AreasLoadError
		if errors.As(err, &areasErr) {
			phase = AreaEditPhaseLoadVocabulary
		}
		return AreaEditResult{}, &AreaEditIOError{Phase: phase, Path: path, File: c.layout.AreasPath(), Cause: err}
	}
	now := c.Areas()

	if before.LoadedFromFile() && !now.LoadedFromFile() {
		return AreaEditResult{}, &AreaVocabularyVanishedError{File: now.Path()}
	}

	p, err := plan(before, now)
	if err != nil {
		return AreaEditResult{}, err
	}

	var written []string
	if p.rewrite != nil {
		written, err = c.rewriteAreaAssignmentsLocked(p.rewrite)
		if err != nil {
			return AreaEditResult{}, &AreaEditIOError{
				Phase: AreaEditPhaseCascade, Path: p.path, NewPath: p.newPath, File: now.Path(),
				Disposition: p.disposition, Written: written, Members: p.members, Cause: err,
			}
		}
	}

	// THE MEMBERSHIP QUESTION IS ASKED AGAIN, from disk, as late as it can be.
	// The plan answered it from the nibs this process holds, and the store's
	// write lock is a contract between nibs processes — it does not hold off a
	// `git pull` in .nibs. A file landing after the re-read above is in no set
	// the cascade walked, so the write below would retire a declaration that file
	// still carries.
	if p.emptied != "" {
		if err := reloadNibsBeforeAreaWrite(c); err != nil {
			return AreaEditResult{}, &AreaEditIOError{
				Phase: AreaEditPhaseConfirm, Path: p.path, NewPath: p.newPath, File: now.Path(),
				Disposition: p.disposition, Written: written, Members: p.members, Cause: err,
			}
		}
		if left := c.areaMembersLocked(now, p.emptied); len(left) > 0 {
			return AreaEditResult{}, &AreaMembersArrivedError{
				Path: p.emptied, Members: left, Written: written, NewPath: p.newPath,
			}
		}
	}

	staleLink, err := p.edit.Write()
	if err != nil {
		return AreaEditResult{}, &AreaEditIOError{
			Phase: AreaEditPhaseWrite, Path: p.path, NewPath: p.newPath, File: now.Path(),
			Disposition: p.disposition, Written: written, Members: p.members, Cause: err,
		}
	}

	// Re-read under the same lock so the result carries the vocabulary this edit
	// wrote, and so an areas subscriber in this process wakes on the edit rather
	// than on the watcher's debounce. loadAreasLocked keeps the pre-edit
	// vocabulary when the file cannot be read back, so ignoring the failure would
	// report success over it. The subscriber tick it makes takes subMu under
	// c.mu, which is the established order.
	if err := reloadAreasAfterEdit(c); err != nil {
		return AreaEditResult{}, &AreaEditIOError{
			Phase: AreaEditPhaseReload, Path: p.path, NewPath: p.newPath, File: now.Path(),
			Disposition: p.disposition, Written: written, Members: p.members, Cause: err,
		}
	}

	return AreaEditResult{
		Areas:           c.Areas(),
		Members:         p.members,
		Written:         written,
		NewPath:         p.newPath,
		DeclaredBelow:   p.declaredBelow,
		StaleLinkTarget: staleLink,
	}, nil
}

// requireDeclaredArea refuses a path the store's vocabulary does not declare,
// telling apart one that WAS declared when this store was last read from one
// that never existed.
func requireDeclaredArea(before, now *config.Areas, path string, role AreaPathRole) error {
	if path != "" && now.IsValid(path) {
		return nil
	}
	if path != "" && before.IsValid(path) {
		return &AreaRetiredWhileWaitingError{Path: path, Role: role}
	}
	return &AreaUndeclaredError{Path: path, Role: role, Areas: now}
}

// areaMembersLocked returns the ids of every nib assigned at or below path, in
// id order, read the same way rewriteAreaAssignmentsLocked reads it.
//
// The stored pointers are read into plain strings immediately, per the
// live-pointer discipline: nothing here holds one across the writes that follow.
func (c *Core) areaMembersLocked(areas *config.Areas, path string) []string {
	var ids []string
	for id, b := range c.nibs {
		if areas.IsWithin(b.Area, path) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// countDeclaredBelow counts the areas declared beneath path, which a retire
// takes with it.
func countDeclaredBelow(areas *config.Areas, path string) int {
	n := 0
	for _, declared := range areas.Paths() {
		if declared != path && areas.IsWithin(declared, path) {
			n++
		}
	}
	return n
}

// splitAreaPath separates a path into its parent's path (empty at the top level)
// and the node's own name. joinAreaPath is its inverse.
func splitAreaPath(path string) (parent, name string) {
	i := strings.LastIndex(path, config.AreaPathSeparator)
	if i < 0 {
		return "", path
	}
	return path[:i], path[i+len(config.AreaPathSeparator):]
}

func joinAreaPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + config.AreaPathSeparator + name
}
