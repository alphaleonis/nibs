package nibcore

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
)

// The area-vocabulary verbs: declaring an area, renaming one, and retiring one
// together with the nibs assigned to it.
//
// They live HERE, whole, rather than in the CLI and the GraphQL resolver,
// because each is a critical section that spans both of this store's locks and
// there is exactly one order they may be taken in. acquireWriteLock states it:
// the lock order is always c.mu then the cross-process file lock. A surface that
// took the file lock first and then reached back into Core for the vocabulary,
// the member set and the cascade — each of which takes c.mu — inverted that
// order, and a concurrent Update (c.mu held, parked on the file lock) then
// deadlocked the process for good: c.mu is never released, so every read wedges
// too, and the file lock's descriptor is never closed, so every other nibs
// process on the machine blocks as well.
//
// Owning the verb is what fixes that BY CONSTRUCTION rather than by convention:
// no surface touches either lock, so no surface can order them wrongly, and
// there is no lock-holding obligation left for a proof-of-lock parameter to
// encode.
//
// What the surfaces keep is the wording. Every refusal below carries FIELDS and
// no prescription: `nibs area rm --unassign` and `unassign: true` are the same
// refusal spoken to two different readers, and a sentence naming either belongs
// to the surface that has that reader.

// AreaDispositionKind is what a retire was told to do with the nibs assigned at
// or below the area it is retiring.
//
// None is a legal answer rather than a missing one — an area nothing is assigned
// to needs no disposition — and it is a THIRD case, not the absence of a move
// target. Deriving the wording from a `move bool` made the no-disposition branch
// describe an unassignment that never ran.
type AreaDispositionKind int

const (
	AreaDispositionNone AreaDispositionKind = iota
	AreaDispositionMove
	AreaDispositionUnassign
)

// AreaDisposition is one retire's disposition together with the target a move
// names. The zero value is "none", so a caller that was given no disposition
// passes nothing.
//
// The two are one value rather than two parameters because they are mutually
// exclusive, and a type that cannot express the contradiction is what spares
// every surface from having to refuse it under the store's write lock. Each
// surface still refuses its own spelling of the contradiction — Cobra's
// MarkFlagsMutuallyExclusive, the resolver's own check — before it calls here.
type AreaDisposition struct {
	Kind AreaDispositionKind
	// MoveTo is the area a move reassigns members to. It is meaningful only for
	// AreaDispositionMove, and an unassign's cleared value is "" by the same
	// token: the empty string IS the legal cleared assignment.
	MoveTo string
}

// MoveAreaMembersTo disposes of a retire's members by reassigning them to
// target; UnassignAreaMembers drops their assignment instead.
func MoveAreaMembersTo(target string) AreaDisposition {
	return AreaDisposition{Kind: AreaDispositionMove, MoveTo: target}
}

func UnassignAreaMembers() AreaDisposition {
	return AreaDisposition{Kind: AreaDispositionUnassign}
}

// AreaEditResult is what one completed area edit did, for a surface to report.
//
// Areas is the vocabulary the edit WROTE, re-read from disk under the same lock,
// so a caller answering with it answers with what it just produced rather than
// with whatever the store happens to hold by the time it renders.
type AreaEditResult struct {
	Areas *config.Areas
	// Members is the set the verb acted on, read BEFORE the cascade: after a
	// partial failure the set has already shrunk, so a count taken then would
	// understate what the run set out to do.
	Members []string
	// Written is the ids the cascade actually rewrote, in id order.
	Written []string
	// NewPath is the path the renamed node answers to now. Empty for the other
	// two verbs.
	NewPath string
	// DeclaredBelow is how many areas were declared BENEATH a retired node,
	// which the retire took with it. It counts declarations, not nibs.
	DeclaredBelow int
	// StaleLinkTarget is set when the areas.yml this edit replaced was a
	// SYMLINK: the atomic write leaves a regular file in its place, and the old
	// target still declares the pre-edit vocabulary. Whoever manages that target
	// will restore it, so a surface that drops this reports success over an edit
	// that is about to be undone.
	StaleLinkTarget string
}

// AreaPathRole says which of an edit's path arguments a refusal is about. A
// surface words the refusal from it; nibcore carries no sentence naming a verb.
type AreaPathRole int

