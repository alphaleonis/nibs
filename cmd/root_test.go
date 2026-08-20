package cmd

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
)

func TestResolveStoreDir(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "project")
	validStoreDir := filepath.Join(projectDir, ".nibs")
	if err := os.MkdirAll(validStoreDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	altStoreDir := filepath.Join(tmpDir, "alt", ".nibs")
	if err := os.MkdirAll(altStoreDir, 0755); err != nil {
		t.Fatalf("failed to create alt store dir: %v", err)
	}

	t.Cleanup(resetRootPersistentFlags)

	t.Run("flag takes highest precedence", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Setenv("NIBS_PATH", altStoreDir)
		nibsPath = validStoreDir

		got, err := resolveStoreDir()
		if err != nil {
			t.Fatalf("resolveStoreDir() error = %v", err)
		}
		if got != validStoreDir {
			t.Errorf("expected flag path %q, got %q", validStoreDir, got)
		}
	})

	t.Run("env var used when flag is empty", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Setenv("NIBS_PATH", altStoreDir)

		got, err := resolveStoreDir()
		if err != nil {
			t.Fatalf("resolveStoreDir() error = %v", err)
		}
		if got != altStoreDir {
			t.Errorf("expected env var path %q, got %q", altStoreDir, got)
		}
	})

	t.Run("--config names the store through its directory", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Setenv("NIBS_PATH", "")
		configPath = filepath.Join(altStoreDir, "config.yml")

		got, err := resolveStoreDir()
		if err != nil {
			t.Fatalf("resolveStoreDir() error = %v", err)
		}
		if got != altStoreDir {
			t.Errorf("expected --config's directory %q, got %q", altStoreDir, got)
		}
	})

	t.Run("upward walk finds the store from a subdirectory", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Setenv("NIBS_PATH", "")
		t.Setenv("NIBS_CONFIG_ROOT", tmpDir)
		deep := filepath.Join(projectDir, "a", "b")
		if err := os.MkdirAll(deep, 0755); err != nil {
			t.Fatalf("mkdir deep: %v", err)
		}
		t.Chdir(deep)

		got, err := resolveStoreDir()
		if err != nil {
			t.Fatalf("resolveStoreDir() error = %v", err)
		}
		if got != validStoreDir {
			t.Errorf("expected discovered store %q, got %q", validStoreDir, got)
		}
	})

	t.Run("invalid flag path returns error", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		nibsPath = "/nonexistent/path"

		_, err := resolveStoreDir()
		if err == nil {
			t.Fatal("expected error for invalid flag path, got nil")
		}
		if !strings.Contains(err.Error(), "does not exist or is not a directory") {
			t.Errorf("expected 'does not exist' error, got %q", err.Error())
		}
	})

	t.Run("invalid env var path returns error", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Setenv("NIBS_PATH", "/nonexistent/env/path")

		_, err := resolveStoreDir()
		if err == nil {
			t.Fatal("expected error for invalid env var path, got nil")
		}
		if !strings.Contains(err.Error(), "does not exist or is not a directory") {
			t.Errorf("expected 'does not exist' error, got %q", err.Error())
		}
	})

	t.Run("no store anywhere returns init suggestion", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Setenv("NIBS_PATH", "")
		bare := t.TempDir()
		t.Setenv("NIBS_CONFIG_ROOT", bare)
		t.Chdir(bare)

		_, err := resolveStoreDir()
		if err == nil {
			t.Fatal("expected error when no store exists, got nil")
		}
		if !strings.Contains(err.Error(), "nibs init") {
			t.Errorf("expected error to suggest 'nibs init', got %q", err.Error())
		}
	})

	t.Run("file path rejected as not a directory", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		filePath := filepath.Join(tmpDir, "not-a-dir")
		if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		nibsPath = filePath

		_, err := resolveStoreDir()
		if err == nil {
			t.Fatal("expected error for file path (not directory), got nil")
		}
		if !strings.Contains(err.Error(), "does not exist or is not a directory") {
			t.Errorf("expected 'does not exist' error, got %q", err.Error())
		}
	})
}

