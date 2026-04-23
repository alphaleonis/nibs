package cmd

import (
	"github.com/alphaleonis/nibs/internal/output"
)

// reportErr returns either a text-path error or a JSON-envelope error
// based on the jsonMode flag. Keeps the CLI's dual-path error convention
// in one place — every command that has a --json flag should use this
// rather than inlining the check.
//
// When jsonMode is true the returned error is the sentinel returned by
// output.Error (it has already written the JSON envelope to stdout and
// the error itself is only used to signal a non-zero exit status).
// When jsonMode is false the original err is passed through unchanged
// so Cobra's default text-error printing takes over.
func reportErr(jsonMode bool, code string, err error) error {
	if jsonMode {
		return output.Error(code, err.Error())
	}
	return err
}
