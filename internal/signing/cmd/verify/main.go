// Command verify checks release signatures against the public keys compiled into
// nibs, so a release signed with an untrusted key fails the release workflow.
//
//	-check-key   Sign a canary in memory with NIBS_SIGNING_KEY and verify it
//	             against the embedded keys. The release workflow runs it before
//	             goreleaser.
//
//	-in / -sig   Verify a file and its detached signature. The release workflow
//	             runs it on checksums.txt after goreleaser.
//
// Neither mode prints key material, and -check-key never writes the key to disk.
package main

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/alphaleonis/nibs/internal/signing"
)

const keyEnv = "NIBS_SIGNING_KEY"

// canary is the message -check-key signs; its content is irrelevant.
const canary = "nibs release signing key correspondence check"

func main() {
	checkKey := flag.Bool("check-key", false, "verify that "+keyEnv+" corresponds to an embedded public key")
	in := flag.String("in", "", "signed file to verify")
	sig := flag.String("sig", "", "detached signature file")
	flag.Parse()

	if err := run(*checkKey, *in, *sig); err != nil {
		fmt.Fprintf(os.Stderr, "nibs-verify: %v\n", err)
		os.Exit(1)
	}
}

func run(checkKey bool, in, sig string) error {
	verifier, err := signing.NewVerifier()
	if err != nil {
		return fmt.Errorf("loading embedded public keys: %w", err)
	}

	switch {
	case checkKey && in == "" && sig == "":
		return runCheckKey(verifier)
	case !checkKey && in != "" && sig != "":
		return runVerifyFile(verifier, in, sig)
	default:
		return errors.New("use either -check-key, or both -in and -sig")
	}
}

func runCheckKey(v *signing.Verifier) error {
	keyPEM := os.Getenv(keyEnv)
	if keyPEM == "" {
		return fmt.Errorf("%s is unset or empty; nothing to check", keyEnv)
	}
	key, err := parsePrivateKey([]byte(keyPEM))
	if err != nil {
		return fmt.Errorf("parsing %s: %w", keyEnv, err)
	}

	if err := v.Verify([]byte(canary), ed25519.Sign(key, []byte(canary))); err != nil {
		return fmt.Errorf("the private key in %s does not correspond to ANY of the %d public keys in "+
			"internal/signing/keys/.\n"+
			"  A release signed with it would be published, look valid, and then be rejected by every\n"+
			"  nibs binary once signature verification is required — recoverable only by reinstalling.\n"+
			"  Either set the secret to a key whose public half is embedded, or embed this key's public\n"+
			"  half and ship a release carrying it before signing with it", keyEnv, v.Keys())
	}

	fmt.Printf("nibs-verify: %s corresponds to one of the %d embedded public keys.\n", keyEnv, v.Keys())
	return nil
}

func runVerifyFile(v *signing.Verifier, in, sig string) error {
	message, err := os.ReadFile(in)
	if err != nil {
		return fmt.Errorf("reading %s: %w", in, err)
	}
	signature, err := os.ReadFile(sig)
	if err != nil {
		return fmt.Errorf("reading %s: %w", sig, err)
	}

	if err := v.Verify(message, signature); err != nil {
		return fmt.Errorf("%s does not verify %s against any of the %d embedded public keys: %w",
			sig, in, v.Keys(), err)
	}

	fmt.Printf("nibs-verify: %s verifies %s against an embedded public key.\n", sig, in)
	return nil
}

// parsePrivateKey applies the same checks as cmd/sign's parser; keep them in step.
func parsePrivateKey(pemBytes []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("not a PEM block")
	}
	if block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("PEM block is %q, want \"PRIVATE KEY\"", block.Type)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("not a PKCS#8 key: %w", err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is %T, want ed25519.PrivateKey", parsed)
	}
	return key, nil
}
