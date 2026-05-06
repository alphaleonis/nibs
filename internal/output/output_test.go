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

// TestError_EnvelopeAndSentinel pins the contract:
//   - Error writes a JSON error envelope to stdout (Success=false, Code,
//     Error fields populated).
//   - The returned error satisfies errors.Is(err, ErrAlreadyReported) so the
//     CLI boundary can suppress the duplicate "Error: ..." line on stderr.
//   - The returned error's .Error() is the original message — the sentinel
//     name does not leak into user-visible text.
func TestError_EnvelopeAndSentinel(t *testing.T) {
	var got error
	out := captureStdout(t, func() {
		got = Error(ErrValidation, "boom")
	})
	if got == nil {
		t.Fatal("Error(...) returned nil; expected non-nil sentinel-bearing error")
	}
	if !errors.Is(got, ErrAlreadyReported) {
		t.Errorf("errors.Is(err, ErrAlreadyReported) = false, want true; err = %v", got)
	}
	if got.Error() != "boom" {
		t.Errorf("err.Error() = %q, want %q (no sentinel leakage)", got.Error(), "boom")
	}
	var env struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nraw: %s", err, out)
	}
	if env.Success {
		t.Errorf("env.Success = true, want false")
	}
	if env.Code != ErrValidation {
		t.Errorf("env.Code = %q, want %q", env.Code, ErrValidation)
	}
	if env.Error != "boom" {
		t.Errorf("env.Error = %q, want %q", env.Error, "boom")
	}
}
