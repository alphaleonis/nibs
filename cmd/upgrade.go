package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/alphaleonis/nibs/internal/output"
	selfupdate "github.com/creativeprojects/go-selfupdate"
	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
)

// upgradeRepoSlug is the GitHub repository go-selfupdate pulls releases from.
const upgradeRepoSlug = "alphaleonis/nibs"

var (
	upgradeCheckOnly bool
	upgradeVersion   string
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade nibs to the latest release",
	Long: `Download, verify, and replace the running nibs binary with the latest
GitHub release (or a specific version with --version).

The download is checksum-verified against the release's checksums.txt and the
replacement rolls back automatically on failure. When nibs was installed by a
package manager (Homebrew, Nix, Scoop, Chocolatey, WinGet, or "go install"),
upgrade defers to that manager and prints guidance instead of self-replacing.`,
	Args: codedNoArgs(nil),
	RunE: runUpgrade,
}

func init() {
	upgradeCmd.Flags().BoolVar(&upgradeCheckOnly, "check", false,
		"Only report whether an update is available; make no changes")
	upgradeCmd.Flags().StringVar(&upgradeVersion, "version", "",
		"Upgrade to a specific version tag (e.g. v0.6.0) instead of the latest")
	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade(cmd *cobra.Command, _ []string) error {
	if version == "" || version == "dev" {
		return cmdError(false, output.ErrValidation,
			"cannot upgrade a development build (nibs %s) — install a released build first", version)
	}

	exe, err := os.Executable()
	if err != nil {
		return cmdError(false, output.ErrFileError, "cannot locate the running executable: %v", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}

	// Defer to a package manager rather than self-replacing a binary it owns.
	if mgr, ok := detectPackageManager(exe, os.Getenv); ok {
		out := cmd.OutOrStdout()
		_, _ = fmt.Fprintf(out, "nibs appears to be installed via %s:\n  %s\n", mgr.name, exe)
		_, _ = fmt.Fprintf(out, "Upgrade with %s instead:\n  %s\n", mgr.name, mgr.hint)
		return nil
	}

	// checksums.txt is verified against a signature made by a key compiled into
	// this binary, and the archives are then verified against that checksums.txt
	// — see newUpgradeValidator. The signing key is not in the release, so this
	// is an anchor a compromised release cannot forge; checksums.txt alone could
	// only ever prove the download was not corrupted in transit.
	validator, err := newUpgradeValidator()
	if err != nil {
		return cmdError(false, output.ErrFileError,
			"loading the release signing keys this binary trusts: %v", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Validator: validator,
		// Prerelease defaults to false: `nibs upgrade` only considers stable
		// releases unless a specific tag is requested via --version.
	})
	if err != nil {
		return cmdError(false, output.ErrFileError, "initializing updater: %v", err)
	}
	slug := selfupdate.ParseSlug(upgradeRepoSlug)
	ctx := cmd.Context()

	var rel *selfupdate.Release
	var found bool
	if upgradeVersion != "" {
		rel, found, err = updater.DetectVersion(ctx, slug, upgradeVersion)
	} else {
		rel, found, err = updater.DetectLatest(ctx, slug)
	}
	if err != nil {
		return cmdError(false, output.ErrFileError, "checking for updates: %v", err)
	}
	if !found {
		target := "latest release"
		if upgradeVersion != "" {
			target = "version " + upgradeVersion
		}
		return cmdError(false, output.ErrNotFound,
			"no %s found for %s/%s", target, runtime.GOOS, runtime.GOARCH)
	}

	current := ensureVersionV(version)
	latest := ensureVersionV(rel.Version())
	out := cmd.OutOrStdout()

	if upgradeCheckOnly {
		if isUpToDate(current, latest) {
			_, _ = fmt.Fprintf(out, "nibs %s is up to date (latest %s).\n", current, latest)
		} else {
			_, _ = fmt.Fprintf(out, "nibs %s is available (current %s) — run `nibs upgrade`.\n", latest, current)
		}
		return nil
	}

	// A bare `upgrade` is a no-op when already current; an explicit --version
	// is honored even if it is the same or older (a deliberate pin/downgrade).
	if upgradeVersion == "" && isUpToDate(current, latest) {
		_, _ = fmt.Fprintf(out, "nibs %s is already up to date.\n", current)
		return nil
	}

	_, _ = fmt.Fprintf(out, "Upgrading nibs %s -> %s ...\n", current, latest)
	if err := updater.UpdateTo(ctx, rel, exe); err != nil {
		return cmdError(false, output.ErrFileError, "upgrading: %v", err)
	}
	_, _ = fmt.Fprintf(out, "Upgraded to %s.\n", latest)
	return nil
}

// isUpToDate reports whether current is at or beyond latest. When either value
// is not valid semver it returns false so the caller offers the update rather
// than silently declining it.
func isUpToDate(current, latest string) bool {
	if !semver.IsValid(current) || !semver.IsValid(latest) {
		return false
	}
	return semver.Compare(current, latest) >= 0
}

// ensureVersionV normalizes a version to the leading-"v" form semver expects.
func ensureVersionV(v string) string {
	if v == "" || v[0] == 'v' {
		return v
	}
	return "v" + v
}

// pkgManager names a package manager that owns the installed binary and the
// command a user should run to upgrade through it.
type pkgManager struct {
	name string
	hint string
}

// detectPackageManager reports whether the executable at exePath lives under a
// path owned by a known package manager, in which case `nibs upgrade` must
// defer to that manager instead of self-replacing the binary. getenv is
// injected so the go-install location can be resolved (and tested) from
// GOBIN/GOPATH/HOME.
func detectPackageManager(exePath string, getenv func(string) string) (pkgManager, bool) {
	// Normalize both separators explicitly (not just filepath.ToSlash, which
	// leaves backslashes untouched off-Windows) so a Windows-style path is
	// matched the same way regardless of the OS running the check.
	p := strings.ReplaceAll(exePath, `\`, "/")
	lower := strings.ToLower(p)

	switch {
	case strings.Contains(p, "/nix/store/"):
		return pkgManager{"Nix", "reinstalling it through your Nix profile or flake"}, true
	case strings.Contains(p, "/Cellar/"),
		strings.Contains(p, "/opt/homebrew/"),
		strings.Contains(lower, "/linuxbrew/"):
		return pkgManager{"Homebrew", "brew upgrade nibs"}, true
	case strings.Contains(lower, "/scoop/"):
		return pkgManager{"Scoop", "scoop update nibs"}, true
	case strings.Contains(lower, "chocolatey"):
		return pkgManager{"Chocolatey", "choco upgrade nibs"}, true
	case strings.Contains(lower, "winget"):
		return pkgManager{"WinGet", "winget upgrade alphaleonis.nibs"}, true
	}

	if inGoBin(p, getenv) {
		return pkgManager{"go install", "go install github.com/alphaleonis/nibs@latest"}, true
	}
	return pkgManager{}, false
}

// inGoBin reports whether p sits directly in a Go install bin directory
// (GOBIN, each GOPATH/bin, or the default HOME/go/bin).
func inGoBin(p string, getenv func(string) string) bool {
	parent := filepath.ToSlash(filepath.Dir(p))

	var dirs []string
	if gobin := getenv("GOBIN"); gobin != "" {
		dirs = append(dirs, gobin)
	}
	if gopath := getenv("GOPATH"); gopath != "" {
		for _, gp := range filepath.SplitList(gopath) {
			dirs = append(dirs, filepath.Join(gp, "bin"))
		}
	}
	if home := getenv("HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, "go", "bin"))
	}

	for _, d := range dirs {
		if filepath.ToSlash(d) == parent {
			return true
		}
	}
	return false
}
