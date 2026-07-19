package nibcore

import (
	"fmt"
	"strings"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibtypes"
)

// ValidateParentInMap checks if a parent is valid for the given nib.
// The parentID must already be normalized (prefix resolution done by caller).
// Returns nil if valid, error otherwise.
// This is a pure function that operates on a map of nibs without locking.
func ValidateParentInMap(nibs map[string]*nib.Nib, b *nib.Nib, parentID string) error {
	if parentID == "" {
		return nil
	}

	parent, ok := nibs[parentID]
	if !ok {
		return fmt.Errorf("parent nib not found: %s", parentID)
	}

	return nibtypes.ValidateParentType(b.EffectiveType(), parent.EffectiveType())
}

// ValidateParent checks if a parent is valid for the given nib.
// Thread-safe wrapper around ValidateParentInMap that handles prefix normalization.
func (c *Core) ValidateParent(b *nib.Nib, parentID string) error {
	if parentID == "" {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	normalizedID := parentID
	if _, ok := c.nibs[parentID]; !ok {
		if c.config != nil && c.config.Nibs.Prefix != "" &&
			!strings.HasPrefix(parentID, c.config.Nibs.Prefix) {
			normalizedID = c.config.Nibs.Prefix + parentID
		}
	}
	return ValidateParentInMap(c.nibs, b, normalizedID)
}

