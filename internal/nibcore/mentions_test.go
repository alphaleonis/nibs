package nibcore

import (
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

func TestFindMentionsInMap(t *testing.T) {
	nibs := map[string]*nib.Nib{
		"nibs-aaa1": {ID: "nibs-aaa1", Title: "A", Status: "todo", Body: "See #bbb2 and #nibs-ccc3 for details."},
		"nibs-bbb2": {ID: "nibs-bbb2", Title: "B", Status: "todo", Body: "No refs."},
		"nibs-ccc3": {ID: "nibs-ccc3", Title: "C", Status: "completed", Body: "Backref to #aaa1."},
		"nibs-ddd4": {ID: "nibs-ddd4", Title: "D", Status: "todo", Body: "Self ref #ddd4 and missing #nope1."},
	}

	t.Run("resolves short and full form in same body", func(t *testing.T) {
		got := FindMentionsInMap(nibs, "nibs-aaa1", "nibs-")
		if len(got) != 2 {
			t.Fatalf("got %d mentions, want 2", len(got))
		}
		if got[0].ID != "nibs-bbb2" || got[1].ID != "nibs-ccc3" {
			t.Errorf("order/IDs = %s, %s; want nibs-bbb2, nibs-ccc3", got[0].ID, got[1].ID)
		}
	})

	t.Run("self-reference excluded", func(t *testing.T) {
		got := FindMentionsInMap(nibs, "nibs-ddd4", "nibs-")
		if len(got) != 0 {
			t.Errorf("got %d mentions, want 0 (self + unresolved only)", len(got))
		}
	})

	t.Run("includes mention to completed nib", func(t *testing.T) {
		// Completed nibs should still appear in mentions — status filtering is the caller's job.
		got := FindMentionsInMap(nibs, "nibs-aaa1", "nibs-")
		var found bool
		for _, b := range got {
			if b.ID == "nibs-ccc3" && b.Status == "completed" {
				found = true
			}
		}
		if !found {
			t.Error("completed nib nibs-ccc3 should still be returned as a mention")
		}
	})

	t.Run("empty body returns nil", func(t *testing.T) {
		got := FindMentionsInMap(nibs, "nibs-bbb2", "nibs-")
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("unknown fromID returns nil", func(t *testing.T) {
		got := FindMentionsInMap(nibs, "does-not-exist", "nibs-")
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("no configured prefix — only full-form tokens resolve", func(t *testing.T) {
		noPrefix := map[string]*nib.Nib{
			"abc1": {ID: "abc1", Body: "ref #def2 and #abc1 self"},
			"def2": {ID: "def2", Body: ""},
		}
		got := FindMentionsInMap(noPrefix, "abc1", "")
		if len(got) != 1 || got[0].ID != "def2" {
			t.Errorf("got %v, want [def2]", got)
		}
	})
}

func TestFindMentionedByInMap(t *testing.T) {
	nibs := map[string]*nib.Nib{
		"nibs-aaa1": {ID: "nibs-aaa1", Title: "A", Status: "todo", Body: "Refs #bbb2."},
		"nibs-bbb2": {ID: "nibs-bbb2", Title: "B", Status: "todo", Body: "No refs."},
		"nibs-ccc3": {ID: "nibs-ccc3", Title: "C", Status: "todo", Body: "Also see #nibs-bbb2."},
		"nibs-ddd4": {ID: "nibs-ddd4", Title: "D", Status: "todo", Body: "Mentions #bbb2 twice: #bbb2."},
	}

	t.Run("returns all inbound mentioners", func(t *testing.T) {
		got := FindMentionedByInMap(nibs, "nibs-bbb2", "nibs-")
		if len(got) != 3 {
			t.Fatalf("got %d, want 3", len(got))
		}
		ids := map[string]bool{}
		for _, b := range got {
			ids[b.ID] = true
		}
		for _, want := range []string{"nibs-aaa1", "nibs-ccc3", "nibs-ddd4"} {
			if !ids[want] {
				t.Errorf("missing %s from mentionedBy", want)
			}
		}
	})

	t.Run("duplicate mentions from same nib count once", func(t *testing.T) {
		got := FindMentionedByInMap(nibs, "nibs-bbb2", "nibs-")
		count := 0
		for _, b := range got {
			if b.ID == "nibs-ddd4" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("nibs-ddd4 appears %d times, want 1", count)
		}
	})

	t.Run("target with no inbound mentions", func(t *testing.T) {
		got := FindMentionedByInMap(nibs, "nibs-aaa1", "nibs-")
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("unknown target returns nil", func(t *testing.T) {
		got := FindMentionedByInMap(nibs, "does-not-exist", "nibs-")
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("self-references excluded from inbound", func(t *testing.T) {
		self := map[string]*nib.Nib{
			"nibs-self": {ID: "nibs-self", Body: "I mention myself: #self and #nibs-self."},
		}
		got := FindMentionedByInMap(self, "nibs-self", "nibs-")
		if got != nil {
			t.Errorf("got %v, want nil (self-ref excluded)", got)
		}
	})
}

func TestCoreFindMentions(t *testing.T) {
	core, _ := setupTestCore(t)

	nibA := &nib.Nib{ID: "aaa1", Title: "A", Status: "todo", Body: "See #bbb2."}
	nibB := &nib.Nib{ID: "bbb2", Title: "B", Status: "todo"}
	for _, b := range []*nib.Nib{nibA, nibB} {
		if err := core.Create(b); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	t.Run("outbound", func(t *testing.T) {
		got := core.FindMentions("aaa1")
		if len(got) != 1 || got[0].ID != "bbb2" {
			t.Errorf("got %v, want [bbb2]", got)
		}
	})

	t.Run("inbound", func(t *testing.T) {
		got := core.FindMentionedBy("bbb2")
		if len(got) != 1 || got[0].ID != "aaa1" {
			t.Errorf("got %v, want [aaa1]", got)
		}
	})
}