// TestResolveStoreDirBindsToTheNEARESTMarker pins the single upward pass.
//
// Discovery used to run TWO independent walks — one for a `.nibs` directory, one
// for a pre-layout `.nibs.yml` — and bind to whatever the first found, with
// nothing comparing their depths. A pre-layout sub-project nested under an
// unrelated ancestor store therefore resolved to the ANCESTOR: `nibs migrate`
// run in the sub-project moved the ancestor's nib files into data/ and stamped
// them, while the sub-project's own files stayed byte-identical. A store the
// user had never named was rewritten, and the project they were standing in was
// not touched at all.
//
// The rule is nearest-marker-wins, and both directions are pinned: a nearer
// pre-layout project must SHADOW an ancestor store (and refuse, because a
// project in that shape has no store to operate on), while a nearer store must
// still win over an ancestor pre-layout project.
func TestResolveStoreDirBindsToTheNEARESTMarker(t *testing.T) {
	// nestedPreLayout builds `<tmp>/parent/.nibs` (a real store) with a
	// pre-layout sub-project at `<tmp>/parent/sub`, and returns both.
	nestedPreLayout := func(t *testing.T) (ancestor, sub string) {
		t.Helper()
		tmp := t.TempDir()
		t.Setenv("NIBS_CONFIG_ROOT", tmp)
		parent := filepath.Join(tmp, "parent")
		ancestor = filepath.Join(parent, store.DirName)
		mkdirAllT(t, filepath.Join(ancestor, store.DataDirName))
		writeFileT(t, filepath.Join(ancestor, store.ConfigFileName), "nibs:\n  prefix: par-\n  id_length: 4\n")
		sub = filepath.Join(parent, "sub")
		mkdirAllT(t, filepath.Join(sub, "nibdata"))
		writeFileT(t, filepath.Join(sub, "nibdata", "sub-b2--two.md"), layoutNib)
		writeFileT(t, filepath.Join(sub, store.LegacyProjectConfigFileName),
			"nibs:\n  prefix: sub-\n  id_length: 4\n  path: nibdata\n")
		return ancestor, sub
	}

	t.Run("a nearer pre-layout project shadows an ancestor store", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		resetRootPersistentFlags()
		t.Setenv("NIBS_PATH", "")
		ancestor, sub := nestedPreLayout(t)
		t.Chdir(sub)

		got, err := resolveStoreDir()
		if err == nil {
			t.Fatalf("resolveStoreDir() = %q; a pre-layout project below a store must refuse, not bind to the store above it", got)
		}
		if got != "" {
			t.Errorf("resolveStoreDir() = %q on the error path, want no store", got)
		}
		msg := err.Error()
		// The refusal must be ABOUT this project, and it must carry the remedy
		// for it — the sub-project's own data directory, not the ancestor's.
		for _, want := range []string{sub, filepath.Join(sub, "nibdata"), "nibs migrate --nibs-path"} {
			if !strings.Contains(msg, want) {
				t.Errorf("refusal = %q, want it to mention %q", msg, want)
			}
		}
		// The ancestor is named as context, never as the thing to act on.
		if !strings.Contains(msg, ancestor) {
			t.Errorf("refusal = %q, want it to name the shadowed ancestor store %q so the reader knows why it stopped answering", msg, ancestor)
		}
		if strings.Contains(msg, "--nibs-path "+ancestor) {
			t.Errorf("refusal = %q prescribes acting on the ancestor store, which belongs to another project", msg)
		}
	})

	t.Run("a deeper directory of the same project still refuses", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		resetRootPersistentFlags()
		t.Setenv("NIBS_PATH", "")
		_, sub := nestedPreLayout(t)
		deep := filepath.Join(sub, "src", "pkg")
		mkdirAllT(t, deep)
		t.Chdir(deep)

		if got, err := resolveStoreDir(); err == nil {
			t.Fatalf("resolveStoreDir() = %q from %q; the nearest marker is still the sub-project's", got, deep)
		}
	})

	t.Run("a nearer store still wins over an ancestor pre-layout project", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		resetRootPersistentFlags()
		t.Setenv("NIBS_PATH", "")
		tmp := t.TempDir()
		t.Setenv("NIBS_CONFIG_ROOT", tmp)
		outer := filepath.Join(tmp, "outer")
		mkdirAllT(t, outer)
		writeFileT(t, filepath.Join(outer, store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: nibdata\n")
		inner := filepath.Join(outer, "inner")
		want := filepath.Join(inner, store.DirName)
		mkdirAllT(t, filepath.Join(want, store.DataDirName))
		t.Chdir(inner)

		got, err := resolveStoreDir()
		if err != nil {
			t.Fatalf("resolveStoreDir() error = %v; the nearer marker is a real store", err)
		}
		if got != want {
			t.Errorf("resolveStoreDir() = %q, want %q", got, want)
		}
	})

	// Both markers in ONE directory is the pre-layout default shape (and what a
	// hand-migrated project leaves behind). The store wins there, because
	// binding it is what lets the migration gate speak for it; preferring the
	// file would refuse every such project with no store bound to migrate.
	t.Run("a store beside the config wins in the same directory", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		resetRootPersistentFlags()
		t.Setenv("NIBS_PATH", "")
		tmp := t.TempDir()
		t.Setenv("NIBS_CONFIG_ROOT", tmp)
		projectDir := filepath.Join(tmp, "proj")
		want := filepath.Join(projectDir, store.DirName)
		mkdirAllT(t, want)
		writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n")
		t.Chdir(projectDir)

		got, err := resolveStoreDir()
		if err != nil {
			t.Fatalf("resolveStoreDir() error = %v; the store beside the config is the marker", err)
		}
		if got != want {
			t.Errorf("resolveStoreDir() = %q, want %q", got, want)
		}
	})
}

// TestMigrateLeavesAnAncestorStoreAloneFromANestedPreLayoutProject is the
// end-to-end half of the same defect, and the one that shows what it COST:
// `nibs migrate` in a nested pre-layout project moved and rewrote the ancestor
// store's files. Every byte under both roots is compared, because the harm here
// is a mutation rather than a wrong answer.
func TestMigrateLeavesAnAncestorStoreAloneFromANestedPreLayoutProject(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetRootPersistentFlags()
	resetMigrateFlags()
	t.Setenv("NIBS_PATH", "")

	tmp := t.TempDir()
	t.Setenv("NIBS_CONFIG_ROOT", tmp)
	parent := filepath.Join(tmp, "parent")
	ancestor := filepath.Join(parent, store.DirName)
	mkdirAllT(t, ancestor)
	writeFileT(t, filepath.Join(ancestor, store.ConfigFileName), "nibs:\n  prefix: par-\n  id_length: 4\n")
	// Pre-layout shape (a nib at the store ROOT), so a migrate that bound here
	// would have work to do and would visibly move it.
	writeFileT(t, filepath.Join(ancestor, "par-a1--parent.md"), layoutNib)
	sub := filepath.Join(parent, "sub")
	mkdirAllT(t, filepath.Join(sub, "nibdata"))
	writeFileT(t, filepath.Join(sub, "nibdata", "sub-b2--two.md"), layoutNib)
	writeFileT(t, filepath.Join(sub, store.LegacyProjectConfigFileName),
		"nibs:\n  prefix: sub-\n  id_length: 4\n  path: nibdata\n")

	before := treeSnapshot(t, tmp)
	t.Chdir(sub)
	out, err := runRootWith(t, "migrate", "--allow-dirty")
	if err == nil {
		t.Fatalf("migrate bound to a store the user never named\nout: %s", out)
	}
	if !strings.Contains(err.Error(), filepath.Join(sub, "nibdata")) {
		t.Errorf("refusal = %q, want it to name this project's own nibs", err.Error())
	}

	after := treeSnapshot(t, tmp)
	if len(before) != len(after) {
		t.Fatalf("migrate changed the file set:\nbefore %v\nafter  %v", before, after)
	}
	for path, sum := range before {
		if after[path] != sum {
			t.Errorf("%s was moved or rewritten by a migrate the user ran elsewhere", path)
		}
	}
}

// treeSnapshot maps every regular file under root to a digest of its contents,
// keyed by root-relative path. It answers "did anything move or change" in one
// comparison, which a per-file stat cannot.
func treeSnapshot(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	files := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		files[filepath.ToSlash(rel)] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotting %s: %v", root, err)
	}
	if len(files) == 0 {
		t.Fatalf("snapshot of %s found no files; it is not reading the tree", root)
	}
	return files
}

