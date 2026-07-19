// Package updatecheck answers a single question for every nibs surface
// (CLI, TUI, web): is there a newer released version than the one running?
//
// It is deliberately dependency-light and platform-agnostic. It compares
// version strings only — it never downloads or matches platform-specific
// release assets. Downloading and replacing the binary for the current
// platform is the job of the `nibs upgrade` command, which owns the
// go-selfupdate integration; a version comparison is correct even for a web
// banner viewed from a different OS than the server binary runs on.
package updatecheck

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/mod/semver"
)

// defaultCooldown is how long a cached result is trusted before the next
// network check. Keeps the notifier to at most one request per window.
const defaultCooldown = 24 * time.Hour

// devVersion is the version string used for detached / `go run` builds, for
// which no meaningful upgrade check can be made.
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

// Fetcher retrieves the latest released version tag. It is an interface so
// tests can supply a fake and the real implementation can be swapped.
type Fetcher interface {
	// LatestVersion returns the latest released version tag (e.g. "v0.6.0").
	LatestVersion(ctx context.Context) (string, error)
}

// Checker performs cached, gated update checks. It is safe to construct with
// NewChecker and use once per command; it is not designed for concurrent use.
type Checker struct {
	current  string
	fetcher  Fetcher
	cacheDir string // "" disables persistence (and therefore checking)
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

// enabled reports whether a check should run at all. It is silent by design:
// detached/dev builds, CI, an explicit opt-out, or a missing cache directory
// all disable the check with no error.
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
	// Common CI convention (GitHub Actions, GitLab, etc. set CI=true).
	if v := os.Getenv("CI"); v != "" && v != "false" && v != "0" {
		return false
	}
	return true
}

// Check returns the update Result and true when it has an opinion. It returns
// ok=false — with no error — whenever the check is gated off, the cache is
// stale and the network fetch fails, or the versions cannot be compared. The
// caller is expected to treat "no opinion" as "say nothing".
//
// When the cache is fresh (within the cooldown) no network request is made.
// When it is stale, a single request is made and the result is cached for the
// next window; a failed request still records the attempt time so a flaky or
// offline network does not trigger a request on every command.
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

// latestVersion returns the latest version from cache when fresh, otherwise
// from the network (updating the cache). ok is false when there is no usable
// version to report.
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
		// Record the attempt time so repeated failures do not hammer the
		// network; preserve any previously cached version.
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

// writeCache persists the state atomically, best-effort (errors are ignored:
// a failure to cache only costs an extra network check next time).
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
	tmp, err := os.CreateTemp(c.cacheDir, "update-check-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, c.cachePath()); err != nil {
		_ = os.Remove(tmpName)
	}
}

// isNewer reports whether latest is a strictly newer semantic version than
// current. comparable is false when either value is not valid semver, in which
// case the caller should stay silent rather than guess.
func isNewer(current, latest string) (newer, comparable bool) {
	cv := ensureV(current)
	lv := ensureV(latest)
	if !semver.IsValid(cv) || !semver.IsValid(lv) {
		return false, false
	}
	return semver.Compare(cv, lv) < 0, true
}

// ensureV normalizes a version to the leading-"v" form semver expects.
func ensureV(v string) string {
	if v == "" || v[0] == 'v' {
		return v
	}
	return "v" + v
}
