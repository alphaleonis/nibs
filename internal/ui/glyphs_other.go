//go:build !windows

package ui

// detectASCIIRequired reports false: terminals outside Windows are assumed to
// display UTF-8.
func detectASCIIRequired() bool {
	return false
}
