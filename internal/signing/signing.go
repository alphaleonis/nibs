// Package signing verifies Ed25519 signatures over release artifacts using a
// set of public keys compiled into the binary.
//
// # Why a hand-written validator
//
// go-selfupdate ships five validators and none of them fits. PGPValidator is
// hardcoded to golang.org/x/crypto/openpgp, which the Go team deprecated as
// "unsafe by design" (GO-2026-5932) and which rejects Ed25519 keys outright.
// ECDSAValidator uses only standard-library crypto, but holds exactly one
// *ecdsa.PublicKey — and PatternValidator cannot compensate, because its
// findValidator returns the first matching validator, so a second one
// registered against the same filename is dead code. That would mean one key,
// unchangeable, for the lifetime of every binary ever shipped.
//
// # Why a set of keys rather than one
//
// The public key reaches a user through their existing install, not through the
// release being verified — that separation is what makes it a trust anchor, and
// it is also why the key cannot be changed after the fact. A binary verifies
// only against keys it already carries, so rotation headroom has to be bought
// up front: ship N public keys, sign with one, and move to the next when the
// active key is compromised or simply due. A key minted later is invisible to
// every binary already in the wild.
//
// Measured, not assumed: the equivalent PGP experiment showed a binary carrying
// master+subkeyA rejects a signature from a subkey created afterwards
// ("signature made by unknown entity"), while one carrying master+A+B accepts
// both.
//
// # What this does not do
//
// Nothing here can un-trust a key. Revocation cannot reach a binary that has
// already shipped, so a stolen key stays valid for old versions until they
// upgrade. Rotation limits future exposure; it does not repair the past.
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

// ErrNoKeyVerifies reports that a signature matched none of the trusted keys.
// It is deliberately indistinguishable between "forged", "signed by a key this
// binary is too old to know" and "corrupted in transit": the verifier cannot
// tell these apart, and pretending otherwise would invent detail it does not
// have.
var ErrNoKeyVerifies = errors.New("signing: signature does not verify against any trusted key")

// Verifier holds the public keys a binary trusts for release signatures.
type Verifier struct {
	keys  []ed25519.PublicKey
	names []string // parallel to keys, for diagnostics only
}

// NewVerifier builds a Verifier from the embedded public keys.
func NewVerifier() (*Verifier, error) {
	return newVerifierFromFS(keyFS, "keys")
}

func newVerifierFromFS(fsys fs.FS, dir string) (*Verifier, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("signing: reading key directory: %w", err)
	}

	// Sorted so key order is stable across builds and platforms, making
	// "which key verified" reproducible rather than dependent on directory
	// iteration order.
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

	// An empty key set would make Verify reject everything, which looks like a
	// signature problem rather than a build problem. Fail at construction so
	// the cause is legible.
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

// Keys reports how many public keys are trusted. Used by tests and diagnostics;
// the count is the rotation headroom remaining across the fleet.
func (v *Verifier) Keys() int { return len(v.keys) }

// Verify reports whether sig is a valid signature over message by any trusted
// key. Every key is tried; a signature is good if any one accepts it.
func (v *Verifier) Verify(message, sig []byte) error {
	// ed25519.Verify panics on a wrong-length key but returns false for a
	// malformed signature, so the length guard here is about signatures.
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
