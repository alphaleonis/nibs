package cmd

import (
	"errors"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
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

// filterTargetErrCode maps the two filter-target failures graph.ApplyFilter
// distinguishes onto the CLI's structured error codes, so a query that could
// not be answered exits differently from one that was answered with nothing.
// It reports ok=false for every other error, leaving the caller's own fallback
// in charge.
//
//   - An id no nib answers to is NOT_FOUND (exit 3). It is recognized through
//     nib.ErrNotFound rather than the concrete type, which is the same channel
//     the GraphQL error presenter keys on (cmd/serve.go), so the CLI and the
//     HTTP server classify one filter failure the same way.
//   - A target that resolved and then could not be read is FILE_ERROR (exit 5)
//     — the io/internal class. Reporting a concurrent delete as a not-found
//     would tell an agent it typed the wrong id when it did not.
//
// The two branches are independent, not ordered: graph.FilterTargetUnreadable
// Error deliberately does not carry nib.ErrNotFound (see its doc comment), so
// neither test can claim the other's error.
func filterTargetErrCode(err error) (string, bool) {
	var unreadable *graph.FilterTargetUnreadableError
	if errors.As(err, &unreadable) {
		return output.ErrFileError, true
	}
	if errors.Is(err, nib.ErrNotFound) {
		return output.ErrNotFound, true
	}
	return "", false
}
