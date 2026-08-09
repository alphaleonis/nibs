package signing

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"testing/fstest"
)

// newKeyPEM returns a freshly generated keypair with the public half encoded
// exactly as `openssl pkey -pubout` writes it, which is the format
// RELEASING.md tells the operator to produce.
func newKeyPEM(t *testing.T) (ed25519.PrivateKey, []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return priv, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func verifierWith(t *testing.T, pems ...[]byte) *Verifier {
	t.Helper()
	fsys := fstest.MapFS{}
	for i, p := range pems {
		fsys[string(rune('a'+i))+".pub"] = &fstest.MapFile{Data: p}
	}
	// MapFS is flat; mirror the embedded layout by nesting under keys/.
	nested := fstest.MapFS{}
	for name, f := range fsys {
		nested["keys/"+name] = f
	}
	v, err := NewVerifierFromFS(nested, "keys")
	if err != nil {
		t.Fatalf("NewVerifierFromFS: %v", err)
	}
	return v
}

func TestVerifyAcceptsAnyTrustedKey(t *testing.T) {
	privA, pemA := newKeyPEM(t)
	privB, pemB := newKeyPEM(t)
	privC, pemC := newKeyPEM(t)
	v := verifierWith(t, pemA, pemB, pemC)

	if got := v.Keys(); got != 3 {
		t.Fatalf("Keys() = %d, want 3", got)
	}

	msg := []byte("checksums.txt contents")
	// The rotation property this design exists for: any embedded key may be the
	// one that signed, so the active key can move without stranding anyone.
	for name, priv := range map[string]ed25519.PrivateKey{"first": privA, "middle": privB, "last": privC} {
		t.Run(name+" key signs", func(t *testing.T) {
			if err := v.Verify(msg, ed25519.Sign(priv, msg)); err != nil {
				t.Errorf("Verify with %s key: %v", name, err)
			}
		})
	}
}

func TestVerifyRejects(t *testing.T) {
	priv, pemA := newKeyPEM(t)
	v := verifierWith(t, pemA)
	msg := []byte("checksums.txt contents")
	good := ed25519.Sign(priv, msg)

	untrusted, _ := newKeyPEM(t)

	tests := []struct {
		name    string
		message []byte
		sig     []byte
	}{
		// The core guard: a release signed by a key this binary does not carry
		// must not verify. This is what a compromised-release attacker has.
		{"signature from an untrusted key", msg, ed25519.Sign(untrusted, msg)},
		// Tampering with the signed content.
		{"tampered message", append(append([]byte{}, msg...), '!'), good},
		{"truncated message", msg[:len(msg)-1], good},
		// Malformed signatures must not panic or pass.
		{"empty signature", msg, nil},
		{"short signature", msg, good[:len(good)-1]},
		{"corrupted signature", msg, func() []byte {
			c := append([]byte{}, good...)
			c[0] ^= 0xFF
			return c
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Verify(tt.message, tt.sig)
			if err == nil {
				t.Fatal("Verify accepted, want rejection")
			}
			if !errors.Is(err, ErrNoKeyVerifies) {
				t.Errorf("error = %v, want it to wrap ErrNoKeyVerifies", err)
			}
		})
	}
}

func TestNewVerifierRejectsBadKeyMaterial(t *testing.T) {
	_, goodPEM := newKeyPEM(t)

	// A *genuine* ECDSA key, generated rather than hardcoded. This must parse
	// cleanly so the failure lands on the ed25519 type assertion — a
	// hand-written PEM here failed earlier at ParsePKIXPublicKey ("P256 point
	// not on curve"), which passes the test while leaving the type check
	// unexercised.
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa: %v", err)
	}
	ecdsaDER, err := x509.MarshalPKIXPublicKey(&ecdsaKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal ecdsa: %v", err)
	}
	ecdsaPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: ecdsaDER})
	if _, err := x509.ParsePKIXPublicKey(ecdsaDER); err != nil {
		t.Fatalf("test fixture is unusable — it must parse to reach the type check: %v", err)
	}

	tests := []struct {
		name  string
		files map[string][]byte
	}{
		{"no keys at all", map[string][]byte{}},
		{"not a PEM block", map[string][]byte{"a.pub": []byte("definitely not pem")}},
		{"wrong PEM type", map[string][]byte{"a.pub": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte{1, 2, 3}})}},
		{"non-ed25519 key", map[string][]byte{"a.pub": ecdsaPEM}},
		// One bad key among good ones must fail the whole construction rather
		// than silently trusting a smaller set than intended.
		{"one bad key among good", map[string][]byte{"a.pub": goodPEM, "b.pub": []byte("garbage")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := fstest.MapFS{}
			for name, data := range tt.files {
				fsys["keys/"+name] = &fstest.MapFile{Data: data}
			}
			if _, err := NewVerifierFromFS(fsys, "keys"); err == nil {
				t.Fatal("NewVerifierFromFS succeeded, want error")
			}
		})
	}
}

// The embedded keys are the ones a shipped binary actually trusts, so a
// malformed or missing file there is a release-breaking defect that no other
// test would catch.
func TestEmbeddedKeysLoad(t *testing.T) {
	v, err := NewVerifier()
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	if v.Keys() < 2 {
		t.Errorf("Keys() = %d; at least two keys should ship, or there is no rotation headroom", v.Keys())
	}

	// Distinctness matters and is easy to get wrong by copying a file: repeated
	// keys look like headroom while providing none.
	seen := map[string]bool{}
	for i, k := range v.keys {
		if seen[string(k)] {
			t.Errorf("key %s duplicates an earlier key", v.names[i])
		}
		seen[string(k)] = true
	}
}
