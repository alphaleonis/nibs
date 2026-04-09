package nib

import (
	"fmt"
	"strings"
)

// Fractional indexing for nib ordering.
//
// Uses base-62 strings (0-9, A-Z, a-z) that sort lexicographically.
// This allows inserting between any two existing keys without reindexing.

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
const base = len(base62) // 62

// charIndex returns the index of c in the base62 alphabet, or -1 if not found.
func charIndex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 36
	default:
		return -1
	}
}

// ValidateOrderKey checks that a key contains only valid base-62 characters.
// Returns nil for empty keys (empty means "no order assigned").
func ValidateOrderKey(key string) error {
	for i := 0; i < len(key); i++ {
		if charIndex(key[i]) == -1 {
			return fmt.Errorf("invalid character %q at position %d in order key %q", key[i], i, key)
		}
	}
	return nil
}

// OrderBetween returns a key that sorts lexicographically between a and b.
// If a is empty, returns a key before b. If b is empty, returns a key after a.
// If both are empty, returns an initial key.
func OrderBetween(a, b string) string {
	if a == "" && b == "" {
		return OrderInitial()
	}
	if a == "" {
		return OrderFirst(b)
	}
	if b == "" {
		return OrderLast(a)
	}
	return midpoint(a, b)
}

// OrderFirst returns a key that sorts before existing.
// Precondition: existing must contain at least one non-'0' character.
// All-'0' keys are at the bottom of the keyspace and have no predecessor.
// Generated keys always start from "a0" (via OrderInitial), so this precondition
// is satisfied for any key produced by this package.
func OrderFirst(existing string) string {
	// Walk past leading '0's (smallest char) to find first non-minimal character
	i := 0
	for i < len(existing) && charIndex(existing[i]) == 0 {
		i++
	}

	if i == len(existing) {
		// All '0's — extend with "0" + midpoint char
		return existing + string(base62[0]) + string(base62[base/2])
	}

	idx := charIndex(existing[i])
	if idx >= 2 {
		return existing[:i] + string(base62[idx/2])
	}

	// idx == 1 (adjacent to '0') — go deeper: prefix + "0" + midpoint
	return existing[:i] + string(base62[0]) + string(base62[base/2])
}

// OrderLast returns a key that sorts after existing.
func OrderLast(existing string) string {
	for i := len(existing) - 1; i >= 0; i-- {
		idx := charIndex(existing[i])
		if idx < base-1 {
			return existing[:i] + string(base62[idx+(base-idx)/2])
		}
	}
	// All 'z's — append midpoint character
	return existing + string(base62[base/2])
}

// OrderInitial returns a starting order key.
func OrderInitial() string {
	return "a0"
}

// OrderKeyN generates n evenly-spaced order keys for backfilling.
// Keys are strictly increasing and spread across the keyspace.
func OrderKeyN(n int) []string {
	if n <= 0 {
		return nil
	}
	keys := make([]string, n)
	prev := ""
	for i := range n {
		keys[i] = OrderBetween(prev, "")
		prev = keys[i]
	}
	return keys
}

// midpoint returns a string that sorts between a and b.
func midpoint(a, b string) string {
	maxLen := max(len(a), len(b))

	for i := range maxLen {
		ai := 0
		if i < len(a) {
			ai = charIndex(a[i])
		}
		bi := base
		if i < len(b) {
			bi = charIndex(b[i])
		}

		if ai == bi {
			continue
		}

		mid := ai + (bi-ai)/2
		if mid > ai {
			// When i > len(a), pad with '0' chars so the result is at position i, not len(a)
			prefix := a
			if i > len(a) {
				prefix = a + strings.Repeat(string(base62[0]), i-len(a))
			} else if i < len(a) {
				prefix = a[:i]
			}
			return prefix + string(base62[mid])
		}

		// Adjacent characters — go deeper using a's prefix + midpoint of remaining space
		var prefix string
		if i < len(a) {
			prefix = a[:i+1]
		} else {
			prefix = a + strings.Repeat(string(base62[0]), i-len(a)) + string(base62[0])
		}
		remA := ""
		if i+1 < len(a) {
			remA = a[i+1:]
		}
		return prefix + suffixMidpoint(remA)
	}

	// Equal up to maxLen — extend with midpoint
	return a + string(base62[base/2])
}

// suffixMidpoint returns a key roughly in the middle of the remaining space after a suffix.
func suffixMidpoint(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		idx := charIndex(s[i])
		if idx < base-1 {
			return s[:i] + string(base62[idx+(base-idx)/2])
		}
	}
	return s + string(base62[base/2])
}
