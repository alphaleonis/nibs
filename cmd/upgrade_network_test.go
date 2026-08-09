package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/creativeprojects/go-selfupdate"

	"github.com/alphaleonis/nibs/internal/signing"
)

// The signed self-update path, exercised end to end over HTTP.
//
// Every piece of this was already covered in isolation — the verifier accepts
// and rejects the right signatures, the release workflow signs and checks its
// own output — but the join never ran: go-selfupdate fetching assets, walking
// its ValidationChain through our PatternValidator, and replacing a binary.
// That join is where the guarantee actually lives, and it is not reachable from
// a unit test of any one part.
//
// The real GitHubSource runs here, pointed at a local server through
// EnterpriseBaseURL, so go-selfupdate's own listing, download and decompression
// code is under test rather than a stand-in for it. Only the hostname is
// substituted. The signing key is generated per test because the release key
// exists only in the `release` environment — hence newUpgradeValidatorFor.

const testUpgradeTag = "v9.9.9"

// newBinaryContent is what a successful upgrade must leave on disk, and what
// every refusal must NOT leave.
const newBinaryContent = "REPLACED-BY-UPGRADE"

const oldBinaryContent = "ORIGINAL-BINARY"

// releaseAssets is one staged release's files, keyed by asset name.
type releaseAssets struct {
	names []string // stable order, so asset IDs are deterministic
	data  map[string][]byte
}

func (r *releaseAssets) add(name string, data []byte) {
	if _, seen := r.data[name]; !seen {
		r.names = append(r.names, name)
	}
	r.data[name] = data
}

// commandName is the file the archive must contain and the file UpdateTo
// replaces; go-selfupdate matches the two by name.
func commandName() string {
	if runtime.GOOS == "windows" {
		return "nibs.exe"
	}
	return "nibs"
}

