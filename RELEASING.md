# Releasing

Releases are built by a GitHub Actions workflow and published as draft GitHub Releases.

## Prerequisites

- Push access to `main`
- CI green on the `main` commit being released — the workflow refuses to tag a
  commit whose latest CI run has not completed successfully
- A changelog entry for the version being released (stable releases only)

## Steps

### 1. Update the changelog

Move items from `## Unreleased` into a new version section in `CHANGELOG.md`:

```markdown
## v0.1.0

### Added
- ...
```

Commit this to `main` and push.

### 2. Trigger the release workflow

Go to **Actions > Release > Run workflow** on GitHub, keep **Use workflow from**
set to `main`, and enter the version. Accepted formats:

- `v0.1.0` — stable release (requires changelog entry)
- `v0.1.0-alpha.1`, `v0.1.0-rc.1` — pre-release (changelog not required)

The workflow will:

1. Refuse to run unless dispatched from `main` and the latest CI run for that
   commit completed successfully (applies to pre-releases too)
2. Validate the version format
3. Verify a matching changelog entry exists (stable releases only)
4. Extract release notes from the changelog, or generate a placeholder for pre-releases
5. Create and push a git tag
6. Build binaries for Linux, macOS, and Windows (amd64 + arm64) via GoReleaser
7. Collect third-party dependency licenses via `go-licenses`
8. Create a **draft** GitHub Release with the binaries and license notices

### 3. Review and publish

The release is created as a draft. Review the release notes and attached archives on the [Releases page](https://github.com/alphaleonis/nibs/releases), then click **Publish**.

## What's in the archives

Each release archive contains:

- `nibs` binary (or `nibs.exe` on Windows)
- `LICENSE.md` (project license, Apache 2.0, with upstream attribution)
- `third_party_licenses/` directory with license files for all dependencies

## Verifying a download

Every release asset carries a [build provenance attestation](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations/using-artifact-attestations-to-establish-provenance-for-builds) — a signed statement that those exact bytes were built by this repository's release workflow, at a specific commit. Verify a downloaded asset with:

```bash
gh attestation verify nibs_linux_amd64.tar.gz --repo alphaleonis/nibs
```

It exits `0` on success and non-zero on failure, so it is safe to use in a script — **but check the exit code directly rather than piping it into something else.** A pipeline reports only its last command's status, which is how rclone's published verification one-liner came to report success on a bad signature ([rclone#8024](https://github.com/rclone/rclone/issues/8024)). Use `&&`, or test `$?`.

This is anchored in Sigstore and GitHub's OIDC identity rather than a key this project holds, so there is no public key to distribute and nothing to rotate.

What it does **not** cover: `nibs upgrade`. That path verifies `checksums.txt`, which lives in the same release as the archives it vouches for, so it detects corruption in transit but not a compromised release. Closing that is tracked separately.

## Local testing

To test the release build locally:

`go-licenses` and `goreleaser` are pinned in `mise.toml`, so `mise install`
provides both at the same versions the release job uses — there is nothing to
install by hand.

```bash
mise install

# Collect licenses
task licenses

# Dry-run GoReleaser
goreleaser release --snapshot --clean
```

Run `goreleaser` through `mise exec -- goreleaser …` if mise's shims are not on
your PATH. Note the dry run executes `.goreleaser.yaml`'s `before` hooks, which
include `cd web && npm ci` — so do not start it while a `task` run is
installing (see CLAUDE.md).
