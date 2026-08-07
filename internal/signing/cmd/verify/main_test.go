package main

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pkcs8PEM(t *testing.T, key ed25519.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// TestCheckKeyRejectsAKeyNoBinaryTrusts is the whole point of this command: a
// signing key whose public half is not embedded must fail the release BEFORE
// anything is published, rather than producing a valid-looking signature that
// strands every install once verification is required.
func TestCheckKeyRejectsAKeyNoBinaryTrusts(t *testing.T) {
	_, stranger, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	t.Setenv(keyEnv, pkcs8PEM(t, stranger))

	err = run(true, "", "")
	if err == nil {
		t.Fatal("check-key accepted a key that no embedded public key corresponds to")
	}
	// The message has to name the consequence, or an operator hitting this at
	// release time cannot tell it apart from a transient failure.
	for _, want := range []string{"does not correspond", "reinstall"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestCheckKeyErrors(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantSub string
	}{
		{"unset", "", "unset or empty"},
		{"not PEM", "garbage", "not a PEM block"},
		{"wrong PEM type", string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte{1}})), `want "PRIVATE KEY"`},
		{"not PKCS#8", string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("nope")})), "not a PKCS#8 key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(keyEnv, tt.key)
			err := run(true, "", "")
			if err == nil {
				t.Fatal("run succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantSub)
			}
		})
	}
}

func TestVerifyFile(t *testing.T) {
	dir := t.TempDir()
	msg := filepath.Join(dir, "checksums.txt")
	sig := msg + ".sig"
	content := []byte("d34db33f  nibs_linux_amd64.tar.gz\n")
	if err := os.WriteFile(msg, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, stranger, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := os.WriteFile(sig, ed25519.Sign(stranger, content), 0o644); err != nil {
		t.Fatalf("write sig: %v", err)
	}

	// A signature from an untrusted key must be rejected — this is the same
	// check a nibs binary will make, so passing here while a binary refuses
	// would defeat the purpose.
	if err := run(false, msg, sig); err == nil {
		t.Error("verify accepted a signature from a key that is not embedded")
	}

	t.Run("missing files are reported, not silently passed", func(t *testing.T) {
		if err := run(false, filepath.Join(dir, "absent.txt"), sig); err == nil {
			t.Error("verify succeeded with a missing input file")
		}
		if err := run(false, msg, filepath.Join(dir, "absent.sig")); err == nil {
			t.Error("verify succeeded with a missing signature file")
		}
	})
}

func TestModeSelection(t *testing.T) {
	tests := []struct {
		name           string
		checkKey       bool
		in, sig        string
		wantUsageError bool
	}{
		{"neither mode", false, "", "", true},
		{"both modes", true, "a", "b", true},
		{"in without sig", false, "a", "", true},
		{"sig without in", false, "", "b", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(tt.checkKey, tt.in, tt.sig)
			if err == nil {
				t.Fatal("run succeeded, want a usage error")
			}
			if tt.wantUsageError && !strings.Contains(err.Error(), "use either") {
				t.Errorf("error = %q, want a usage error", err)
			}
		})
	}
}
