package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/store"
)

// hugoPost is front-mattered markdown that is NOT a nib: it carries a title and
// a date, and no `status:` from the nibs enum. It stands in for the untrusted
// content a cloned repository ships.
const hugoPost = "---\ntitle: Hello\ndate: 2026-01-02\ndraft: false\n---\n\nA blog post.\n"

// symlinkLoopT creates a self-referential symlink at path — a file whose
// os.Stat/os.Open fails with ELOOP rather than ENOENT. It is the portable way to
// produce "the evidence exists but cannot be read" without depending on running
// as an unprivileged user.
func symlinkLoopT(t *testing.T, path string) {
	t.Helper()
	if err := os.Symlink(filepath.Base(path), path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Skipf("this filesystem resolves a self-referential symlink at %s", path)
	}
}

// lockedDirT creates a mode-000 subdirectory of parent — a directory the
// corroboration walk cannot enumerate — and skips where the process can read it
// anyway (running as root).
func lockedDirT(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	mkdirAllT(t, dir)
	if err := os.Chmod(dir, 0); err != nil {
		t.Skipf("chmod 0 unavailable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := os.ReadDir(dir); err == nil {
		t.Skipf("this process can read a mode-000 directory (running as root?)")
	}
	return dir
}

// TestNoStoreFoundSeparatesUnreadableEvidenceFromNoEvidence pins the third answer
// at the ONE entry point that used to collapse it.
//
// hasLegacyStoreShape reports "cannot tell" by returning an error, and dropping it
// left the message free to pick a reason clause that states a definite fact: a
// declared directory holding a real nib behind an unreadable subdirectory was
// reported as holding "nothing written by nibs", and a declared directory that is
// simply gone got the same false reason plus the instruction to delete the only
// record of where the nibs are.
func TestNoStoreFoundSeparatesUnreadableEvidenceFromNoEvidence(t *testing.T) {
	tests := []struct {
		name string
		// build materializes what `nibs.path: nibdata` names.
		build   func(t *testing.T, projectDir string)
		want    []string
		notWant []string
	}{
		{
			name: "a declared directory that cannot be enumerated",
			build: func(t *testing.T, projectDir string) {
				dir := filepath.Join(projectDir, "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "leg-a1--one.md"), layoutNib)
				// Sorts before the nib, so the walk fails before it finds one.
				lockedDirT(t, dir, "asub")
			},
			// The key is the only record of where the nibs are, so the
			// instruction to remove it must be reversed rather than merely
			// omitted.
			want: []string{"cannot be read", "cannot be determined", "do NOT remove the `nibs.path` key"},
			notWant: []string{
				"nothing in it was written by nibs",
				"not an immediate subdirectory",
			},
		},
		{
			name:  "a declared directory that is not there",
			build: func(t *testing.T, projectDir string) {},
			want:  []string{"does not exist", "not where the config says"},
			notWant: []string{
				"nothing in it was written by nibs",
				"remove the `nibs.path` key",
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
			mkdirAllT(t, projectDir)
			tt.build(t, projectDir)
			writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName),
				"nibs:\n  prefix: leg-\n  id_length: 4\n  path: nibdata\n")
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
					t.Errorf("message = %q must not say %q — it was never established", err.Error(), notWant)
				}
			}
		})
	}
}

