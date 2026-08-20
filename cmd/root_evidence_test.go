package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
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
		testskip.SymlinkUnavailable(t, err)
	}
	if _, err := os.Stat(path); err == nil {
		testskip.Unavailable(t, testskip.Symlinks, "this filesystem resolves a self-referential symlink at %s", path)
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
		testskip.Unavailable(t, testskip.UnreadablePaths, "os.Chmod(dir, 0): %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := os.ReadDir(dir); err == nil {
		testskip.Unavailable(t, testskip.UnreadablePaths, "this process reads a mode-000 directory anyway (running as root?)")
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
			// A TORN nib is determinate evidence, not unreadable evidence: the
			// header scan read every byte of it and extracted the keys, it simply
			// never met a closing fence. Routed to the third answer it locked
			// every command out of a pre-layout project — `nibs migrate`
			// included, the one command that would fix it — and prescribed
			// mounting a volume that was never absent.
			name: "a declared directory holding a nib whose front matter never closes",
			build: func(t *testing.T, tmp string) string {
				proj := filepath.Join(tmp, "proj")
				dir := filepath.Join(proj, "nibdata")
				mkdirAllT(t, dir)
				// No `status:`, so it does not corroborate — which is what makes
				// this a refusal row at all. The point is WHICH refusal.
				writeFileT(t, filepath.Join(dir, "leg-a1--torn.md"), "---\ntitle: Torn\n")
				writeFileT(t, filepath.Join(proj, store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n  path: nibdata\n")
				return dir
			},
			want:    []string{"nothing in it was written by nibs"},
			notWant: []string{"cannot tell whether", "mount the volume"},
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
// side of the store-evidence guard.
//
// hasLegacyStoreShape only confirms a store that is an immediate subdirectory of
// the project holding `.nibs.yml`, and it requires an artifact nibs itself wrote
// inside it. The retired key accepted more shapes than that, and every shape it
// refuses must still get advice that CONVERGES — the previous message prescribed
// `nibs migrate --nibs-path <dataDir>`, which the guard then refused, leaving
// `nibs init` (which the same message calls harmful) as the only remaining
// suggestion.
//
// Every case with a nib to recover EXECUTES the printed remedy and then requires
// `nibs list` to find that nib — asserting only the wording is what let a dead end
// ship. The one case without a nib (an untrusted directory naming itself, where
// there is nothing of the user's to move) stops at the wording and at the guard
// really refusing the directory, which is the whole claim for that row.
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
			if strings.Contains(err.Error(), "run `nibs init` to create one") {
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

// requireCaseInsensitivePathsT reports that dir is on a volume reaching one name
// through two spellings, and skips through internal/testskip where it is not.
//
// Probed in the test's OWN directory rather than decided from runtime.GOOS: case
// sensitivity is a property of the VOLUME, and one process sees both — WSL mounts
// case-insensitive DrvFs beside case-sensitive ext4, and macOS ships either — so
// the answer follows TMPDIR rather than the platform. A fixture staging two
// spellings of one directory is unbuildable where they are two directories, which
// is what makes this a testskip capability rather than an assertion.
func requireCaseInsensitivePathsT(t *testing.T, dir string) {
	t.Helper()
	probe := filepath.Join(dir, "case-probe")
	writeFileT(t, probe, "probe\n")
	t.Cleanup(func() { _ = os.Remove(probe) })
	if _, err := os.Stat(filepath.Join(dir, "CASE-PROBE")); err != nil {
		testskip.Unavailable(t, testskip.CaseInsensitivePaths, "os.Stat of a differently cased name: %v", err)
	}
}

// TestRefusalReportsWhatTheProjectConfigNames pins the naming clause of the
// explicit route's no-evidence refusal to what was actually CHECKED.
//
// "no `.nibs.yml` beside it names it" is a claim about a file, and the check
// behind it is a textual path comparison (see sameDir) — so a `.nibs.yml` sitting
// right there naming somewhere else made the refusal deny its own existence. The
// clause has to state the declared value where there is one, and keep the flat
// denial only where there is genuinely no config to speak of.
//
// The remedy has to converge with it. A config declaring some other store
// describes a pre-layout project, and "run `nibs init` there" is the advice that
// strands one — so where the flat denial goes, so does that advice, and what
// takes its place is preLayoutRemedy's answer: the same one the discovery route
// gives for the same project, which prescribes a command only for the shapes the
// store-evidence guard accepts.
func TestRefusalReportsWhatTheProjectConfigNames(t *testing.T) {
	flatDenial := "no " + store.LegacyProjectConfigFileName + " beside it names it"
	initAdvice := "or run `nibs init` there"

	tests := []struct {
		name string
		// build lays out the project and returns the directory to name.
		build   func(t *testing.T, projectDir string) string
		want    []string
		notWant []string
	}{
		{
			name: "a project config naming a different path",
			build: func(t *testing.T, projectDir string) string {
				writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName),
					"nibs:\n  prefix: leg-\n  id_length: 4\n  path: docs/nibs\n")
				dir := filepath.Join(projectDir, "ci")
				mkdirAllT(t, dir)
				return dir
			},
			// `docs/nibs` is a shape hasLegacyStoreShape refuses, so the remedy
			// is the manual one — and it must WARN against `nibs init` where the
			// flat denial prescribed it.
			want:    []string{"is not a nibs store", strconv.Quote("docs/nibs"), "do NOT run `nibs init`"},
			notWant: []string{flatDenial, initAdvice},
		},
		{
			// preLayoutRemedy's precondition: a store beside the pre-layout
			// config. Its remedy says to create that directory, so this branch
			// answers instead — and names the store rather than either `nibs
			// init` or a directory that is already there.
			name: "a project config naming a different path, with a store beside it",
			build: func(t *testing.T, projectDir string) string {
				writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName),
					"nibs:\n  prefix: leg-\n  id_length: 4\n  path: docs/nibs\n")
				mkdirAllT(t, filepath.Join(projectDir, store.DirName, store.DataDirName))
				dir := filepath.Join(projectDir, "ci")
				mkdirAllT(t, dir)
				return dir
			},
			want:    []string{"is not a nibs store", "the store this project already has"},
			notWant: []string{flatDenial, "nibs init"},
		},
		{
			// The determinate absence still has to read as one: with no config
			// beside it, the flat denial is what the check established, and
			// `nibs init` is advice the reader can act on.
			name: "no project config beside it at all",
			build: func(t *testing.T, projectDir string) string {
				dir := filepath.Join(projectDir, "ci")
				mkdirAllT(t, dir)
				return dir
			},
			want: []string{"is not a nibs store", flatDenial, initAdvice},
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
			nibsPath = tt.build(t, projectDir)

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
					t.Errorf("refusal = %q, must not say %q — it was never established", err.Error(), notWant)
				}
			}
		})
	}
}