// TestResolveStoreDirStatsTheNamedLegacyConfig pins the three shapes
// `--config <dir>/.nibs.yml` can be in. The guard was a bare basename
// comparison with no stat, so all three got ONE message: "a project still
// carrying that file has not been migrated: run `nibs migrate` in <dir>".
//
//   - for a file that is not there, that prescribes a command in a directory
//     that need not exist — the path-existence class of defect;
//   - for an already-migrated project both halves are false: the file is gone,
//     the project is migrated, and the prescribed migrate answers "Store is up
//     to date" while `--nibs-path <dir>/.nibs` — which resolves — went
//     unmentioned.
//
// Every remedy asserted here is also run through the resolver by
// TestEveryRefusalNamesAReachablePathAndARunnableCommand; what these rows add is
// that the message's CLAIMS match what is on disk.
func TestResolveStoreDirStatsTheNamedLegacyConfig(t *testing.T) {
	tests := []struct {
		name string
		// build lays out the project and returns the --config value.
		build func(t *testing.T, projectDir string) string
		// want and notWant take the project directory so the assertions can name
		// the exact remedy rather than a fragment of one.
		want    func(projectDir string) []string
		notWant []string
	}{
		{
			// Migrated: `.nibs` is there, the pre-layout file is not.
			name: "the file is absent and the store is there",
			build: func(t *testing.T, projectDir string) string {
				mkdirAllT(t, filepath.Join(projectDir, store.DirName))
				return filepath.Join(projectDir, store.LegacyProjectConfigFileName)
			},
			want: func(projectDir string) []string {
				return []string{
					filepath.Join(projectDir, store.LegacyProjectConfigFileName) + " does not exist",
					"--nibs-path " + filepath.Join(projectDir, store.DirName),
				}
			},
			// Neither claim survives contact with the filesystem here.
			notWant: []string{"has not been migrated", "run `nibs migrate` in"},
		},
		{
			name: "the file is absent and there is no store either",
			build: func(t *testing.T, projectDir string) string {
				return filepath.Join(projectDir, "gone", store.LegacyProjectConfigFileName)
			},
			want: func(projectDir string) []string {
				return []string{
					filepath.Join(projectDir, "gone", store.LegacyProjectConfigFileName) + " does not exist",
					"--nibs-path",
				}
			},
			// With nothing on disk to name, the message must not invent a store —
			// `--nibs-path <project>/.nibs` here would be a path that is not there.
			notWant: []string{"has not been migrated", "--nibs-path " + string(filepath.Separator)},
		},
		{
			// Genuinely pre-layout: the file is there, the nibs are where its
			// retired key says, and there is no `<project>/.nibs`.
			name: "the file is there and names where the nibs live",
			build: func(t *testing.T, projectDir string) string {
				dir := filepath.Join(projectDir, "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "leg-a1--one.md"), layoutNib)
				legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
				writeFileT(t, legacy, "nibs:\n  prefix: leg-\n  id_length: 4\n  path: nibdata\n")
				return legacy
			},
			// The remedy is anchored to the NAMED project's own store, not to a
			// migrate whose meaning depends on where the reader is standing.
			want: func(projectDir string) []string {
				// Rendered through shellArg, as the message is: on Windows every
				// path carries separators the quote-trigger set matches, so pinning
				// the bare spelling here passed on POSIX and failed there.
				return []string{"nibs migrate --nibs-path " + shellArg(filepath.Join(projectDir, "nibdata"))}
			},
			notWant: []string{"run `nibs migrate` in"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			resetRootPersistentFlags()
			t.Setenv("NIBS_PATH", "")
			projectDir := filepath.Join(t.TempDir(), "proj")
			mkdirAllT(t, projectDir)
			configPath = tt.build(t, projectDir)

			got, err := resolveStoreDir()
			if err == nil {
				t.Fatalf("resolveStoreDir() = %q; --config at the pre-layout file names the PROJECT, not a store", got)
			}
			for _, want := range tt.want(projectDir) {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal = %q, want it to mention %q", err.Error(), want)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(err.Error(), notWant) {
					t.Errorf("refusal = %q, want it NOT to claim %q", err.Error(), notWant)
				}
			}
		})
	}
}

// TestResolveStoreDirStatsAMisnamedConfig is the sibling guard's half of the
// same missing stat: it advised `--nibs-path <dir>` in bare prose for a file it
// never looked for, so a mistyped path produced a rename instruction for a file
// that is not there and a store path derived from it.
func TestResolveStoreDirStatsAMisnamedConfig(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	resetRootPersistentFlags()
	t.Setenv("NIBS_PATH", "")

	missing := filepath.Join(t.TempDir(), "proj", "settings.yml")
	configPath = missing

	_, err := resolveStoreDir()
	if err == nil {
		t.Fatal("resolveStoreDir accepted a --config that is neither config.yml nor present")
	}
	if !strings.Contains(err.Error(), missing+" does not exist") {
		t.Errorf("refusal = %q, want it to report the file's absence", err.Error())
	}
	if strings.Contains(err.Error(), "rename it") {
		t.Errorf("refusal = %q tells the reader to rename a file that is not there", err.Error())
	}
}

// TestResolveStoreDirRefusesTheLegacyProjectConfig pins the first of the two
// guards that keep a mis-aimed --config from turning a project directory into
// a store. `--config <project>/.nibs.yml` was the DOCUMENTED way to work
// against another project before the layout inversion, and its directory is
// the project, not the store — so accepting it would point every command
// (`nibs migrate` above all) at the project tree. The refusal names the
// replacement rather than merely rejecting.
//
// The assertions DISCRIMINATE between the two guards. Both refuse this fixture
// and both name the project directory and config.yml, so asserting only those
// passes with this guard deleted — the fallback evidence guard would satisfy
// them. `assertRefusedByConfigGuard` keys on the phrases only this guard's
// message carries, and asserts the fallback's own phrase is absent.
func TestResolveStoreDirRefusesTheLegacyProjectConfig(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	resetRootPersistentFlags()
	t.Setenv("NIBS_PATH", "")

	projectDir := t.TempDir()
	storeDir := filepath.Join(projectDir, store.DirName)
	mkdirAllT(t, storeDir)
	legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
	writeFileT(t, legacy, "nibs:\n  prefix: leg-\n")
	configPath = legacy

	_, err := resolveStoreDir()
	if err == nil {
		t.Fatal("resolveStoreDir accepted --config at the pre-layout .nibs.yml; that path names the PROJECT, not the store")
	}
	assertRefusedByConfigGuard(t, err, projectDir)
}

