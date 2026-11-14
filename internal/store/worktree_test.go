package store

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/testskip"
)

// fixtureGitEnv isolates fixture git commands from the developer's config and
// from any GIT_* variable the test run inherited, such as GIT_DIR inside a hook.
func fixtureGitEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(kv), "GIT_") {
			env = append(env, kv)
		}
	}
	return append(env,
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = fixtureGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// initRepo creates a repository with one commit at dir, so worktrees can be
// added to it. Extra init arguments go before the directory.
func initRepo(t *testing.T, dir string, initArgs ...string) {
	t.Helper()
	runGit(t, filepath.Dir(dir), append(append([]string{"init", "-q"}, initArgs...), dir)...)
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

// shortenGitTimeout keeps a fixture that makes git hang from costing the full
// production deadline.
func shortenGitTimeout(t *testing.T) {
	t.Helper()
	saved := gitTimeout
	gitTimeout = 500 * time.Millisecond
	t.Cleanup(func() { gitTimeout = saved })
}

// mainWithWorktree builds root/m with one commit and a linked worktree at
// root/w, outside the main tree, and returns both paths.
func mainWithWorktree(t *testing.T, root string) (mainTree, worktree string) {
	t.Helper()
	mainTree = filepath.Join(root, "m")
	worktree = filepath.Join(root, "w")
	initRepo(t, mainTree)
	runGit(t, mainTree, "worktree", "add", "-q", worktree)
	return mainTree, worktree
}

// resolved returns path with symlinks evaluated, since git prints resolved paths
// and the temp directory sits behind a symlink on some platforms.
func resolved(t *testing.T, path string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", path, err)
	}
	return r
}

// TestFindNearestMarker_Worktree covers the fallback from a linked worktree
// with no marker of its own to the main working tree.
func TestFindNearestMarker_Worktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		testskip.Unavailable(t, testskip.Git, "git is not on PATH: %v", err)
	}

	tests := []struct {
		name string
		// build lays out the tree under root and returns the directory to walk
		// from and the marker expected there.
		build func(t *testing.T, root string) (start string, want Marker)
	}{
		{
			name: "a linked worktree falls back to the main clone's .nibs",
			build: func(t *testing.T, root string) (string, Marker) {
				mainTree, wt := mainWithWorktree(t, root)
				return mkSub(t, wt, "sub"), Marker{Kind: MarkerStore, Path: mkStore(t, mainTree)}
			},
		},
		{
			name: "the worktree's own .nibs wins",
			build: func(t *testing.T, root string) (string, Marker) {
				mainTree, wt := mainWithWorktree(t, root)
				mkStore(t, mainTree)
				return wt, Marker{Kind: MarkerStore, Path: mkStore(t, wt)}
			},
		},
		{
			name: "git missing from PATH gives no fallback",
			build: func(t *testing.T, root string) (string, Marker) {
				mainTree, wt := mainWithWorktree(t, root)
				mkStore(t, mainTree)
				t.Setenv("PATH", t.TempDir())
				return wt, Marker{}
			},
		},
		{
			// The would-be main tree beside the bare repository holds a store,
			// so only the basename rule keeps the fallback from binding to it.
			name: "a bare repo gives no fallback",
			build: func(t *testing.T, root string) (string, Marker) {
				src := filepath.Join(root, "src")
				initRepo(t, src)
				bare := filepath.Join(mkSub(t, root, "bare"), "b.git")
				runGit(t, root, "clone", "-q", "--bare", src, bare)
				mkStore(t, filepath.Dir(bare))
				wt := filepath.Join(mkSub(t, root, "wts"), "w")
				runGit(t, bare, "worktree", "add", "-q", wt)
				return wt, Marker{}
			},
		},
		{
			name: "a --separate-git-dir repo gives no fallback",
			build: func(t *testing.T, root string) (string, Marker) {
				gitDir := filepath.Join(mkSub(t, root, "sep"), "m.git")
				mainTree := filepath.Join(root, "m")
				initRepo(t, mainTree, "--separate-git-dir", gitDir)
				mkStore(t, mainTree)
				mkStore(t, filepath.Dir(gitDir))
				wt := filepath.Join(mkSub(t, root, "wts"), "w")
				runGit(t, mainTree, "worktree", "add", "-q", wt)
				return wt, Marker{}
			},
		},
		{
			// Honored, these would make a plain directory report the real
			// worktree and lead the fallback to the main clone's store.
			name: "a GIT_DIR pointing elsewhere is ignored",
			build: func(t *testing.T, root string) (string, Marker) {
				mainTree, wt := mainWithWorktree(t, root)
				mkStore(t, mainTree)
				t.Setenv("GIT_DIR", filepath.Join(mainTree, ".git", "worktrees", "w"))
				t.Setenv("GIT_WORK_TREE", wt)
				return mkSub(t, root, "plain"), Marker{}
			},
		},
		{
			name: "a main tree above NIBS_CONFIG_ROOT gives no fallback",
			build: func(t *testing.T, root string) (string, Marker) {
				mainTree := filepath.Join(root, "m")
				initRepo(t, mainTree)
				mkStore(t, mainTree)
				ceiling := mkSub(t, root, "c")
				wt := filepath.Join(ceiling, "w")
				runGit(t, mainTree, "worktree", "add", "-q", wt)
				t.Setenv("NIBS_CONFIG_ROOT", ceiling)
				return wt, Marker{}
			},
		},
		{
			// git prints resolved paths, so the ceiling is compared resolved too.
			name: "a NIBS_CONFIG_ROOT reached through a symlink admits the main tree",
			build: func(t *testing.T, root string) (string, Marker) {
				mainTree, wt := mainWithWorktree(t, root)
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(root, link); err != nil {
					testskip.SymlinkUnavailable(t, err)
				}
				t.Setenv("NIBS_CONFIG_ROOT", link)
				return wt, Marker{Kind: MarkerStore, Path: mkStore(t, mainTree)}
			},
		},
		{
			// git takes the common dir from a gitdir the tree itself supplies.
			name: "a .git file with an in-tree gitdir naming another repo gives no fallback",
			build: func(t *testing.T, root string) (string, Marker) {
				victim := filepath.Join(root, "victim")
				initRepo(t, victim)
				mkStore(t, victim)
				evil := mkSub(t, root, "evil")
				fake := mkSub(t, evil, "fakegit")
				writeFile(t, filepath.Join(evil, ".git"), "gitdir: fakegit\n")
				writeFile(t, filepath.Join(fake, "commondir"), filepath.ToSlash(filepath.Join(victim, ".git"))+"\n")
				writeFile(t, filepath.Join(fake, "HEAD"), "ref: refs/heads/main\n")
				writeFile(t, filepath.Join(fake, "gitdir"), filepath.ToSlash(filepath.Join(evil, ".git"))+"\n")
				return evil, Marker{}
			},
		},
		{
			// The worktree entry is real, but its back-pointer names the real
			// worktree, not this directory.
			name: "a .git file claiming another directory's worktree entry gives no fallback",
			build: func(t *testing.T, root string) (string, Marker) {
				mainTree, _ := mainWithWorktree(t, root)
				mkStore(t, mainTree)
				impostor := mkSub(t, root, "x")
				entry := filepath.Join(mainTree, ".git", "worktrees", "w")
				writeFile(t, filepath.Join(impostor, ".git"), "gitdir: "+filepath.ToSlash(entry)+"\n")
				return impostor, Marker{}
			},
		},
		{
			name: "a worktree moved with git worktree move still falls back",
			build: func(t *testing.T, root string) (string, Marker) {
				mainTree, wt := mainWithWorktree(t, root)
				moved := filepath.Join(mkSub(t, root, "elsewhere"), "moved")
				runGit(t, mainTree, "worktree", "move", wt, moved)
				return moved, Marker{Kind: MarkerStore, Path: mkStore(t, mainTree)}
			},
		},
		{
			// The form `git worktree add --relative-paths` writes, relative to
			// the worktree's git dir.
			name: "a relative back-pointer still falls back",
			build: func(t *testing.T, root string) (string, Marker) {
				mainTree, wt := mainWithWorktree(t, root)
				entry := filepath.Join(mainTree, ".git", "worktrees", "w")
				rel, err := filepath.Rel(entry, filepath.Join(wt, ".git"))
				if err != nil {
					t.Fatalf("Rel: %v", err)
				}
				writeFile(t, filepath.Join(entry, "gitdir"), filepath.ToSlash(rel)+"\n")
				return wt, Marker{Kind: MarkerStore, Path: mkStore(t, mainTree)}
			},
		},
		{
			// A repository shipped in an archive controls its own worktree
			// entries, so the back-pointer read must not block.
			name: "a back-pointer that is a named pipe gives no fallback",
			build: func(t *testing.T, root string) (string, Marker) {
				mainTree, wt := mainWithWorktree(t, root)
				mkStore(t, mainTree)
				backPointer := filepath.Join(mainTree, ".git", "worktrees", "w", "gitdir")
				if err := os.Remove(backPointer); err != nil {
					t.Fatalf("Remove: %v", err)
				}
				if err := mkfifo(backPointer); err != nil {
					testskip.Unavailable(t, testskip.NamedPipes, "mkfifo(%s): %v", backPointer, err)
				}
				return wt, Marker{}
			},
		},
		{
			// git follows the link to the real worktree's entry, and that entry's
			// back-pointer names the link's target.
			name: "a .git symlink to another worktree's .git file gives no fallback",
			build: func(t *testing.T, root string) (string, Marker) {
				mainTree, wt := mainWithWorktree(t, root)
				mkStore(t, mainTree)
				evil := mkSub(t, root, "evil")
				if err := os.Symlink(filepath.Join(wt, ".git"), filepath.Join(evil, ".git")); err != nil {
					testskip.SymlinkUnavailable(t, err)
				}
				return evil, Marker{}
			},
		},
		{
			// No `.git` file sits above the start, so no linked worktree can hold
			// it and git must not run at all.
			name: "a main clone whose git would hang gives no fallback",
			build: func(t *testing.T, root string) (string, Marker) {
				shortenGitTimeout(t)
				mainTree := filepath.Join(root, "m")
				initRepo(t, mainTree)
				hangGit(t, filepath.Join(mainTree, ".git"))
				return mkSub(t, mainTree, "sub"), Marker{}
			},
		},
		{
			// A symlinked `.git` cannot pass isRegisteredWorktree, so git need
			// not run even though it would follow the link and hang.
			name: "a .git symlink to a gitdir whose git would hang gives no fallback",
			build: func(t *testing.T, root string) (string, Marker) {
				shortenGitTimeout(t)
				mainTree := filepath.Join(root, "m")
				initRepo(t, mainTree)
				hangGit(t, filepath.Join(mainTree, ".git"))
				evil := mkSub(t, root, "evil")
				if err := os.Symlink(filepath.Join(mainTree, ".git"), filepath.Join(evil, ".git")); err != nil {
					testskip.SymlinkUnavailable(t, err)
				}
				return evil, Marker{}
			},
		},
		{
			// git skips a `.git` directory that is not a repository and keeps
			// walking to the worktree's `.git` file.
			name: "an empty .git directory below a linked worktree's root still falls back",
			build: func(t *testing.T, root string) (string, Marker) {
				mainTree, wt := mainWithWorktree(t, root)
				sub := mkSub(t, wt, "sub")
				mkSub(t, sub, ".git")
				return sub, Marker{Kind: MarkerStore, Path: mkStore(t, mainTree)}
			},
		},
		{
			name: "a pre-layout main clone yields MarkerLegacyProject",
			build: func(t *testing.T, root string) (string, Marker) {
				mainTree, wt := mainWithWorktree(t, root)
				return wt, Marker{Kind: MarkerLegacyProject, Path: writeLegacy(t, mainTree)}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("NIBS_CONFIG_ROOT", root)
			start, want := tt.build(t, root)

			got, err := FindNearestMarker(start)
			if err != nil {
				t.Fatalf("FindNearestMarker() error = %v", err)
			}
			if got.Kind != want.Kind {
				t.Fatalf("FindNearestMarker() = %+v, want %+v", got, want)
			}
			if want.Kind != MarkerNone && resolved(t, got.Path) != resolved(t, want.Path) {
				t.Errorf("FindNearestMarker() = %+v, want %+v", got, want)
			}
		})
	}
}

