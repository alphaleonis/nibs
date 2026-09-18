package graph

import (
	"context"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph/model"
)

// areaNodeIn finds one declared area in a payload's flattened vocabulary.
func areaNodeIn(areas []*model.Area, path string) *model.Area {
	for _, a := range areas {
		if a.Path == path {
			return a
		}
	}
	return nil
}

// TestUpdateAreaSetsEachFieldOnItsOwn is the gap this mutation was grown to
// close: description and color could be set at creation and changed by nothing
// afterwards, on any surface.
func TestUpdateAreaSetsEachFieldOnItsOwn(t *testing.T) {
	tests := []struct {
		name  string
		input model.UpdateAreaInput
		want  func(*testing.T, *model.Area)
	}{
		{
			name:  "a description alone",
			input: model.UpdateAreaInput{Path: "web", Description: ptr("The browser client")},
			want: func(t *testing.T, n *model.Area) {
				if n.Description != "The browser client" {
					t.Errorf("description = %q, want the value set", n.Description)
				}
			},
		},
		{
			name:  "a color alone",
			input: model.UpdateAreaInput{Path: "web", Color: ptr("teal")},
			want: func(t *testing.T, n *model.Area) {
				if n.Color != "teal" {
					t.Errorf("color = %q, want the value set", n.Color)
				}
			},
		},
		{
			name: "all three at once",
			input: model.UpdateAreaInput{
				Path: "web", NewName: ptr("platform"),
				Description: ptr("Everything the browser runs"), Color: ptr("#00aaff"),
			},
			want: func(t *testing.T, n *model.Area) {
				if n.Description != "Everything the browser runs" || n.Color != "#00aaff" {
					t.Errorf("node = %+v, want the description and color set alongside the rename", *n)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, _ := setupTestResolverWithAreas(t)

			payload, err := resolver.Mutation().UpdateArea(context.Background(), tt.input)
			if err != nil {
				t.Fatalf("UpdateArea: %v", err)
			}

			// A rename moves the path, so look the node up by where it landed.
			path := tt.input.Path
			if tt.input.NewName != nil {
				path = *tt.input.NewName
			}
			node := areaNodeIn(payload.Config.Areas, path)
			if node == nil {
				t.Fatalf("%s is not declared after the update: %v", path, pathsOf(payload.Config.Areas))
			}
			tt.want(t, node)
		})
	}
}

// TestUpdateAreaLeavesOmittedFieldsAlone is the tri-state at the wire: a field
// the client did not send keeps whatever the store declares. Collapse omitted
// into empty and a client setting one field wipes the others beside it.
func TestUpdateAreaLeavesOmittedFieldsAlone(t *testing.T) {
	resolver, _ := setupTestResolverWithAreas(t)

	if _, err := resolver.Mutation().UpdateArea(context.Background(), model.UpdateAreaInput{
		Path: "web", Description: ptr("Kept"), Color: ptr("teal"),
	}); err != nil {
		t.Fatalf("seeding the fields: %v", err)
	}

	payload, err := resolver.Mutation().UpdateArea(context.Background(),
		model.UpdateAreaInput{Path: "web", Color: ptr("slate")})
	if err != nil {
		t.Fatalf("UpdateArea: %v", err)
	}

	node := areaNodeIn(payload.Config.Areas, "web")
	if node.Color != "slate" {
		t.Errorf("color = %q, want the value this call set", node.Color)
	}
	if node.Description != "Kept" {
		t.Errorf("description = %q, want the stored one, untouched by a call that did not send it", node.Description)
	}
}

// TestUpdateAreaClearsAFieldSentEmpty is the other half: "" is a clear, and it
// has to survive the wire-to-engine mapping rather than being normalized to
// "unset" somewhere between them.
func TestUpdateAreaClearsAFieldSentEmpty(t *testing.T) {
	resolver, _ := setupTestResolverWithAreas(t)

	if _, err := resolver.Mutation().UpdateArea(context.Background(), model.UpdateAreaInput{
		Path: "web", Description: ptr("Written so it can be cleared"),
	}); err != nil {
		t.Fatalf("seeding the description: %v", err)
	}

	payload, err := resolver.Mutation().UpdateArea(context.Background(),
		model.UpdateAreaInput{Path: "web", Description: ptr("")})
	if err != nil {
		t.Fatalf("UpdateArea: %v", err)
	}
	if node := areaNodeIn(payload.Config.Areas, "web"); node.Description != "" {
		t.Errorf("description = %q, want it cleared", node.Description)
	}
}

// TestUpdateAreaRefusesAnUpdateThatSetsNothing: the store raises this before it
// takes the lock, and this surface has to word it in terms of the three fields a
// client may send — naming them IS the repair.
func TestUpdateAreaRefusesAnUpdateThatSetsNothing(t *testing.T) {
	resolver, _ := setupTestResolverWithAreas(t)

	_, err := resolver.Mutation().UpdateArea(context.Background(), model.UpdateAreaInput{Path: "web"})
	if err == nil {
		t.Fatal("an update setting nothing was accepted, which reports success over an edit that never happened")
	}
	for _, want := range []string{"newName", "description", "color"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name the %s field a caller may send", err.Error(), want)
		}
	}
}