// assertRefusedByConfigGuard asserts that err is resolveStoreDir's --config
// guard refusing a path aimed at the pre-layout `.nibs.yml`, and not the
// fallback store-evidence guard that would reject the same fixture one step
// later.
//
// The distinction is load-bearing rather than pedantic: the fallback guard
// decides on filesystem SHAPE, so a project that happens to carry store-shaped
// evidence slips past it, and this guard is then the only thing standing
// between `--config <project>/.nibs.yml` and `nibs migrate` walking the project
// tree. A test that cannot tell the two apart cannot catch this guard's removal.
//
// Both callers' fixtures carry a `.nibs` store beside the config, so the remedy
// asserted here is the one anchored to THAT directory. The guard used to
// prescribe `nibs migrate` in the config's directory instead, which is advice
// about a CWD rather than about a store: run there, the upward walk answers with
// whatever store it reaches first, and for an already-migrated project it also
// asserted the project was unmigrated and prescribed a migrate that reports
// nothing to do.
func assertRefusedByConfigGuard(t *testing.T, err error, projectDir string) {
	t.Helper()
	// Phrases unique to this guard: it talks about the pre-layout CONFIG and
	// where that file SITS. The fallback talks about a pre-layout STORE.
	storeDir := filepath.Join(projectDir, store.DirName)
	for _, want := range []string{"pre-layout config", "sits beside the store", "--nibs-path " + storeDir, store.ConfigFileName} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %q, want it to mention %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), "not a nibs store") {
		t.Errorf("the fallback evidence guard fired, not the --config guard: %v", err)
	}
}

// TestResolveStoreDirRefusesConfigCombinedWithAnExplicitStore pins that --config
// and an explicitly named store cannot be supplied together.
//
// Alone, --config names the store through its containing directory. Together with
// --nibs-path or NIBS_PATH the store comes from one and the config from the other,
// which is exactly what resolveCLIStore's invariant forbids: `nibs new
// --nibs-path A --config B/config.yml` wrote a nib into store A carrying store
// B's prefix and id length, and since ids derive from filenames that is a
// persisted misnaming rather than a display artifact.
func TestResolveStoreDirRefusesConfigCombinedWithAnExplicitStore(t *testing.T) {
	tests := []struct {
		name  string
		route func(t *testing.T, storeDir string)
		want  string
	}{
		{
			name:  "--nibs-path",
			route: func(_ *testing.T, storeDir string) { nibsPath = storeDir },
			want:  "--config and --nibs-path cannot be combined",
		},
		{
			name:  "NIBS_PATH",
			route: func(t *testing.T, storeDir string) { t.Setenv("NIBS_PATH", storeDir) },
			want:  "--config cannot be combined with NIBS_PATH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			resetRootPersistentFlags()
			t.Setenv("NIBS_PATH", "")

			tmp := t.TempDir()
			// Two genuine stores, so nothing but the combination can refuse.
			storeA := filepath.Join(tmp, "aaa", store.DirName)
			storeB := filepath.Join(tmp, "zzz", store.DirName)
			for dir, prefix := range map[string]string{storeA: "aaa-", storeB: "zzz-"} {
				mkdirAllT(t, filepath.Join(dir, store.DataDirName))
				writeFileT(t, filepath.Join(dir, store.ConfigFileName), "nibs:\n  prefix: "+prefix+"\n  id_length: 4\n")
			}
			tt.route(t, storeA)
			configPath = filepath.Join(storeB, store.ConfigFileName)

			_, err := resolveStoreDir()
			if err == nil {
				t.Fatal("resolveStoreDir accepted a store and a config from different projects")
			}
			for _, want := range []string{tt.want, storeA, storeB} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal = %q, want it to mention %q", err.Error(), want)
				}
			}
		})
	}

	// The refusal is deliberately unconditional, so the SELF-CONSISTENT spelling —
	// both flags naming the same store — is refused too. Narrowing it to a
	// disagreement would sanction the one spelling that teaches the habit: the
	// invocation silently changes meaning the moment either value moves.
	t.Run("both naming the same store is still refused", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		resetRootPersistentFlags()
		t.Setenv("NIBS_PATH", "")

		storeDir := filepath.Join(t.TempDir(), "proj", store.DirName)
		mkdirAllT(t, filepath.Join(storeDir, store.DataDirName))
		writeFileT(t, filepath.Join(storeDir, store.ConfigFileName), "nibs:\n  prefix: p-\n  id_length: 4\n")

		nibsPath = storeDir
		configPath = filepath.Join(storeDir, store.ConfigFileName)
		if got, err := resolveStoreDir(); err == nil {
			t.Fatalf("resolveStoreDir() = %q; the combination is refused whether or not the two agree", got)
		} else if !strings.Contains(err.Error(), "cannot be combined") {
			t.Errorf("refusal = %q, want it to name the combination", err.Error())
		}
	})

	t.Run("--config alone still names the store", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		resetRootPersistentFlags()
		t.Setenv("NIBS_PATH", "")

		storeDir := filepath.Join(t.TempDir(), "proj", store.DirName)
		mkdirAllT(t, filepath.Join(storeDir, store.DataDirName))
		writeFileT(t, filepath.Join(storeDir, store.ConfigFileName), "nibs:\n  prefix: p-\n")
		configPath = filepath.Join(storeDir, store.ConfigFileName)

		got, err := resolveStoreDir()
		if err != nil {
			t.Fatalf("--config alone must keep working: %v", err)
		}
		if got != storeDir {
			t.Errorf("resolveStoreDir() = %q, want %q", got, storeDir)
		}
	})
}

