// Package input resolves the value of a prose-carrying flag from an input
// channel: standard input ("-") or a file ("@FILE"). It exists so that
// multi-line Markdown never has to ride on a shell argument, where quoting and
// escaping are fragile and error-prone. The resolver is deliberately cobra-free
// so the CLI, its tests, and future callers can share one uniform rule.
package input

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ErrInlineProse is returned by Prose when a prose flag receives a bare inline
// value instead of the input channel. It is a caller/usage error, not an I/O
// failure, so callers map it to a validation exit code.
var ErrInlineProse = errors.New(`inline text is not allowed here; use "-" to read stdin or "@FILE" to read from a file`)

// ErrEmptyPath is returned by Prose when a value is "@" with no path after it.
// Like ErrInlineProse it is a usage error, not an I/O failure.
var ErrEmptyPath = errors.New(`"@" must be followed by a file path`)

// IOError wraps a failure encountered while reading the input channel — stdin
// or the named file. Callers distinguish it from the usage errors above (via
// errors.As) so a failed read maps to an I/O exit code while a misused flag
// maps to a validation exit code.
type IOError struct{ Err error }

func (e *IOError) Error() string { return e.Err.Error() }
func (e *IOError) Unwrap() error { return e.Err }

// Prose resolves the value of a prose/body flag from the input channel:
//
//   - ""      resolves to "" so callers can uniformly ignore an unset flag.
//   - "-"     reads all of stdin.
//   - "@PATH" reads the named file.
//
// Any other (bare inline) value is rejected with ErrInlineProse. Read failures
// are returned wrapped in *IOError.
func Prose(value string, stdin io.Reader) (string, error) {
	switch {
	case value == "":
		return "", nil
	case value == "-":
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", &IOError{Err: fmt.Errorf("reading stdin: %w", err)}
		}
		return string(data), nil
	case strings.HasPrefix(value, "@"):
		path := value[1:]
		if path == "" {
			return "", ErrEmptyPath
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", &IOError{Err: fmt.Errorf("reading %s: %w", path, err)}
		}
		return string(data), nil
	default:
		return "", ErrInlineProse
	}
}