const (
	// AreaPathRenamed is the node a rename names, AreaPathRetired the node a
	// retire names, AreaPathMoveTarget a retire's reassignment target, and
	// AreaPathParent the declared node a new area is nested under.
	AreaPathRenamed AreaPathRole = iota
	AreaPathRetired
	AreaPathMoveTarget
	AreaPathParent
)

// AreaUndeclaredError refuses a path this store's vocabulary does not declare.
//
// It carries the vocabulary rather than a rendering of it because the two
// directions are separate sentences — "must be one of" followed by nothing reads
// as a bug in nibs, where the real answer is that this project has never
// declared a vocabulary, which is a config edit and not a different argument —
// and each surface has its own remedy for the second.
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
// The comparison is made for ONE purpose: to word a refusal. Every decision the
// verb makes comes from the vocabulary the store declares now, and this only
// tells a caller whose argument was true when they gave it apart from one who
// named a path that never existed — so a wrong answer costs a sentence and never
// a write. StoreRePrefixedError carries its Loaded prefix on the same terms.
//
// WHAT "BEFORE" MEANS DIFFERS BY PROCESS, and the surfaces word it accordingly.
// A one-shot CLI process loads the store once and never reloads it, so the
// earlier vocabulary is literally the one it read at startup. A serve process
// reloads on every areas.yml event, so its earlier vocabulary is only "the one
// this store last read" — a tighter baseline, not the same claim. Nothing here
// asserts either; the field is the fact, and the sentence is the surface's.
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
// It takes BOTH halves to say the file vanished, and only that shape is refused.
// Absence is carried in the vocabulary itself rather than re-observed on the
// path, the same way mintingVocabulary carries it: config.LoadAreas answers a
// MISSING file with an empty vocabulary and a nil error, so a file that vanished
// under a waiting edit is indistinguishable from a concurrent retire having
// taken every node — which is what AreaRetiredWhileWaitingError would then name.
// A store that never had an areas.yml reaches the same empty vocabulary and is a
// legitimate shape, so it falls through to AreaUndeclaredError, whose "declares
// no areas" is both the true cause and the reachable remedy.
type AreaVocabularyVanishedError struct {
	// File is the areas.yml that is gone. A surface that may name a path names
	// this one; one that may not says only that the file is gone.
	File string
}

func (e *AreaVocabularyVanishedError) Error() string {
	return "this store's areas vocabulary could not be read under its write lock: the file does not exist"
}

// AreaNameUnchangedError refuses renaming a node to the name it already has.
//
// The planner ACCEPTS this one: the result is a valid vocabulary, so it hands
// back an edit and the file is rewritten with nothing changed about what it
// declares — an answer a caller cannot tell apart from a real rename.
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
// area", which describes the file rather than the argument. Asked here, the
// refusal can name the sibling that is in the way.
type AreaNameTakenError struct {
	Path    string
	NewName string
	// Sibling is the path the new name would collide with — the parent's, with
	// NewName as its last segment.
	Sibling string
}

func (e *AreaNameTakenError) Error() string {
	return fmt.Sprintf("this store already declares area %q", config.RenderAreaPath(e.Sibling))
}

// AreaAlreadyDeclaredError refuses declaring an area at a path the store already
// declares: two siblings with one name make one path mean two nodes.
type AreaAlreadyDeclaredError struct{ Path string }

func (e *AreaAlreadyDeclaredError) Error() string {
	return fmt.Sprintf("this store already declares area %q", config.RenderAreaPath(e.Path))
}

// AreaParentUndeclaredError refuses a nested declaration whose parent the store
// does not declare. A parent is never created on the way.
//
// It is separate from AreaUndeclaredError because that one speaks about a node
// to act ON, where this speaks about one to nest UNDER, and the two have
// different remedies.
type AreaParentUndeclaredError struct {
	Path   string
	Parent string
}

func (e *AreaParentUndeclaredError) Error() string {
	return fmt.Sprintf("this store declares no area %q to nest %q under",
		config.RenderAreaPath(e.Parent), config.RenderAreaPath(e.Path))
}

// AreaMembersPresentError refuses retiring an area work is still assigned to
// with no disposition for that work: retiring it would leave every member
// carrying a path the vocabulary no longer declares, and every later write to
// them refused for it.
type AreaMembersPresentError struct {
	Path    string
	Members []string
}

func (e *AreaMembersPresentError) Error() string {
	return fmt.Sprintf("cannot retire area %q: %d nib(s) are assigned at or below it",
		config.RenderAreaPath(e.Path), len(e.Members))
}

