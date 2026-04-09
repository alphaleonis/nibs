package graph

import (
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// Compile-time checks: *nibcore.Core must satisfy all role interfaces.
var (
	_ NibReader       = (*nibcore.Core)(nil)
	_ NibWriter       = (*nibcore.Core)(nil)
	_ NibValidator    = (*nibcore.Core)(nil)
	_ BlockingChecker = (*nibcore.Core)(nil)
)
