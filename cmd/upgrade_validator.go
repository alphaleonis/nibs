package cmd

import (
	"github.com/creativeprojects/go-selfupdate"

	"github.com/alphaleonis/nibs/internal/signing"
)

// signatureValidator adapts internal/signing to go-selfupdate's Validator
// interface.
//
// The adapter lives here rather than in internal/signing so that package stays
// free of the go-selfupdate dependency: it is the thing a released binary
// trusts, and the fewer things it pulls in the better. Notably go-selfupdate
// links golang.org/x/crypto/openpgp, which the Go team deprecated as unsafe and
// unmaintained (GO-2026-5932) — internal/signing deliberately touches none of it.
type signatureValidator struct {
	verifier *signing.Verifier
}

// Validate checks the detached signature over the released file. `release` is
// the file's bytes and `asset` is the signature asset go-selfupdate fetched
// using the name GetValidationAssetName returned.
func (s signatureValidator) Validate(_ string, release, asset []byte) error {
	return s.verifier.Verify(release, asset)
}

// GetValidationAssetName names the detached signature to fetch alongside the
// file, matching what .goreleaser.yaml's `signs` stage publishes
// (`signature: "${artifact}.sig"`).
func (s signatureValidator) GetValidationAssetName(releaseFilename string) string {
	return releaseFilename + ".sig"
}

// newUpgradeValidator builds the validation chain `nibs upgrade` runs against a
// release:
//
//	archive        -> its digest must appear in checksums.txt
//	checksums.txt  -> must carry a signature from a key compiled into this binary
//	*.sig          -> not validated (it is the signature; validating it would loop)
//
// The composition is what makes the signature meaningful. checksums.txt ships in
// the same release as the archives it vouches for, so on its own it proves only
// that the download was not corrupted in transit — anyone able to write the
// release could rewrite both. The signing key is not in the release, so a valid
// signature is an anchor the release itself cannot forge.
//
// Order matters: PatternValidator returns the FIRST matching pattern, so the
// catch-all "*" must be added last. SkipValidation moves its rule to the front
// regardless, so *.sig is matched before "*" whatever the call order.
//
// Consequence worth knowing: go-selfupdate fetches the validation asset during
// DETECTION, so a release published without a `checksums.txt.sig` is not merely
// rejected — it is not detected at all. Every release from this point on must be
// signed, which is why the release workflow fails rather than publishing
// unsigned, and verifies the signature against these same keys before finishing.
// It also means `--version` cannot reach a release predating signing.
func newUpgradeValidator() (selfupdate.Validator, error) {
	verifier, err := signing.NewVerifier()
	if err != nil {
		return nil, err
	}
	return new(selfupdate.PatternValidator).
		Add("checksums.txt", signatureValidator{verifier: verifier}).
		SkipValidation("*.sig").
		Add("*", &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"}), nil
}
