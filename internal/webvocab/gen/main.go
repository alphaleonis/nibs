// Command gen writes the generated web vocabulary module to its committed
// location. Run through `task codegen` (go:generate in internal/webvocab), not
// by hand.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alphaleonis/nibs/internal/webvocab"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "webvocab: ", err)
		os.Exit(1)
	}
}

func run() error {
	rendered, err := webvocab.Render()
	if err != nil {
		return err
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	out := filepath.Join(root, filepath.FromSlash(webvocab.OutputPath))
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return os.WriteFile(out, []byte(rendered), 0o644)
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
