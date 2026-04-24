package tui

import (
	"testing"
)

// Behavior 20: KeyMap exposes MoveUp / MoveDown bindings for ctrl+up / ctrl+down,
// surfaced via FullHelp for discoverability. (The dispatch itself stays as an
// inline string switch in list.go; these bindings are for help text only.)
func TestKeyMap_MoveUpMoveDownBindings(t *testing.T) {
	km := DefaultKeyMap()

	t.Run("MoveUp bound to ctrl+up", func(t *testing.T) {
		keys := km.MoveUp.Keys()
		if !containsString(keys, "ctrl+up") {
			t.Errorf("expected MoveUp to include ctrl+up, got %v", keys)
		}
		if help := km.MoveUp.Help(); help.Key == "" || help.Desc == "" {
			t.Errorf("expected MoveUp to have non-empty help, got %+v", help)
		}
	})

	t.Run("MoveDown bound to ctrl+down", func(t *testing.T) {
		keys := km.MoveDown.Keys()
		if !containsString(keys, "ctrl+down") {
			t.Errorf("expected MoveDown to include ctrl+down, got %v", keys)
		}
		if help := km.MoveDown.Help(); help.Key == "" || help.Desc == "" {
			t.Errorf("expected MoveDown to have non-empty help, got %+v", help)
		}
	})

	t.Run("FullHelp surfaces both bindings", func(t *testing.T) {
		rows := km.FullHelp()
		var foundUp, foundDown bool
		for _, row := range rows {
			for _, b := range row {
				if containsString(b.Keys(), "ctrl+up") {
					foundUp = true
				}
				if containsString(b.Keys(), "ctrl+down") {
					foundDown = true
				}
			}
		}
		if !foundUp {
			t.Error("FullHelp did not include MoveUp (ctrl+up)")
		}
		if !foundDown {
			t.Error("FullHelp did not include MoveDown (ctrl+down)")
		}
	})
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
