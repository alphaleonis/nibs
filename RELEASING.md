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

**Requires GitHub CLI 2.49 or newer** — the `attestation` subcommand does not exist before that, and older versions fail with `unknown command "attestation"` rather than anything resembling a verification failure.

It exits `0` on success and non-zero on failure, so it is safe to use in a script — **but check the exit code directly rather than piping it into something else.** A pipeline reports only its last command's status, which is how rclone's published verification one-liner came to report success on a bad signature ([rclone#8024](https://github.com/rclone/rclone/issues/8024)). Use `&&`, or test `$?`.

Checking the exit code is not optional advice here: **on success outside a terminal the command prints nothing at all.** The human-readable summary is TTY-only, so a script that judges success by output rather than status sees the same empty output for "verified" as it would for a command that never ran. Add `--format json` when you want the details programmatically — it reports the source repository, the workflow that built the artifact, and the signing identity.

A modified artifact fails as `HTTP 404: Not Found` on the attestations lookup rather than as a signature error. That is the expected shape: attestations are indexed by artifact digest, so altering a single byte means no attestation exists for it. Exit status is still non-zero.

This is anchored in Sigstore and GitHub's OIDC identity rather than a key this project holds, so there is no public key to distribute and nothing to rotate.

### The signed checksum file

Releases also publish `checksums.txt.sig`, a detached Ed25519 signature over `checksums.txt`, produced in the release job from a key held in the `release` environment. Verify it with the matching public key from `internal/signing/keys/`:

```bash
openssl pkeyutl -verify -pubin -inkey nibs-signing-1.pub \
  -rawin -in checksums.txt -sigfile checksums.txt.sig
```

Unlike the attestation, this anchor is a key compiled into every nibs binary, which is what lets `nibs upgrade` check it without contacting an external service.

**`nibs upgrade` does not require this signature yet.** It still verifies `checksums.txt` alone. The signature is being published first, on its own, so that at least one release has produced a valid one before any binary depends on it — otherwise the first release where signing matters would also be the first where it had ever run, and a release that failed to sign would strand every user on the version demanding a signature.

### Rotating the signing key

Three keypairs were generated up front and all three public keys ship in every binary; **key 1 is currently active**. A binary can only verify against keys it already carries, so this headroom cannot be extended later — a fourth key would be invisible to everything already installed.

To rotate, replace the `NIBS_SIGNING_KEY` secret in the `release` environment with the next private key and update the note above. No client change is needed: the verifier accepts a signature from any embedded key.

The release job checks this for you before it tags or publishes anything:

```bash
NIBS_SIGNING_KEY="$(cat nibs-signing-2.key)" go run ./internal/signing/cmd/verify -check-key
```

If the secret is ever set to a key whose public half is not embedded, that step fails the release rather than publishing a signature no binary can verify. Run it yourself after rotating if you want confirmation without dispatching a release — it prints only a yes/no and never the key.

Rotation limits future exposure; it cannot un-trust a stolen key on binaries already shipped, because revocation has no way to reach them.

## Local testing

To test the release build locally:

`go-licenses` and `goreleaser` are pinned in `mise.toml`, so `mise install`
provides both at the same versions the release job uses — there is nothing to
install by hand.

```bash
mise install

# Build what the archives need: generated code, web assets, license notices
task release:prep

# Dry-run GoReleaser
goreleaser release --snapshot --clean --skip=sign
```

`task release:prep` is required, and is not merely a convenience: `.goreleaser.yaml`
deliberately has **no `before.hooks`**, because before-hooks inherit the environment
of the goreleaser process — which in CI holds the signing key. Running `npm ci` there
would expose the key to install scripts from every package in the web dependency tree.
Forgetting the prep step fails loudly rather than producing a broken archive
(`embed.go:5:12: pattern all:web/dist: no matching files found`).

`--skip=sign` is not optional locally: the release signs `checksums.txt` with the
Ed25519 key held in the `release` environment, and there is no key on a developer
machine. Without the flag the dry run fails at the signing stage — deliberately,
because a release that cannot sign must fail rather than publish unsigned.

Run `goreleaser` through `mise exec -- goreleaser …` if mise's shims are not on
your PATH. Note the dry run executes `.goreleaser.yaml`'s `before` hooks, which
include `cd web && npm ci` — so do not start it while a `task` run is
installing (see CLAUDE.md).
