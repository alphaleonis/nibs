# Releasing

Releases are built by a GitHub Actions workflow and published as draft GitHub Releases.

## Prerequisites

- Push access to `main`
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

Go to **Actions > Release > Run workflow** on GitHub and enter the version. Accepted formats:

- `v0.1.0` — stable release (requires changelog entry)
- `v0.1.0-alpha.1`, `v0.1.0-rc.1` — pre-release (changelog not required)

The workflow will:

1. Validate the version format
2. Verify a matching changelog entry exists (stable releases only)
3. Extract release notes from the changelog, or generate a placeholder for pre-releases
4. Create and push a git tag
5. Build binaries for Linux, macOS, and Windows (amd64 + arm64) via GoReleaser
6. Collect third-party dependency licenses via `go-licenses`
7. Create a **draft** GitHub Release with the binaries and license notices

### 3. Review and publish

The release is created as a draft. Review the release notes and attached archives on the [Releases page](https://github.com/alphaleonis/nibs/releases), then click **Publish**.

## What's in the archives

Each release archive contains:

- `nibs` binary (or `nibs.exe` on Windows)
- `LICENSE.md` (project license, Apache 2.0, with upstream attribution)
- `third_party_licenses/` directory with license files for all dependencies

## Local testing

To test the release build locally:

```bash
# Collect licenses (requires go-licenses)
go install github.com/google/go-licenses@latest
mise licenses

# Dry-run GoReleaser
goreleaser release --snapshot --clean
```
