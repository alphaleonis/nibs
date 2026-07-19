package graph

import (
	"context"
	"testing"

	"github.com/alphaleonis/nibs/internal/updatecheck"
)

func TestUpdateStatusResult(t *testing.T) {
	t.Run("no opinion reports current with no update", func(t *testing.T) {
		got := updateStatusResult("v0.5.0", updatecheck.Result{}, false)
		if got.Current != "v0.5.0" || got.Latest != "" || got.UpdateAvailable {
			t.Errorf("got %+v, want current=v0.5.0 latest='' updateAvailable=false", got)
		}
	})

	t.Run("update available maps through", func(t *testing.T) {
		res := updatecheck.Result{Current: "v0.5.0", Latest: "v0.6.0", UpdateAvailable: true}
		got := updateStatusResult("v0.5.0", res, true)
		if got.Current != "v0.5.0" || got.Latest != "v0.6.0" || !got.UpdateAvailable {
			t.Errorf("got %+v, want the result mapped through", got)
		}
	})

	t.Run("up to date maps through", func(t *testing.T) {
		res := updatecheck.Result{Current: "v0.6.0", Latest: "v0.6.0", UpdateAvailable: false}
		got := updateStatusResult("v0.6.0", res, true)
		if got.UpdateAvailable {
			t.Errorf("got updateAvailable=true, want false")
		}
	})
}

// TestUpdateStatusResolver_DevBuildIsBestEffort exercises the real resolver on a
// dev build: the check is gated off (no network), so it must return a non-nil
// status with updateAvailable=false and no error.
func TestUpdateStatusResolver_DevBuildIsBestEffort(t *testing.T) {
	r := &queryResolver{&Resolver{Version: "dev"}}
	got, err := r.UpdateStatus(context.Background())
	if err != nil {
		t.Fatalf("resolver must be best-effort (no error), got %v", err)
	}
	if got == nil || got.UpdateAvailable {
		t.Errorf("dev build must report no update, got %+v", got)
	}
}
