package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
)

// stubGitTimeout makes discovery report a git timeout wherever the real
// discovery found no marker, without waiting for the real deadline.
func stubGitTimeout(t *testing.T) {
	t.Helper()
	saved := findNearestMarkerFn
	t.Cleanup(func() { findNearestMarkerFn = saved })
	findNearestMarkerFn = func(dir string) (store.Marker, error) {
		marker, err := store.FindNearestMarker(dir)
		if err == nil && marker.Kind == store.MarkerNone {
			return store.Marker{}, &store.GitTimeoutError{Dir: dir, Timeout: 5 * time.Second}
		}
		return marker, err
	}
}

// gitTimeoutProject returns a directory with no nibs marker anywhere above it,
// bounded by NIBS_CONFIG_ROOT, as the cwd.
func gitTimeoutProject(t *testing.T) string {
	t.Helper()
	t.Setenv("NIBS_PATH", "")
	tmp := t.TempDir()
	t.Setenv("NIBS_CONFIG_ROOT", tmp)
	projectDir := filepath.Join(tmp, "proj")
	mkdirAllT(t, projectDir)
	t.Chdir(projectDir)
	return projectDir
}

// assertCannotTellAfterGitTimeout checks the refusal names the cwd and the
// deadline, and does not send the reader to create a store that may exist.
func assertCannotTellAfterGitTimeout(t *testing.T, msg, cwd string) {
	t.Helper()
	for _, want := range []string{"cannot tell", cwd, "5s", "--nibs-path"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal = %q, want it to name %q", msg, want)
		}
	}
	if strings.Contains(msg, "nibs init") {
		t.Errorf("refusal = %q advises `nibs init`, which splits the store when a main working tree holds one", msg)
	}
}

func TestResolveStoreDirCannotTellWhenGitTimesOut(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	resetRootPersistentFlags()
	stubGitTimeout(t)
	cwd := gitTimeoutProject(t)

	_, err := resolveStoreDir()
	if err == nil {
		t.Fatal("resolveStoreDir found a store where the walk found none and git timed out")
	}
	assertCannotTellAfterGitTimeout(t, err.Error(), cwd)
}

// TestPrimeCannotTellWhenGitTimesOut covers prime, which stays silent where
// there is no nibs project, and so must not read a timeout as one.
func TestPrimeCannotTellWhenGitTimesOut(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	resetRootPersistentFlags()
	stubGitTimeout(t)
	cwd := gitTimeoutProject(t)

	out, err := runRootWith(t, "prime")
	if err == nil {
		t.Fatalf("prime succeeded after git timed out; output:\n%s", out)
	}
	assertCannotTellAfterGitTimeout(t, err.Error(), cwd)
}

// TestPrimeStaysSilentInAMainCloneWhoseGitHangs runs the real discovery in a
// repository with no `.git` file above the cwd, where a hanging git must not
// turn prime's silence into a refusal.
func TestPrimeStaysSilentInAMainCloneWhoseGitHangs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		testskip.Unavailable(t, testskip.Git, "git is not on PATH: %v", err)
	}
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	resetRootPersistentFlags()
	t.Setenv("NIBS_PATH", "")
	tmp := t.TempDir()
	t.Setenv("NIBS_CONFIG_ROOT", tmp)
	mainTree := filepath.Join(tmp, "m")
	gitInit := exec.Command("git", "init", "-q", mainTree)
	gitInit.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	commondir := filepath.Join(mainTree, ".git", "commondir")
	if err := mkfifo(commondir); err != nil {
		testskip.Unavailable(t, testskip.NamedPipes, "mkfifo(%s): %v", commondir, err)
	}
	sub := filepath.Join(mainTree, "src")
	mkdirAllT(t, sub)
	t.Chdir(sub)

	out, err := runRootWith(t, "prime")
	if err != nil {
		t.Fatalf("prime in a main clone with no nibs marker = %v, want a silent success", err)
	}
	if out != "" {
		t.Errorf("prime wrote %q where there is no nibs project, want nothing", out)
	}
}
