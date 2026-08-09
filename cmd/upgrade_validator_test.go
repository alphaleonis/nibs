package cmd

import (
	"crypto/ed25519"
	"testing"

	"github.com/creativeprojects/go-selfupdate"

	"github.com/alphaleonis/nibs/internal/signing"
)

// TestUpgradeValidatorRoutesEachAssetKind pins the composition, which is where
// this is easy to get wrong: PatternValidator returns the FIRST matching
// pattern, so a catch-all added too early would silently send checksums.txt to
// the ChecksumValidator (asking it to verify itself) and the signature to a
// validator that would try to fetch a signature for the signature.
func TestUpgradeValidatorRoutesEachAssetKind(t *testing.T) {
	v, err := newUpgradeValidator()
	if err != nil {
		t.Fatalf("newUpgradeValidator: %v", err)
	}

	tests := []struct {
		name          string
		filename      string
		wantAssetName string
	}{
		// An archive is verified by digest against checksums.txt.
		{"archive", "nibs_linux_amd64.tar.gz", "checksums.txt"},
		{"windows archive", "nibs_windows_amd64.zip", "checksums.txt"},
		// checksums.txt is verified by its detached signature.
		{"checksum file", "checksums.txt", "checksums.txt.sig"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := v.GetValidationAssetName(tt.filename); got != tt.wantAssetName {
				t.Errorf("GetValidationAssetName(%q) = %q, want %q", tt.filename, got, tt.wantAssetName)
			}
		})
	}
}

// TestUpgradeValidatorSkipsTheSignatureItself guards the loop: the signature
// must not itself require a signature, or validation never terminates.
func TestUpgradeValidatorSkipsTheSignatureItself(t *testing.T) {
	v, err := newUpgradeValidator()
	if err != nil {
		t.Fatalf("newUpgradeValidator: %v", err)
	}

	// Skipped entries validate trivially whatever bytes they are handed.
	if err := v.Validate("checksums.txt.sig", []byte("anything"), nil); err != nil {
		t.Errorf("Validate(checksums.txt.sig) = %v, want nil (skipped)", err)
	}

	rv, ok := v.(selfupdate.RecursiveValidator)
	if !ok {
		t.Fatal("validator is not a RecursiveValidator; the chain cannot recurse")
	}
	if rv.MustContinueValidation("checksums.txt.sig") {
		t.Error("MustContinueValidation(checksums.txt.sig) = true — validation would recurse forever")
	}
	// ...but it MUST continue for checksums.txt, or the signature is never checked.
	if !rv.MustContinueValidation("checksums.txt") {
		t.Error("MustContinueValidation(checksums.txt) = false — the signature would never be verified")
	}
}

// TestSignatureValidatorRejectsUntrustedAndTampered is the security assertion:
// only a signature from a key this binary carries may pass.
func TestSignatureValidatorRejectsUntrustedAndTampered(t *testing.T) {
	verifier, err := signing.NewVerifier()
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	sv := signatureValidator{verifier: verifier}

	content := []byte("d34db33f  nibs_linux_amd64.tar.gz\n")
	_, untrusted, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	tests := []struct {
		name    string
		message []byte
		sig     []byte
	}{
		// The case that matters: a release signed by someone else's key.
		{"signature from an untrusted key", content, ed25519.Sign(untrusted, content)},
		{"tampered checksums", append(append([]byte{}, content...), '!'), ed25519.Sign(untrusted, content)},
		{"empty signature", content, nil},
		{"garbage signature", content, []byte("not a signature")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := sv.Validate("checksums.txt", tt.message, tt.sig); err == nil {
				t.Error("Validate accepted, want rejection")
			}
		})
	}
}

// TestUpgradeValidatorCarriesTheShippedKeys guards against the chain being built
// from an empty key set, which would reject every release rather than fail loudly.
func TestUpgradeValidatorCarriesTheShippedKeys(t *testing.T) {
	verifier, err := signing.NewVerifier()
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	if verifier.Keys() < 2 {
		t.Errorf("Keys() = %d; at least two must ship or there is no rotation headroom", verifier.Keys())
	}
}
