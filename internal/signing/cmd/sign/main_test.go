package main

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/signing"
)

// pkcs8PEM encodes a private key the way `openssl genpkey -algorithm ed25519`
// writes it — the format RELEASING.md tells the operator to produce and the
// format the GitHub secret holds.
func pkcs8PEM(t *testing.T, key ed25519.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// TestSignVerifyRoundTrip is the load-bearing test: it proves the signer and
// internal/signing agree on the format. Each side being independently correct
// would not catch a disagreement between them.
func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	dir := t.TempDir()
	in := filepath.Join(dir, "checksums.txt")
	out := in + ".sig"
	content := []byte("d34db33f  nibs_linux_amd64.tar.gz\ncafe1234  nibs_windows_amd64.zip\n")
	if err := os.WriteFile(in, content, 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	t.Setenv(keyEnv, pkcs8PEM(t, priv))
	if err := run(in, out); err != nil {
		t.Fatalf("run: %v", err)
	}

	sig, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read signature: %v", err)
	}
	if len(sig) != ed25519.SignatureSize {
		t.Errorf("signature is %d bytes, want %d", len(sig), ed25519.SignatureSize)
	}
	if !ed25519.Verify(pub, content, sig) {
		t.Error("signature does not verify against the matching public key")
	}

	// And prove it fails for the right reason, not merely that it succeeds:
	// a signature over different content must not verify.
	if ed25519.Verify(pub, append(content, '!'), sig) {
		t.Error("signature verified over modified content")
	}

	// The signature must be rejected by a Verifier that does not carry this
	// key — the case that matters if a signing key is ever swapped.
	other, err := signing.NewVerifier()
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	if err := other.Verify(content, sig); err == nil {
		t.Error("a Verifier holding only the shipped keys accepted a signature from a throwaway key")
	}
}

func TestRunErrors(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	goodKey := pkcs8PEM(t, priv)

	ecdsaLike := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not der")}))

	tests := []struct {
		name    string
		key     string
		in      string // "" means use a real temp file
		wantSub string
	}{
		// The most important failure: no key must stop the release rather than
		// letting it publish unsigned.
		{"missing key", "", "", "NIBS_SIGNING_KEY is unset"},
		{"key is not PEM", "just some text", "", "not a PEM block"},
		{"wrong PEM type", string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte{1}})), "", `want "PRIVATE KEY"`},
		{"PEM body is not PKCS#8", ecdsaLike, "", "not a PKCS#8 key"},
		{"input file missing", goodKey, filepath.Join(t.TempDir(), "absent.txt"), "reading"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			in := tt.in
			if in == "" {
				in = filepath.Join(dir, "checksums.txt")
				if err := os.WriteFile(in, []byte("x"), 0o644); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
			out := filepath.Join(dir, "sig")

			t.Setenv(keyEnv, tt.key)
			err := run(in, out)
			if err == nil {
				t.Fatal("run succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantSub)
			}
			if _, statErr := os.Stat(out); statErr == nil {
				t.Error("a signature file was written despite the error")
			}
		})
	}
}

func TestRunRequiresBothPaths(t *testing.T) {
	t.Setenv(keyEnv, "irrelevant")
	for _, tt := range []struct{ name, in, out string }{
		{"no input", "", "sig"},
		{"no output", "checksums.txt", ""},
		{"neither", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := run(tt.in, tt.out); err == nil {
				t.Error("run succeeded, want error")
			}
		})
	}
}
