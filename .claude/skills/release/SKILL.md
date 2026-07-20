---
name: release
description: Cut a nibs release end-to-end — author and scrub the changelog, merge to main, wait for green CI, dispatch the gated Release workflow, verify and revise the draft, then stop for a human to publish. User-invoked only.
invoke-by: user
---

You are cutting a nibs release. `RELEASING.md` is the source of truth; follow the steps
below **in order**. This drives the gated **Release** workflow and **stops at a draft** —
a human reviews and clicks Publish.

If a version was passed as an argument (e.g. `/release v0.7.0`), use it: `$ARGUMENTS`.
If none was given, propose one in step 1.

## Hard rules — do not violate

- **Release only from `main` with green CI.** Never dispatch the workflow until CI has
  concluded `success` for the exact release commit. `release.yml` enforces this at the
  gate, but verify it yourself first — do not lean on the gate as your only check.
- **The public changelog must not reference private nibs.** Strip every `(Refs: nibs-…)`
  trailer and any `nibs-<id>` from `CHANGELOG.md` before releasing. The changelog and the
  GitHub release notes are public; nib work items are not.
- **Stop at the draft. Never auto-publish** — no `gh release edit --draft=false`, no
  `gh release create`. Publishing is the human's decision.
- **Halt on any error.** If a `git` / `gh` / CI / workflow step fails, stop and report the
  root cause. Never retry blindly or force past a red gate.

## Steps

### 1. Version
If a version argument was given (`vX.Y.Z` or a pre-release like `vX.Y.Z-rc.1`), use it.
Otherwise read the latest tag (`git tag --sort=-v:refname | head -1`) and the
`## [Unreleased]` section of `CHANGELOG.md`, propose the next semver bump, and **confirm
with the user** before continuing. A version containing `-` is a pre-release.

### 2. Sync check
`git fetch` (and `git -C .nibs fetch`). Ensure the working tree is clean and `main` /
`develop` are not behind their remotes. If dirty or behind, **stop and ask** — do not
auto-stash, auto-rebase, or build on a stale base.

### 3. Changelog
Stable releases need a `## <version>` section in `CHANGELOG.md` with real, user-facing
entries (Keep a Changelog: `### Added` / `### Changed` / `### Fixed`). If the content sits
under `## [Unreleased]`, move it into a dated `## <version> - <YYYY-MM-DD>` section.

Then **scrub private nib references** from the whole file:

    sed -E -i 's/ *\(Refs:[^)]*\)//g' CHANGELOG.md

and confirm only benign tokens remain (`grep -nE 'nibs-[a-z0-9]{4}' CHANGELOG.md` should
show only CLI flags like `--nibs-path` or `#id` syntax examples, never a work-item ref).

Verify with the workflow's own logic before proceeding:

    grep -q "^## <version>" CHANGELOG.md                                   # entry exists
    awk '/^## <version>/{f=1;next} /^## /{if(f)exit} f' CHANGELOG.md | wc -c  # notes non-empty

Pre-releases skip the changelog-entry requirement (the workflow generates a placeholder).

### 4. Merge to `main`
Releases cut from `main`. Merge `develop` into it and commit the changelog there:

    git switch main && git merge --ff-only origin/main
    git merge --no-ff develop -m "Merge develop into main: <summary>"
    # edit / commit CHANGELOG.md on main

Resolve any conflict carefully; **stop if unsure**.

### 5. Push `main` and wait for green CI
`git push origin main`, then wait for CI on the tip commit to conclude:

    SHA=$(git rev-parse HEAD)
    RUN=$(gh run list --workflow=ci.yml --branch main --json headSha,databaseId \
      --jq "[.[] | select(.headSha==\"$SHA\")] | first | .databaseId")
    gh run watch "$RUN" --exit-status

If CI is anything but `success`, **stop** — do not release from a red or unverified commit.

### 6. Dispatch the release
Confirm the tag is free (`git tag -l <version>`), then:

    gh workflow run release.yml --ref main -f version=<version>

Find the new run (`gh run list --workflow=release.yml --limit 1 --json databaseId`) and
`gh run watch <id> --exit-status`. If it fails at the gate or any step, stop and report.

### 7. Verify the draft

    gh release view <version> --json isDraft,body,assets

Confirm: `isDraft=true`; the body (notes) is non-empty and matches the changelog entry;
all six platform archives (`nibs_{darwin,linux,windows}_{amd64,arm64}`) plus
`checksums.txt` are attached (7 assets).

### 8. Revise the release notes — required, do not skip
Review the draft's rendered notes against our standards: Keep a Changelog structure, clear
user-facing phrasing, American English, and **no private nib references**. If anything
needs improving, edit the `CHANGELOG.md` entry (the source of truth), re-extract the notes,
and re-apply them to the draft:

    gh release edit <version> --notes-file <notes-file>

Always do this pass before handing off.

### 9. Stop for human publish
Report the draft's URL and a short summary of what was released. **Do not publish** — tell
the user to review the notes and archives and click Publish.

### 10. Housekeeping
Fast-forward `develop` to the released commit so the next work isn't on a stale base:

    git switch develop && git merge --ff-only main && git push origin develop
