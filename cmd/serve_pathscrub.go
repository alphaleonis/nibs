package cmd

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// The placeholders a scrubbed message names a directory by. They keep the
// sentence readable — "open <store>/areas.yml: permission denied" still says
// which file failed — while the part that identifies the machine is gone.
const (
	storePathPlaceholder   = "<store>"
	projectPathPlaceholder = "<project>"
	lockDirPlaceholder     = "<lockdir>"
)

// servedErrorPresenter is the error presenter `nibs serve` installs: the
// extensions.code tagging of etagErrorPresenter, then a scrub of the rendered
// message. Which paths that scrub removes is newStorePathScrubber's doc.
//
// The presenter is where it runs because graphql.AddError puts every error the
// executor adds to a response through it, so all of the schema's mutations are
// covered at once and a new one cannot walk past it — and because
// cmd/graphql.go's executeQuery builds the same executable schema without one,
// so `nibs query` keeps the path an operator needs to repair a broken store.
// Redacting inside internal/graph would blind that too.
//
// Only Message is rewritten. gqlErr.Err still holds the original chain, which is
// what cmd/errors.go and cmd/set.go classify on.
func servedErrorPresenter(storeRoot, lockDir string) graphql.ErrorPresenterFunc {
	scrub := newStorePathScrubber(storeRoot, lockDir)
	return func(ctx context.Context, err error) *gqlerror.Error {
		gqlErr := etagErrorPresenter(ctx, err)
		if gqlErr == nil {
			return nil
		}
		gqlErr.Message = scrub(gqlErr.Message)
		return gqlErr
	}
}

// newStorePathScrubber returns a function replacing the directories a served
// message must not locate with placeholders.
//
// It works on the rendered TEXT rather than by unwrapping to *fs.PathError,
// because a path reaches a message two independent ways and only one of them is
// an OS error: nibs' own wrappers interpolate the path with %s, sometimes with
// no OS error involved at all (config.LoadAreas on unparseable YAML,
// config.ReadConfigFile on a directory). A type-based fix closes one channel and
// ships green.
//
// CANONICAL SCOPE (which paths a served message can still carry). This doc is
// its single authoritative statement; servedErrorPresenter and the served
// disclosure guard in serve_path_disclosure_test.go defer to it rather than
// restate it.
//
// Replaced: the store directory, the project directory holding it, and lockDir —
// the OS temp directory nibcore keeps this store's lock file in
// (nibcore.Core.LockDir), which every store mutation opens before it touches the
// store. Each is covered under every spelling this process can derive — as
// given, absolute, resolved through symlinks, and with forward slashes — because
// which one a message carries depends on which the code that rendered it held.
// Every other path survives verbatim: a home-directory config, a document link
// elsewhere on the filesystem.
func newStorePathScrubber(root, lockDir string) func(string) string {
	seen := map[string]bool{}
	var pairs []string
	register := func(needle, replacement string) {
		if seen[needle] {
			return
		}
		seen[needle] = true
		pairs = append(pairs, needle, replacement)
	}
	// A relative or root-level directory is refused rather than replaced:
	// "." or "/" as a needle would rewrite text that is not a path at all.
	usable := func(dir string) (string, bool) {
		dir = filepath.Clean(dir)
		if !filepath.IsAbs(dir) || filepath.Dir(dir) == dir {
			return "", false
		}
		return dir, true
	}
	// add replaces the directory wherever it is named, a bare mention included:
	// a message naming the project with nothing after it locates the machine as
	// precisely as one naming a file inside it.
	add := func(dir, placeholder string) {
		dir, ok := usable(dir)
		if !ok {
			return
		}
		for _, spelling := range []string{dir, filepath.ToSlash(dir)} {
			register(spelling, placeholder)
		}
	}
	// addContainerOf replaces the directory only where a separator follows it,
	// carrying that separator into the replacement so the rest of the path still
	// reads. The boundary keeps a short needle out of a longer name that merely
	// begins with it — "/tmp" against "/tmpfiles" — and costs nothing here,
	// since what this directory discloses it discloses through a file in it.
	addContainerOf := func(dir, placeholder string) {
		dir, ok := usable(dir)
		if !ok {
			return
		}
		native := string(filepath.Separator)
		register(dir+native, placeholder+native)
		register(filepath.ToSlash(dir)+"/", placeholder+"/")
	}
	spellings := func(dir string) []string {
		// An empty directory names nothing: filepath.Abs would turn it into the
		// process's cwd, which is not a path this scrub is about.
		if dir == "" {
			return nil
		}
		out := []string{dir}
		if abs, err := filepath.Abs(dir); err == nil {
			out = append(out, abs)
		}
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			out = append(out, resolved)
		}
		return out
	}

	// Every store spelling before any project one, and both before the lock
	// directory: strings.Replacer breaks a tie between two needles matching at
	// the same offset by argument order, the project directory is a prefix of the
	// store that sits in it, and a store served out of the OS temp directory
	// (`task demo` serves one) sits inside the lock directory. The longer needle
	// is the one that leaves the message saying more.
	storeSpellings := spellings(root)
	for _, s := range storeSpellings {
		add(s, storePathPlaceholder)
	}
	for _, s := range storeSpellings {
		add(filepath.Dir(s), projectPathPlaceholder)
	}
	for _, s := range spellings(lockDir) {
		addContainerOf(s, lockDirPlaceholder)
	}

	if len(pairs) == 0 {
		return func(msg string) string { return msg }
	}
	replacer := strings.NewReplacer(pairs...)
	return replacer.Replace
}
