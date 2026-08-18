package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
)

// symlinkT links target at path, skipping where the platform will not create a
// symlink at all (CI's windows leg commonly needs elevation).
func symlinkT(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		testskip.SymlinkUnavailable(t, err)
	}
}

// outwardLinkedStore builds the fixture the store-relocation hazard was
// reproduced on: a pre-layout project whose real nibs live in `docs/nibs`, and a
// committed `.nibs` symlink leading to a tree OUTSIDE the project that holds an
// ordinary front-mattered blog post. It returns the project directory and the
// linked-to directory.
func outwardLinkedStore(t *testing.T) (projectDir, outside string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("NIBS_CONFIG_ROOT", tmp)

	outside = filepath.Join(tmp, "outside")
	mkdirAllT(t, outside)
	writeFileT(t, filepath.Join(outside, "post.md"), hugoPost)

	projectDir = filepath.Join(tmp, "proj")
	mkdirAllT(t, filepath.Join(projectDir, "docs", "nibs"))
	writeFileT(t, filepath.Join(projectDir, "docs", "nibs", "leg-a1--one.md"), layoutNib)
	writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName),
		"nibs:\n  prefix: leg-\n  id_length: 4\n  path: docs/nibs\n")
	symlinkT(t, outside, filepath.Join(projectDir, store.DirName))
	return projectDir, outside
}

// TestOutwardSymlinkedStoreIsRefusedOnEveryRoute drives the four-step chain that
// reproduced the store-relocation incident through the store's own NAME.
//
// `looksLikeStore`'s name clause accepted any directory CALLED `.nibs` with no
// containment test and no evidence, and the upward walk matched the same name —
// so a committed `.nibs -> /outside` bound as the store on the discovery route
// with no flag at all, and `nibs migrate` then planned to sweep that outside tree
// into `<project>/.nibs` while the project's real nibs in `docs/nibs` went
// untouched and unreferenced.
//
// The name is evidence only for a REAL directory: for a link, the name and the
// directory are different things, and the name is the whole basis of that
// short-circuit.
func TestOutwardSymlinkedStoreIsRefusedOnEveryRoute(t *testing.T) {
	t.Run("discovery binds nothing", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		resetRootPersistentFlags()
		t.Setenv("NIBS_PATH", "")
		projectDir, outside := outwardLinkedStore(t)
		t.Chdir(projectDir)

		got, err := resolveStoreDir()
		if err == nil {
			t.Fatalf("resolveStoreDir() = %q; a `.nibs` symlink leading to %s carries no evidence of being a store", got, outside)
		}
		if got != "" {
			t.Errorf("resolveStoreDir() = %q on the error path, want no store", got)
		}
	})

	t.Run("the explicit routes refuse the same shape", func(t *testing.T) {
		routes := []struct {
			name  string
			apply func(t *testing.T, link string)
		}{
			{"--nibs-path", func(t *testing.T, link string) { nibsPath = link }},
			{"NIBS_PATH", func(t *testing.T, link string) { t.Setenv("NIBS_PATH", link) }},
			{"--config", func(t *testing.T, link string) {
				configPath = filepath.Join(link, store.ConfigFileName)
			}},
		}
		for _, route := range routes {
			t.Run(route.name, func(t *testing.T) {
				t.Cleanup(resetRootPersistentFlags)
				resetRootPersistentFlags()
				t.Setenv("NIBS_PATH", "")
				projectDir, _ := outwardLinkedStore(t)
				route.apply(t, filepath.Join(projectDir, store.DirName))

				if got, err := resolveStoreDir(); err == nil {
					t.Fatalf("resolveStoreDir() = %q via %s; the link carries no store evidence", got, route.name)
				}
			})
		}
	})

	// The harm this closes is a MUTATION, so the assertion is on the plan rather
	// than on a message: `nibs migrate` must not enumerate a single file outside
	// the project, with or without the retired key that step 3 told the reader to
	// remove.
	t.Run("migrate cannot plan a relocation of the outside tree", func(t *testing.T) {
		for _, retiredKey := range []bool{true, false} {
			name := "with the retired nibs.path key"
			if !retiredKey {
				name = "after removing the retired nibs.path key, exactly as instructed"
			}
			t.Run(name, func(t *testing.T) {
				t.Cleanup(resetRootPersistentFlags)
				t.Cleanup(resetMigrateFlags)
				resetRootPersistentFlags()
				resetMigrateFlags()
				t.Setenv("NIBS_PATH", "")
				projectDir, outside := outwardLinkedStore(t)
				if !retiredKey {
					writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName),
						"nibs:\n  prefix: leg-\n  id_length: 4\n")
				}
				t.Chdir(projectDir)

				out, err := runRootWith(t, "migrate", "--dry-run")
				if err == nil {
					t.Fatalf("`nibs migrate --dry-run` planned against a `.nibs` symlink leading out of the project:\n%s", out)
				}
				if strings.Contains(out, outside) {
					t.Errorf("the preview enumerates %s, a tree outside the project:\n%s", outside, out)
				}
			})
		}
	})
}

