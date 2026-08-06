package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

// fakeFetcher records calls and returns a fixed version/error.
type fakeFetcher struct {
	version string
	err     error
	calls   int
}

func (f *fakeFetcher) LatestVersion(context.Context) (string, error) {
	f.calls++
	return f.version, f.err
}

// unusedFetcher fails the test if the network is consulted at all.
type unusedFetcher struct{ t *testing.T }

func (u unusedFetcher) LatestVersion(context.Context) (string, error) {
	u.t.Helper()
	u.t.Fatal("fetcher called but a network request was not expected")
	return "", nil
}

// newTestChecker builds a Checker with an injected fetcher, a temp cache dir,
// and a neutralized environment so ambient CI (e.g. when the suite itself runs
// in CI) does not gate the check off.
func newTestChecker(t *testing.T, current string, f Fetcher) *Checker {
	t.Helper()
	t.Setenv("CI", "")
	t.Setenv("NIBS_NO_UPDATE_CHECK", "")
	return &Checker{
		current:  current,
		fetcher:  f,
		cacheDir: t.TempDir(),
		cooldown: defaultCooldown,
		now:      time.Now,
	}
}

func TestCheck_UpdateAvailable(t *testing.T) {
	f := &fakeFetcher{version: "v0.6.0"}
	c := newTestChecker(t, "v0.5.0", f)

	res, ok := c.Check(context.Background())
	if !ok {
		t.Fatal("expected an opinion, got ok=false")
	}
	if !res.UpdateAvailable {
		t.Errorf("expected UpdateAvailable=true, got false (current=%q latest=%q)", res.Current, res.Latest)
	}
	if res.Latest != "v0.6.0" || res.Current != "v0.5.0" {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestCheck_NoUpdate(t *testing.T) {
	cases := []struct {
		name    string
		current string
		latest  string
	}{
		{"same", "v0.6.0", "v0.6.0"},
		{"running newer", "v0.7.0", "v0.6.0"},
		{"running prerelease of newer", "v0.6.0-rc.1", "v0.5.9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestChecker(t, tc.current, &fakeFetcher{version: tc.latest})
			res, ok := c.Check(context.Background())
			if !ok {
				t.Fatal("expected an opinion, got ok=false")
			}
			if res.UpdateAvailable {
				t.Errorf("expected UpdateAvailable=false for current=%q latest=%q", tc.current, tc.latest)
			}
		})
	}
}

func TestCheck_GatedOff(t *testing.T) {
	cases := []struct {
		name    string
		current string
		setenv  map[string]string
	}{
		{"dev build", devVersion, nil},
		{"empty version", "", nil},
		{"CI true", "v0.5.0", map[string]string{"CI": "true"}},
		{"opt-out", "v0.5.0", map[string]string{"NIBS_NO_UPDATE_CHECK": "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeFetcher{version: "v0.6.0"}
			c := newTestChecker(t, tc.current, f)
			for k, v := range tc.setenv {
				t.Setenv(k, v)
			}
			_, ok := c.Check(context.Background())
			if ok {
				t.Error("expected the check to be gated off (ok=false)")
			}
			if f.calls != 0 {
				t.Errorf("gated check must not hit the network, got %d calls", f.calls)
			}
		})
	}
}

func TestCheck_CacheHitSkipsNetwork(t *testing.T) {
	c := newTestChecker(t, "v0.5.0", unusedFetcher{t})
	// Seed a fresh cache; the fetcher must not be called.
	c.writeCache(cacheState{CheckedAt: time.Now(), Latest: "v0.6.0"})

	res, ok := c.Check(context.Background())
	if !ok {
		t.Fatal("expected an opinion from cache, got ok=false")
	}
	if !res.UpdateAvailable || res.Latest != "v0.6.0" {
		t.Errorf("expected update to v0.6.0 from cache, got %+v", res)
	}
}

func TestCheck_StaleCacheRefetches(t *testing.T) {
	f := &fakeFetcher{version: "v0.6.0"}
	c := newTestChecker(t, "v0.5.0", f)
	// Seed a stale cache (older than the cooldown) pointing at an old version.
	c.writeCache(cacheState{CheckedAt: time.Now().Add(-2 * defaultCooldown), Latest: "v0.5.1"})

	res, ok := c.Check(context.Background())
	if !ok {
		t.Fatal("expected an opinion, got ok=false")
	}
	if f.calls != 1 {
		t.Errorf("expected exactly one network call for a stale cache, got %d", f.calls)
	}
	if res.Latest != "v0.6.0" {
		t.Errorf("expected refreshed latest v0.6.0, got %q", res.Latest)
	}
	// The refreshed value must be persisted.
	if got, _ := c.readCache(); got.Latest != "v0.6.0" {
		t.Errorf("expected cache updated to v0.6.0, got %q", got.Latest)
	}
}

func TestCheck_FetchFailureRecordsAttempt(t *testing.T) {
	f := &fakeFetcher{err: errors.New("network down")}
	c := newTestChecker(t, "v0.5.0", f)
	before := time.Now()

	_, ok := c.Check(context.Background())
	if ok {
		t.Error("expected no opinion when the fetch fails")
	}
	got, have := c.readCache()
	if !have {
		t.Fatal("expected an attempt to be recorded in the cache")
	}
	if got.CheckedAt.Before(before) {
		t.Errorf("expected CheckedAt to be recorded at/after the attempt, got %v", got.CheckedAt)
	}
}

func TestCheck_UncomparableVersionStaysSilent(t *testing.T) {
	c := newTestChecker(t, "v0.5.0", &fakeFetcher{version: "not-a-version"})
	_, ok := c.Check(context.Background())
	if ok {
		t.Error("expected no opinion when the latest tag is not valid semver")
	}
}

func TestCheck_NoCacheDirDisabled(t *testing.T) {
	c := newTestChecker(t, "v0.5.0", &fakeFetcher{version: "v0.6.0"})
	c.cacheDir = "" // simulate os.UserCacheDir() failure
	if _, ok := c.Check(context.Background()); ok {
		t.Error("expected the check disabled when no cache dir is available")
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest   string
		newer, comparable bool
	}{
		{"v0.5.0", "v0.6.0", true, true},
		{"0.5.0", "0.6.0", true, true},  // missing "v" normalized on both sides
		{"v0.5.0", "0.6.0", true, true}, // mixed
		{"v0.6.0", "v0.6.0", false, true},
		{"v0.7.0", "v0.6.0", false, true},
		{"v0.6.0-rc.1", "v0.6.0", true, true}, // prerelease < release
		{"v0.6.0", "v0.6.0-rc.1", false, true},
		{"v0.5.0", "garbage", false, false},
		{"garbage", "v0.6.0", false, false},
	}
	for _, tc := range cases {
		newer, comparable := isNewer(tc.current, tc.latest)
		if newer != tc.newer || comparable != tc.comparable {
			t.Errorf("isNewer(%q,%q)=(%v,%v), want (%v,%v)",
				tc.current, tc.latest, newer, comparable, tc.newer, tc.comparable)
		}
	}
}

// TestCacheState_RoundTrip guards the on-disk JSON format the three surfaces
// share, so a rename of a json tag is caught.
func TestCacheState_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := &Checker{cacheDir: dir}
	want := cacheState{CheckedAt: time.Now().UTC().Truncate(time.Second), Latest: "v1.2.3"}
	c.writeCache(want)

	data, err := os.ReadFile(c.cachePath())
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["checked_at"]; !ok {
		t.Error("cache JSON missing checked_at key")
	}
	if raw["latest"] != "v1.2.3" {
		t.Errorf("cache JSON latest=%v, want v1.2.3", raw["latest"])
	}
}
