---
name: cleanup
description: Use when asked to clean up, trim, shorten or reduce comments in one or more source files, or when comment volume in a file is called excessive
---

# cleanup

Trimming is the easy half and agents already do it well. **A pass that only
shortens preserves every false claim in the file, in a shorter and more confident
form.**

Measured on this repo. Two agents cleaned the same file from the same bytes,
against nine faults verified in advance:

| | comment lines | faults found | faults missed |
|---|---|---|---|
| trim only | 320 → 218 | **1 of 9** | 6 |
| this procedure | 320 → 222 | **4 of 9**, plus 2 more nobody had found | 2 |

Identical volume. The difference is entirely in what got checked. One survivor
shows why: the trimming agent *condensed* a claim by a clause and kept the false
part — "a vocabulary that declares anything reaches here with `areas:` already a
literal sequence", which a `!!binary`-keyed file disproves.

Comments are the only artifact here with no compiler and no test behind them.

## Five passes. The order is not negotiable.

Trimming first is what produces a clean-looking file full of false statements.

### 1. Verify — before editing anything

Read every comment as a **claim**. Check the checkable ones. Rank by how far the
claim reaches: of 18 faults found by hand, 6 described something outside the
declaration they sat on, and nothing recompiles when the far end moves.

| priority | claim shape | how to check |
|---|---|---|
| 1 | names a symbol (`Foo.Bar`, `pkg.Thing`) | **`.claude/skills/cleanup/check-symbol.sh Bar internal/ cmd/`** — do not hand-roll the grep; three hand-written patterns produced false phantoms while this skill was written |
| 2 | counts or quantifies call sites ("two callers", "every reader goes through it") | grep, then **read the matched lines** |
| 3 | describes another package's behavior, output or CLI text | open that file; run that command |
| 4 | says a test or guard enforces something | open it — does it assert that, or something adjacent? |
| 5 | gives a worked example | trace it; an example is the claim least likely to have been checked |
| 6 | claims siblings "defer here rather than re-derive" | grep the named sites — 2 of 4 such claims in this repo were false |
| 7 | describes a **convention** ("this is how X refers to Y") | check every site that should follow it; a const's own doc described a convention one of its two call sites ignored, and all three test runs missed it |

Keep two lists: **faults**, and **facts you established**. The second list is not
bookkeeping — pass 2 needs it.

### 2. Reconcile — walk each verified fact back across the whole file

A fact you confirm in one place refutes claims **elsewhere**, and they will not
find you. In the measured run the agent proved that a `!!binary` key decodes into
the `areas` field while `mappingValueNode` misses it — then left a comment 300
lines away asserting the opposite, untouched.

For every fact on your list, search the file for other comments touching the same
mechanism, and ask whether each still holds. This pass finds what pass 1
structurally cannot.

### 3. Trim what survived — one declaration at a time

Do not sweep the file. Take each declaration in turn and make **that** comment
justify itself before moving on; a sweep applies the tests shallowly to
everything. The manual pass this skill is drawn from reached 115 comment lines
from 320 by working one declaration at a time; a four-pass sweep of the same file
stopped at 234.

Apply in order to each comment. Stop at the first that fires.

1. **Does the identifier already say it?** Delete, don't reword.
2. **Does the adjacent code already say it?** The `if` above, the named return,
   the error message two lines down.
3. **Is it enforced by a message the user reads?** That message is the canonical
   explanation; the declaration states the rule, not the reason.
4. **What wrong edit does it warn against?** No answer → delete. This is the test,
   not "does it sound essential".
5. **What does the reader DO differently?** No answer → it describes rather than
   instructs, and the sentence that should be first is missing.
6. **Could the case it distinguishes actually occur?** A comment ruling out what a
   validator makes impossible guards nothing.
7. **Is it provenance or history?** How a value got into a bad state is not what
   to do about it.

Then, of the whole declaration: **what is this thing, stated from the code?** An
empty opening sentence survives any number of trims, because each pass edits
*around* it.

### 4. Register — cut the clause that argues

Content trimming leaves sentences that are correct and still one clause too long.
Each surviving comment gets read once more for **voice**: a comment states a
constraint; it does not argue for it, defend it, or explain the reasoning that led
to it. On the file this skill was drawn from, this pass alone took 187 lines to
124 — more than any other single step.

Cut, in every surviving comment:

- **The argument.** "the bluntness is the point", "that order is the whole point",
  "deliberately", "rather than X, which would…" — the constraint stays, the case
  for it goes.
- **The actor-less sentence.** "is what one edit needs of a store" — nobody does
  anything. Rewrite with a subject, or delete.
- **Mood.** "owes it", "must account for", "has to be safe" — say what to do.
- **The comparative with no baseline.** "strict", "safer", "cheaper" — compared to
  what? Usually an alternative nobody proposed.
- **The trailing clause.** "…so that", "…which is what lets", "…because" after a
  constraint that was already complete.

