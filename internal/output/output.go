package output

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/alphaleonis/nibs/internal/nib"
)

// ErrAlreadyReported is the sentinel the CLI boundary uses to detect that
// an error has already been reported as a JSON envelope on stdout. When
// Execute() sees errors.Is(err, ErrAlreadyReported) it suppresses the
// duplicate "Error: <msg>" stderr print so callers piping `2>&1 | jq`
// receive a single parseable JSON document.
var ErrAlreadyReported = errors.New("already reported as JSON envelope")

// reportedError carries the original user-visible message AND satisfies
// errors.Is(err, ErrAlreadyReported). The sentinel name itself never
// surfaces in .Error() output — only the original message does.
type reportedError struct{ msg string }

func (e *reportedError) Error() string        { return e.msg }
func (e *reportedError) Is(target error) bool { return target == ErrAlreadyReported }

// Error codes for JSON responses
const (
	ErrNotFound      = "NOT_FOUND"
	ErrNoNibsDir    = "NO_BEANS_DIR"
	ErrInvalidStatus = "INVALID_STATUS"
	ErrFileError     = "FILE_ERROR"
	ErrValidation    = "VALIDATION_ERROR"
	ErrConflict      = "CONFLICT"
)

// Response is the standard JSON response envelope.
type Response struct {
	Success  bool         `json:"success"`
	Nib     *nib.Nib   `json:"nib,omitempty"`
	Nibs    []*nib.Nib `json:"nibs,omitempty"`
	Count    int          `json:"count,omitempty"`
	Message  string       `json:"message,omitempty"`
	Warnings []string     `json:"warnings,omitempty"`
	Error    string       `json:"error,omitempty"`
	Code     string       `json:"code,omitempty"`
	Path     string       `json:"path,omitempty"`
}

// JSON outputs a response as JSON to stdout.
func JSON(resp Response) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}

// Success outputs a successful single-nib response.
func Success(b *nib.Nib, message string) error {
	return JSON(Response{
		Success: true,
		Nib:    b,
		Message: message,
	})
}

// SuccessWithWarnings outputs a successful single-nib response with warnings.
func SuccessWithWarnings(b *nib.Nib, message string, warnings []string) error {
	return JSON(Response{
		Success:  true,
		Nib:     b,
		Message:  message,
		Warnings: warnings,
	})
}

// SuccessSingle outputs a single nib directly (no wrapper).
// This allows intuitive jq usage: nibs show --json <id> | jq '.title'
func SuccessSingle(b *nib.Nib) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(b)
}

// SuccessMultiple outputs a nib array directly (no wrapper).
// This allows intuitive jq usage: nibs list --json | jq '.[]'
func SuccessMultiple(nibs []*nib.Nib) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(nibs)
}

// SuccessMessage outputs a success response with just a message.
func SuccessMessage(message string) error {
	return JSON(Response{
		Success: true,
		Message: message,
	})
}

// SuccessInit outputs a success response for init command.
func SuccessInit(path string) error {
	return JSON(Response{
		Success: true,
		Message: "Initialized .nibs directory",
		Path:    path,
	})
}

// Error outputs an error response and returns an error for command handling.
//
// The CLI boundary (reportExitError in cmd/root.go) recognises the returned
// error via errors.Is(err, ErrAlreadyReported) and suppresses its own
// "Error: <msg>\n" stderr print, so callers piping `2>&1 | jq` see exactly
// one parseable JSON document. Wrap freely with fmt.Errorf("…: %w", err) —
// the sentinel survives errors.Is's unwrap chain.
//
// The returned error:
//   - has Error() == message (no sentinel leakage in user-visible text)
//   - satisfies errors.Is(err, ErrAlreadyReported) so the Cobra boundary
//     can suppress the duplicate stderr "Error: ..." line.
func Error(code string, message string) error {
	_ = JSON(Response{
		Success: false,
		Error:   message,
		Code:    code,
	})
	return &reportedError{msg: message}
}

// JSONRaw outputs any value as pretty-printed JSON to stdout.
func JSONRaw(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ErrorFrom outputs an error response from an existing error.
func ErrorFrom(code string, err error) error {
	return Error(code, err.Error())
}
