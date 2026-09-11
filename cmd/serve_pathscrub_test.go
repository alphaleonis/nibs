package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/testskip"
)

// TestStorePathScrubberNamesTheStoreWithoutLocatingIt pins what the scrub keeps
// and what it removes: the file that failed is still identifiable, the directory
// holding it is not.
func TestStorePathScrubberNamesTheStoreWithoutLocatingIt(t *testing.T) {
	tmp := canonicalTempDir(t)
	project := filepath.Join(tmp, "proj")
	store := filepath.Join(project, ".nibs")
	// Outside the project, the way the OS temp directory holding a store's lock
	// file is outside the store it locks.
	lockDir := filepath.Join(tmp, "lock")
	mkdirAllT(t, store)
	mkdirAllT(t, lockDir)
	scrub := newStorePathScrubber(store, lockDir)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "an OS error's embedded path",
			in:   fmt.Sprintf("open %s: permission denied", filepath.Join(store, "areas.yml")),
			want: "open " + storePathPlaceholder + string(filepath.Separator) + "areas.yml: permission denied",
		},
		{
			name: "a path nibs interpolated itself",
			in:   fmt.Sprintf("%s is not readable as an areas vocabulary: yaml: line 1", filepath.Join(store, "areas.yml")),
			want: storePathPlaceholder + string(filepath.Separator) + "areas.yml is not readable as an areas vocabulary: yaml: line 1",
		},
		{
			name: "the project directory on its own",
			in:   "under " + project,
			want: "under " + projectPathPlaceholder,
		},
		{
			name: "the lock file a mutation could not open",
			in: fmt.Sprintf("opening lock file: open %s: permission denied",
				filepath.Join(lockDir, "nibs-write-1fe95e51f3527e63.lock")),
			want: "opening lock file: open " + lockDirPlaceholder + string(filepath.Separator) +
				"nibs-write-1fe95e51f3527e63.lock: permission denied",
		},
		{
			// The separator boundary addContainerOf registers: without it this
			// needle eats the start of a sibling that merely begins with the
			// lock directory's name.
			name: "a sibling whose name begins with the lock directory's",
			in:   "open " + lockDir + "files: permission denied",
			want: "open " + lockDir + "files: permission denied",
		},
		{
			name: "a message naming no path",
			in:   "etag mismatch: provided abc, current is def",
			want: "etag mismatch: provided abc, current is def",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := scrub(tc.in); got != tc.want {
				t.Errorf("scrub(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestStorePathScrubberPrefersTheStoreInsideTheLockDirectory pins the needle
// ORDER newStorePathScrubber registers in. A store served out of the OS temp
// directory — `task demo` serves one — sits inside the lock directory, so both
// match at the same offset and strings.Replacer settles it by argument order.
// The store is the spelling that leaves the message saying which file failed.
func TestStorePathScrubberPrefersTheStoreInsideTheLockDirectory(t *testing.T) {
	lockDir := canonicalTempDir(t)
	store := filepath.Join(lockDir, "demo", ".nibs")
	mkdirAllT(t, store)

	msg := "open " + filepath.Join(store, "areas.yml") + ": permission denied"
	want := "open " + storePathPlaceholder + string(filepath.Separator) + "areas.yml: permission denied"
	if got := newStorePathScrubber(store, lockDir)(msg); got != want {
		t.Errorf("scrub(%q) = %q, want %q", msg, got, want)
	}
}

// TestStorePathScrubberCoversTheResolvedSpelling executes the claim in
// newStorePathScrubber's doc comment that the resolved spelling is covered.
// Which spelling a message carries depends on whether the code that rendered it
// resolved the path, and the two differ without any symlink in the store on
// macOS (/var) and on a Windows runner (an 8.3 alias) — a symlinked temp
// directory is how that divergence is reproduced here.
func TestStorePathScrubberCoversTheResolvedSpelling(t *testing.T) {
	real := filepath.Join(canonicalTempDir(t), "real")
	mkdirAllT(t, filepath.Join(real, ".nibs"))
	link := filepath.Join(canonicalTempDir(t), "link")
	if err := os.Symlink(real, link); err != nil {
		testskip.SymlinkUnavailable(t, err)
	}

	// No lock directory: the subject here is the store's own spellings.
	scrub := newStorePathScrubber(filepath.Join(link, ".nibs"), "")

	for _, spelling := range []string{link, real} {
		msg := fmt.Sprintf("open %s: permission denied", filepath.Join(spelling, ".nibs", "areas.yml"))
		if got := scrub(msg); strings.Contains(got, spelling) {
			t.Errorf("scrub(%q) = %q, which still names %s", msg, got, spelling)
		}
	}
}

// TestStorePathScrubberLeavesUncoveredPathsUnchanged is the closing half of the
// canonical scope on newStorePathScrubber: a path in none of the directories it
// covers is not its subject. The guard exists because the doc would otherwise be
// the only statement of the limit, and a limit stated only in prose is the kind
// that quietly stops being true.
func TestStorePathScrubberLeavesUncoveredPathsUnchanged(t *testing.T) {
	tmp := canonicalTempDir(t)
	store := filepath.Join(tmp, "proj", ".nibs")
	lockDir := filepath.Join(tmp, "lock")
	mkdirAllT(t, store)
	mkdirAllT(t, lockDir)
	elsewhere := filepath.Join(tmp, "elsewhere", "cache")

	msg := "open " + elsewhere + ": permission denied"
	if got := newStorePathScrubber(store, lockDir)(msg); got != msg {
		t.Errorf("scrub(%q) = %q, want it unchanged", msg, got)
	}
}

// TestServedErrorPresenterKeepsTheContractsTheScrubSitsOn covers the two things
// the presenter must not disturb while rewriting a message: the wire code plus
// the "etag mismatch" wording the web client keeps as a substring fallback (see
// web/src/lib/nibForm.svelte.ts, isEtagConflict), and the Go error chain that
// cmd/set.go's mutationErrCode walks.
func TestServedErrorPresenterKeepsTheContractsTheScrubSitsOn(t *testing.T) {
	tmp := canonicalTempDir(t)
	store := filepath.Join(tmp, "proj", ".nibs")
	mkdirAllT(t, store)
	present := servedErrorPresenter(store, filepath.Join(tmp, "lock"))
	ctx := context.Background()

	t.Run("an etag conflict keeps its code and its wording", func(t *testing.T) {
		err := &nibcore.ETagMismatchError{Provided: "abc123", Current: "def456"}

		gqlErr := present(ctx, err)

		if gqlErr.Extensions["code"] != wireCodeETagMismatch {
			t.Errorf("extensions.code = %v, want %q", gqlErr.Extensions["code"], wireCodeETagMismatch)
		}
		if gqlErr.Message != err.Error() {
			t.Errorf("message = %q, want the verbatim %q", gqlErr.Message, err.Error())
		}
	})

	t.Run("a scrubbed message still carries its chain", func(t *testing.T) {
		// The shape the area resolvers produce: a typed IO failure wrapped in a
		// sentence, whose rendered text names the store.
		cause := fmt.Errorf("open %s: permission denied", filepath.Join(store, "areas.yml"))
		err := fmt.Errorf("nothing was written: %w",
			&nibcore.AreaEditIOError{Phase: nibcore.AreaEditPhaseLoadVocabulary, File: filepath.Join(store, "areas.yml"), Cause: cause})

		gqlErr := present(ctx, err)

		if strings.Contains(gqlErr.Message, store) {
			t.Errorf("message = %q, want the store path gone", gqlErr.Message)
		}
		var ioErr *nibcore.AreaEditIOError
		if !errors.As(gqlErr, &ioErr) {
			t.Errorf("%T no longer unwraps to *nibcore.AreaEditIOError", gqlErr)
		}
		if code, ok := mutationErrCode(gqlErr); !ok || code != output.ErrFileError {
			t.Errorf("mutationErrCode = (%q, %v), want (%q, true)", code, ok, output.ErrFileError)
		}
	})
}