// TestResolveStoreDirSeparatesNoEvidenceFromUnreadableEvidence pins the
// three-way answer store resolution has to give. "No evidence" and "the evidence
// could not be read" lead to OPPOSITE advice: the first refusal tells the user to
// run `nibs init` here, and doing that over a real store whose config merely
// exceeded the read ceiling creates a second, empty store beside their data.
func TestResolveStoreDirSeparatesNoEvidenceFromUnreadableEvidence(t *testing.T) {
	tests := []struct {
		name string
		// build materializes the fixture and returns the directory to name.
		build func(t *testing.T, tmp string) string
		// want are substrings the refusal must carry.
		want []string
		// notWant are substrings it must not: the harmful advice.
		notWant []string
	}{
		{
			// An oversized config.yml is a REAL store's config. Reporting it as
			// absence of evidence made the store answer "is not a nibs store …
			// or run `nibs init` there" over the user's data.
			name: "a config.yml over the read ceiling",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, store.ConfigFileName),
					"nibs:\n  prefix: nd-\n# "+strings.Repeat("x", config.MaxConfigBytes)+"\n")
				return dir
			},
			want:    []string{"cannot tell whether", "configuration limit"},
			notWant: []string{"run `nibs init` there", "is not a nibs store"},
		},
		{
			// config.RetiredNibsPath used to collapse "unreadable" into "no key",
			// and this is the call site where that answer decides whether
			// `nibs migrate` may rewrite a directory.
			name: "a .nibs.yml that cannot be read",
			build: func(t *testing.T, tmp string) string {
				proj := filepath.Join(tmp, "proj")
				dir := filepath.Join(proj, "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "leg-a1--one.md"), layoutNib)
				symlinkLoopT(t, filepath.Join(proj, store.LegacyProjectConfigFileName))
				return dir
			},
			want:    []string{"cannot tell whether"},
			notWant: []string{"run `nibs init` there", "is not a nibs store"},
		},
		{
			// The same collapse one layer down: the file opens, so the read
			// ceiling is what stops it, and the answer must not be "no key".
			name: "a .nibs.yml over the read ceiling",
			build: func(t *testing.T, tmp string) string {
				proj := filepath.Join(tmp, "proj")
				dir := filepath.Join(proj, "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "leg-a1--one.md"), layoutNib)
				writeFileT(t, filepath.Join(proj, store.LegacyProjectConfigFileName),
					"nibs:\n  path: nibdata\n# "+strings.Repeat("x", config.MaxConfigBytes)+"\n")
				return dir
			},
			want:    []string{"cannot tell whether", "configuration limit"},
			notWant: []string{"run `nibs init` there", "is not a nibs store"},
		},
		{
			name: "a .nibs.yml that is not YAML at all",
			build: func(t *testing.T, tmp string) string {
				proj := filepath.Join(tmp, "proj")
				dir := filepath.Join(proj, "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "leg-a1--one.md"), layoutNib)
				writeFileT(t, filepath.Join(proj, store.LegacyProjectConfigFileName), "nibs: [unterminated\n")
				return dir
			},
			want:    []string{"cannot tell whether"},
			notWant: []string{"run `nibs init` there", "is not a nibs store"},
		},
		{
			// The determinate no still has to read as one.
			name: "a config.yml that is genuinely not a nibs config",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "ci")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, store.ConfigFileName), "jobs:\n  build:\n    steps: []\n")
				return dir
			},
			want:    []string{"is not a nibs store"},
			notWant: []string{"cannot tell whether"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			resetRootPersistentFlags()
			t.Setenv("NIBS_PATH", "")
			nibsPath = tt.build(t, t.TempDir())

			got, err := resolveStoreDir()
			if err == nil {
				t.Fatalf("resolveStoreDir() = %q with no error", got)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal = %q, want it to mention %q", err.Error(), want)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(err.Error(), notWant) {
					t.Errorf("refusal = %q, must not say %q", err.Error(), notWant)
				}
			}
		})
	}
}

