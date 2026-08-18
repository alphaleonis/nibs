package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/store"
)

// TestPreLayoutRemedyTreatsAnUnusablePathAsAbsence pins which of preLayoutRemedy's
// three answers a `nibs.path` that CANNOT NAME A DIRECTORY belongs to.
//
// The three answers are: it is there (relocate it), it is not there (find the
// files), and it could not be established (resolve the I/O problem, and change
// nothing). The third one's remedy — mount the volume, fix its permissions — is
// written for a path that is real but unreadable, and it also tells the reader NOT
// to remove the `nibs.path` key. A path the operating system rejects on sight is
// not unreadable evidence; it is absence, and sending its reader to check a mount
// wastes the one instruction they were given.
//
// The fixture is platform-specific because the failure is: each OS is asked for a
// path IT refuses, so the test proves the real classification rather than a
// synthetic errno's. See unusablepath_windows.go / unusablepath_other.go.
func TestPreLayoutRemedyTreatsAnUnusablePathAsAbsence(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	resetRootPersistentFlags()
	t.Setenv("NIBS_PATH", "")

	tmp := t.TempDir()
	t.Setenv("NIBS_CONFIG_ROOT", tmp)
	projectDir := filepath.Join(tmp, "proj")
	sub := filepath.Join(projectDir, "sub")
	mkdirAllT(t, sub)

	declared := unusablePathFixture(t, projectDir)
	writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName),
		"nibs:\n  path: \""+declared+"\"\n")
	t.Chdir(sub)

	_, err := resolveStoreDir()
	if err == nil {
		t.Fatal("resolveStoreDir accepted a project whose nibs.path cannot name a directory")
	}
	msg := err.Error()

	// The remedy for unreadable evidence, which is the wrong one here.
	if strings.Contains(msg, "mount the volume") {
		t.Errorf("an unusable nibs.path was reported as unreadable evidence, so the remedy sends the reader to check a mount that cannot help: %q", msg)
	}
	if strings.Contains(msg, "do NOT remove the `nibs.path` key") {
		t.Errorf("an unusable nibs.path was reported as undetermined, so the reader is told to keep a key that names nowhere: %q", msg)
	}
	// The remedy for absence, which is the right one.
	if !strings.Contains(msg, "does not exist") {
		t.Errorf("an unusable nibs.path must be reported as absence: %q", msg)
	}
	// Whichever answer it gives, it must still name the key — that is the
	// diagnostic the reader acts on, and a reclassification that dropped it would
	// trade one bad message for another.
	if !strings.Contains(msg, "nibs.path") {
		t.Errorf("reclassifying must not cost the diagnostic: %q", msg)
	}
}

// TestUnusablePathFixtureIsReallyUnusable keeps the test above from passing
// vacuously. If the fixture ever becomes a path the OS accepts, the assertions
// would be measuring an ordinary missing directory — which reaches the absence
// branch anyway, through fs.ErrNotExist, and would prove nothing about this fix.
func TestUnusablePathFixtureIsReallyUnusable(t *testing.T) {
	projectDir := t.TempDir()
	declared := unusablePathFixture(t, projectDir)

	_, err := statPathForTest(filepath.Join(projectDir, declared))
	if err == nil {
		t.Fatalf("the fixture path %q is stattable, so it is not unusable and the classification test asserts nothing", declared)
	}
	if isNotExistForTest(err) {
		t.Fatalf("the fixture path %q fails as ordinary absence (%v), which the untouched branch already handles — the classification test asserts nothing", declared, err)
	}
	if !isUnusablePath(err) {
		t.Fatalf("the fixture path %q fails with %v, which isUnusablePath does not recognize — the classification under test never fires", declared, err)
	}
}
