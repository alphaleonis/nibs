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
	// alias produces a second spelling of the store inside projectDir and returns
	// the value the retired key should carry.
	tests := []struct {
		name  string
		alias func(t *testing.T, projectDir string) string
	}{
		{
			name: "a symlink beside the store",
			alias: func(t *testing.T, projectDir string) string {
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
			alias: func(t *testing.T, projectDir string) string {
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
		build func(t *testing.T, tmp string, alias func(t *testing.T, projectDir string) string) string
		// mustNotSay is the remedy that branch used to print.
		mustNotSay string
	}{
		{
			name: "the shape guard accepts the declared directory",
			build: func(t *testing.T, tmp string, alias func(t *testing.T, projectDir string) string) string {
				projectDir := filepath.Join(tmp, "proj")
				storeDir := filepath.Join(projectDir, store.DirName)
				mkdirAllT(t, storeDir)
				writeFileT(t, filepath.Join(storeDir, "leg-a1--one.md"), layoutNib)
				declared := alias(t, projectDir)
				writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName),
					"nibs:\n  prefix: leg-\n  id_length: 4\n  path: "+declared+"\n")
				return storeDir
			},
			mustNotSay: "migrate that store instead",
		},
		{
			name: "the shape guard does not",
			build: func(t *testing.T, tmp string, alias func(t *testing.T, projectDir string) string) string {
				projectDir := filepath.Join(tmp, "proj")
				storeDir := filepath.Join(projectDir, store.DirName)
				mkdirAllT(t, storeDir)
				writeFileT(t, filepath.Join(storeDir, "leg-a1--one.md"), layoutNib)
				declared := alias(t, projectDir)
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

// TestMigrateTwoSpellingsRowsStillDiscriminate is the control for the table
// above: with the declared directory a GENUINELY different one, each branch must
// print the remedy its row forbids.
//
// Without this, the two rows assert only that neither message appears — which
// stays true if hasLegacyStoreShape ever stopped accepting the first row's
// fixture and both collapsed onto the same branch.
func TestMigrateTwoSpellingsRowsStillDiscriminate(t *testing.T) {
	tests := []struct {
		name string
		// build lays out a project whose retired key names a real OTHER
		// directory, and returns the store to migrate.
		build func(t *testing.T, tmp string) string
		want  string
	}{
		{
			name: "the shape guard accepts the declared directory",
			build: func(t *testing.T, tmp string) string {
				projectDir := filepath.Join(tmp, "proj")
				storeDir := filepath.Join(projectDir, store.DirName)
				mkdirAllT(t, storeDir)
				writeFileT(t, filepath.Join(storeDir, "leg-a1--one.md"), layoutNib)
				other := filepath.Join(projectDir, "nibdata")
				mkdirAllT(t, other)
				writeFileT(t, filepath.Join(other, "leg-b2--two.md"), layoutNib)
				writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName),
					"nibs:\n  prefix: leg-\n  id_length: 4\n  path: nibdata\n")
				return storeDir
			},
			want: "migrate that store instead",
		},
		{
			name: "the shape guard does not",
			build: func(t *testing.T, tmp string) string {
				projectDir := filepath.Join(tmp, "proj")
				storeDir := filepath.Join(projectDir, store.DirName)
				mkdirAllT(t, storeDir)
				writeFileT(t, filepath.Join(storeDir, "leg-a1--one.md"), layoutNib)
				other := filepath.Join(projectDir, "nibdata")
				mkdirAllT(t, other)
				writeFileT(t, filepath.Join(other, "hello.md"), hugoPost)
				writeFileT(t, filepath.Join(storeDir, store.ConfigFileName),
					"nibs:\n  prefix: leg-\n  id_length: 4\n  path: nibdata\n")
				return storeDir
			},
			want: "move them into",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			t.Cleanup(resetMigrateFlags)
			resetRootPersistentFlags()
			resetMigrateFlags()
			t.Setenv("NIBS_PATH", "")
			tmp := t.TempDir()
			t.Setenv("NIBS_CONFIG_ROOT", tmp)
			storeDir := tt.build(t, tmp)

			out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
			if err == nil {
				t.Fatalf("migrate did not refuse\nout: %s", out)
			}
			msg := err.Error()
			if !strings.Contains(msg, tt.want) {
				t.Errorf("refusal = %q, want the branch's own remedy %q — the row above no longer discriminates", msg, tt.want)
			}
			if strings.Contains(msg, "under a different spelling") {
				t.Errorf("refusal calls two genuinely different directories one:\n%s", msg)
			}
		})
	}
}

// TestMigrateDoesNotTellTheUserToDeleteTheOnlyStore pins the third site of the
// same false-claim class, and the one where following the refusal DESTROYS data.
//
// storeRelocationPending compares the store's BASENAME bytewise, so a store
// reached through a second spelling is read as sitting somewhere other than
// `.nibs` and the relocation is planned. planStoreRelocation then finds `.nibs`
// occupied — by the same directory — and concluded there were two stores, one of
// them stale. "Remove the other" removes the only one.
//
// It also CONTRADICTED the refusal the same project gets through its other
// spelling ("one directory under two names"), which is how it surfaced.
func TestMigrateDoesNotTellTheUserToDeleteTheOnlyStore(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetRootPersistentFlags()
	resetMigrateFlags()
	t.Setenv("NIBS_PATH", "")
	tmp := t.TempDir()
	t.Setenv("NIBS_CONFIG_ROOT", tmp)

	projectDir := filepath.Join(tmp, "proj")
	storeDir := filepath.Join(projectDir, store.DirName)
	mkdirAllT(t, storeDir)
	writeFileT(t, filepath.Join(storeDir, "leg-a1--one.md"), layoutNib)
	if err := os.Symlink(store.DirName, filepath.Join(projectDir, "alias")); err != nil {
		testskip.SymlinkUnavailable(t, err)
	}
	writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName),
		"nibs:\n  prefix: leg-\n  id_length: 4\n  path: alias\n")

	out, err := runRootWith(t, "--nibs-path", filepath.Join(projectDir, "alias"), "migrate", "--allow-dirty")
	if err == nil {
		t.Fatalf("migrate planned a relocation of a store onto itself\nout: %s", out)
	}
	msg := err.Error()
	if strings.Contains(msg, "remove the other") {
		t.Errorf("refusal tells the reader to delete the project's only store:\n%s", msg)
	}
	if !strings.Contains(msg, "ONE directory under two spellings") {
		t.Errorf("refusal does not say the two paths are one directory:\n%s", msg)
	}
	// The remedy has to be a command the resolver accepts, and the one that
	// converges: naming the store the way nibs finds it.
	if !strings.Contains(msg, "--nibs-path "+storeDir) {
		t.Errorf("refusal does not prescribe naming the store at %s:\n%s", storeDir, msg)
	}
}