// TestSymlinkedStoreCarryingRealEvidenceStillResolves is the accept side, and the
// reason the fix is an evidence rule rather than a containment one: keeping nibs
// out of the code repository through a link — `.nibs -> ~/sync/proj-nibs` — is a
// legitimate layout, and a link that leads to something that really IS a store
// must keep working on every route.
func TestSymlinkedStoreCarryingRealEvidenceStillResolves(t *testing.T) {
	build := func(t *testing.T) (link string) {
		tmp := t.TempDir()
		t.Setenv("NIBS_CONFIG_ROOT", tmp)
		real := filepath.Join(tmp, "sync", "proj-nibs")
		mkdirAllT(t, filepath.Join(real, store.DataDirName))
		writeFileT(t, filepath.Join(real, store.ConfigFileName), "nibs:\n  prefix: syn-\n  id_length: 4\n")
		projectDir := filepath.Join(tmp, "proj")
		mkdirAllT(t, projectDir)
		link = filepath.Join(projectDir, store.DirName)
		symlinkT(t, real, link)
		return link
	}

	t.Run("discovery", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		resetRootPersistentFlags()
		t.Setenv("NIBS_PATH", "")
		link := build(t)
		t.Chdir(filepath.Dir(link))

		got, err := resolveStoreDir()
		if err != nil {
			t.Fatalf("resolveStoreDir() error = %v; the link leads to a real store", err)
		}
		if got != link {
			t.Errorf("resolveStoreDir() = %q, want %q", got, link)
		}
	})

	t.Run("--nibs-path", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		resetRootPersistentFlags()
		t.Setenv("NIBS_PATH", "")
		link := build(t)
		nibsPath = link

		got, err := resolveStoreDir()
		if err != nil {
			t.Fatalf("resolveStoreDir() error = %v; the link leads to a real store", err)
		}
		if got != link {
			t.Errorf("resolveStoreDir() = %q, want %q", got, link)
		}
	})

	// A link that stays INSIDE the project is the shape CLAUDE.md calls
	// deliberately accepted, and it reaches acceptance through the same evidence
	// rule rather than through a containment exemption.
	t.Run("a link that stays inside the project", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		resetRootPersistentFlags()
		t.Setenv("NIBS_PATH", "")
		tmp := t.TempDir()
		t.Setenv("NIBS_CONFIG_ROOT", tmp)
		projectDir := filepath.Join(tmp, "proj")
		real := filepath.Join(projectDir, "nibdata")
		mkdirAllT(t, filepath.Join(real, store.DataDirName))
		writeFileT(t, filepath.Join(real, store.ConfigFileName), "nibs:\n  prefix: nd-\n  id_length: 4\n")
		link := filepath.Join(projectDir, store.DirName)
		symlinkT(t, real, link)
		t.Chdir(projectDir)

		got, err := resolveStoreDir()
		if err != nil {
			t.Fatalf("resolveStoreDir() error = %v; a link inside the project is still a store", err)
		}
		if got != link {
			t.Errorf("resolveStoreDir() = %q, want %q", got, link)
		}
	})
}

// TestPrimeRefusesAnOutwardSymlinkedStore pins that the onboarding prompt agrees
// with the commands it teaches.
//
// `nibs prime` runs from an agent's startup and answers "does this project use
// nibs". Emitting the prompt over a `.nibs` that every command then refuses is
// the same wrong answer the pre-layout branch beside it exists to avoid — the
// agent is primed on a store it cannot touch.
func TestPrimeRefusesAnOutwardSymlinkedStore(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	resetRootPersistentFlags()
	t.Setenv("NIBS_PATH", "")
	projectDir, _ := outwardLinkedStore(t)
	t.Chdir(projectDir)

	out, err := runRootWith(t, "prime")
	if err == nil {
		t.Fatalf("`nibs prime` primed an agent on a store every command refuses:\n%s", out)
	}
	if strings.Contains(out, "nibs cheat") {
		t.Errorf("the prompt was emitted anyway:\n%s", out)
	}
}

// TestPrimeStillAnswersForASymlinkedStoreThatIsReal is the accept side: the
// evidence rule must not make `nibs prime` silent for a project whose nibs
// legitimately live behind a link.
func TestPrimeStillAnswersForASymlinkedStoreThatIsReal(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	resetRootPersistentFlags()
	t.Setenv("NIBS_PATH", "")
	tmp := t.TempDir()
	t.Setenv("NIBS_CONFIG_ROOT", tmp)
	real := filepath.Join(tmp, "sync", "proj-nibs")
	mkdirAllT(t, filepath.Join(real, store.DataDirName))
	writeFileT(t, filepath.Join(real, store.ConfigFileName), "nibs:\n  prefix: syn-\n  id_length: 4\n")
	projectDir := filepath.Join(tmp, "proj")
	mkdirAllT(t, projectDir)
	symlinkT(t, real, filepath.Join(projectDir, store.DirName))
	t.Chdir(projectDir)

	out, err := runRootWith(t, "prime")
	if err != nil {
		t.Fatalf("`nibs prime` refused a project whose store is a link to a real store: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Error("`nibs prime` emitted nothing for a project that has a store")
	}
}
