package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/store"
)

// setupAreaMutationApp is setupQueryTestApp over a store that DECLARES a
// vocabulary, which the area mutations need to have anything to edit.
func setupAreaMutationApp(t *testing.T) *App {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatalf("creating the test store: %v", err)
	}
	if err := os.WriteFile(store.NewLayout(nibsDir).AreasPath(),
		[]byte("areas:\n    - name: web\n      children:\n        - name: ui\n    - name: auth\n"), 0644); err != nil {
		t.Fatalf("writing the areas vocabulary: %v", err)
	}
	core := nibcore.New(nibsDir, config.Default())
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("loading the test store: %v", err)
	}
	return newApp(core, true)
}

// TestAreaMutationErrorClassesMatchTheAreaCommands pins that the two outcomes
// `nibs area rename` and `nibs area rm` already split on — a refusal about the
// argument, and a failure of the filesystem — carry the same classes when the
// same edit arrives over GraphQL.
//
// One user error must not carry two verdicts depending on which surface raised
// it: exit 2 says "fix what you sent", exit 5 says "the store is not what it
// should be, and rerunning may well work".
func TestAreaMutationErrorClassesMatchTheAreaCommands(t *testing.T) {
	t.Run("a content refusal is a validation error", func(t *testing.T) {
		app := setupAreaMutationApp(t)

		_, _, err := executeQuery(app,
			`mutation { renameArea(input: {path: "nosuch", newName: "platform"}) { prefix } }`, nil, "")
		if err == nil {
			t.Fatal("renameArea over an undeclared path returned no error")
		}
		var ce *output.CodedError
		if !errors.As(err, &ce) {
			t.Fatalf("expected *output.CodedError, got %T: %v", err, err)
		}
		if ce.Code != output.ErrValidation {
			t.Errorf("code = %q, want %q", ce.Code, output.ErrValidation)
		}
		if got := output.ExitCode(ce.Code); got != output.ExitValidation {
			t.Errorf("exit code = %d, want %d", got, output.ExitValidation)
		}
	})

	// The store loads cleanly and the vocabulary is corrupted afterwards, so the
	// failure lands where the sequence re-reads the store under its write lock —
	// no chmod, and therefore no capability this platform may lack.
	t.Run("a store that cannot be re-read is a file error", func(t *testing.T) {
		app := setupAreaMutationApp(t)
		if err := os.WriteFile(store.NewLayout(app.Core.Root()).AreasPath(),
			[]byte("areas: [ this is not a vocabulary\n"), 0644); err != nil {
			t.Fatalf("corrupting the areas vocabulary: %v", err)
		}

		_, _, err := executeQuery(app,
			`mutation { renameArea(input: {path: "web", newName: "platform"}) { prefix } }`, nil, "")
		if err == nil {
			t.Fatal("renameArea over an unreadable vocabulary returned no error")
		}
		var ce *output.CodedError
		if !errors.As(err, &ce) {
			t.Fatalf("expected *output.CodedError, got %T: %v", err, err)
		}
		if ce.Code != output.ErrFileError {
			t.Errorf("code = %q, want %q", ce.Code, output.ErrFileError)
		}
		if got := output.ExitCode(ce.Code); got != output.ExitIO {
			t.Errorf("exit code = %d, want %d", got, output.ExitIO)
		}
		if !strings.Contains(ce.Error(), "nothing was written") {
			t.Errorf("the message does not say the store was left alone: %s", ce.Error())
		}
	})
}