// TestRefusalDoesNotDenyAConfigThatNamesTheDirectoryInAnotherCase is the same
// property where it is least visible, and where the old wording was flatly
// false: on a case-insensitive volume the declared `NibData` and the on-disk
// `nibdata` are ONE directory, so a refusal saying no config names it denied a
// file that names it — and then told the reader to run `nibs init` on top of
// their nibs.
//
// The refusal itself is deliberately unchanged: sameDir compares paths as text
// on every platform, because widening what counts as "the same directory" widens
// an authorization decision (what this guard accepts, `nibs migrate` may move and
// rewrite) onto a filesystem-dependent guess. What has to hold is that the
// message states the declared value rather than denying it exists, and hands the
// reader the way out: here the declared store is a shape the evidence guard
// accepts, so the remedy is the `nibs migrate` that relocates it — a command,
// not a spelling exercise.
func TestRefusalDoesNotDenyAConfigThatNamesTheDirectoryInAnotherCase(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	resetRootPersistentFlags()
	t.Setenv("NIBS_PATH", "")

	tmp := t.TempDir()
	requireCaseInsensitivePathsT(t, tmp)
	t.Setenv("NIBS_CONFIG_ROOT", tmp)

	projectDir := filepath.Join(tmp, "proj")
	onDisk := filepath.Join(projectDir, "nibdata")
	mkdirAllT(t, onDisk)
	writeFileT(t, filepath.Join(onDisk, "leg-a1--one.md"), layoutNib)
	writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName),
		"nibs:\n  prefix: leg-\n  id_length: 4\n  path: NibData\n")

	// The config's own spelling resolves, which is what makes the refusal below
	// a statement about spelling rather than about the fixture: both names reach
	// one store holding one real nib.
	declaredSpelling := filepath.Join(projectDir, "NibData")
	nibsPath = declaredSpelling
	if got, err := resolveStoreDir(); err != nil || got != declaredSpelling {
		t.Fatalf("resolveStoreDir(%s) = (%q, %v), want the store the config names", declaredSpelling, got, err)
	}

	resetRootPersistentFlags()
	nibsPath = onDisk
	got, err := resolveStoreDir()
	if err == nil {
		t.Fatalf("resolveStoreDir(%s) = %q with no error; this row exists for the refusal", onDisk, got)
	}
	assertClaimsNoDifference(t, err, onDisk, declaredSpelling)
	if want := strconv.Quote("NibData"); !strings.Contains(err.Error(), want) {
		t.Errorf("refusal = %q, want it to name %s — the reader cannot see the difference in spelling unless the message shows it", err.Error(), want)
	}
	// shellArg rather than the raw path: the argument is rendered for a shell,
	// and a test that restated the rendering would pass against a renderer that
	// disagreed with it.
	if want := "`nibs migrate --nibs-path " + shellArg(declaredSpelling) + "`"; !strings.Contains(err.Error(), want) {
		t.Errorf("refusal = %q, want it to prescribe %s — the declared store is one the evidence guard accepts, so the reader gets a command rather than being sent back to spell the path again", err.Error(), want)
	}
}

