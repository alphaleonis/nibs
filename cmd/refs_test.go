package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// setupRefsTestApp builds an App with the given prefix so mention tests can
// exercise both short- and full-form ID resolution.
func setupRefsTestApp(t *testing.T, prefix string) *App {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}
	cfg := config.DefaultWithPrefix(prefix)
	core := nibcore.New(nibsDir, cfg)
	if err := core.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}
	return &App{Core: core}
}

func TestMentionsGraphQLEndToEnd(t *testing.T) {
	app := setupRefsTestApp(t, "nibs-")

	// nibs-aaa1 mentions #bbb2 (short) and #nibs-ccc3 (full).
	// nibs-ddd4 mentions #aaa1 in its body.
	nibs := []*nib.Nib{
		{ID: "nibs-aaa1", Slug: "a", Title: "A", Status: "todo", Body: "See #bbb2 and #nibs-ccc3 for details."},
		{ID: "nibs-bbb2", Slug: "b", Title: "B", Status: "todo", Body: "No refs here."},
		{ID: "nibs-ccc3", Slug: "c", Title: "C", Status: "completed", Body: "Backref to #aaa1."},
		{ID: "nibs-ddd4", Slug: "d", Title: "D", Status: "todo", Body: "Also mentions #aaa1."},
	}
	for _, b := range nibs {
		if err := app.Core.Create(b); err != nil {
			t.Fatalf("create %s: %v", b.ID, err)
		}
	}

	t.Run("outbound mentions", func(t *testing.T) {
		query := `{ nib(id: "nibs-aaa1") { mentions { id } mentionIds } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery: %v", err)
		}
		var data struct {
			Nib struct {
				Mentions   []struct{ ID string } `json:"mentions"`
				MentionIds []string              `json:"mentionIds"`
			} `json:"nib"`
		}
		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("unmarshal: %v\nraw: %s", err, result)
		}
		if len(data.Nib.Mentions) != 2 {
			t.Fatalf("got %d mentions, want 2 (%s)", len(data.Nib.Mentions), result)
		}
		got := []string{data.Nib.Mentions[0].ID, data.Nib.Mentions[1].ID}
		if got[0] != "nibs-bbb2" || got[1] != "nibs-ccc3" {
			t.Errorf("mentions order = %v, want [nibs-bbb2 nibs-ccc3]", got)
		}
		if len(data.Nib.MentionIds) != 2 {
			t.Errorf("mentionIds = %v, want 2 entries", data.Nib.MentionIds)
		}
	})

	t.Run("inbound mentions", func(t *testing.T) {
		query := `{ nib(id: "nibs-aaa1") { mentionedBy { id } } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery: %v", err)
		}
		var data struct {
			Nib struct {
				MentionedBy []struct{ ID string } `json:"mentionedBy"`
			} `json:"nib"`
		}
		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(data.Nib.MentionedBy) != 2 {
			t.Fatalf("got %d inbound mentions, want 2 (%s)", len(data.Nib.MentionedBy), result)
		}
		ids := map[string]bool{}
		for _, m := range data.Nib.MentionedBy {
			ids[m.ID] = true
		}
		if !ids["nibs-ccc3"] || !ids["nibs-ddd4"] {
			t.Errorf("got %v, want nibs-ccc3 and nibs-ddd4 in mentionedBy", ids)
		}
	})

	t.Run("filter mentions by excluding completed", func(t *testing.T) {
		query := `{ nib(id: "nibs-aaa1") { mentions(filter: { excludeStatus: ["completed"] }) { id } } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery: %v", err)
		}
		var data struct {
			Nib struct {
				Mentions []struct{ ID string } `json:"mentions"`
			} `json:"nib"`
		}
		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(data.Nib.Mentions) != 1 || data.Nib.Mentions[0].ID != "nibs-bbb2" {
			t.Errorf("got %+v, want exactly [nibs-bbb2]", data.Nib.Mentions)
		}
	})

	t.Run("filter nibs by mentionsId", func(t *testing.T) {
		// Nibs whose bodies mention nibs-aaa1 → nibs-ccc3, nibs-ddd4.
		query := `{ nibs(filter: { mentionsId: "nibs-aaa1" }) { id } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery: %v", err)
		}
		var data struct {
			Nibs []struct{ ID string } `json:"nibs"`
		}
		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(data.Nibs) != 2 {
			t.Fatalf("got %d, want 2 (%s)", len(data.Nibs), result)
		}
		ids := map[string]bool{data.Nibs[0].ID: true, data.Nibs[1].ID: true}
		if !ids["nibs-ccc3"] || !ids["nibs-ddd4"] {
			t.Errorf("got %v, want {nibs-ccc3, nibs-ddd4}", ids)
		}
	})

	t.Run("filter nibs by mentionedById", func(t *testing.T) {
		// Nibs mentioned in nibs-aaa1's body → nibs-bbb2, nibs-ccc3.
		query := `{ nibs(filter: { mentionedById: "nibs-aaa1" }) { id } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery: %v", err)
		}
		var data struct {
			Nibs []struct{ ID string } `json:"nibs"`
		}
		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(data.Nibs) != 2 {
			t.Fatalf("got %d, want 2 (%s)", len(data.Nibs), result)
		}
	})
}

func TestRefsCommandFindsMentions(t *testing.T) {
	app := setupRefsTestApp(t, "nibs-")

	nibs := []*nib.Nib{
		{ID: "nibs-a1", Title: "A", Status: "todo", Body: "Refs #b2."},
		{ID: "nibs-b2", Title: "B", Status: "todo", Body: ""},
	}
	for _, b := range nibs {
		if err := app.Core.Create(b); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	t.Run("outbound via Core.FindMentions", func(t *testing.T) {
		got := app.Core.FindMentions("nibs-a1")
		if len(got) != 1 || got[0].ID != "nibs-b2" {
			t.Errorf("got %v, want [nibs-b2]", got)
		}
	})

	t.Run("inbound via Core.FindMentionedBy", func(t *testing.T) {
		got := app.Core.FindMentionedBy("nibs-b2")
		if len(got) != 1 || got[0].ID != "nibs-a1" {
			t.Errorf("got %v, want [nibs-a1]", got)
		}
	})
}
