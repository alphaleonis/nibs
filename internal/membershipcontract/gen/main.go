// Command gen writes the generated membership parity contract to its committed
// location. Run through `task codegen` (go:generate in
// internal/membershipcontract), not by hand.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alphaleonis/nibs/internal/membershipcontract"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "membershipcontract: ", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	out := filepath.Join(root, filepath.FromSlash(membershipcontract.OutputPath))
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return os.WriteFile(out, []byte(membershipcontract.Render()), 0o644)
}

// moduleRoot walks up from the working directory (the go:generate package
// directory) to the directory holding go.mod.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod in any parent of the working directory")
		}
		dir = parent
	}
}
