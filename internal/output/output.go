package output

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/alphaleonis/nibs/internal/nib"
)

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
func Error(code string, message string) error {
	_ = JSON(Response{
		Success: false,
		Error:   message,
		Code:    code,
	})
	return fmt.Errorf("%s", message)
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