// TestConfigFlagMustNameTheStoresOwnConfig pins the other half of the harm the
// --config/--nibs-path exclusion was written for. The flag's only meaning is
// "name the store through this file's directory", so a differently named file
// splits the store and the config apart inside ONE flag: the store resolved to
// the directory while the config came from the named file, and
// `--config <store>/config.yml.bak` persisted a nib into the real store under the
// backup's prefix. Ids derive from filenames, so that is not a display artifact.
func TestConfigFlagMustNameTheStoresOwnConfig(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	resetRootPersistentFlags()
	t.Setenv("NIBS_PATH", "")

	storeDir := filepath.Join(t.TempDir(), "proj", store.DirName)
	mkdirAllT(t, filepath.Join(storeDir, store.DataDirName))
	writeFileT(t, filepath.Join(storeDir, store.ConfigFileName), "nibs:\n  prefix: real-\n  id_length: 4\n")
	writeFileT(t, filepath.Join(storeDir, store.ConfigFileName+".bak"), "nibs:\n  prefix: other-\n  id_length: 8\n")

	configPath = filepath.Join(storeDir, store.ConfigFileName+".bak")
	got, err := resolveStoreDir()
	if err == nil {
		t.Fatalf("resolveStoreDir() = %q with no error; a config.yml.bak names no store", got)
	}
	for _, want := range []string{"must name a store's " + store.ConfigFileName, "prefix and id length"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %q, want it to mention %q", err.Error(), want)
		}
	}

	// The store's own config.yml stays accepted through the same route.
	resetRootPersistentFlags()
	configPath = filepath.Join(storeDir, store.ConfigFileName)
	got, err = resolveStoreDir()
	if err != nil {
		t.Fatalf("resolveStoreDir() with the store's own config: %v", err)
	}
	if got != storeDir {
		t.Errorf("resolveStoreDir() = %q, want %q", got, storeDir)
	}
}

// TestResolveStoreDirNormalizesTheStorePath pins that a trailing slash — what
// shell tab completion produces — names the same store as the bare spelling.
// Every derivation downstream is LEXICAL: filepath.Dir("<p>/.nibs/") is
// "<p>/.nibs", so the slashed spelling shifted store.Layout.ProjectDir() one
// level INTO the store, hid the project's `.nibs.yml` from the migration gate,
// and let `nibs new` persist a nib under the default prefix into a store every
// other spelling refuses.
func TestResolveStoreDirNormalizesTheStorePath(t *testing.T) {
	for _, slash := range []bool{false, true} {
		name := "bare"
		if slash {
			name = "trailing separator"
		}
		t.Run(name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			resetRootPersistentFlags()
			t.Setenv("NIBS_PATH", "")

			projectDir, storeDir := writeLegacyStoreNamed(t, store.DirName,
				"nibs:\n  prefix: leg-\n  id_length: 4\n", map[string]string{
					"leg-a1--one.md": layoutNib,
				})
			_ = projectDir
			spelling := storeDir
			if slash {
				spelling += string(os.PathSeparator)
			}

			nibsPath = spelling
			got, err := resolveStoreDir()
			if err != nil {
				t.Fatalf("resolveStoreDir(%q): %v", spelling, err)
			}
			if got != storeDir {
				t.Errorf("resolveStoreDir(%q) = %q, want the cleaned %q", spelling, got, storeDir)
			}

			// The gate is the consumer that matters: it derives the project from
			// the store's parent to find the pre-layout `.nibs.yml`.
			resetNewFlags()
			t.Cleanup(resetNewFlags)
			out, newErr := runRootWith(t, "--nibs-path", spelling, "new", "slash route", "-d", "-")
			if newErr == nil {
				t.Fatalf("`nibs new` succeeded on a store needing migration (out: %s)", out)
			}
			if !strings.Contains(newErr.Error(), "needs migration") {
				t.Errorf("error = %q, want the pending-migration refusal", newErr.Error())
			}
		})
	}
}

