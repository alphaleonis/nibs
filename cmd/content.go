package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/alphaleonis/nibs/internal/input"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
)

// resolveBodyFlag resolves full-body content from the --body and --body-file
// flags. --body accepts only the input channel ("-" for stdin, "@FILE" for a
// file) — never inline prose; --body-file names a file directly. cobra marks
// the two mutually exclusive, so at most one is set.
func resolveBodyFlag(bodyValue, bodyFile string) (string, error) {
	if bodyFile != "" {
		data, err := os.ReadFile(bodyFile)
		if err != nil {
			return "", &input.IOError{Err: fmt.Errorf("reading %s: %w", bodyFile, err)}
		}
		return string(data), nil
	}
	return input.Prose(bodyValue, os.Stdin)
}

// resolveAppendFlag resolves --body-append content from the input channel
// ("-"/"@FILE") and trims a trailing newline so appended sections do not accrue
// blank lines.
func resolveAppendFlag(value string) (string, error) {
	text, err := input.Prose(value, os.Stdin)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(text, "\n"), nil
}

// inputError maps an input-channel resolution error to the correct CLI error
// code: a failed read (*input.IOError) is a FILE_ERROR (exit 5); a usage error
// (inline prose or a malformed "@") is a validation error (exit 2).
func inputError(jsonMode bool, err error) error {
	var ioErr *input.IOError
	if errors.As(err, &ioErr) {
		return cmdError(jsonMode, output.ErrFileError, "%s", err)
	}
	return cmdError(jsonMode, output.ErrValidation, "%s", err)
}

// applyTags adds tags to a nib, returning an error if any tag is invalid.
func applyTags(b *nib.Nib, tags []string) error {
	for _, tag := range tags {
		if err := b.AddTag(tag); err != nil {
			return err
		}
	}
	return nil
}

// formatCycle formats a cycle path for display.
func formatCycle(path []string) string {
	return strings.Join(path, " → ")
}

// cmdError returns an appropriate error for JSON or text mode.
// Note: Use %v instead of %w for error arguments - wrapping is not preserved in JSON mode.
func cmdError(jsonMode bool, code string, format string, args ...any) error {
	if jsonMode {
		return output.Error(code, fmt.Sprintf(format, args...))
	}
	return fmt.Errorf(format, args...)
}
