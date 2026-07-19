package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubFetcher_ParsesTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("unexpected Accept header: %q", got)
		}
		_, _ = w.Write([]byte(`{"tag_name":"v0.6.0","name":"v0.6.0","prerelease":false}`))
	}))
	defer srv.Close()

	f := newGitHubFetcher(srv.URL)
	got, err := f.LatestVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "v0.6.0" {
		t.Errorf("LatestVersion=%q, want v0.6.0", got)
	}
}

func TestGitHubFetcher_ErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	f := newGitHubFetcher(srv.URL)
	if _, err := f.LatestVersion(context.Background()); err == nil {
		t.Error("expected an error for a non-200 response")
	}
}

func TestGitHubFetcher_ErrorsOnEmptyTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":""}`))
	}))
	defer srv.Close()

	f := newGitHubFetcher(srv.URL)
	if _, err := f.LatestVersion(context.Background()); err == nil {
		t.Error("expected an error when tag_name is empty")
	}
}
