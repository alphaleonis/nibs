package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

// stdoutMu serializes os.Stdout swaps within this test file. Tests in
// internal/output capture stdout because Error/JSON write directly to the
// process stdout (bypassing any injected writer). The mutex protects
// against accidental t.Parallel() introductions.
var stdoutMu sync.Mutex

// captureStdout captures writes made directly to os.Stdout while fn runs.
// Mirrors the cmd-package helper of the same name.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	stdoutMu.Lock()
	defer stdoutMu.Unlock()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	closed := false
	defer func() {
		if !closed {
			_ = w.Close()
		}
	}()
	fn()
	_ = w.Close()
	closed = true

	select {
	case s := <-done:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("captureStdout timed out")
		return ""
	}
}

// TestError_EnvelopeAndSentinel pins the --json error contract:
//   - Error writes {"error":{"code","message"}} to stdout — a single
//     top-level "error" object, no success/data wrapper.
//   - The returned error satisfies errors.Is(err, ErrAlreadyReported) so the
//     CLI boundary can suppress the duplicate "Error: ..." line on stderr.
//   - The returned error's .Error() is the original message — the code and
//     sentinel do not leak into user-visible text.
func TestError_EnvelopeAndSentinel(t *testing.T) {
	var got error
	out := captureStdout(t, func() {
		got = Error(ErrValidation, "boom")
	})
	if got == nil {
		t.Fatal("Error(...) returned nil; expected non-nil coded error")
	}
	if !errors.Is(got, ErrAlreadyReported) {
		t.Errorf("errors.Is(err, ErrAlreadyReported) = false, want true; err = %v", got)
	}
	if got.Error() != "boom" {
		t.Errorf("err.Error() = %q, want %q (no code/sentinel leakage)", got.Error(), "boom")
	}
	// No top-level success/code/error keys — only a nested "error" object.
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("unmarshal envelope: %v\nraw: %s", err, out)
	}
	for _, k := range []string{"success", "code", "data"} {
		if _, ok := raw[k]; ok {
			t.Errorf("envelope must not carry top-level %q key; raw: %s", k, out)
		}
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nraw: %s", err, out)
	}
	if env.Error.Code != ErrValidation {
		t.Errorf("env.error.code = %q, want %q", env.Error.Code, ErrValidation)
	}
	if env.Error.Message != "boom" {
		t.Errorf("env.error.message = %q, want %q", env.Error.Message, "boom")
	}
}

// TestTextError pins get's single-stream text error contract: "error <CODE>:
// <message>" on stdout, returning a reported CodedError carrying the code.
func TestTextError(t *testing.T) {
	var got error
	out := captureStdout(t, func() {
		got = TextError(ErrNotFound, "nib not found: x1")
	})
	if !errors.Is(got, ErrAlreadyReported) {
		t.Errorf("TextError result must satisfy ErrAlreadyReported; got %v", got)
	}
	var ce *CodedError
	if !errors.As(got, &ce) || ce.Code != ErrNotFound {
		t.Fatalf("TextError result = %v, want *CodedError with code %q", got, ErrNotFound)
	}
	if out != "error NOT_FOUND: nib not found: x1\n" {
		t.Errorf("stdout = %q, want %q", out, "error NOT_FOUND: nib not found: x1\n")
	}
}

// TestExitCode pins the single source of truth for CLI exit statuses: each
// structured error CODE maps to a stable process exit code, unknown codes
// collapse to ExitError.
func TestExitCode(t *testing.T) {
	tests := []struct {
		code string
		want int
	}{
		{ErrNotFound, ExitNotFound},        // 3
		{ErrValidation, ExitValidation},    // 2
		{ErrInvalidStatus, ExitValidation}, // 2
		{ErrConflict, ExitConflict},        // 4
		{ErrFileError, ExitIO},             // 5
		{ErrNoNibsDir, ExitIO},             // 5
		{"SOMETHING_ELSE", ExitError},      // 1
		{"", ExitError},                    // 1
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			if got := ExitCode(tt.code); got != tt.want {
				t.Errorf("ExitCode(%q) = %d, want %d", tt.code, got, tt.want)
			}
		})
	}
}
