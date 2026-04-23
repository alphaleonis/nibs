package cmd

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
)

// TestReportErr_TextMode verifies that in non-JSON mode reportErr passes the
// original error through unchanged so Cobra's default text-error printing
// takes over and no JSON envelope is written.
func TestReportErr_TextMode(t *testing.T) {
	origErr := errors.New("boom")
	out := captureStdout(t, func() {
		got := reportErr(false, output.ErrValidation, origErr)
		if got != origErr {
			t.Errorf("reportErr(false, ...) = %v, want identity pass-through of %v", got, origErr)
		}
	})
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected NO stdout output in text mode, got: %q", out)
	}
}

// TestReportErr_JSONMode verifies that in JSON mode reportErr writes a JSON
// error envelope with the given code and the error message. This is the
// single-source-of-truth for the dual-path error convention used by every
// --json-aware command in cmd/.
func TestReportErr_JSONMode(t *testing.T) {
	origErr := errors.New("bad things happened")
	var got error
	out := captureStdout(t, func() {
		got = reportErr(true, output.ErrValidation, origErr)
	})
	if got == nil {
		t.Fatal("reportErr(true, ...) returned nil; expected non-nil sentinel")
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
		t.Errorf("envelope.Success = true, want false")
	}
	if env.Code != output.ErrValidation {
		t.Errorf("envelope.Code = %q, want %q", env.Code, output.ErrValidation)
	}
	if env.Error != "bad things happened" {
		t.Errorf("envelope.Error = %q, want %q", env.Error, "bad things happened")
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
				Code string `json:"code"`
			}
			if err := json.Unmarshal([]byte(out), &env); err != nil {
				t.Fatalf("unmarshal: %v\nraw: %s", err, out)
			}
			if env.Code != tt.code {
				t.Errorf("envelope.Code = %q, want %q", env.Code, tt.code)
			}
		})
	}
}
