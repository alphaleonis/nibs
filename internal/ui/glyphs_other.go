//go:build !windows

package ui

// detectASCIIRequired reports whether the platform requires ASCII fallbacks.
// On non-Windows systems modern terminals are UTF-8 by convention, so this
// always returns false.
func detectASCIIRequired() bool {
	return false
}