// AreaDispositionEmptyError refuses a disposition that has nothing to act on.
// Letting it succeed would report members disposed of when there were none, and
// a silent no-op is the one answer a caller cannot tell apart from a real one.
//
// It is reachable two ways: by naming a disposition for an area nothing is
// assigned to, and by rerunning after a cascade completed and the config write
// did not. Dropping the disposition retires the area from either state, which is
// what both surfaces prescribe.
type AreaDispositionEmptyError struct {
	Path        string
	Disposition AreaDisposition
}

func (e *AreaDispositionEmptyError) Error() string {
	return fmt.Sprintf("no nib is assigned at or below area %q, so there is nothing to dispose of",
		config.RenderAreaPath(e.Path))
}

// AreaMoveTargetWithinError refuses reassigning members INTO the subtree being
// retired: the target is about to stop existing, so the move would leave that
// work carrying a path the vocabulary no longer declares — the state the member
// refusal exists to prevent, reached through the remedy for it.
type AreaMoveTargetWithinError struct {
	Target string
	Path   string
}

func (e *AreaMoveTargetWithinError) Error() string {
	return fmt.Sprintf("cannot move members to %q: it is declared at or below %q, which is being retired",
		config.RenderAreaPath(e.Target), config.RenderAreaPath(e.Path))
}

// AreaEditPhase is where in one area edit a filesystem failure landed. It is
// what selects the sentence a surface prints, because the phases differ in what
// is already on disk and therefore in whether a rerun is the repair.
type AreaEditPhase int

const (
	// AreaEditPhaseLock is the store's cross-process write lock; the two load
	// phases are the re-read under it, split because Load reads the vocabulary
	// and then walks the nibs and those are different files with different
	// repairs; AreaEditPhaseCascade is a member rewrite; AreaEditPhaseWrite the
	// areas.yml write; AreaEditPhaseReload the re-read of the file just written.
	AreaEditPhaseLock AreaEditPhase = iota
	AreaEditPhaseLoadVocabulary
	AreaEditPhaseLoadNibs
	AreaEditPhaseCascade
	AreaEditPhaseWrite
	AreaEditPhaseReload
)

// AreaEditIOError reports that an area vocabulary edit failed on the
// FILESYSTEM, as opposed to config.AreaEditRefusal, which is about the file's
// CONTENT. The two have different audiences and different repairs: a refusal is
// the caller's argument to fix and nothing about it is repaired by rerunning,
// where this one is the machine's and a rerun often is the repair. That is the
// split `nibs area rename` and `nibs area rm` report as exit 5 versus exit 2,
// and cmd/set.go's mutationErrCode maps this type so `nibs query` agrees.
//
// It implements NO Unwrap, and that is deliberate rather than an omission: the
// causes it carries are whatever the operating system and the config loader
// handed back, and exposing them to errors.Is would let any classifier keyed on
// a sentinel claim this error and answer for it. Cause stays inspectable as a
// field.
//
// Error() is a plain statement of the fields, carrying no prescription — a rerun
// is a `nibs area rm` to one caller and a mutation to another, and neither
// sentence belongs here. Each surface words its own from those fields. One that
// must ALSO stay classifiable — the GraphQL resolver, whose answer cmd/set.go
// maps to an exit status — wraps this error inside its sentence, which works
// because the wrapper unwraps to this type and this type unwraps to nothing.
type AreaEditIOError struct {
	Phase AreaEditPhase
	// Path is the area the verb was acting on, NewPath the one a rename was
	// producing, and Disposition what a retire was told to do with the members.
	Path        string
	NewPath     string
	Disposition AreaDisposition
	// File is the store's areas.yml, for a surface whose reader owns the
	// directory it sits in. Error never renders it, for the reason
	// config.AreaEditRefusal never renders its own: these sentences reach an
	// HTTP client that has no business knowing where the store is on disk.
	File string
	// Written is what the cascade had rewritten when the failure landed, and
	// Members the set it set out to rewrite. A partial-failure sentence needs
	// both: the first is what is persisted, the second what the run was for.
	Written []string
	Members []string
	Cause   error
}

func (e *AreaEditIOError) Error() string {
	return fmt.Sprintf("this area edit failed while %s: %v", e.Phase.describe(), e.Cause)
}