// hangGit makes git block on gitDir until it is killed, by putting a named pipe
// where git reads the commondir file.
func hangGit(t *testing.T, gitDir string) {
	t.Helper()
	commondir := filepath.Join(gitDir, "commondir")
	if err := mkfifo(commondir); err != nil {
		testskip.Unavailable(t, testskip.NamedPipes, "mkfifo(%s): %v", commondir, err)
	}
}

// TestFindNearestMarker_GitTimeout covers a git call that only the deadline
// ends, under a `.git` file of the shape a linked worktree or a submodule has.
func TestFindNearestMarker_GitTimeout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		testskip.Unavailable(t, testskip.Git, "git is not on PATH: %v", err)
	}
	for _, sub := range []string{"", filepath.Join("a", "b")} {
		t.Run("start "+filepath.ToSlash(filepath.Join("hang", sub)), func(t *testing.T) {
			shortenGitTimeout(t)
			root := t.TempDir()
			t.Setenv("NIBS_CONFIG_ROOT", root)
			hang := mkSub(t, root, "hang")
			fake := mkSub(t, hang, "fake")
			writeFile(t, filepath.Join(hang, ".git"), "gitdir: fake\n")
			writeFile(t, filepath.Join(fake, "HEAD"), "ref: refs/heads/main\n")
			hangGit(t, fake)
			start := filepath.Join(hang, sub)
			if err := os.MkdirAll(start, 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}

			got, err := FindNearestMarker(start)
			var timeout *GitTimeoutError
			if !errors.As(err, &timeout) {
				t.Fatalf("FindNearestMarker() = %+v, %v; want a *GitTimeoutError", got, err)
			}
			if got.Kind != MarkerNone {
				t.Errorf("FindNearestMarker() = %+v, want no marker beside the timeout", got)
			}
			if timeout.Dir != start || timeout.Timeout != gitTimeout {
				t.Errorf("GitTimeoutError = %+v, want Dir %q and Timeout %v", timeout, start, gitTimeout)
			}
		})
	}
}

// TestUnderGitFile_UnreadableEntry covers a `.git` entry the walk cannot
// check, which leaves the answer to git.
func TestUnderGitFile_UnreadableEntry(t *testing.T) {
	locked := mkSub(t, t.TempDir(), "locked")
	start := mkSub(t, locked, "sub")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if _, err := os.Lstat(filepath.Join(start, ".git")); !os.IsPermission(err) {
		testskip.Unavailable(t, testskip.UnreadablePaths, "Lstat under a mode-0 directory returned %v", err)
	}

	if !underGitFile(start) {
		t.Error("underGitFile() = false for an entry it cannot check, want true so git decides")
	}
}