// falseDifferenceClaims are the things a refusal must never say about a fixture
// where the declared value and the named directory reach ONE directory under two
// names. Each is false there: the flat denial says no config beside it names this
// directory when one does, and the "different directory" wording restates the
// same unestablished conclusion — sameDir compares cleaned absolute paths as
// text, which answers whether the two SPELL the same path and says nothing about
// whether they open the same directory. Both are wordings this refusal has
// carried, which is why the list has two entries rather than one.
var falseDifferenceClaims = []string{
	"no " + store.LegacyProjectConfigFileName + " beside it names it",
	"names a different directory",
}

// assertClaimsNoDifference fails when err asserts a difference between two names
// that are one directory. It is the negative half of the two aliasing guards it
// sits between: everything they pin POSITIVELY — the declared value is echoed, a
// runnable command is prescribed — holds just as well in a message that also
// tells the reader their config points somewhere else, so the positive
// assertions alone cannot see this defect.
func assertClaimsNoDifference(t *testing.T, err error, named, declaredSpelling string) {
	t.Helper()
	for _, claim := range falseDifferenceClaims {
		if strings.Contains(err.Error(), claim) {
			t.Errorf("refusal = %q, and %q is false here: %s and %s are two names for one directory, so nothing about this fixture differs",
				err.Error(), claim, named, declaredSpelling)
		}
	}
}