// TestResolveStoreDirRequiresStoreEvidence pins the second guard: a directory
// named EXPLICITLY — by --nibs-path, by NIBS_PATH, or through --config's
// containing directory — must carry positive evidence that it is a store.
// Without it, a path aimed one level too high (`--nibs-path <project>`)
// resolves to the project tree, and `nibs migrate` relocates and rewrites
// every front-mattered .md it finds there.
//
// The evidence has to be something only a nibs store PRODUCES, because this
// decision authorizes moving and rewriting a whole subtree: a config.yml that
// parses as a nibs config, or a pre-layout `.nibs.yml` whose retired
// `nibs.path` names this very directory. A directory merely CALLED something a
// store also calls its own — `data/`, `archive/`, a file named `config.yml` —
// is not evidence; `data/` is a standard Hugo directory, and accepting it
// resolved every Hugo site root as a store.
//
// The table walks the boundary in both directions, including shapes BETWEEN the
// two clauses: half-evidence (a `.nibs.yml` with no `nibs.path`, a `nibs.path`
// naming a sibling), and near-misses on the config clause (a config.yml that is
// not a nibs config, a DIRECTORY by that name).
func TestResolveStoreDirRequiresStoreEvidence(t *testing.T) {
	tests := []struct {
		name   string
		build  func(t *testing.T, tmp string) string
		accept bool
		// refusal lists the substrings a rejected shape's message must carry.
		//
		// It defaults to the FLAT DENIAL's own clause rather than to the lead
		// "not a nibs store", which every refusal on this path opens with. When
		// there was one such site the lead identified it; there are now three,
		// so a default matching the lead matches whichever fired and a row that
		// changes production branch stays green — which is how two rows below
		// silently moved from the flat denial to preLayoutRemedy's answer inside
		// one round. Rows whose shape routes elsewhere name their own clauses.
		refusal []string
	}{
		{
			name: "a .nibs store holding data/",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", store.DirName)
				mkdirAllT(t, filepath.Join(dir, store.DataDirName))
				return dir
			},
			accept: true,
		},
		{
			name: "an empty directory named .nibs",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", store.DirName)
				mkdirAllT(t, dir)
				return dir
			},
			accept: true,
		},
		{
			name: "a differently named store holding only config.yml",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, store.ConfigFileName), "nibs:\n  prefix: nd-\n")
				return dir
			},
			accept: true,
		},
		{
			name: "a differently named store holding config.yml and data/",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "nibdata")
				mkdirAllT(t, filepath.Join(dir, store.DataDirName))
				writeFileT(t, filepath.Join(dir, store.ConfigFileName), "nibs:\n  prefix: nd-\n")
				return dir
			},
			accept: true,
		},
		{
			name: "a config.yml that is not a nibs config",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "ci")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, store.ConfigFileName), "jobs:\n  build:\n    steps: []\n")
				return dir
			},
		},
		{
			name: "a config.yml whose `nibs` key is not a mapping",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "ci")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, store.ConfigFileName), "nibs: 3\n")
				return dir
			},
		},
		{
			name: "a config.yml that is not even YAML",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "ci")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, store.ConfigFileName), "nibs: [unterminated\n")
				return dir
			},
		},
		{
			name: "a DIRECTORY named config.yml",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "generated")
				mkdirAllT(t, filepath.Join(dir, store.ConfigFileName))
				return dir
			},
		},
		{
			name: "a Hugo-shaped site root holding only data/",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "hugo-site")
				mkdirAllT(t, filepath.Join(dir, store.DataDirName))
				writeFileT(t, filepath.Join(dir, store.DataDirName, "params.yaml"), "unrelated: yaml\n")
				mkdirAllT(t, filepath.Join(dir, "content", "posts"))
				writeFileT(t, filepath.Join(dir, "content", "posts", "hello.md"), "---\ntitle: Hello\n---\n\nBody.\n")
				return dir
			},
		},
		{
			name: "a directory holding only archive/",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "mail")
				mkdirAllT(t, filepath.Join(dir, store.ArchiveDirName))
				return dir
			},
		},
		{
			name: "the legacy shape: a .nibs.yml whose nibs.path names this directory",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "leg-a1--one.md"), layoutNib)
				writeFileT(t, filepath.Join(tmp, "proj", store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: nibdata\n")
				return dir
			},
			accept: true,
		},
		{
			name: "a nibs.path naming this directory, which holds no nib files yet",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(tmp, "proj", store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: nibdata\n")
				return dir
			},
			accept: true,
		},
		{
			name: "a nibs.path given as an absolute path",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(tmp, "proj", store.LegacyProjectConfigFileName),
					"nibs:\n  prefix: leg-\n  path: "+filepath.ToSlash(dir)+"\n")
				return dir
			},
			accept: true,
		},
		{
			// Any sibling directory of an unmigrated project holding any .md —
			// a shape this table once pinned as ACCEPTED, which let `nibs migrate`
			// delete the project's real `.nibs.yml` and relocate it there.
			name: "a sibling docs directory of an unmigrated project",
			build: func(t *testing.T, tmp string) string {
				proj := filepath.Join(tmp, "proj")
				mkdirAllT(t, filepath.Join(proj, store.DirName))
				writeFileT(t, filepath.Join(proj, store.DirName, "leg-a1--one.md"), layoutNib)
				writeFileT(t, filepath.Join(proj, store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n")
				dir := filepath.Join(proj, "docs")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "README.md"), "# Docs\n")
				return dir
			},
		},
		{
			name: "a .nibs.yml whose nibs.path names a DIFFERENT directory",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "docs")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "guide.md"), layoutNib)
				mkdirAllT(t, filepath.Join(tmp, "proj", "nibdata"))
				writeFileT(t, filepath.Join(tmp, "proj", store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: nibdata\n")
				return dir
			},
			// Not the flat denial: a config that declares SOME other store is a
			// pre-layout project, so the branch that answers is the one carrying
			// preLayoutRemedy. Both clauses are named — the one that reports what
			// the comparison established, and the remedy it composes — because a
			// row that named only the shared "not a nibs store" lead let this
			// shape change branch with the table green.
			refusal: []string{"beside it sets a retired nibs.path", "nibs migrate --nibs-path"},
		},
		{
			name: "a project directory with neither shape",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj")
				mkdirAllT(t, filepath.Join(dir, "docs"))
				writeFileT(t, filepath.Join(dir, "docs", "post.md"), "---\ntitle: A post\n---\n\nBody.\n")
				return dir
			},
		},
		{
			name: "markdown at the top level but no .nibs.yml beside it",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "notes")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "README.md"), "# Notes\n")
				return dir
			},
		},
		{
			name: "a .nibs.yml beside it but no markdown at the top level",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "notes")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(tmp, "proj", store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n")
				return dir
			},
		},
		{
			// A `.nibs.yml` with no `nibs.path` describes a store at
			// <project>/.nibs, which the name clause already accepts — so this
			// directory, whatever it holds, is not the store it names.
			name: "a .nibs.yml with no nibs.path beside a top-level nib file",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "leg-a1--one.md"), layoutNib)
				writeFileT(t, filepath.Join(tmp, "proj", store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n")
				return dir
			},
		},
		{
			// A cloned repository chooses its own `nibs.path`, and
			// pre-layout `nibs init` never wrote a value other than `.nibs`, so a
			// config naming an ordinary content directory is hand-authored. The
			// naming alone must not authorize `nibs migrate` to move every
			// front-mattered .md under it into data/ and rewrite each as a nib
			// render — something inside has to have been written by nibs.
			name: "a nibs.path naming a content directory of an untrusted repo",
			build: func(t *testing.T, tmp string) string {
				repo := filepath.Join(tmp, "repo")
				dir := filepath.Join(repo, "content")
				mkdirAllT(t, filepath.Join(dir, "posts"))
				writeFileT(t, filepath.Join(dir, "posts", "hello.md"), hugoPost)
				writeFileT(t, filepath.Join(dir, "about.md"), hugoPost)
				writeFileT(t, filepath.Join(repo, store.LegacyProjectConfigFileName), "nibs:\n  path: content\n")
				return dir
			},
			refusal: []string{"nothing in it was written by nibs"},
		},
		{
			// The accept side for a TORN nib: a half-written nib is still a nib
			// somebody wrote, which is exactly what corroboration asks. The scan
			// errors on it (the front matter never closes) and the walk must read
			// that as determinate rather than as "cannot tell".
			//
			// The tear is AFTER the keys the format requires, which is the only
			// torn file that can still answer the question: raising the bar to
			// what nib.Render always writes means a file torn BEFORE `status:`
			// no longer corroborates on its own. That costs nothing on a real
			// store, where corroboration needs any ONE file to pass, and a store
			// with no other file is the empty-directory exemption's case.
			name: "a nibs.path naming a directory whose only nib has an unclosed header",
			build: func(t *testing.T, tmp string) string {
				proj := filepath.Join(tmp, "proj")
				dir := filepath.Join(proj, "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "leg-a1--torn.md"), "---\n# leg-a1\nversion: 1\ntitle: Torn\nstatus: todo\n")
				writeFileT(t, filepath.Join(proj, store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: nibdata\n")
				return dir
			},
			accept: true,
		},
		{
			// The accept side of the same rule, so the corroboration cannot be
			// satisfied by "it holds some markdown": one nib among the documents
			// is what makes this the project's store.
			name: "a nibs.path naming a directory that holds a nib among other markdown",
			build: func(t *testing.T, tmp string) string {
				proj := filepath.Join(tmp, "proj")
				dir := filepath.Join(proj, "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "README.md"), "# Notes\n")
				writeFileT(t, filepath.Join(dir, "guide.md"), hugoPost)
				writeFileT(t, filepath.Join(dir, "leg-a1--one.md"), layoutNib)
				writeFileT(t, filepath.Join(proj, store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: nibdata\n")
				return dir
			},
			accept: true,
		},
		{
			// The evidence is the config naming a PATH, and sameDir compares
			// paths rather than resolving them — so a symlink aimed at the
			// declared directory is refused rather than accepted by another
			// name. Refusing is the safe direction: the real path still works.
			name: "a symlink pointing at the directory nibs.path names",
			build: func(t *testing.T, tmp string) string {
				real := filepath.Join(tmp, "proj", "nibdata")
				mkdirAllT(t, real)
				writeFileT(t, filepath.Join(tmp, "proj", store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: nibdata\n")
				link := filepath.Join(tmp, "proj", "link")
				if err := os.Symlink(real, link); err != nil {
					testskip.SymlinkUnavailable(t, err)
				}
				return link
			},
			// The row still refuses, but through the pre-layout branch rather
			// than the flat denial — the config names a real declared store, it
			// just does not spell THIS path. Naming both clauses is what makes a
			// future move between those branches visible here.
			refusal: []string{"beside it sets a retired nibs.path", "nibs migrate --nibs-path"},
		},
		{
			// The containment half of the parent-only rule. `nibs.path` naming an
			// immediate subdirectory is satisfied LEXICALLY by a symlink pointing
			// anywhere, and the store walk opens its root — so a repository
			// shipping `.nibs.yml` plus `store -> /outside` had nibs print
			// `nibs migrate --nibs-path <repo>/store`, which moved and rewrote
			// every front-mattered file outside the project.
			name: "a nibs.path naming a symlink that leaves the project",
			build: func(t *testing.T, tmp string) string {
				outside := filepath.Join(tmp, "victim")
				mkdirAllT(t, outside)
				writeFileT(t, filepath.Join(outside, "journal.md"), layoutNib)
				repo := filepath.Join(tmp, "repo")
				mkdirAllT(t, repo)
				writeFileT(t, filepath.Join(repo, store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: store\n")
				link := filepath.Join(repo, "store")
				if err := os.Symlink(outside, link); err != nil {
					testskip.SymlinkUnavailable(t, err)
				}
				return link
			},
			refusal: []string{"not an immediate subdirectory"},
		},
		{
			// The accept side of the same rule: resolving symlinks is a
			// containment test, not a ban on them. A store reached by a link that
			// stays inside the project is still the project's store.
			name: "a nibs.path naming a symlink that stays inside the project",
			build: func(t *testing.T, tmp string) string {
				proj := filepath.Join(tmp, "proj")
				real := filepath.Join(proj, "real")
				mkdirAllT(t, real)
				writeFileT(t, filepath.Join(real, "leg-a1--one.md"), layoutNib)
				writeFileT(t, filepath.Join(proj, store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: store\n")
				link := filepath.Join(proj, "store")
				if err := os.Symlink(real, link); err != nil {
					testskip.SymlinkUnavailable(t, err)
				}
				return link
			},
			accept: true,
		},
		{
			// The corroboration exemption for a markdown-free directory was
			// unbounded: what acceptance authorizes is a whole-directory rename
			// plus deletion of the project's `.nibs.yml`, which has nothing to do
			// with rewriting file CONTENTS. An asset directory was renamed to
			// `.nibs` wholesale.
			name: "a nibs.path naming a populated directory with no markdown in it",
			build: func(t *testing.T, tmp string) string {
				proj := filepath.Join(tmp, "proj")
				dir := filepath.Join(proj, "assets")
				mkdirAllT(t, filepath.Join(dir, "img"))
				writeFileT(t, filepath.Join(dir, "style.css"), "body{}\n")
				writeFileT(t, filepath.Join(dir, "img", "logo.svg"), "<svg/>\n")
				writeFileT(t, filepath.Join(proj, store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: assets\n")
				return dir
			},
			refusal: []string{"nothing in it was written by nibs"},
		},
		{
			// The same exemption reached a second way: the store walk prunes dot
			// directories, so markdown living only under one read as "no markdown
			// at all" and the directory passed as a freshly created store.
			name: "a nibs.path naming a directory whose only markdown is under a dot directory",
			build: func(t *testing.T, tmp string) string {
				proj := filepath.Join(tmp, "proj")
				dir := filepath.Join(proj, "vault")
				mkdirAllT(t, filepath.Join(dir, ".obsidian"))
				writeFileT(t, filepath.Join(dir, ".obsidian", "hidden-note.md"), layoutNib)
				writeFileT(t, filepath.Join(proj, store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: vault\n")
				return dir
			},
			refusal: []string{"nothing in it was written by nibs"},
		},
		{
			// The bar used to be a single key, and `status:` is not nibs' to
			// claim: a vault tracking its own notes that way had `notes/`
			// renamed to `.nibs` and every file in it rewritten as a nib render.
			name: "a nibs.path naming a note vault whose markdown carries a nibs status",
			build: func(t *testing.T, tmp string) string {
				proj := filepath.Join(tmp, "proj")
				dir := filepath.Join(proj, "notes")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "task.md"), vaultNote)
				writeFileT(t, filepath.Join(dir, "journal.md"), vaultNote)
				writeFileT(t, filepath.Join(proj, store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: notes\n")
				return dir
			},
			refusal: []string{"nothing in it was written by nibs"},
		},
		{
			// The same key on a docs site. hugoPost carries no `status:` and was
			// already refused; this is the shape that carries one, which is what
			// made the old bar reachable by ordinary content.
			name: "a nibs.path naming a content directory whose posts carry a nibs status",
			build: func(t *testing.T, tmp string) string {
				repo := filepath.Join(tmp, "repo")
				dir := filepath.Join(repo, "content")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "hello.md"), hugoPostWithStatus)
				writeFileT(t, filepath.Join(repo, store.LegacyProjectConfigFileName), "nibs:\n  path: content\n")
				return dir
			},
			refusal: []string{"nothing in it was written by nibs"},
		},
		{
			// The accept side of the raised bar, stated on its own so the rule
			// has an anchor that does not depend on any other row's fixture: a
			// file carrying what nib.Render always writes IS evidence.
			name: "a nibs.path naming a directory holding one nibs-rendered file",
			build: func(t *testing.T, tmp string) string {
				proj := filepath.Join(tmp, "proj")
				dir := filepath.Join(proj, "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "leg-a1--one.md"), layoutNib)
				writeFileT(t, filepath.Join(proj, store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: nibdata\n")
				return dir
			},
			accept: true,
		},
		{
			// The boundary: everything nib.Render writes EXCEPT the id comment.
			// Hand-authored front matter reaches the three keys far more easily
			// than it reaches the marker, so the marker is what the rule turns
			// on and it needs a row that isolates it.
			name: "a nibs.path naming a directory whose only candidate lacks the id comment",
			build: func(t *testing.T, tmp string) string {
				proj := filepath.Join(tmp, "proj")
				dir := filepath.Join(proj, "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "one.md"), "---\nversion: 1\ntitle: One\nstatus: todo\ntype: task\n---\n\nBody.\n")
				writeFileT(t, filepath.Join(proj, store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: nibdata\n")
				return dir
			},
			refusal: []string{"nothing in it was written by nibs"},
		},
		{
			// The mover treats a file whose header cannot be READ as a possible
			// nib and moves it into data/ anyway; the authorizer read the same
			// file as proof that nothing here was written by nibs. Undecided is
			// the only answer that agrees with both.
			name: "a nibs.path naming a directory whose only nib cannot be read",
			build: func(t *testing.T, tmp string) string {
				proj := filepath.Join(tmp, "proj")
				dir := filepath.Join(proj, "nibdata")
				mkdirAllT(t, dir)
				nib := filepath.Join(dir, "leg-a1--one.md")
				writeFileT(t, nib, layoutNib)
				if err := os.Chmod(nib, 0); err != nil {
					testskip.Unavailable(t, testskip.UnreadablePaths, "os.Chmod(nib, 0): %v", err)
				}
				t.Cleanup(func() { _ = os.Chmod(nib, 0o644) })
				if f, err := os.Open(nib); err == nil {
					_ = f.Close()
					testskip.Unavailable(t, testskip.UnreadablePaths, "this process reads a mode-000 file anyway (running as root?)")
				}
				writeFileT(t, filepath.Join(proj, store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: nibdata\n")
				return dir
			},
			refusal: []string{"cannot tell whether"},
		},
	}

	// The guard must cover every route to an explicitly named directory, not
	// just the flag: NIBS_PATH and --config's containing directory reach the
	// same branch.
	routes := []struct {
		name  string
		apply func(t *testing.T, dir string)
	}{
		{"--nibs-path", func(t *testing.T, dir string) { nibsPath = dir }},
		{"NIBS_PATH", func(t *testing.T, dir string) { t.Setenv("NIBS_PATH", dir) }},
		{"--config", func(t *testing.T, dir string) {
			configPath = filepath.Join(dir, store.ConfigFileName)
		}},
	}

	for _, tt := range tests {
		for _, route := range routes {
			t.Run(tt.name+" via "+route.name, func(t *testing.T) {
				t.Cleanup(resetRootPersistentFlags)
				resetRootPersistentFlags()
				t.Setenv("NIBS_PATH", "")
				dir := tt.build(t, t.TempDir())
				route.apply(t, dir)

				got, err := resolveStoreDir()
				if tt.accept {
					if err != nil {
						t.Fatalf("resolveStoreDir() error = %v, want the store %s", err, dir)
					}
					if got != dir {
						t.Errorf("resolveStoreDir() = %q, want %q", got, dir)
					}
					return
				}
				if err == nil {
					t.Fatalf("resolveStoreDir() = %q with no error; %s carries no evidence of being a store", got, dir)
				}
				wantRefusals := tt.refusal
				if len(wantRefusals) == 0 {
					wantRefusals = []string{"and no " + store.LegacyProjectConfigFileName + " beside it names it"}
				}
				for _, wantRefusal := range wantRefusals {
					if !strings.Contains(err.Error(), wantRefusal) {
						t.Errorf("refusal = %q, want it to mention %q", err.Error(), wantRefusal)
					}
				}
			})
		}
	}
}

// TestResolveStoreDirExplainsAPreLayoutProject pins the discovery message for
// the population the layout inversion is hardest on: a project whose data
// lived outside `.nibs` via the retired `nibs.path` key. It has no `.nibs`
// DIRECTORY, so the upward walk finds nothing — and the generic "run nibs
// init" answer is the one action that strands its data, creating an empty
// store with a derived prefix beside the real files.
//
// The `nibs.path` case then EXECUTES the remedy the message prints and requires
// the project to end up migrated. Asserting only the wording is what let a
// message ship whose remedy could not be satisfied by any filesystem action: it
// said to move the data into `<project>/.nibs/` and re-run, after which the
// migration compared the config VALUE `nibs.path` against the store root and
// refused with the same instruction, forever.
func TestResolveStoreDirExplainsAPreLayoutProject(t *testing.T) {
	tests := []struct {
		name       string
		configBody string
		want       []string
		notWant    []string
		// remedy runs what the message told the user to do, and reports the
		// store it should now be possible to discover.
		remedy func(t *testing.T, projectDir string) string
	}{
		{
			name:       "nibs.path names the real data directory",
			configBody: "nibs:\n  prefix: leg-\n  path: nibdata\n",
			want:       []string{"nibs.path", "nibdata", ".nibs", "nibs migrate --nibs-path"},
			notWant:    []string{"run `nibs init` to create one"},
			remedy: func(t *testing.T, projectDir string) string {
				t.Helper()
				t.Cleanup(resetMigrateFlags)
				resetMigrateFlags()
				resetRootPersistentFlags()
				dataDir := filepath.Join(projectDir, "nibdata")
				if out, err := runRootWith(t, "--nibs-path", dataDir, "migrate", "--allow-dirty"); err != nil {
					t.Fatalf("the remedy the message printed did not work: %v\nout: %s", err, out)
				}
				return filepath.Join(projectDir, store.DirName)
			},
		},
		{
			name:       "a pre-layout config with no nibs.path",
			configBody: "nibs:\n  prefix: leg-\n",
			want:       []string{".nibs.yml", "nibs migrate"},
			notWant:    []string{"run `nibs init` to create one"},
			remedy: func(t *testing.T, projectDir string) string {
				t.Helper()
				t.Cleanup(resetMigrateFlags)
				resetMigrateFlags()
				resetRootPersistentFlags()
				// "create <project>/.nibs and move this project's nib files
				// into it, then run `nibs migrate`".
				storeDir := filepath.Join(projectDir, store.DirName)
				mkdirAllT(t, storeDir)
				if err := os.Rename(filepath.Join(projectDir, "nibdata", "leg-a1--one.md"),
					filepath.Join(storeDir, "leg-a1--one.md")); err != nil {
					t.Fatalf("moving the nib files: %v", err)
				}
				if out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty"); err != nil {
					t.Fatalf("the remedy the message printed did not work: %v\nout: %s", err, out)
				}
				return storeDir
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			resetRootPersistentFlags()
			t.Setenv("NIBS_PATH", "")

			tmp := t.TempDir()
			t.Setenv("NIBS_CONFIG_ROOT", tmp)
			projectDir := filepath.Join(tmp, "proj")
			// The data directory `nibs.path` names, holding the project's only
			// nib — the message quotes this path, and the remedy migrates it.
			mkdirAllT(t, filepath.Join(projectDir, "nibdata"))
			writeFileT(t, filepath.Join(projectDir, "nibdata", "leg-a1--one.md"), layoutNib)
			writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName), tt.configBody)
			t.Chdir(projectDir)

			_, err := resolveStoreDir()
			if err == nil {
				t.Fatal("resolveStoreDir found a store where there is no .nibs directory")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("message = %q, want it to mention %q", err.Error(), want)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(err.Error(), notWant) {
					t.Errorf("message = %q, must not suggest %q — that is what strands the data", err.Error(), notWant)
				}
			}

			// Follow the message, then require the project to be discoverable:
			// no --nibs-path, from the project root.
			storeDir := tt.remedy(t, projectDir)
			resetRootPersistentFlags()
			t.Setenv("NIBS_PATH", "")
			got, resolveErr := resolveStoreDir()
			if resolveErr != nil {
				t.Fatalf("after following the remedy the store is still not discoverable: %v", resolveErr)
			}
			if got != storeDir {
				t.Errorf("resolveStoreDir() = %q, want the migrated store %q", got, storeDir)
			}
			resetListFlags()
			t.Cleanup(resetListFlags)
			out, listErr := runRootWith(t, "list", "--all", "--json")
			if listErr != nil {
				t.Fatalf("list after following the remedy: %v", listErr)
			}
			if ids := envelopeIDs(parseListEnvelope(t, out)); !ids["leg-a1"] {
				t.Errorf("list ids = %v, want leg-a1 — the migrated nib", ids)
			}
		})
	}
}

// TestReportExitError pins the CLI error boundary's contract. It is the ONE
// place that decides the process exit status, mapping each error's structured
// CODE to a stable exit code via output.ExitCode:
//   - nil err → exit 0, no stderr
//   - plain (uncoded) err → exit 1, stderr "Error: <msg>\n"
//   - plain filesystem/IO err → exit 5 (ExitIO), stderr "Error: <msg>\n"
//   - reported CodedError (JSON envelope / get text line already on stdout) →
//     exit mapped from code, NO stderr (a redundant "Error:" line would
//     corrupt `2>&1 | jq` callers)
//   - non-reported CodedError (shared text path) → exit mapped from code,
//     stderr "Error: <msg>\n" (the boundary owns the print)
//
// The `err` field is a thunk so the test can construct the error inside
// captureStdout — output.Error writes a JSON envelope on construction,
// and we don't want that envelope leaking into the test's stdout.
func TestReportExitError(t *testing.T) {
	tests := []struct {
		name       string
		err        func() error
		wantCode   int
		wantStderr string
	}{
		{
			name:       "nil error returns 0 with empty stderr",
			err:        func() error { return nil },
			wantCode:   output.ExitOK,
			wantStderr: "",
		},
		{
			name:       "plain error returns generic exit with Error: prefix",
			err:        func() error { return errors.New("boom") },
			wantCode:   output.ExitError,
			wantStderr: "Error: boom\n",
		},
		{
			name:       "plain filesystem error maps to ExitIO",
			err:        func() error { return &fs.PathError{Op: "open", Path: "x", Err: fs.ErrNotExist} },
			wantCode:   output.ExitIO,
			wantStderr: "Error: open x: file does not exist\n",
		},
		{
			name:       "reported validation error maps to ExitValidation, no stderr",
			err:        func() error { return output.Error(output.ErrValidation, "bad") },
			wantCode:   output.ExitValidation,
			wantStderr: "",
		},
		{
			name: "wrapped reported not-found error maps to ExitNotFound, no stderr",
			err: func() error {
				return fmt.Errorf("context: %w", output.Error(output.ErrNotFound, "missing"))
			},
			wantCode:   output.ExitNotFound,
			wantStderr: "",
		},
		{
			name:       "non-reported coded error maps code and prints to stderr",
			err:        func() error { return &output.CodedError{Code: output.ErrConflict, Msg: "clash"} },
			wantCode:   output.ExitConflict,
			wantStderr: "Error: clash\n",
		},
		{
			name:       "unknown code collapses to ExitError",
			err:        func() error { return &output.CodedError{Code: "WAT", Msg: "huh"} },
			wantCode:   output.ExitError,
			wantStderr: "Error: huh\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			// captureStdout absorbs the JSON envelope output.Error writes
			// on construction. The boundary itself only writes to the
			// stderr Writer passed in.
			_ = captureStdout(t, func() {
				code := reportExitError(&stderr, tt.err())
				if code != tt.wantCode {
					t.Errorf("exit code = %d, want %d", code, tt.wantCode)
				}
			})
			if got := stderr.String(); got != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", got, tt.wantStderr)
			}
		})
	}
}
