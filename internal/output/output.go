package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/alphaleonis/nibs/internal/nib"
)

// ErrAlreadyReported is the sentinel the CLI boundary uses to detect that
// an error has already been reported to stdout (a --json envelope or get's
// single-stream text line). When the boundary sees errors.Is(err,
// ErrAlreadyReported) it suppresses the duplicate "Error: <msg>" stderr
// print so callers piping `2>&1 | jq` receive a single parseable document.
var ErrAlreadyReported = errors.New("already reported to stdout")

// CodedError carries a stable error CODE from a command up to the CLI
// boundary (reportExitError in cmd/root.go), which maps the code to a
// process exit status via ExitCode. It also carries the user-visible
// message and a Reported flag.
//
// When Reported is true the command has already written the user-visible
// report to stdout — either the --json error envelope (Error) or get's
// single-stream text line (TextError) — so the boundary must NOT also print
// "Error: <msg>" to stderr (that would duplicate the report and corrupt
// callers piping `2>&1 | jq`). A reported CodedError therefore satisfies
// errors.Is(err, ErrAlreadyReported), preserving the historical sentinel
// contract. The sentinel name never surfaces in Error() output — only the
// original message does.
//
// Err (optional) is the wrapped cause. Unwrap exposes it so callers'
// errors.Is/As chains (e.g. sentinel errors like a command's not-found)
// survive being carried through the text-error path.
type CodedError struct {
	Code     string
	Msg      string
	Reported bool
	Err      error
}

func (e *CodedError) Error() string { return e.Msg }

func (e *CodedError) Unwrap() error { return e.Err }

func (e *CodedError) Is(target error) bool {
	return e.Reported && target == ErrAlreadyReported
}

// Error codes for JSON responses
const (
	ErrNotFound      = "NOT_FOUND"
	ErrNoNibsDir     = "NO_BEANS_DIR"
	ErrInvalidStatus = "INVALID_STATUS"
	ErrFileError     = "FILE_ERROR"
	ErrValidation    = "VALIDATION_ERROR"
	ErrConflict      = "CONFLICT"
)

// Process exit codes. The CLI boundary maps every error's structured CODE to
// one of these via ExitCode so agents can branch on $? without parsing text.
const (
	ExitOK         = 0
	ExitError      = 1 // generic / uncategorized failure
	ExitValidation = 2 // VALIDATION_ERROR, INVALID_STATUS
	ExitNotFound   = 3 // NOT_FOUND
	ExitConflict   = 4 // CONFLICT (etag / optimistic-concurrency)
	ExitIO         = 5 // FILE_ERROR, NO_*_DIR (filesystem / IO)
)

// ExitCode maps a structured error CODE (one of the Err* constants) to a
// stable process exit status. Unknown codes collapse to ExitError. This is
// the single source of truth for CLI exit statuses — the cmd boundary is the
// only caller.
func ExitCode(code string) int {
	switch code {
	case ErrNotFound:
		return ExitNotFound
	case ErrValidation, ErrInvalidStatus:
		return ExitValidation
	case ErrConflict:
		return ExitConflict
	case ErrFileError, ErrNoNibsDir:
		return ExitIO
	default:
		return ExitError
	}
}

// Response is the standard JSON response envelope.
type Response struct {
	Success  bool       `json:"success"`
	Nib      *nib.Nib   `json:"nib,omitempty"`
	Nibs     []*nib.Nib `json:"nibs,omitempty"`
	Count    int        `json:"count,omitempty"`
	Message  string     `json:"message,omitempty"`
	Warnings []string   `json:"warnings,omitempty"`
	Path     string     `json:"path,omitempty"`
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
		Nib:     b,
		Message: message,
	})
}

// SuccessWithWarnings outputs a successful single-nib response with warnings.
func SuccessWithWarnings(b *nib.Nib, message string, warnings []string) error {
	return JSON(Response{
		Success:  true,
		Nib:      b,
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

// errorEnvelope is the --json error contract (design §5.5): a single
// top-level "error" object, no success/data wrapper.
//
//	{"error": {"code": "<CODE>", "message": "<msg>"}}
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error writes the --json error contract to stdout and returns a reported
// CodedError. Wrap freely with fmt.Errorf("…: %w", err) — the CLI boundary
// uses errors.As to recover the code and errors.Is(err, ErrAlreadyReported)
// still holds through the unwrap chain.
//
// The returned error:
//   - has Error() == message (no code/sentinel leakage in user-visible text)
//   - carries the code so the boundary maps it to a stable exit status
//   - satisfies errors.Is(err, ErrAlreadyReported) so the boundary suppresses
//     its duplicate stderr "Error: ..." line (one parseable JSON document for
//     callers piping `2>&1 | jq`).
func Error(code string, message string) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(errorEnvelope{Error: errorBody{Code: code, Message: message}})
	return &CodedError{Code: code, Msg: message, Reported: true}
}

// TextError writes get's single-stream text error to stdout — "error <CODE>:
// <message>" — and returns a reported CodedError. Text-mode agents read
// stdout + $? without a 2>stderr split (design §5.5), so unlike the shared
// stderr text path this keeps the error on stdout.
func TextError(code string, message string) error {
	_, _ = fmt.Fprintf(os.Stdout, "error %s: %s\n", code, message)
	return &CodedError{Code: code, Msg: message, Reported: true}
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
