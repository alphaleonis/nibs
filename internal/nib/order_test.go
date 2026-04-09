package nib

import (
	"strings"
	"testing"
)

func TestOrderBetween(t *testing.T) {
	tests := []struct {
		name string
		a, b string
	}{
		{name: "between two keys", a: "a0", b: "b0"},
		{name: "between adjacent single chars", a: "a", b: "c"},
		{name: "between with shared prefix", a: "a0", b: "a2"},
		{name: "empty a means before b", a: "", b: "a0"},
		{name: "empty b means after a", a: "a0", b: ""},
		{name: "both empty returns initial", a: "", b: ""},
		{name: "adjacent digits", a: "a0", b: "a1"},
		{name: "near start of alphabet", a: "0", b: "1"},
		{name: "near end of alphabet", a: "y", b: "z"},
		{name: "a is prefix of b with zeros", a: "a0", b: "a00V"},
		{name: "a is prefix of b with deep zeros", a: "a0", b: "a000V"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := OrderBetween(tt.a, tt.b)
			if result == "" {
				t.Fatal("OrderBetween returned empty string")
			}
			if tt.a != "" && result <= tt.a {
				t.Errorf("result %q should be > a %q", result, tt.a)
			}
			if tt.b != "" && result >= tt.b {
				t.Errorf("result %q should be < b %q", result, tt.b)
			}
		})
	}
}

func TestValidateOrderKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"valid key", "a0V", false},
		{"empty key", "", false},
		{"invalid character hyphen", "a-b", true},
		{"invalid character slash", "a/0", true},
		{"invalid character space", "a 0", true},
		{"all valid base62", "09AZaz", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOrderKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateOrderKey(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
			}
		})
	}
}

func TestOrderBetweenRepeatedInsertions(t *testing.T) {
	// Simulate inserting many items at the end — keys should always be strictly increasing
	t.Run("repeated append", func(t *testing.T) {
		prev := ""
		for i := 0; i < 100; i++ {
			key := OrderBetween(prev, "")
			if prev != "" && key <= prev {
				t.Fatalf("iteration %d: key %q should be > prev %q", i, key, prev)
			}
			prev = key
		}
	})

	// Simulate inserting many items at the start — keys should always be strictly decreasing
	t.Run("repeated prepend", func(t *testing.T) {
		next := "a0"
		for i := 0; i < 100; i++ {
			key := OrderBetween("", next)
			if key >= next {
				t.Fatalf("iteration %d: key %q should be < next %q", i, key, next)
			}
			next = key
		}
	})

	// Simulate inserting between the same two keys repeatedly (always insert in the middle)
	t.Run("repeated midpoint", func(t *testing.T) {
		a, b := "a0", "b0"
		for i := 0; i < 50; i++ {
			mid := OrderBetween(a, b)
			if mid <= a {
				t.Fatalf("iteration %d: mid %q should be > a %q", i, mid, a)
			}
			if mid >= b {
				t.Fatalf("iteration %d: mid %q should be < b %q", i, mid, b)
			}
			// Insert after the midpoint next time
			a = mid
		}
	})
}

func TestOrderFieldRoundTrip(t *testing.T) {
	t.Run("order field is parsed from frontmatter", func(t *testing.T) {
		input := `---
title: Test
status: todo
order: a0
---
`
		b, err := Parse(strings.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		if b.Order != "a0" {
			t.Errorf("Order = %q, want %q", b.Order, "a0")
		}
	})

	t.Run("order field is rendered to frontmatter", func(t *testing.T) {
		b := &Nib{
			Version: 1,
			Title:   "Test",
			Status:  "todo",
			Order:   "a0",
		}
		data, err := b.Render()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "order: a0") {
			t.Errorf("rendered output should contain 'order: a0', got:\n%s", data)
		}
	})

	t.Run("empty order field is omitted from frontmatter", func(t *testing.T) {
		b := &Nib{
			Version: 1,
			Title:   "Test",
			Status:  "todo",
		}
		data, err := b.Render()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "order:") {
			t.Errorf("rendered output should not contain 'order:', got:\n%s", data)
		}
	})

	t.Run("order field round-trips correctly", func(t *testing.T) {
		original := &Nib{
			Version: 1,
			Title:   "Test",
			Status:  "todo",
			Order:   "b5Xm",
		}
		data, err := original.Render()
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := Parse(strings.NewReader(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Order != original.Order {
			t.Errorf("Order = %q after round-trip, want %q", parsed.Order, original.Order)
		}
	})
}

func TestSortByOrder(t *testing.T) {
	t.Run("sorts by order key lexicographically", func(t *testing.T) {
		nibs := []*Nib{
			{ID: "3", Order: "c0"},
			{ID: "1", Order: "a0"},
			{ID: "2", Order: "b0"},
		}
		SortByOrder(nibs)
		if nibs[0].ID != "1" || nibs[1].ID != "2" || nibs[2].ID != "3" {
			t.Errorf("got order %s,%s,%s; want 1,2,3", nibs[0].ID, nibs[1].ID, nibs[2].ID)
		}
	})

	t.Run("unordered nibs sort after ordered ones", func(t *testing.T) {
		nibs := []*Nib{
			{ID: "unordered", Title: "Z"},
			{ID: "ordered", Order: "a0", Title: "A"},
		}
		SortByOrder(nibs)
		if nibs[0].ID != "ordered" {
			t.Errorf("first nib should be ordered, got %s", nibs[0].ID)
		}
		if nibs[1].ID != "unordered" {
			t.Errorf("second nib should be unordered, got %s", nibs[1].ID)
		}
	})

	t.Run("multiple unordered nibs sort by title", func(t *testing.T) {
		nibs := []*Nib{
			{ID: "2", Title: "Zebra"},
			{ID: "1", Title: "Apple"},
			{ID: "0", Order: "a0", Title: "Middle"},
		}
		SortByOrder(nibs)
		if nibs[0].ID != "0" {
			t.Errorf("first should be ordered nib, got %s", nibs[0].ID)
		}
		if nibs[1].ID != "1" {
			t.Errorf("second should be Apple, got %s (%s)", nibs[1].ID, nibs[1].Title)
		}
		if nibs[2].ID != "2" {
			t.Errorf("third should be Zebra, got %s (%s)", nibs[2].ID, nibs[2].Title)
		}
	})

	t.Run("all unordered nibs sort by title", func(t *testing.T) {
		nibs := []*Nib{
			{ID: "2", Title: "Zebra"},
			{ID: "1", Title: "Apple"},
		}
		SortByOrder(nibs)
		if nibs[0].ID != "1" || nibs[1].ID != "2" {
			t.Errorf("got %s,%s; want 1,2", nibs[0].ID, nibs[1].ID)
		}
	})
}

func TestOrderKeyN(t *testing.T) {
	tests := []struct {
		name string
		n    int
	}{
		{name: "zero keys", n: 0},
		{name: "one key", n: 1},
		{name: "three keys", n: 3},
		{name: "ten keys", n: 10},
		{name: "fifty keys", n: 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := OrderKeyN(tt.n)
			if tt.n == 0 {
				if keys != nil {
					t.Fatalf("expected nil for n=0, got %v", keys)
				}
				return
			}
			if len(keys) != tt.n {
				t.Fatalf("expected %d keys, got %d", tt.n, len(keys))
			}
			// All keys must be strictly increasing
			for i := 1; i < len(keys); i++ {
				if keys[i] <= keys[i-1] {
					t.Errorf("keys[%d]=%q should be > keys[%d]=%q", i, keys[i], i-1, keys[i-1])
				}
			}
		})
	}
}