// TestRefusalDoesNotDenyAConfigThatNamesTheDirectoryThroughASymlink is the same
// property as the case-variant guard above, staged so it runs EVERYWHERE.
//
// That guard needs a volume reaching one directory through two spellings, which
// this project's Linux machines and CI's ubuntu leg do not have — so a refusal
// reworded to assert exactly the difference it forbids clears it untouched, which
// is what a guard that skips where it is needed buys. A symlink alias stages the
// identical situation (`path: link`, `link -> nibdata`, one inode, and the
// declared spelling resolving as the store) on any filesystem with symlinks at
// all, which is every leg the case-variant fixture is unbuildable on.
//
// What both pin is that the refusal states the COMPARISON — the declared value,
// and that the match is textual — rather than concluding from a failed textual
// match that the config names somewhere else. Nothing here argues sameDir should
// resolve links: widening it would widen an authorization decision (what this
// guard accepts, `nibs migrate` may move and rewrite) onto a filesystem-dependent
// guess. The refusal is correct; only its account of itself has to be.
func TestRefusalDoesNotDenyAConfigThatNamesTheDirectoryThroughASymlink(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	resetRootPersistentFlags()
	t.Setenv("NIBS_PATH", "")

	tmp := t.TempDir()
	t.Setenv("NIBS_CONFIG_ROOT", tmp)

	projectDir := filepath.Join(tmp, "proj")
	onDisk := filepath.Join(projectDir, "nibdata")
	mkdirAllT(t, onDisk)
	writeFileT(t, filepath.Join(onDisk, "leg-a1--one.md"), layoutNib)

	declaredSpelling := filepath.Join(projectDir, "link")
	if err := os.Symlink(onDisk, declaredSpelling); err != nil {
		testskip.SymlinkUnavailable(t, err)
	}
	// A filesystem that quietly copies or resolves the link away leaves two
	// directories, and the aliasing this guard is about cannot be staged there —
	// the other half of what testskip.Symlinks covers.
	realInfo, statErr := os.Stat(onDisk)
	if statErr != nil {
		t.Fatalf("stat %s: %v", onDisk, statErr)
	}
	linkInfo, statErr := os.Stat(declaredSpelling)
	if statErr != nil {
		t.Fatalf("stat %s: %v", declaredSpelling, statErr)
	}
	if !os.SameFile(realInfo, linkInfo) {
		testskip.Unavailable(t, testskip.Symlinks, "os.SameFile(%s, %s) = false, so the link is not an alias here", onDisk, declaredSpelling)
	}

	writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName),
		"nibs:\n  prefix: leg-\n  id_length: 4\n  path: link\n")

	// The config's own spelling resolves, which is what makes the refusal below a
	// statement about spelling rather than about the fixture: both names reach one
	// store holding one real nib.
	nibsPath = declaredSpelling
	if got, err := resolveStoreDir(); err != nil || got != declaredSpelling {
		t.Fatalf("resolveStoreDir(%s) = (%q, %v), want the store the config names", declaredSpelling, got, err)
	}

	resetRootPersistentFlags()
	nibsPath = onDisk
	got, err := resolveStoreDir()
	if err == nil {
		t.Fatalf("resolveStoreDir(%s) = %q with no error; this guard exists for the refusal", onDisk, got)
	}
	assertClaimsNoDifference(t, err, onDisk, declaredSpelling)
	if want := strconv.Quote("link"); !strings.Contains(err.Error(), want) {
		t.Errorf("refusal = %q, want it to name %s — the reader cannot see which spelling the config used unless the message shows it", err.Error(), want)
	}
	if want := "`nibs migrate --nibs-path " + shellArg(declaredSpelling) + "`"; !strings.Contains(err.Error(), want) {
		t.Errorf("refusal = %q, want it to prescribe %s — the declared store is one the evidence guard accepts, so the reader gets a command rather than being sent back to spell the path again", err.Error(), want)
	}
}

// nibShapedForeignFile is a docs page wearing the shape nib.Render writes: the
// id comment, then the three keys renderFrontMatter never omits. nibs did not
// write it, and no scan of a file's front matter can tell — which is exactly
// what the corroboration check can and cannot establish.
const nibShapedForeignFile = "---\n# leg-a1\nversion: 1\ntitle: Configuration\nstatus: draft\n---\n\nHow to configure the thing.\n"