// describe names a phase for AreaEditIOError's own message. It is not a remedy
// and names no command.
//
// EVERY ARM IS NAMED and there is no default one, so a phase added to the iota
// block above is not silently described as the one that happens to sit last.
// Each sentence here says what is already on disk, which is what decides whether
// a rerun is the repair — lending one phase's sentence to another is a wrong
// answer to that question, in the one error whose stated purpose is telling the
// phases apart.
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
	case AreaEditPhaseWrite:
		return "writing the store's areas.yml"
	case AreaEditPhaseReload:
		return "re-reading the areas.yml it had just written"
	}
	return fmt.Sprintf("in a phase of the edit this build cannot name (%d)", int(p))
}

// reloadAreasAfterEdit is Core.loadAreasLocked, indirected so a test can drive the
// one IO phase the filesystem will not produce on demand: the edit's own re-read
// of the file it has just written failing. Every other phase has a real fault to
// inject — fsutil.RenameFn for the two writes, an unreadable store for the
// re-read — and this one has none, because the bytes that were just written are
// the bytes that are read back. It follows fsutil.RenameFn's shape: a seam owned
// by the package that declares it.
var reloadAreasAfterEdit = (*Core).loadAreasLocked

// AddArea declares a new area at path, with the description and color it is
// given, and returns the vocabulary as it then stands.
//
// It rewrites no nib, and the reason is narrower than "a new area has no
// members": a nib may already CARRY the path, left on it by a retire or a hand
// edit, and every write to that nib is refused until the vocabulary declares it
// again. Declaring it IS that repair, and the repair rewrites nothing.
//
// The path's SHAPE is the caller's to judge before calling — config.ValidateNewAreaPath
// and config.ValidateAreaColor answer from the arguments alone, so asking them
// out here is what keeps a typo from sitting silent behind another writer's lock.
// The planner asks them again regardless.
func (c *Core) AddArea(path, description, color string) (AreaEditResult, error) {
	parent, _ := splitAreaPath(path)
	return c.editArea(path, func(before, now *config.Areas) (areaPlan, error) {
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
// A rename is a NAME edit and never a move: the parent segments carry over
// verbatim, and a member assigned BELOW the renamed node keeps the remainder it
// carried, because renaming a parent moves its children's paths without changing
// their names.
func (c *Core) RenameArea(path, newName string) (AreaEditResult, error) {
	return c.editArea(path, func(before, now *config.Areas) (areaPlan, error) {
		if err := requireDeclaredArea(before, now, path, AreaPathRenamed); err != nil {
			return areaPlan{}, err
		}
		parent, oldName := splitAreaPath(path)
		// Asked after the path is known to be declared because it SPEAKS about
		// the node at path: over an undeclared one it would assert something
		// about a node that is not there.
		if newName == oldName {
			return areaPlan{}, &AreaNameUnchangedError{Path: path, Name: newName}
		}
		if sibling := joinAreaPath(parent, newName); now.IsValid(sibling) {
			return areaPlan{}, &AreaNameTakenError{Path: path, NewName: newName, Sibling: sibling}
		}

		// The config edit is resolved BEFORE the first nib is touched. A member
		// rewrite is durable the moment it lands, so a refusal that could only
		// fire after the cascade would leave the members carrying a path the
		// vocabulary does not declare, and every later write to them refused for
		// it. Planning first moves every refusal the editor can make to before
		// that point, where it leaves the store untouched.
		edit, err := config.PlanRenameStoredArea(now.StoreDir(), path, newName)
		if err != nil {
			return areaPlan{}, err
		}

		newPath := joinAreaPath(parent, newName)
		return areaPlan{
			path:    path,
			newPath: newPath,
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
// Every member lands ON a move target rather than keeping the remainder it
// carried below the retiring node: the target declares no such child, so
// preserving it would move each member to another undeclared path.
func (c *Core) RemoveArea(path string, disposition AreaDisposition) (AreaEditResult, error) {
	return c.editArea(path, func(before, now *config.Areas) (areaPlan, error) {
		if err := requireDeclaredArea(before, now, path, AreaPathRetired); err != nil {
			return areaPlan{}, err
		}

		// BOTH questions about a move target are asked of the vocabulary re-read
		// under the lock, because the target is exactly as perishable as the node
		// being retired: a concurrent retire of the target finishing while this
		// edit waited leaves it declared in any earlier snapshot and gone from the
		// store, and the reassignment then walks every member into the state the
		// member refusal exists to prevent.
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

		// MoveTo is "" for an unassign, which is the legal cleared value.
		target := disposition.MoveTo
		return areaPlan{
			path:          path,
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
	path          string
	newPath       string
	edit          *config.StoredAreaEdit
	members       []string
	disposition   AreaDisposition
	declaredBelow int
	// rewrite claims the members this verb cascades through. Nil for a verb that
	// cascades nothing.
	rewrite func(area string) (string, bool)
}

// editArea is the one critical section every area verb runs in: c.mu, then the
// store's cross-process write lock, then the re-read under both, then plan,
// cascade, write and reload — with both locks held throughout and released only
// on the way out.
//
// THE LOCK ORDER IS THE POINT. acquireWriteLock documents it as always c.mu then
// the file lock, and every other Core mutator obeys it; taking them the other way
// round anywhere deadlocks the process against an ordinary concurrent Update.
//
// BOTH HALVES OF THE EDIT ARE ONE CRITICAL SECTION, and that is the point too.
// The member cascade and the `areas:` rewrite are read-modify-writes of shared
// state, and the second rewrites the whole file. Split across two critical
// sections, two concurrent edits interleave: each reads the pre-edit config, each
// writes the whole file back, and the loser's declaration is gone while its
// cascade sits on disk. Both callers report success, so nothing ever says to
// rerun, and the members it moved are write-refused from then on.
//
// It BLOCKS on the file lock rather than refusing, matching every other store
// mutation: the other holder is another nibs process finishing one operation.
//
// THE COST IS READER AVAILABILITY, and it is paid deliberately. c.mu is held
// from before the file lock is asked for until after the reload, so it spans an
// untimed wait for another process, a walk of every nib file in the store, the
// member cascade, the whole-file config write and the re-read. Get, All and
// Search all read under c.mu, and a GraphQL query resolver reaches the store
// through them, so under `nibs serve` a read blocks for the length of the edit —
// a step beyond the single-nib mutators, none of which walks the store or
// rewrites a config file under this lock. That
// is the price of the paragraph above: narrowing the span is what would let two
// edits interleave and lose one's declaration. Whether the availability can be
// recovered without giving that up is nibs-8465.
//
// THE RE-READ IS WHY BLOCKING IS SAFE. Waiting means another process was
// mid-write while this one held the state it started with. A concurrent
// `nibs config set-prefix` renames every file in the store, so a cascade over the
// old paths would write each member back under its pre-rename name; and a
// concurrent area edit reshapes the very tree the membership question is asked
// over, so a member sitting on a path this process never loaded would answer "not
// a member" and be left on it after the node above it is renamed or retired away.
// Reading the store again HERE is what makes both ordinary events.
//
// The vocabulary is the ONE part of a store's configuration a reload may replace,
// and it is replaced here: it lives behind an atomic pointer precisely so a
// reload cannot race the off-lock readers. Everything in config.yml is left alone
// and stays fixed at construction.
//
// A failure to re-read is a REFUSAL rather than a fallback to what was already
// loaded: an area edit is a cascading rewrite of the store, planning it is about
// to read that very file anyway, and refusing here leaves the store untouched
// where a fallback would decide from state with no evidence it is still current.
func (c *Core) editArea(path string, plan func(before, now *config.Areas) (areaPlan, error)) (AreaEditResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	before := c.Areas()

	release, err := c.acquireWriteLock()
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

	staleLink, err := p.edit.Write()
	if err != nil {
		return AreaEditResult{}, &AreaEditIOError{
			Phase: AreaEditPhaseWrite, Path: p.path, NewPath: p.newPath, File: now.Path(),
			Disposition: p.disposition, Written: written, Members: p.members, Cause: err,
		}
	}

	// Re-read under the same lock so the result carries the vocabulary this edit
	// wrote, and so an areas subscriber in this process wakes on the edit rather
	// than on the watcher's debounce. loadAreasLocked keeps the vocabulary it could
	// still read when the file cannot be read back, which is the one the edit
	// replaced — so ignoring the failure would answer with the pre-edit
	// vocabulary and call the edit a success. The subscriber tick it makes takes
	// subMu under c.mu, which is the established order.
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
// telling apart a path that WAS declared when this store was last read — which
// another process retired or renamed while this edit waited for the lock — from
// one that never existed.
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
// id order — the set a retire refuses over and a rename cascades through, read
// the same way rewriteAreaAssignmentsLocked reads it.
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

// splitAreaPath separates a path into the path of its parent (empty at the top
// level) and the node's own name — the half a rename replaces. joinAreaPath is
// its inverse.
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
