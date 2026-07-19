package cmd

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
)

// TestReportErr_TextMode verifies that in non-JSON mode reportErr returns a
// non-reported CodedError carrying the code and message (so the CLI boundary
// maps the exit status and prints "Error: <msg>" to stderr) and writes NO
// JSON envelope to stdout.
func TestReportErr_TextMode(t *testing.T) {
	origErr := errors.New("boom")
	var got error
	out := captureStdout(t, func() {
		got = reportErr(false, output.ErrValidation, origErr)
	})
	var ce *output.CodedError
	if !errors.As(got, &ce) {
		t.Fatalf("reportErr(false, ...) = %v, want *output.CodedError", got)
	}
	if ce.Reported {
		t.Errorf("text-mode CodedError.Reported = true, want false (boundary owns the stderr print)")
	}
	if ce.Code != output.ErrValidation {
		t.Errorf("CodedError.Code = %q, want %q", ce.Code, output.ErrValidation)
	}
	if ce.Msg != "boom" {
		t.Errorf("CodedError.Msg = %q, want %q", ce.Msg, "boom")
	}
	if errors.Is(got, output.ErrAlreadyReported) {
		t.Error("text-mode err must NOT satisfy ErrAlreadyReported (nothing written to stdout)")
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected NO stdout output in text mode, got: %q", out)
	}
}

// TestReportErr_JSONMode verifies that in JSON mode reportErr writes the
// {error:{code,message}} contract to stdout and returns a reported
// CodedError. This is the single-source-of-truth for the dual-path error
// convention used by every --json-aware command in cmd/.
func TestReportErr_JSONMode(t *testing.T) {
	origErr := errors.New("bad things happened")
	var got error
	out := captureStdout(t, func() {
		got = reportErr(true, output.ErrValidation, origErr)
	})
	if got == nil {
		t.Fatal("reportErr(true, ...) returned nil; expected non-nil coded error")
	}
	if !errors.Is(got, output.ErrAlreadyReported) {
		t.Errorf("JSON-mode err must satisfy ErrAlreadyReported; got %v", got)
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
	if env.Error.Code != output.ErrValidation {
		t.Errorf("envelope error.code = %q, want %q", env.Error.Code, output.ErrValidation)
	}
	if env.Error.Message != "bad things happened" {
		t.Errorf("envelope error.message = %q, want %q", env.Error.Message, "bad things happened")
	}
}

// TestReportErr_JSONMode_DifferentCodes verifies that reportErr propagates
// the caller-provided code unchanged. Every callsite in refs/show picks a
// code from output.Err* based on the error category — the helper must not
// coerce them.
func TestReportErr_JSONMode_DifferentCodes(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{"validation", output.ErrValidation},
		{"not-found", output.ErrNotFound},
		{"conflict", output.ErrConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() {
				_ = reportErr(true, tt.code, errors.New("x"))
			})
			var env struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(out), &env); err != nil {
				t.Fatalf("unmarshal: %v\nraw: %s", err, out)
			}
			if env.Error.Code != tt.code {
				t.Errorf("envelope error.code = %q, want %q", env.Error.Code, tt.code)
			}
		})
	}
}