```go
// The caller owes it the store's cross-process write lock
// (nibcore.AcquireStoreLock) across both steps, or a concurrent editor's write
// is lost when this one lands.
→
// Hold the store's write lock (nibcore.AcquireStoreLock) across both steps.
```
```go
// It is asked of the WHOLE document rather than of the `areas:` subtree, and
// the bluntness is the point: deciding whether a particular anchor "actually
// affects areas" is the reasoning it exists to replace.
→
// It searches the whole document, not the `areas:` subtree.
```
```go
// Renaming is a NAME edit, not a move, so the caller supplies a bare name and
// this never re-parents anything.
→
// newName is a bare name; nothing is re-parented.
```

The register pass rewrites sentences, so it is where a load-bearing word gets
lost: `resolves a rename` → `renames`. Every sentence it touches goes through
pass 5.

### 5. Re-read cold — one trim pass is never enough

Two jobs, both on the file as it now stands:

- **Check what you rewrote.** Rewriting introduces faults. Trimming keeps the
  short, confident fragment and drops the substance: `resolves a rename` →
  `renames` (the function writes nothing); a label kept after its defining clauses
  were cut. For a comment shaped `LABEL: definition`, cut the label and keep the
  definition, or cut both — never keep the label alone.
- **Trim again.** A first sweep with every rule in hand reached 36.2%; re-reading
  against one question took it to 33.1%, and later passes to 22.2%. The first pass
  edits around things it should edit through.

## Keeping a block

A block may be kept. What it may not be is kept *silently*: **every surviving
block over 12 lines is listed in your report, with the specific test above it
passed.** One line each.

This is not paperwork. A run that stopped at a 40-line block wrote "kept
deliberately — it is the canonical statement", and that sentence is a licence, not
a reason. Eight of them in a list is not writable if they are not true.

Do not target a ratio either. Files here landed at 22–24%; a concurrency file
correctly stayed at 45%. Stop when you run out of comments that fail the tests —
but if you are about to stop with blocks over 12 lines, write the list first and
see whether you still can.

## Calibration

Real before/after pairs from this repo. The target register is flat and finished
— a constraint, not an argument for it.

```go
// File is the config file the refusal is about, empty for one that judges
// an argument alone.
→
// The config file the error relates to. May be empty.
```
```go
// AreaEditRefusal is a refusal about the config file's CONTENT — a vocabulary
// declared in a shape these edits cannot address, or a result the loader would
// reject — as opposed to a failure to read or write the file.
// [+ 11 more lines: why the two are separated, and a claimed "mechanism"]
→
// AreaEditRefusal is a refusal about the config file's content, not a failure to
// read or write it. Report as a validation error; retrying will not help.
```
```go
// Paths returns every declared area path in DECLARATION order, a parent
// immediately before the subtree it heads. The order is the file's own, which
// is what lets a project read its vocabulary back the way it wrote it.
→
// Paths returns every declared area path in DECLARATION order, a parent
// immediately before the subtree it heads.
```
```go
// List RENDERS every declared area path as a comma-separated list, for the
// messages that have to say what the vocabulary holds: each path goes through
// safetext.Strip, the count and each path's length bounded, with the elision
// stated. Display text, not data: use Paths where the values are wanted.
→
// List renders the declared paths as one comma-separated string for a message.
// Display text, not data — bounded and elided, so use Paths for the values.
```

The cuts, in order: an explanation the identifier already gives; a paragraph
defending a design; a fact restated as gloss and consequence; a description of
what a function it calls does internally.

## Structure

- A fact about one field goes **on that field** (Go: trailing `// note` for short,
  above-field for a sentence). Delete first, *then* redistribute — a sentence
  sitting on the right declaration looks justified by its placement.
- A property must sit on the declaration that **establishes** it. If a reader
  believed the sentence and looked directly below, would they find the thing?
- Never describe what a function you call does internally, unless that is your own
  contract.
- Rewrite an unenforceable universal as an instruction: "every reader goes through
  it" → "read through this". Same guidance, no claim to rot.

## Required output

All five. A report without the fault list is an incomplete run, however good the
trim.

1. **Comment lines before → after**, and the largest remaining block.
2. **Faults found** — location, the claim, the evidence it is false. Say "none
   found" explicitly; do not omit the slot.
3. **Claims verified and kept** — what you checked that held. This is what
   separates a verified file from an unexamined one.
4. **Gates run**, with output: formatter, linter, the project's test runner.
5. **Blocks over 12 lines that survived**, one line each, naming the test each passed.

Code defects found while verifying are **reported, not fixed**, unless asked.

## Red flags

- "Every factual claim is a condensation of one already in the file." This is the
  failure, verbatim, from the run that found 1 of 9.
- Verifying a mechanism and not searching the file for what it refutes.
- Calling a grep's silence a result. Prove the pattern works on a symbol you know
  exists — `grep "DefaultType ="` missed a gofmt-aligned `DefaultType     = "task"`
  and put a phantom into this skill as its own motivating example.
- Finishing with no fault list and no "none found".