// TestResolveStoreDirExplainsANibsPathMigrateCannotRelocate covers the REJECT
// side of the store-evidence guard — the direction the previous round never
// enumerated.
//
// hasLegacyStoreShape only confirms a store that is an immediate subdirectory of
// the project holding `.nibs.yml`, and it requires an artifact nibs itself wrote
// inside it. The retired key accepted more shapes than that, and every shape it
// refuses must still get advice that CONVERGES — the previous message prescribed
// `nibs migrate --nibs-path <dataDir>`, which the guard then refused, leaving
// `nibs init` (which the same message calls harmful) as the only remaining
// suggestion.
//
// Each case executes the printed remedy and then requires `nibs list` to find the
// nib. Asserting only the wording is what let a dead end ship.
func TestResolveStoreDirExplainsANibsPathMigrateCannotRelocate(t *testing.T) {
	tests := []struct {
		name string
		// build lays out the project and returns the `nibs.path` VALUE to write
		// plus the directory that value resolves to.
		build func(t *testing.T, projectDir string) (declared, dataDir string)
		want  []string
		// nibFile is the file the remedy must move, or "" when the case has no
		// nib to recover (an untrusted directory naming itself).
		nibFile string
	}{
		{
			name: "nested below the project",
			build: func(t *testing.T, projectDir string) (string, string) {
				dir := filepath.Join(projectDir, "docs", "nibs")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "leg-a1--one.md"), layoutNib)
				return "docs/nibs", dir
			},
			want:    []string{"not an immediate subdirectory", "remove the `nibs.path` key"},
			nibFile: "leg-a1--one.md",
		},
		{
			name: "an absolute path outside the project",
			build: func(t *testing.T, projectDir string) (string, string) {
				dir := filepath.Join(filepath.Dir(projectDir), "outside")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "leg-a1--one.md"), layoutNib)
				return filepath.ToSlash(dir), dir
			},
			want:    []string{"not an immediate subdirectory", "remove the `nibs.path` key"},
			nibFile: "leg-a1--one.md",
		},
		{
			name: "the project directory itself",
			build: func(t *testing.T, projectDir string) (string, string) {
				writeFileT(t, filepath.Join(projectDir, "leg-a1--one.md"), layoutNib)
				return ".", projectDir
			},
			want:    []string{"not an immediate subdirectory", "remove the `nibs.path` key"},
			nibFile: "leg-a1--one.md",
		},
		{
			// The untrusted-repository shape: a clone chooses its own
			// `nibs.path`, and pre-layout `nibs init` never wrote a value other
			// than `.nibs` — so a config naming an ordinary content directory is
			// hand-authored, and nibs must not synthesize a mutating command from
			// it.
			name: "a directory holding nothing nibs wrote",
			build: func(t *testing.T, projectDir string) (string, string) {
				dir := filepath.Join(projectDir, "content")
				mkdirAllT(t, filepath.Join(dir, "posts"))
				writeFileT(t, filepath.Join(dir, "posts", "hello.md"), hugoPost)
				return "content", dir
			},
			want: []string{"nothing in it was written by nibs", "remove the `nibs.path` key"},
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
			mkdirAllT(t, projectDir)
			declared, dataDir := tt.build(t, projectDir)
			legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
			writeFileT(t, legacy, "nibs:\n  prefix: leg-\n  id_length: 4\n  path: "+declared+"\n")
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
			// The command must not be advertised for a shape the guard refuses.
			if strings.Contains(err.Error(), "nibs migrate --nibs-path") {
				t.Errorf("message = %q prescribes `nibs migrate --nibs-path`, which the store-evidence guard refuses for this shape", err.Error())
			}
			if strings.Contains(err.Error(), "run 'nibs init' to create one") {
				t.Errorf("message = %q suggests nibs init, which strands the data", err.Error())
			}
			// And the guard must really refuse it, so the two agree.
			nibsPath = dataDir
			if _, resolveErr := resolveStoreDir(); resolveErr == nil {
				t.Errorf("resolveStoreDir accepted %s as a store; the message says migrate will not relocate it", dataDir)
			}
			resetRootPersistentFlags()

			if tt.nibFile == "" {
				return
			}

			// Follow the printed remedy verbatim: create <project>/.nibs, move
			// the nib files into it, remove the retired key, run `nibs migrate`.
			storeDir := filepath.Join(projectDir, store.DirName)
			mkdirAllT(t, storeDir)
			if renameErr := os.Rename(filepath.Join(dataDir, tt.nibFile), filepath.Join(storeDir, tt.nibFile)); renameErr != nil {
				t.Fatalf("moving the nib files: %v", renameErr)
			}
			writeFileT(t, legacy, "nibs:\n  prefix: leg-\n  id_length: 4\n")
			t.Cleanup(resetMigrateFlags)
			resetMigrateFlags()
			if out, migrateErr := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty"); migrateErr != nil {
				t.Fatalf("the remedy the message printed did not work: %v\nout: %s", migrateErr, out)
			}

			resetRootPersistentFlags()
			t.Setenv("NIBS_PATH", "")
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
