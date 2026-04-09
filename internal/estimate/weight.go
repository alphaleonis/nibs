package estimate

// weights maps estimate sizes to their point values.
var weights = map[string]int{
	"s":  1,
	"m":  3,
	"l":  5,
	"xl": 8,
}

// DefaultWeight is used for unestimated or unknown estimates (equivalent to M).
const DefaultWeight = 3

// Weight returns the point weight for an estimate size.
// Unknown or empty estimates default to DefaultWeight (M=3).
func Weight(estimate string) int {
	if w, ok := weights[estimate]; ok {
		return w
	}
	return DefaultWeight
}
