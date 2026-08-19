package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
)

// TestMigrateDoesNotInstructAMoveBetweenTwoSpellingsOfOneDirectory pins the
// false-claim class at stripRetiredNibsPath's two misleading branches.
//
// `sameDir` compares paths BYTEWISE, so a second spelling of one directory reads
// as a second directory — and the two branches then addressed it as one:
//
//   - the shape-accepts branch prescribed `nibs migrate --nibs-path <declared>`,
//     which is the store the reader is already migrating;
//   - the fall-through branch said "if the nibs you want are the ones in X, move
//     them into Y first", which is a move from a directory into itself.
//
// Both are no-ops dressed as remedies, and the reader has no way to tell without
// resolving the two paths by hand.
//
// A SYMLINK ALIAS reaches the state without a case-insensitive volume, so the
// property is guarded on every platform; the cased row below is the shape it was
// originally reproduced on and skips through testskip where the filesystem
// cannot host it.
func TestMigrateDoesNotInstructAMoveBetweenTwoSpellingsOfOneDirectory(t *testing.T) {
	// aliasFn produces a second spelling of storeDir inside projectDir and
	// returns the value the retired key should carry.
	tests := []struct {
		name  string
		alias func(t *testing.T, projectDir, storeDir string) string
	}{
		{
			name: "a symlink beside the store",
			alias: func(t *testing.T, projectDir, storeDir string) string {
				if err := os.Symlink(store.DirName, filepath.Join(projectDir, "alias")); err != nil {
					testskip.SymlinkUnavailable(t, err)
				}
				return "alias"
			},
		},
		{
			// The shape this was reproduced on: `.NIBS` and `.nibs` are one
			// directory where the filesystem folds case, and nothing in the
			// message resolved either.
			name: "a differently cased spelling of the store's own name",
			alias: func(t *testing.T, projectDir, storeDir string) string {
				requireCaseInsensitivePathsT(t, projectDir)
				return strings.ToUpper(store.DirName)
			},
		},
	}

	// The two branches differ in whether a pre-layout `.nibs.yml` NAMES the
	// declared directory, which is what hasLegacyStoreShape needs to accept it.
	branches := []struct {
		name string
		// build lays the project out and returns the store to migrate.
		build func(t *testing.T, tmp string, alias func(t *testing.T, projectDir, storeDir string) string) string
		// mustNotSay is the remedy that branch used to print.
		mustNotSay string
	}{
		{
			name: "the shape guard accepts the declared directory",
			build: func(t *testing.T, tmp string, alias func(t *testing.T, projectDir, storeDir string) string) string {
				projectDir := filepath.Join(tmp, "proj")
				storeDir := filepath.Join(projectDir, store.DirName)
				mkdirAllT(t, storeDir)
				writeFileT(t, filepath.Join(storeDir, "leg-a1--one.md"), layoutNib)
				declared := alias(t, projectDir, storeDir)
				writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName),
					"nibs:\n  prefix: leg-\n  id_length: 4\n  path: "+declared+"\n")
				return storeDir
			},
			mustNotSay: "migrate that store instead",
		},
		{
			name: "the shape guard does not",
			build: func(t *testing.T, tmp string, alias func(t *testing.T, projectDir, storeDir string) string) string {
				projectDir := filepath.Join(tmp, "proj")
				storeDir := filepath.Join(projectDir, store.DirName)
				mkdirAllT(t, storeDir)
				writeFileT(t, filepath.Join(storeDir, "leg-a1--one.md"), layoutNib)
				declared := alias(t, projectDir, storeDir)
				// The retired key in the store's OWN config, which is the caller
				// that runs when no `.nibs.yml` sits beside the store — so nothing
				// names the declared directory and the shape guard refuses it.
				writeFileT(t, filepath.Join(storeDir, store.ConfigFileName),
					"nibs:\n  prefix: leg-\n  id_length: 4\n  path: "+declared+"\n")
				return storeDir
			},
			mustNotSay: "move them into",
		},
	}

	for _, tt := range tests {
		for _, branch := range branches {
			t.Run(tt.name+" / "+branch.name, func(t *testing.T) {
				t.Cleanup(resetRootPersistentFlags)
				t.Cleanup(resetMigrateFlags)
				resetRootPersistentFlags()
				resetMigrateFlags()
				t.Setenv("NIBS_PATH", "")
				tmp := t.TempDir()
				t.Setenv("NIBS_CONFIG_ROOT", tmp)
				storeDir := branch.build(t, tmp, tt.alias)

				out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
				if err == nil {
					t.Fatalf("migrate did not refuse a config still carrying the retired key\nout: %s", out)
				}
				msg := err.Error()
				// The refusal STAYS — sameDir governs the branch, and only the
				// wording changes. What must go is the instruction that cannot be
				// carried out.
				if strings.Contains(msg, branch.mustNotSay) {
					t.Errorf("refusal still prescribes %q for a directory that IS the store being migrated:\n%s", branch.mustNotSay, msg)
				}
				if !strings.Contains(msg, "under a different spelling") {
					t.Errorf("refusal does not say the declared path is the store under another spelling:\n%s", msg)
				}
				// The one remedy that converges has to survive.
				if !strings.Contains(msg, "remove the retired `nibs.path` key") {
					t.Errorf("refusal drops the remedy that works:\n%s", msg)
				}
			})
		}
	}
}
