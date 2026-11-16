// Command gen writes the generated filter-language parity contracts to their
// committed locations. Run it through `task codegen`.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alphaleonis/nibs/internal/filterlangcontract"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "filterlangcontract:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	modules := map[string]string{
		filterlangcontract.OutputPath:      filterlangcontract.Render(),
		filterlangcontract.InputOutputPath: filterlangcontract.RenderInput(),
	}
	for path, content := range modules {
		out := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
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
