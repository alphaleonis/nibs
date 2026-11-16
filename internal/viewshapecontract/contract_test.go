package viewshapecontract

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/alphaleonis/nibs/internal/viewshape"
)

// TestCasesPassInGo is the Go half of the parity table: viewshape.Shape must
// produce each case's rows. The web half is viewShapeContract.test.ts.
func TestCasesPassInGo(t *testing.T) {
	for _, c := range Cases() {
		t.Run(c.Name, func(t *testing.T) {
			got, err := viewshape.Shape(c.Nibs, c.View)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, c.Want) {
				t.Errorf("rows differ\n got: %+v\nwant: %+v", got, c.Want)
			}
			for _, r := range c.Reorders {
				from, to := rowOf(t, got, r.From), rowOf(t, got, r.To)
				if stays := viewshape.StaysInGroup(c.Nibs, got, from, to); stays != r.Stays {
					t.Errorf("StaysInGroup(%s, %s) = %t, want %t", r.From, r.To, stays, r.Stays)
				}
			}
		})
	}
}

func rowOf(t *testing.T, rows []viewshape.Row, id string) int {
	t.Helper()
	i := slices.IndexFunc(rows, func(r viewshape.Row) bool { return r.NibID == id })
	if i < 0 {
		t.Fatalf("no row shows %s", id)
	}
	return i
}

// TestGeneratedViewShapeContractIsFresh holds the committed module
// byte-identical to Render, so the web runs the cases this package defines.
func TestGeneratedViewShapeContractIsFresh(t *testing.T) {
	want, err := Render()
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(filepath.Join(moduleRoot(t), filepath.FromSlash(OutputPath)))
	if err != nil {
		t.Fatalf("reading the committed %s: %v — run `task codegen` and commit the result", OutputPath, err)
	}
	if string(committed) != want {
		t.Fatalf("%s is stale: the cases or the renderer changed without regenerating it — run `task codegen` and commit the result", OutputPath)
	}
}

// moduleRoot walks up from the package directory to the directory holding
// go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod in any parent of the working directory")
		}
		dir = parent
	}
}
