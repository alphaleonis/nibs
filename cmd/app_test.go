package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/spf13/cobra"
)

func TestGetApp(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}

	testCfg := config.Default()
	testCore := nibcore.New(nibsDir, testCfg)
	if err := testCore.Load(); err != nil {
		t.Fatal(err)
	}

	app := &App{Core: testCore}

	// Create a command with App stored in context
	cmd := &cobra.Command{}
	cmd.SetContext(withApp(cmd.Context(), app))

	got := getApp(cmd)
	if got == nil {
		t.Fatal("getApp returned nil")
		return
	}
	if got.Core != testCore {
		t.Error("getApp returned App with wrong Core")
	}
}

func TestGetAppPanicsWithDescriptiveMessage(t *testing.T) {
	cmd := &cobra.Command{Use: "test-cmd"}
	// No App stored in context — getApp should panic with an actionable message

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected getApp to panic when App is missing from context")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected panic message to be a string, got %T: %v", r, r)
		}
		if !strings.Contains(msg, "test-cmd") {
			t.Errorf("panic message should include the command name, got: %s", msg)
		}
	}()

	getApp(cmd)
}

func TestAppConfig(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}

	testCfg := config.Default()
	testCore := nibcore.New(nibsDir, testCfg)
	if err := testCore.Load(); err != nil {
		t.Fatal(err)
	}

	app := &App{Core: testCore}
	got := app.Config()
	if got != testCfg {
		t.Error("App.Config() should return Core's config")
	}
}

func TestAppNewResolver(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}

	testCfg := config.Default()
	testCore := nibcore.New(nibsDir, testCfg)
	if err := testCore.Load(); err != nil {
		t.Fatal(err)
	}

	app := &App{Core: testCore}
	resolver := app.newResolver()

	if resolver == nil {
		t.Fatal("newResolver returned nil")
		return
	}
	if resolver.Reader != testCore {
		t.Error("resolver.Reader should be Core")
	}
	if resolver.Writer != testCore {
		t.Error("resolver.Writer should be Core")
	}
	if resolver.Validator != testCore {
		t.Error("resolver.Validator should be Core")
	}
	if resolver.Blocking != testCore {
		t.Error("resolver.Blocking should be Core")
	}
}
