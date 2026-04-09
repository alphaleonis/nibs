package nib

import "errors"

// ErrNotFound is returned when a nib cannot be found by ID.
var ErrNotFound = errors.New("nib not found")

// IncomingLink represents a link from another nib to a target nib.
type IncomingLink struct {
	FromNib  *Nib
	LinkType string
}
