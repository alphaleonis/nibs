// Package updatecheck reports whether a newer nibs release exists than the
// running version, for the CLI, TUI and web.
//
// It compares version strings only and never downloads release assets; `nibs
// upgrade` replaces the binary.
package updatecheck

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"golang.org/x/mod/semver"

	"github.com/alphaleonis/nibs/internal/fsutil"
)

// defaultCooldown is how long a cached result is trusted before the next
// network check.
const defaultCooldown = 24 * time.Hour

// devVersion is the version cmd reports when the build set none (e.g. `go run`).
const devVersion = "dev"

// Result is the outcome of a successful check.
type Result struct {
	// Current is the running version (as passed to NewChecker), e.g. "v0.5.1".
	Current string
	// Latest is the newest released version, e.g. "v0.6.0".
	Latest string
	// UpdateAvailable is true when Latest is a strictly newer semantic
	// version than Current.
	UpdateAvailable bool
}

// Fetcher retrieves the latest released version tag. Tests substitute a fake.
type Fetcher interface {
	// LatestVersion returns the latest released version tag (e.g. "v0.6.0").
	LatestVersion(ctx context.Context) (string, error)
}

// Checker performs cached, gated update checks. Construct one per check; it is
// not safe for concurrent use.
type Checker struct {
	current  string
	fetcher  Fetcher
	cacheDir string // "" disables checking
	cooldown time.Duration
	now      func() time.Time
}

// NewChecker returns a Checker for the given running version, wired to the
// public GitHub releases API and the user cache directory.
func NewChecker(current string) *Checker {
	c := &Checker{
		current:  current,
		fetcher:  newGitHubFetcher(defaultLatestURL),
		cooldown: defaultCooldown,
		now:      time.Now,
	}
	if dir, err := os.UserCacheDir(); err == nil {
		c.cacheDir = filepath.Join(dir, "nibs")
	}
	return c
}

// enabled reports whether a check may run: not for an empty version or
// devVersion, without a cache directory, with NIBS_NO_UPDATE_CHECK set, or in CI.
func (c *Checker) enabled() bool {
	if c.current == "" || c.current == devVersion {
		return false
	}
	if c.cacheDir == "" {
		return false
	}
	if os.Getenv("NIBS_NO_UPDATE_CHECK") != "" {
		return false
	}
	// GitHub Actions, GitLab CI and others set CI=true.
	if v := os.Getenv("CI"); v != "" && v != "false" && v != "0" {
		return false
	}
	return true
}

// Check returns the update Result and true when it has an opinion. It returns
// ok=false, never an error, when the check is disabled, the fetch fails, a fresh
// cache holds no version, or the versions are not comparable. Say nothing then.
//
// A fresh cache (within the cooldown) answers without a request. Otherwise one
// request is made and its outcome cached; a failed request records the attempt
// and keeps the previously cached version.
func (c *Checker) Check(ctx context.Context) (Result, bool) {
	if !c.enabled() {
		return Result{}, false
	}

	latest, ok := c.latestVersion(ctx)
	if !ok {
		return Result{}, false
	}

	newer, comparable := isNewer(c.current, latest)
	if !comparable {
		return Result{}, false
	}
	return Result{Current: c.current, Latest: latest, UpdateAvailable: newer}, true
}

// latestVersion returns the latest version from a fresh cache or the network.
func (c *Checker) latestVersion(ctx context.Context) (string, bool) {
	cached, haveCache := c.readCache()
	if haveCache && c.now().Sub(cached.CheckedAt) < c.cooldown {
		if cached.Latest == "" {
			return "", false
		}
		return cached.Latest, true
	}

	latest, err := c.fetcher.LatestVersion(ctx)
	if err != nil {
		// Record the attempt so failures wait out the cooldown; keep the
		// cached version.
		prev := ""
		if haveCache {
			prev = cached.Latest
		}
		c.writeCache(cacheState{CheckedAt: c.now(), Latest: prev})
		return "", false
	}

	c.writeCache(cacheState{CheckedAt: c.now(), Latest: latest})
	return latest, true
}

// cacheState is the on-disk cache format.
type cacheState struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

func (c *Checker) cachePath() string {
	return filepath.Join(c.cacheDir, "update-check.json")
}

// readCache returns the cached state, or ok=false if it is missing/unreadable.
func (c *Checker) readCache() (cacheState, bool) {
	data, err := os.ReadFile(c.cachePath())
	if err != nil {
		return cacheState{}, false
	}
	var s cacheState
	if err := json.Unmarshal(data, &s); err != nil {
		return cacheState{}, false
	}
	return s, true
}

// writeCache replaces the cache file atomically. Errors are ignored; the next
// command checks again.
func (c *Checker) writeCache(s cacheState) {
	if c.cacheDir == "" {
		return
	}
	if err := os.MkdirAll(c.cacheDir, 0o755); err != nil {
		return
	}
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	_ = fsutil.AtomicWriteFile(c.cachePath(), data, 0o644)
}

// isNewer reports whether latest is a strictly newer semantic version than
// current. comparable is false when either is not valid semver.
func isNewer(current, latest string) (newer, comparable bool) {
	cv := ensureV(current)
	lv := ensureV(latest)
	if !semver.IsValid(cv) || !semver.IsValid(lv) {
		return false, false
	}
	// semver reads a `git describe` suffix as a prerelease, which sorts before
	// its base tag, so a build past v0.8.3 would be offered v0.8.3. Such a build
	// is at or past its base, so compare the base itself.
	cv = describeSuffix.ReplaceAllString(cv, "")
	return semver.Compare(cv, lv) < 0, true
}

// describeSuffix matches what `git describe --tags --dirty` appends to a tag:
// "-<commits>-g<hash>" when HEAD is past the tag, and "-dirty" for local changes.
var describeSuffix = regexp.MustCompile(`(-[0-9]+-g[0-9a-f]+)?(-dirty)?$`)

// ensureV normalizes a version to the leading-"v" form semver expects.
func ensureV(v string) string {
	if v == "" || v[0] == 'v' {
		return v
	}
	return "v" + v
}
