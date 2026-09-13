package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/alphaleonis/nibs/internal/nib"
)

// ErrAlreadyReported matches any CodedError whose Reported is set. The CLI
// boundary reads Reported itself and skips its stderr "Error:" line.
var ErrAlreadyReported = errors.New("already reported to stdout")

// CodedError carries an error code to the CLI boundary (reportExitError), which
// maps it to an exit status with ExitCode. Reported means the report is already
// on stdout, and makes the error match ErrAlreadyReported. Err is the optional
// wrapped cause.
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
	ErrInvalidStatus = "INVALID_STATUS"
	ErrFileError     = "FILE_ERROR"
	ErrValidation    = "VALIDATION_ERROR"
	ErrConflict      = "CONFLICT"
	ErrHierarchy     = "HIERARCHY"
	// A surgical body replace matched zero times, or more than once (ErrorText).
	ErrTextNotFound  = "TEXT_NOT_FOUND"
	ErrTextAmbiguous = "TEXT_AMBIGUOUS"
	// ErrUncategorized is for a failure no other code describes. Do not use it as
	// a default.
	ErrUncategorized = "UNCATEGORIZED"
)

// Process exit codes; ExitCode maps each error code to one.
const (
	ExitOK         = 0
	ExitError      = 1 // generic / uncategorized failure
	ExitValidation = 2 // VALIDATION_ERROR, INVALID_STATUS, HIERARCHY, TEXT_*
	ExitNotFound   = 3 // NOT_FOUND
	ExitConflict   = 4 // CONFLICT (etag / optimistic-concurrency)
	ExitIO         = 5 // FILE_ERROR (filesystem / IO)
)

// exitCodes classifies every error code. Add each new Err* constant here: a code
// missing from the map exits 1, and no test notices.
var exitCodes = map[string]int{
	ErrNotFound:      ExitNotFound,
	ErrValidation:    ExitValidation,
	ErrInvalidStatus: ExitValidation,
	ErrHierarchy:     ExitValidation,
	ErrTextNotFound:  ExitValidation,
	ErrTextAmbiguous: ExitValidation,
	ErrConflict:      ExitConflict,
	ErrFileError:     ExitIO,
	ErrUncategorized: ExitError,
}

// ExitCode returns the exit status for code, or ExitError for an unknown code.
func ExitCode(code string) int {
	if exit, ok := exitCodes[code]; ok {
		return exit
	}
	return ExitError
}

// GeneralCode returns the most general code with code's exit status:
// VALIDATION_ERROR, NOT_FOUND, CONFLICT, FILE_ERROR or UNCATEGORIZED. Use it to
// report several failures that share an exit status but not a code.
func GeneralCode(code string) string {
	switch ExitCode(code) {
	case ExitValidation:
		return ErrValidation
	case ExitNotFound:
		return ErrNotFound
	case ExitConflict:
		return ErrConflict
	case ExitIO:
		return ErrFileError
	case ExitError:
		return ErrUncategorized
	default:
		// Unreachable while ExitCode returns only the Exit* constants.
		return ErrUncategorized
	}
}

// Response is the standard JSON response envelope.
type Response struct {
	Success bool       `json:"success"`
	Nib     *nib.Nib   `json:"nib,omitempty"`
	Nibs    []*nib.Nib `json:"nibs,omitempty"`
	Count   int        `json:"count,omitempty"`
	Message string     `json:"message,omitempty"`
	Path    string     `json:"path,omitempty"`
}

// Response has no Warnings field: warnings travel on the owning command's result
// type (see nibcontext.Summary.Warnings).

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

// SuccessMultiple outputs a nib array with no envelope.
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

// errorEnvelope is the --json error shape:
//
//	{"error": {"code": "<CODE>", "message": "<msg>"}}
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// AllowedParentTypes is set only for HIERARCHY.
	AllowedParentTypes []string `json:"allowedParentTypes,omitempty"`
	// CurrentEtag is set only for a CONFLICT that one token reconciles; when it is
	// absent, read the message before concluding a retry is impossible. The token
	// belongs to one nib. In a bulk reorder the "failed to reorder <id>: " prefix
	// names it, and the other listed nibs may be stale or already written.
	CurrentEtag string `json:"currentEtag,omitempty"`
	// Occurrences is set only for TEXT_NOT_FOUND and TEXT_AMBIGUOUS; it is a pointer
	// so a count of 0 is emitted.
	Occurrences *int `json:"occurrences,omitempty"`
}

// Error writes the --json error envelope to stdout and returns a reported
// CodedError whose Error() is message. It may be wrapped with %w.
func Error(code string, message string) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(errorEnvelope{Error: errorBody{Code: code, Message: message}})
	return &CodedError{Code: code, Msg: message, Reported: true}
}

// ErrorHierarchy is Error for ErrHierarchy, adding allowedParentTypes.
func ErrorHierarchy(message string, allowedParentTypes []string) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(errorEnvelope{Error: errorBody{
		Code:               ErrHierarchy,
		Message:            message,
		AllowedParentTypes: allowedParentTypes,
	}})
	return &CodedError{Code: ErrHierarchy, Msg: message, Reported: true}
}

// ErrorConflict is Error for ErrConflict, adding currentEtag when it is not empty
// (see errorBody.CurrentEtag).
func ErrorConflict(message, currentEtag string) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(errorEnvelope{Error: errorBody{
		Code:        ErrConflict,
		Message:     message,
		CurrentEtag: currentEtag,
	}})
	return &CodedError{Code: ErrConflict, Msg: message, Reported: true}
}

// ErrorText is Error for ErrTextNotFound or ErrTextAmbiguous, adding occurrences.
func ErrorText(code, message string, occurrences int) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(errorEnvelope{Error: errorBody{
		Code:        code,
		Message:     message,
		Occurrences: &occurrences,
	}})
	return &CodedError{Code: code, Msg: message, Reported: true}
}

// TextError writes `nibs get`'s text-mode error line, "error <CODE>: <message>", to
// stdout and returns a reported CodedError.
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