// TestCorroborationDocMatchesWhatTheArtifactProves holds every comment that
// describes the corroborating artifact to what the check actually establishes.
//
// The bar is now the whole shape nib.Render writes, not the lone `status:` that
// ordinary notes reached. That is a real narrowing, and it is pinned where it
// belongs — in TestResolveStoreDirRequiresStoreEvidence. What it is NOT is a
// proof of provenance, and that is this test's subject: the check reads a shape,
// so anything that knows the shape can wear it, and a comment claiming otherwise
// would be the reason a future reader stopped looking for what really bounds the
// damage.
//
// The claim is measured rather than assumed, and BOTH directions fail. If the
// check ever stops accepting a hand-authored file in the rendered shape, this
// test fails too — the comments would then be free to say something it forbids,
// and that is a decision to make deliberately rather than to inherit.
func TestCorroborationDocMatchesWhatTheArtifactProves(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "content")
	mkdirAllT(t, dir)
	writeFileT(t, filepath.Join(dir, "configuration.md"), nibShapedForeignFile)

	corroborated, err := declaredStoreCorroborated(dir)
	if err != nil {
		t.Fatalf("declaredStoreCorroborated(%s): %v", dir, err)
	}
	if !corroborated {
		t.Fatalf("declaredStoreCorroborated refused a file in the rendered shape that nibs did not write; "+
			"the check now establishes more than a shape, so re-read the comments it holds "+
			"(%d of them) and this guard together before changing either", len(artifactComments(t)))
	}

	for _, site := range artifactComments(t) {
		for _, sentence := range docSentences(site.doc) {
			if claimsTheKeyIsExclusive(sentence) ||
				(namesForeignFrontMatterTool(sentence) && deniesCarryingTheKey(sentence)) {
				t.Errorf("%s says %q, but the check reads a SHAPE — a file nibs never wrote "+
					"passes it whenever it carries that shape", site.name, strings.TrimSpace(sentence))
			}
		}
	}

	doc := docCommentOf(t, rootRefusalFile, "declaredStoreCorroborated")
	if !strings.Contains(doc, "isRealImmediateChild") {
		t.Errorf("declaredStoreCorroborated's doc comment does not name isRealImmediateChild; "+
			"corroboration only stops an accident, so the comment has to point at what actually "+
			"bounds a repository that chose its own `nibs.path`\n\ncomment:\n%s", doc)
	}
}

// artifactComment is one comment that tells a reader what the corroborating
// `status:` proves.
type artifactComment struct {
	name string
	doc  string
}

// artifactComments returns every comment that describes the corroborating
// artifact. Both are listed because the overstated claim was written twice, in
// different words — one naming other tools, one naming none — so a guard reading
// only the predicate's own comment would have left the other standing.
func artifactComments(t *testing.T) []artifactComment {
	t.Helper()
	return []artifactComment{
		{
			name: "declaredStoreCorroborated's doc comment",
			doc:  docCommentOf(t, rootRefusalFile, "declaredStoreCorroborated"),
		},
		{
			name: "fmHeader.status's comment",
			doc:  docCommentOfField(t, "migrate.go", "fmHeader", "status"),
		},
		{
			// Where the rule itself now lives, and so where the next
			// overstatement of it would be written.
			name: "nibFileFormat's doc comment",
			doc:  docCommentOfType(t, "migrate.go", "nibFileFormat"),
		},
	}
}

// claimsTheKeyIsExclusive matches a sentence asserting that carrying the key
// tells a nib from anything else — "distinguishes a file nibs wrote from any
// other front-mattered markdown", "anything only nibs writes". It is the same
// claim deniesCarryingTheKey catches, made WITHOUT naming whose front matter is
// being ruled out, which is how the second instance escaped the first guard.
var claimsTheKeyIsExclusive = regexp.MustCompile(
	`(?i)(?:\bdistinguish\w*\b.{0,80}?\bfrom\b|\bonly nibs\b\s*\w*\s*(?:writes|wrote|produces)|\banything only nibs\b)`).MatchString

