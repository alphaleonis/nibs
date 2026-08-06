package estimate

import "testing"

func TestWeight(t *testing.T) {
	tests := []struct {
		estimate string
		want     int
	}{
		{"s", 1},
		{"m", 3},
		{"l", 5},
		{"xl", 8},
		{"", 3},        // default to M weight
		{"unknown", 3}, // unknown defaults to M weight
	}

	for _, tt := range tests {
		name := tt.estimate
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			got := Weight(tt.estimate)
			if got != tt.want {
				t.Errorf("Weight(%q) = %d, want %d", tt.estimate, got, tt.want)
			}
		})
	}
}
