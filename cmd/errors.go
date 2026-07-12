package cmd

import (
	"github.com/alphaleonis/nibs/internal/output"
)

// reportErr returns either a text-path error or a JSON-envelope error based
// on the jsonMode flag. Keeps the CLI's dual-path error convention in one
// place — every command that has a --json flag should use this rather than
// inlining the check.
//
// Both paths carry the structured code to the CLI boundary (reportExitError)
// so the exit status is code-driven in both modes:
//
//   - jsonMode true: output.Error writes the {error:{code,message}} envelope
//     to stdout and returns a reported CodedError (the boundary suppresses
//     its stderr print).
//   - jsonMode false: return a non-reported CodedError carrying the code and
//     message. The boundary owns the stderr "Error: <msg>" print; only the
//     exit status is now mapped from the code.
func reportErr(jsonMode bool, code string, err error) error {
	if jsonMode {
		return output.Error(code, err.Error())
	}
	// Wrap the cause (Err) so callers' errors.Is/As chains survive; the
	// boundary recovers the code via errors.As and prints Msg to stderr.
	return &output.CodedError{Code: code, Msg: err.Error(), Err: err}
}