// docCommentOfField returns the comment on the named field of the named struct
// type, failing the test when any part of that path is missing — a guard that
// stops finding what it reads must fail rather than pass vacuously.
func docCommentOfField(t *testing.T, file, typeName, field string) string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != typeName {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("%s in %s is not a struct type", typeName, file)
			}
			for _, f := range st.Fields.List {
				for _, name := range f.Names {
					if name.Name != field {
						continue
					}
					if f.Doc == nil {
						t.Fatalf("%s.%s in %s has no comment", typeName, field, file)
					}
					return f.Doc.Text()
				}
			}
			t.Fatalf("%s in %s has no field named %s", typeName, file, field)
		}
	}
	t.Fatalf("%s declares no type named %s", file, typeName)
	return ""
}

// foreignFrontMatterTool matches a name for someone else's front matter — the
// subject of the claim this guard is about. The corrected comment names the same
// tools to say the opposite thing, so naming one is not by itself a failure.
var foreignFrontMatterTool = regexp.MustCompile(`(?i)\b(hugo|jekyll|obsidian|dendron|mkdocs|docusaurus|docs[- ]site)\b`)

// deniesCarryingTheKey matches a negated claim about carrying, having, using or
// writing something — "does not carry it", "never carries a status". It is
// deliberately narrower than "any negation": a sentence may say what
// corroboration does NOT prove without asserting anything about another tool's
// front matter.
var deniesCarryingTheKey = regexp.MustCompile(`(?i)\b(?:does not|do not|doesn't|don't|never|cannot|can't|no)\s+(?:\w+\s+){0,3}(?:carry|carries|carried|have|has|use|uses|used|write|writes|set|sets)\b`).MatchString

// namesForeignFrontMatterTool reports whether s names someone else's front
// matter generator or note vault.
func namesForeignFrontMatterTool(s string) bool {
	return foreignFrontMatterTool.MatchString(s)
}

// docSentences splits a doc comment into sentences so a claim is judged against
// the words around it. Both halves of the claim must land in ONE sentence: a
// comment that names Hugo in one place and denies something unrelated in another
// is not making this claim.
func docSentences(doc string) []string {
	return regexp.MustCompile(`(?m)(?:\.\s|\.$|\n\s*\n)`).Split(doc, -1)
}

// docCommentOf returns the doc comment on the named top-level function in the
// given source file, failing the test when either is missing — a guard that
// stops finding what it reads must fail rather than pass vacuously.
func docCommentOf(t *testing.T, file, fn string) string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	for _, decl := range parsed.Decls {
		d, ok := decl.(*ast.FuncDecl)
		if !ok || d.Name.Name != fn {
			continue
		}
		if d.Doc == nil {
			t.Fatalf("%s in %s has no doc comment", fn, file)
		}
		return d.Doc.Text()
	}
	t.Fatalf("%s declares no function named %s", file, fn)
	return ""
}

// vaultNote is a note vault's own markdown: a title, tags, and a `status:` the
// vault tracks its own work with. It is not a nib and nibs never wrote it, but
// it carries the one key store corroboration used to key on.
const vaultNote = "---\ntitle: Weekly review\ntags: [notes]\nstatus: in-progress\n---\n\n# Weekly review\n"

// hugoPostWithStatus is hugoPost's shape carrying a nibs `status:` value, which
// docs sites routinely do to mark a page's own state.
const hugoPostWithStatus = "---\ntitle: Hello\ndate: 2026-01-02\nstatus: draft\n---\n\nA blog post.\n"

// docCommentOfType returns the doc comment on the named top-level type, failing
// the test when it is missing — a guard that stops finding what it reads must
// fail rather than pass vacuously.
func docCommentOfType(t *testing.T, file, typeName string) string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != typeName {
				continue
			}
			// A single-spec `type X struct` carries its comment on the GenDecl;
			// one inside a parenthesized block carries it on the TypeSpec.
			if ts.Doc != nil {
				return ts.Doc.Text()
			}
			if gen.Doc != nil {
				return gen.Doc.Text()
			}
			t.Fatalf("%s in %s has no doc comment", typeName, file)
		}
	}
	t.Fatalf("%s declares no type named %s", file, typeName)
	return ""
}
