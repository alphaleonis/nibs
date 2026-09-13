// Package signing verifies Ed25519 signatures over release artifacts against the
// public keys compiled into the binary.
//
// go-selfupdate's validators do not fit: PGPValidator depends on the deprecated
// golang.org/x/crypto/openpgp, and ECDSAValidator holds a single key, which would
// leave no way to rotate.
//
// A binary trusts only the keys compiled into it, so ship spare public keys ahead
// of any rotation. A key cannot be un-trusted in binaries already shipped.
package signing

import (
	"crypto/ed25519"
	"crypto/x509"
	"embed"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
)

//go:embed keys/*.pub
var keyFS embed.FS

// ErrNoKeyVerifies reports that a signature verifies against no trusted key. A
// forged, corrupted or unknown-key signature all return it.
var ErrNoKeyVerifies = errors.New("signing: signature does not verify against any trusted key")

// Verifier holds the public keys a binary trusts for release signatures.
type Verifier struct {
	keys  []ed25519.PublicKey
	names []string // parallel to keys, for diagnostics only
}

// NewVerifier builds a Verifier from the embedded public keys.
func NewVerifier() (*Verifier, error) {
	return NewVerifierFromFS(keyFS, "keys")
}

// NewVerifierFromFS builds a Verifier from every `*.pub` in dir of fsys. Tests use
// it to trust a keypair they generated.
func NewVerifierFromFS(fsys fs.FS, dir string) (*Verifier, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("signing: reading key directory: %w", err)
	}

	// Sort for a stable key order across builds and platforms.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && path.Ext(e.Name()) == ".pub" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	v := &Verifier{}
	for _, name := range names {
		raw, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("signing: reading %s: %w", name, err)
		}
		key, err := parsePublicKey(raw)
		if err != nil {
			return nil, fmt.Errorf("signing: %s: %w", name, err)
		}
		v.keys = append(v.keys, key)
		v.names = append(v.names, name)
	}

	// Fail here: an empty key set would reject every signature.
	if len(v.keys) == 0 {
		return nil, errors.New("signing: no public keys embedded; the binary cannot verify any release")
	}
	return v, nil
}

func parsePublicKey(pemBytes []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("not a PEM block")
	}
	if block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("PEM block is %q, want \"PUBLIC KEY\"", block.Type)
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing PKIX public key: %w", err)
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key is %T, want ed25519.PublicKey", parsed)
	}
	return key, nil
}

// Keys returns the number of trusted keys.
func (v *Verifier) Keys() int { return len(v.keys) }

// Verify reports whether sig is a valid signature over message by any trusted
// key. Every key is tried; a signature is good if any one accepts it.
func (v *Verifier) Verify(message, sig []byte) error {
	// Report a wrong-length signature explicitly; ed25519.Verify only returns false.
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: signature is %d bytes, want %d", ErrNoKeyVerifies, len(sig), ed25519.SignatureSize)
	}
	for _, key := range v.keys {
		if ed25519.Verify(key, message, sig) {
			return nil
		}
	}
	return ErrNoKeyVerifies
}
