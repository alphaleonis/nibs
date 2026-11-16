//go:build !windows

package nibcore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// redirectTempDir points os.TempDir at a fresh directory for the test and
// returns it, along with the lock directory nibs derives under it.
func redirectTempDir(t *testing.T) (tmp, locks string) {
	t.Helper()
	tmp = t.TempDir()
	t.Setenv("TMPDIR", tmp)
	return tmp, filepath.Join(tmp, fmt.Sprintf("nibs-%d", os.Getuid()))
}

func TestLockFilesGoInAPrivatePerUserDirectory(t *testing.T) {
	_, locks := redirectTempDir(t)
	root := t.TempDir()

	if got := filepath.Dir(writeLockPath(root)); got != locks {
		t.Fatalf("the write lock lives in %s, want %s", got, locks)
	}
	if got := filepath.Dir(serveLockPath(root)); got != locks {
		t.Fatalf("the serve lock lives in %s, want %s", got, locks)
	}

	lock, err := AcquireStoreLock(root)
	if err != nil {
		t.Fatalf("AcquireStoreLock: %v", err)
	}
	defer func() { _ = lock.Release() }()
	serve, err := AcquireServeLock(root)
	if err != nil {
		t.Fatalf("AcquireServeLock: %v", err)
	}
	defer func() { _ = serve.Release() }()

	info, err := os.Lstat(locks)
	if err != nil {
		t.Fatalf("stat the lock directory: %v", err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Errorf("the lock directory has mode %v, want a directory with 0700", info.Mode())
	}
	for _, path := range []string{writeLockPath(root), serveLockPath(root)} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s has mode %v, want 0600", filepath.Base(path), info.Mode().Perm())
		}
	}
}

func TestLocksInThePerUserDirectoryStillExclude(t *testing.T) {
	redirectTempDir(t)
	root := t.TempDir()

	held, err := AcquireStoreLock(root)
	if err != nil {
		t.Fatalf("AcquireStoreLock: %v", err)
	}
	defer func() { _ = held.Release() }()

	release, err := acquireFileLockTry(writeLockPath(root))
	if err == nil {
		_ = release()
		t.Fatal("a second descriptor took the write lock while the first held it")
	}
	if !errors.Is(err, errLockHeld) {
		t.Fatalf("the second try failed with %v, want errLockHeld", err)
	}
}

// lockAcquirers is every route into a lock file, so a refusal of the lock
// directory is proved on each of them.
var lockAcquirers = []struct {
	name    string
	acquire func(root string) error
}{
	{"store lock", func(root string) error {
		l, err := AcquireStoreLock(root)
		if err == nil {
			_ = l.Release()
		}
		return err
	}},
	{"try", func(root string) error {
		release, err := acquireFileLockTry(writeLockPath(root))
		if err == nil {
			_ = release()
		}
		return err
	}},
	{"cancellable wait", func(root string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		release, err := acquireFileLockWaiting(ctx, writeLockPath(root))
		if err == nil {
			_ = release()
		}
		return err
	}},
	{"serve lock", func(root string) error {
		l, err := AcquireServeLock(root)
		if err == nil {
			_ = l.Release()
		}
		return err
	}},
	{"serve exclusion", func(root string) error {
		l, err := AcquireServeExclusion(root)
		if err == nil {
			_ = l.Release()
		}
		return err
	}},
}

func TestAnUnsafeLockDirectoryIsRefused(t *testing.T) {
	tests := []struct {
		name    string
		plant   func(t *testing.T, locks string)
		problem string
	}{
		{
			name: "a symlink to a directory this user owns",
			plant: func(t *testing.T, locks string) {
				if err := os.Symlink(t.TempDir(), locks); err != nil {
					t.Fatalf("symlink: %v", err)
				}
			},
			problem: "symlink",
		},
		{
			name: "a directory other users can write to",
			plant: func(t *testing.T, locks string) {
				if err := os.Mkdir(locks, 0o700); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.Chmod(locks, 0o777); err != nil {
					t.Fatalf("chmod: %v", err)
				}
			},
			problem: "0777",
		},
		{
			name: "a directory other users can read",
			plant: func(t *testing.T, locks string) {
				if err := os.Mkdir(locks, 0o700); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.Chmod(locks, 0o750); err != nil {
					t.Fatalf("chmod: %v", err)
				}
			},
			problem: "0750",
		},
		{
			name: "a regular file",
			plant: func(t *testing.T, locks string) {
				if err := os.WriteFile(locks, nil, 0o600); err != nil {
					t.Fatalf("write: %v", err)
				}
			},
			problem: "not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, acq := range lockAcquirers {
				t.Run(acq.name, func(t *testing.T) {
					_, locks := redirectTempDir(t)
					tt.plant(t, locks)

					err := acq.acquire(t.TempDir())
					if err == nil {
						t.Fatal("the lock was taken in an unsafe lock directory")
					}
					// The directory's path holds the test's name and random digits, so
					// the problem is looked for in the rest of the message.
					msg := err.Error()
					if !strings.Contains(msg, locks) || !strings.Contains(strings.ReplaceAll(msg, locks, ""), tt.problem) {
						t.Errorf("the refusal does not name the directory %s and %q: %v", locks, tt.problem, err)
					}
				})
			}
		})
	}
}
