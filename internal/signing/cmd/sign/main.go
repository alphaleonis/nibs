// Command sign produces a detached Ed25519 signature over a file.
//
// GoReleaser invokes it for `artifacts: checksum`, so the signature covers
// checksums.txt and the checksum file in turn covers the archives. That
// composition is what internal/signing verifies on the way back in.
//
// The private key arrives as PEM in NIBS_SIGNING_KEY rather than as a path or a
// flag: an argument is visible in the process list to every other process on
// the machine, and a file would have to be written to disk and cleaned up. The
// program prints nothing derived from the key, and every error message is
// written to describe the shape of the problem without quoting key material.
package main

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
)

const keyEnv = "NIBS_SIGNING_KEY"

func main() {
	in := flag.String("in", "", "file to sign")
	out := flag.String("out", "", "path to write the detached signature to")
	flag.Parse()

	if err := run(*in, *out); err != nil {
		fmt.Fprintf(os.Stderr, "nibs-sign: %v\n", err)
		os.Exit(1)
	}
}

func run(in, out string) error {
	if in == "" || out == "" {
		return errors.New("both -in and -out are required")
	}

	// Absent or empty means the release would otherwise be published unsigned,
	// which is worse than failing: a binary that requires a signature would
	// refuse to upgrade to it, and the cause would surface much later.
	keyPEM := os.Getenv(keyEnv)
	if keyPEM == "" {
		return fmt.Errorf("%s is unset or empty. It must hold the PEM-encoded Ed25519 private key.\n"+
			"  In CI it comes from the `release` environment secret of the same name.\n"+
			"  For a local dry run, skip signing instead: goreleaser release --snapshot --clean --skip=sign", keyEnv)
	}

	key, err := parsePrivateKey([]byte(keyPEM))
	if err != nil {
		return fmt.Errorf("parsing %s: %w", keyEnv, err)
	}

	message, err := os.ReadFile(in)
	if err != nil {
		return fmt.Errorf("reading %s: %w", in, err)
	}

	if err := os.WriteFile(out, ed25519.Sign(key, message), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", out, err)
	}

	fmt.Printf("nibs-sign: wrote %s (%d bytes signed)\n", out, len(message))
	return nil
}

func parsePrivateKey(pemBytes []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("not a PEM block (expected the output of `openssl genpkey -algorithm ed25519`)")
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
