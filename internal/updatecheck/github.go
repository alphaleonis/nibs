package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// defaultLatestURL answers with the newest release that is neither a prerelease
// nor a draft, the same release install.sh resolves.
const defaultLatestURL = "https://api.github.com/repos/alphaleonis/nibs/releases/latest"

// fetchTimeout bounds the network request, and with it how long a check can
// delay a command.
const fetchTimeout = 3 * time.Second

// githubFetcher retrieves the latest release tag from the GitHub API.
type githubFetcher struct {
	url    string
	client *http.Client
}

func newGitHubFetcher(url string) *githubFetcher {
	return &githubFetcher{
		url:    url,
		client: &http.Client{Timeout: fetchTimeout},
	}
}

// LatestVersion returns the tag_name of the latest release (e.g. "v0.6.0").
func (g *githubFetcher) LatestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases API returned %s", resp.Status)
	}

	// Cap the read against an endpoint streaming an oversized body.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("github releases API returned no tag_name")
	}
	return payload.TagName, nil
}
