package cmd

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/store"
)

// dirEntryNames lists a directory's entries by name, sorted, so a test can
// assert that a refused command wrote NOTHING into it.
func dirEntryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// TestInitRefusesASymlinkedStoreDirectory pins the guard that keeps `nibs init`
// from reconstructing the store-relocation hazard by hand.
//
// Core.Init is os.MkdirAll on `<store>/data`, and MkdirAll FOLLOWS a symlink —
// so in a project carrying a committed `.nibs -> /outside` the store was created
// INSIDE the link's target. That directory then holds a config.yml that parses,
// so every route binds it and `nibs migrate`'s layout step moves its
// front-mattered files into data/ and rewrites each as a nib render: exactly the
// mutation the resolution-side guard refuses, reached by running the one command
// that guard's own refusal names.
//
// The rule is "not through a link", with no exemption. An empty destination is
// deliberately refused too — see the empty-destination row.
func TestInitRefusesASymlinkedStoreDirectory(t *testing.T) {
	tests := []struct {
		name string
		// destination materializes what the link points at and returns it, or
		// "" for a link that leads nowhere.
		destination func(t *testing.T, tmp string) string
	}{
		{
			// The reproduced hazard: a repository ships the link and the
			// destination is the user's own tree.
			name: "a destination holding files that are not a store",
			destination: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "outside")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "post.md"), hugoPost)
				return dir
			},
		},
		{
			// The deliberate off-repo setup — `ln -s ~/sync/proj-nibs .nibs`
			// then `nibs init`. It is refused with everything else, which is a
			// RECORDED removal rather than an oversight: nothing on disk tells
			// this shape apart from the hazard above at the moment init runs,
			// and the destination being empty is a property of the victim's
			// filesystem rather than of the link.
			name: "an empty destination",
			destination: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "sync", "proj-nibs")
				mkdirAllT(t, dir)
				return dir
			},
		},
		{
			// A link leading nowhere used to surface as the raw
			// "mkdir <link>: file exists" from MkdirAll — a message that names
			// a path the reader sees as a link, calls it a file, and says it
			// exists when what they are looking at is broken.
			name: "a destination that is not there",
			destination: func(t *testing.T, tmp string) string {
				return ""
			},
		},
	}

	// The guard has to cover BOTH routes to the store directory, or the one it
	// misses is the one an agent following a refusal reaches.
	routes := []struct {
		name string
		run  func(t *testing.T, projectDir, link string) error
	}{
		{"cwd", func(t *testing.T, projectDir, link string) error {
			t.Chdir(projectDir)
			rootCmd.SetArgs([]string{"init"})
			return rootCmd.Execute()
		}},
		{"--nibs-path", func(t *testing.T, projectDir, link string) error {
			rootCmd.SetArgs([]string{"--nibs-path", link, "init"})
			return rootCmd.Execute()
		}},
	}

	for _, tt := range tests {
		for _, route := range routes {
			t.Run(tt.name+" via "+route.name, func(t *testing.T) {
				t.Cleanup(resetInitFlags)
				resetInitFlags()
				t.Setenv("NIBS_PATH", "")
				tmp := t.TempDir()
				t.Setenv("NIBS_CONFIG_ROOT", tmp)

				destination := tt.destination(t, tmp)
				var before []string
				target := destination
				if target == "" {
					target = filepath.Join(tmp, "nowhere")
				} else {
					before = dirEntryNames(t, destination)
				}

				projectDir := filepath.Join(tmp, "proj")
				mkdirAllT(t, projectDir)
				link := filepath.Join(projectDir, store.DirName)
				symlinkT(t, target, link)

				err := route.run(t, projectDir, link)
				if err == nil {
					t.Fatalf("`nibs init` initialized a store through a `.nibs` symlink leading to %s", target)
				}
				msg := err.Error()
				if !strings.Contains(msg, link) {
					t.Errorf("refusal = %q, want it to name the link %q", msg, link)
				}
				// MkdirAll's own error is the shape this replaces; leaking it
				// means the guard did not fire and the error came from below.
				if strings.Contains(msg, "file exists") {
					t.Errorf("refusal = %q is MkdirAll's error, not the guard's", msg)
				}

				// The harm is a WRITE, so the assertion is on the filesystem
				// rather than on the message.
				if destination != "" {
					if got := dirEntryNames(t, destination); !equalStrings(got, before) {
						t.Errorf("the refused run wrote into %s: entries %v, want %v", destination, got, before)
					}
				} else if _, statErr := os.Lstat(target); statErr == nil {
					t.Errorf("the refused run created %s, the link's missing destination", target)
				}
			})
		}
	}
}