func archiveName() string {
	return fmt.Sprintf("nibs_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
}

// tarGz builds a one-file .tar.gz, mirroring the shape GoReleaser publishes.
func tarGz(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// checksumsFor renders the `<sha256>  <name>` file GoReleaser publishes.
func checksumsFor(files map[string][]byte) []byte {
	var b strings.Builder
	for name, data := range files {
		fmt.Fprintf(&b, "%x  %s\n", sha256.Sum256(data), name)
	}
	return []byte(b.String())
}

func generateKey(t *testing.T) (ed25519.PrivateKey, []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return priv, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// verifierTrusting builds the Verifier a "binary" in this test carries.
func verifierTrusting(t *testing.T, pubPEM []byte) *signing.Verifier {
	t.Helper()
	v, err := signing.NewVerifierFromFS(fstest.MapFS{
		"keys/test.pub": &fstest.MapFile{Data: pubPEM},
	}, "keys")
	if err != nil {
		t.Fatalf("NewVerifierFromFS: %v", err)
	}
	return v
}

// serveRelease stands up a GitHub-shaped API for exactly one release. It
// answers the two calls go-selfupdate makes: list the releases, then fetch an
// asset by id. Paths are matched by suffix so the go-github enterprise "/api/v3"
// prefix does not have to be modeled.
func serveRelease(t *testing.T, assets *releaseAssets, tag string, prerelease bool) string {
	t.Helper()

	var baseURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases"):
			list := []map[string]any{{
				"id":           int64(1),
				"tag_name":     tag,
				"name":         tag,
				"body":         "test release",
				"draft":        false,
				"prerelease":   prerelease,
				"published_at": "2026-08-09T00:00:00Z",
				"assets":       assetsJSON(assets, baseURL),
			}}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(list); err != nil {
				t.Errorf("encode release list: %v", err)
			}

		case strings.Contains(r.URL.Path, "/releases/assets/"):
			idx := strings.LastIndex(r.URL.Path, "/")
			var id int
			if _, err := fmt.Sscanf(r.URL.Path[idx+1:], "%d", &id); err != nil {
				http.Error(w, "bad asset id", http.StatusBadRequest)
				return
			}
			if id < 1 || id > len(assets.names) {
				http.Error(w, "no such asset", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			if _, err := w.Write(assets.data[assets.names[id-1]]); err != nil {
				t.Errorf("write asset: %v", err)
			}

		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	baseURL = srv.URL
	return srv.URL
}

func assetsJSON(assets *releaseAssets, baseURL string) []map[string]any {
	out := make([]map[string]any, 0, len(assets.names))
	for i, name := range assets.names {
		id := int64(i + 1)
		out = append(out, map[string]any{
			"id":                   id,
			"name":                 name,
			"size":                 len(assets.data[name]),
			"browser_download_url": fmt.Sprintf("%s/download/%s", baseURL, name),
		})
	}
	return out
}

// stageOptions varies the release a test serves. The zero value is a correctly
// signed stable release tagged testUpgradeTag.
type stageOptions struct {
	corrupt    string
	tag        string // defaults to testUpgradeTag
	prerelease bool
}

// stageRelease assembles a release, applies the named corruption, and serves it.
// Returns the server URL and the Verifier the "running binary" trusts.
func stageRelease(t *testing.T, opts stageOptions) (string, *signing.Verifier) {
	t.Helper()

	corrupt := opts.corrupt
	tag := opts.tag
	if tag == "" {
		tag = testUpgradeTag
	}

	priv, pubPEM := generateKey(t)
	verifier := verifierTrusting(t, pubPEM)

	archive := tarGz(t, commandName(), newBinaryContent)
	checksums := checksumsFor(map[string][]byte{archiveName(): archive})

	// The signature is made over checksums.txt exactly as the release job does.
	signingKey := priv
	if corrupt == "untrusted-key" {
		// A well-formed signature from a key the binary does not carry — the
		// case a compromised release that can sign with *something* produces.
		signingKey, _ = generateKey(t)
	}
	sig := ed25519.Sign(signingKey, checksums)

	if corrupt == "corrupt-sig" {
		sig[0] ^= 0xFF
	}
	if corrupt == "tampered-archive" {
		// Checksums stay as signed; the archive no longer matches them.
		archive = tarGz(t, commandName(), "MALICIOUS-PAYLOAD")
	}

	assets := &releaseAssets{data: map[string][]byte{}}
	assets.add(archiveName(), archive)
	assets.add("checksums.txt", checksums)
	if corrupt != "missing-sig" {
		assets.add("checksums.txt.sig", sig)
	}

	return serveRelease(t, assets, tag, opts.prerelease), verifier
}

// attemptUpgrade runs the real detect+update path against the staged release,
// returning whether the release was detected and any update error. The target
// file starts as oldBinaryContent so the caller can assert it was left alone.
func attemptUpgrade(t *testing.T, baseURL string, verifier *signing.Verifier, tag string) (found bool, target string, err error) {
	t.Helper()

	target = filepath.Join(t.TempDir(), commandName())
	if writeErr := os.WriteFile(target, []byte(oldBinaryContent), 0o755); writeErr != nil {
		t.Fatalf("seed target binary: %v", writeErr)
	}

	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{EnterpriseBaseURL: baseURL})
	if err != nil {
		t.Fatalf("NewGitHubSource: %v", err)
	}
	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source:    source,
		Validator: newUpgradeValidatorFor(verifier),
	})
	if err != nil {
		t.Fatalf("NewUpdater: %v", err)
	}

	ctx := context.Background()
	rel, found, err := updater.DetectVersion(ctx, selfupdate.ParseSlug(upgradeRepoSlug), tag)
	if err != nil {
		return false, target, err
	}
	if !found {
		return false, target, nil
	}
	return true, target, updater.UpdateTo(ctx, rel, target)
}

func targetContent(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	return string(b)
}

// TestUpgradeAcceptsASignedRelease is the positive half of the join: a properly
// signed release is detected, validated and installed, and the binary on disk
// is actually replaced.
func TestUpgradeAcceptsASignedRelease(t *testing.T) {
	baseURL, verifier := stageRelease(t, stageOptions{})

	found, target, err := attemptUpgrade(t, baseURL, verifier, testUpgradeTag)
	if err != nil {
		t.Fatalf("upgrade against a correctly signed release failed: %v", err)
	}
	if !found {
		t.Fatal("a correctly signed release was not detected")
	}
	if got := targetContent(t, target); got != newBinaryContent {
		t.Errorf("binary not replaced: content = %q, want %q", got, newBinaryContent)
	}
}

// TestUpgradeRefusesUnsignedOrTamperedReleases is the half that matters for
// security: each corruption must stop the upgrade AND leave the existing binary
// untouched. A refusal that still overwrites the binary would be worthless, so
// both are asserted every time.
func TestUpgradeRefusesUnsignedOrTamperedReleases(t *testing.T) {
	tests := []struct {
		name string
		opts stageOptions
	}{
		{"signature does not verify", stageOptions{corrupt: "corrupt-sig"}},
		{"signature from a key the binary does not trust", stageOptions{corrupt: "untrusted-key"}},
		{"release publishes no signature at all", stageOptions{corrupt: "missing-sig"}},
		{"archive does not match the signed checksums", stageOptions{corrupt: "tampered-archive"}},
		// A pre-release reached by an explicit --version, which skips the
		// pre-release filter — so the signature is the only thing standing
		// between it and the running binary.
		{"pre-release named by --version with a bad signature", stageOptions{
			corrupt: "corrupt-sig", tag: "v0.0.1-sigtest.1", prerelease: true,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseURL, verifier := stageRelease(t, tt.opts)

			tag := tt.opts.tag
			if tag == "" {
				tag = testUpgradeTag
			}
			found, target, err := attemptUpgrade(t, baseURL, verifier, tag)

			// Either outcome is a refusal: a missing signature makes the
			// release undetectable (the ValidationChain cannot be built),
			// while a bad one fails during validation.
			if found && err == nil {
				t.Errorf("upgrade succeeded against a %s release, want refusal", tt.name)
			}
			if got := targetContent(t, target); got != oldBinaryContent {
				t.Errorf("existing binary was modified despite refusal: content = %q, want %q",
					got, oldBinaryContent)
			}
		})
	}
}
