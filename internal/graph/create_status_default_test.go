package graph

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/store"
)

// TestCreateNibAppliesTheDefaultStatus holds createNib to its schema description:
// a status left out takes the store's default, and it is written into the file,
// as `nibs new` writes it, rather than supplied only when read.
func TestCreateNibAppliesTheDefaultStatus(t *testing.T) {
	empty, draft := "", "draft"
	tests := []struct {
		name          string
		defaultStatus string // "" leaves config.Default's
		status        *string
		want          string
	}{
		{name: "an omitted status takes the default", want: "todo"},
		{name: "an empty status takes the default", status: &empty, want: "todo"},
		{name: "a configured default is the one applied", defaultStatus: "in-progress", want: "in-progress"},
		{name: "a given status is kept", status: &draft, want: "draft"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir := filepath.Join(t.TempDir(), store.DirName)
			if err := os.MkdirAll(store.NewLayout(nibsDir).DataDir(), 0o755); err != nil {
				t.Fatal(err)
			}
			cfg := config.Default()
			if tt.defaultStatus != "" {
				cfg.Nibs.DefaultStatus = tt.defaultStatus
			}
			core := nibcore.New(nibsDir, cfg)
			if err := core.Load(); err != nil {
				t.Fatalf("load: %v", err)
			}
			resolver := &Resolver{Reader: core, Writer: core, Validator: core, Blocking: core, Orderer: NewOrderer(core, core)}

			got, err := resolver.Mutation().CreateNib(context.Background(), model.CreateNibInput{Title: "Fresh", Status: tt.status})
			if err != nil {
				t.Fatalf("CreateNib: %v", err)
			}
			if got.Status != tt.want {
				t.Errorf("status = %q, want %q", got.Status, tt.want)
			}
			raw, err := os.ReadFile(filepath.Join(core.Root(), got.Path))
			if err != nil {
				t.Fatalf("reading the created nib: %v", err)
			}
			if !strings.Contains(string(raw), "\nstatus: "+tt.want+"\n") {
				t.Errorf("the file does not carry status %q:\n%s", tt.want, raw)
			}
		})
	}
}