// equalStrings reports whether two sorted string slices hold the same entries.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestInitStillInitializesARealStoreDirectory is the counterweight: the guard is
// about links, and must not touch the ordinary case or the case `nibs init`
// itself creates.
func TestInitStillInitializesARealStoreDirectory(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "a project with no .nibs yet"
		if existing {
			name = "a real .nibs directory already there"
		}
		t.Run(name, func(t *testing.T) {
			t.Cleanup(resetInitFlags)
			resetInitFlags()
			t.Setenv("NIBS_PATH", "")
			tmp := t.TempDir()
			t.Setenv("NIBS_CONFIG_ROOT", tmp)
			projectDir := filepath.Join(tmp, "myproj")
			mkdirAllT(t, projectDir)
			if existing {
				mkdirAllT(t, filepath.Join(projectDir, store.DirName))
			}
			t.Chdir(projectDir)

			rootCmd.SetArgs([]string{"init"})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("init refused a real store directory: %v", err)
			}
			cfg := loadInitCfg(t, projectDir)
			if cfg.Nibs.Prefix != "myproj-" {
				t.Errorf("cfg prefix = %q, want %q", cfg.Nibs.Prefix, "myproj-")
			}
		})
	}
}

// TestInitRemediesTheSymlinkedStoreRefusalPrescribes holds the refusal to this
// project's standing rule: a refusal is the whole remedy, so both routes it
// names have to work, and the one that replaces a workflow this change removed
// has to reach the same place that workflow did.
//
// The second route is the load-bearing one. `ln -s ~/sync/proj-nibs .nibs` then
// `nibs init` was a real way to keep nibs out of the code repository, and it
// derived the project's own prefix. Naming the directory instead reaches the same
// store — but the prefix comes from the store's PARENT rather than the project,
// which is exactly why the refusal names --prefix alongside --nibs-path.
func TestInitRemediesTheSymlinkedStoreRefusalPrescribes(t *testing.T) {
	t.Run("remove the link and run nibs init here", func(t *testing.T) {
		t.Cleanup(resetInitFlags)
		resetInitFlags()
		t.Setenv("NIBS_PATH", "")
		tmp := t.TempDir()
		t.Setenv("NIBS_CONFIG_ROOT", tmp)
		outside := filepath.Join(tmp, "outside")
		mkdirAllT(t, outside)
		projectDir := filepath.Join(tmp, "myproj")
		mkdirAllT(t, projectDir)
		link := filepath.Join(projectDir, store.DirName)
		symlinkT(t, outside, link)
		t.Chdir(projectDir)

		if err := os.Remove(link); err != nil {
			t.Fatalf("remove the link: %v", err)
		}
		rootCmd.SetArgs([]string{"init"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("the prescribed remedy failed: %v", err)
		}
		if cfg := loadInitCfg(t, projectDir); cfg.Nibs.Prefix != "myproj-" {
			t.Errorf("cfg prefix = %q, want %q", cfg.Nibs.Prefix, "myproj-")
		}
	})

	t.Run("name the store with --nibs-path and --prefix, then link at it", func(t *testing.T) {
		t.Cleanup(resetInitFlags)
		resetInitFlags()
		t.Setenv("NIBS_PATH", "")
		tmp := t.TempDir()
		t.Setenv("NIBS_CONFIG_ROOT", tmp)
		real := filepath.Join(tmp, "sync", "proj-nibs")
		mkdirAllT(t, real)
		projectDir := filepath.Join(tmp, "myproj")
		mkdirAllT(t, projectDir)

		rootCmd.SetArgs([]string{"--nibs-path", real, "init", "--prefix", "myproj-"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("the prescribed remedy failed: %v", err)
		}
		// The link goes on AFTER, and the store it reaches must resolve — which
		// is the accept side of the resolution rule (see
		// TestSymlinkedStoreCarryingRealEvidenceStillResolves): the store now
		// carries a config.yml, so the link is evidence-backed.
		resetRootPersistentFlags()
		link := filepath.Join(projectDir, store.DirName)
		symlinkT(t, real, link)
		t.Chdir(projectDir)

		got, err := resolveStoreDir()
		if err != nil {
			t.Fatalf("the store the remedy created does not resolve through the link: %v", err)
		}
		if got != link {
			t.Errorf("resolveStoreDir() = %q, want %q", got, link)
		}
		// The prefix is why the refusal names --prefix at all: without it this
		// store would be called after `sync`, the directory that happens to
		// contain it.
		cfg, cfgErr := config.Load(filepath.Join(real, store.ConfigFileName))
		if cfgErr != nil {
			t.Fatalf("load the remedy's config: %v", cfgErr)
		}
		if cfg.Nibs.Prefix != "myproj-" {
			t.Errorf("cfg prefix = %q, want %q", cfg.Nibs.Prefix, "myproj-")
		}
	})
}
