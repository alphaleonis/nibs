package nib

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// scalarExtraNode builds a plain-scalar yaml.Node for constructing Nib.Extra
// values directly in tests (mirrors what Parse captures for an unknown key).
func scalarExtraNode(value string) yaml.Node {
	return yaml.Node{Kind: yaml.ScalarNode, Value: value}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedTitle  string
		expectedStatus string
		expectedBody   string
		wantErr        bool
	}{
		{
			name: "basic nib",
			input: `---
title: Test Nib
status: todo
---

This is the body.`,
			expectedTitle:  "Test Nib",
			expectedStatus: "todo",
			expectedBody:   "\nThis is the body.",
		},
		{
			name: "with timestamps",
			input: `---
title: With Times
status: in-progress
created_at: 2024-01-15T10:30:00Z
updated_at: 2024-01-16T14:45:00Z
---

Body content here.`,
			expectedTitle:  "With Times",
			expectedStatus: "in-progress",
			expectedBody:   "\nBody content here.",
		},
		{
			name: "empty body",
			input: `---
title: No Body
status: completed
---`,
			expectedTitle:  "No Body",
			expectedStatus: "completed",
			expectedBody:   "",
		},
		{
			name: "multiline body",
			input: `---
title: Multi Line
status: todo
---

# Header

- Item 1
- Item 2

Paragraph text.`,
			expectedTitle:  "Multi Line",
			expectedStatus: "todo",
			expectedBody:   "\n# Header\n\n- Item 1\n- Item 2\n\nParagraph text.",
		},
		{
			name:           "plain text without frontmatter",
			input:          `Just plain text without any YAML frontmatter.`,
			expectedTitle:  "",
			expectedStatus: "",
			expectedBody:   "Just plain text without any YAML frontmatter.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nib, err := Parse(strings.NewReader(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if nib.Title != tt.expectedTitle {
				t.Errorf("Title = %q, want %q", nib.Title, tt.expectedTitle)
			}
			if nib.Status != tt.expectedStatus {
				t.Errorf("Status = %q, want %q", nib.Status, tt.expectedStatus)
			}
			if nib.Body != tt.expectedBody {
				t.Errorf("Body = %q, want %q", nib.Body, tt.expectedBody)
			}
		})
	}
}

func TestParseWithType(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedType string
	}{
		{
			name: "with type field",
			input: `---
title: Bug Report
status: todo
type: bug
---

Description of the bug.`,
			expectedType: "bug",
		},
		{
			name: "without type field",
			input: `---
title: No Type
status: todo
---

No type specified.`,
			expectedType: "",
		},
		{
			// Backwards compatibility: nibs with types not in current config
			// should still be readable without error
			name: "with unknown type (backwards compatibility)",
			input: `---
title: Legacy Nib
status: todo
type: deprecated-type-no-longer-in-config
---`,
			expectedType: "deprecated-type-no-longer-in-config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nib, err := Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if nib.Type != tt.expectedType {
				t.Errorf("Type = %q, want %q", nib.Type, tt.expectedType)
			}
		})
	}
}

func TestParseWithPriority(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		expectedPriority string
	}{
		{
			name: "with priority field",
			input: `---
title: Urgent Bug
status: todo
type: bug
priority: critical
---

Fix this immediately.`,
			expectedPriority: "critical",
		},
		{
			name: "without priority field",
			input: `---
title: Normal Task
status: todo
---

No priority specified.`,
			expectedPriority: "",
		},
		{
			name: "with high priority",
			input: `---
title: Important Feature
status: in-progress
priority: high
---`,
			expectedPriority: "high",
		},
		{
			// Migration: "deferred" was removed as a priority (it is now a
			// status). A legacy file carrying priority: deferred must load
			// without error, normalized to "low".
			name: "deferred priority migrates to low",
			input: `---
title: Later Task
status: draft
priority: deferred
---`,
			expectedPriority: "low",
		},
		{
			// Pins the migration to the single literal "deferred": any other
			// (even invalid) priority must pass through Parse unmodified, so a
			// future edit can't silently widen the normalization.
			name: "non-deferred invalid priority passes through unchanged",
			input: `---
title: Odd Task
status: todo
priority: urgent
---`,
			expectedPriority: "urgent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nib, err := Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if nib.Priority != tt.expectedPriority {
				t.Errorf("Priority = %q, want %q", nib.Priority, tt.expectedPriority)
			}
		})
	}
}

func TestRenderWithPriority(t *testing.T) {
	tests := []struct {
		name     string
		nib      *Nib
		contains []string
	}{
		{
			name: "with priority",
			nib: &Nib{
				Title:    "High Priority",
				Status:   "todo",
				Priority: "high",
			},
			contains: []string{
				"title: High Priority",
				"status: todo",
				"priority: high",
			},
		},
		{
			name: "without priority",
			nib: &Nib{
				Title:  "No Priority",
				Status: "todo",
			},
			contains: []string{
				"title: No Priority",
				"status: todo",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered, err := tt.nib.Render()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			content := string(rendered)
			for _, want := range tt.contains {
				if !strings.Contains(content, want) {
					t.Errorf("Render() missing %q in:\n%s", want, content)
				}
			}

			// Verify priority is NOT in output when empty
			if tt.nib.Priority == "" && strings.Contains(content, "priority:") {
				t.Errorf("Render() should not contain 'priority:' when priority is empty:\n%s", content)
			}
		})
	}
}

func TestPriorityRoundtrip(t *testing.T) {
	priorities := []string{"critical", "high", "normal", "low", ""}

	for _, priority := range priorities {
		t.Run(priority, func(t *testing.T) {
			original := &Nib{
				Title:    "Test Nib",
				Status:   "todo",
				Priority: priority,
			}

			rendered, err := original.Render()
			if err != nil {
				t.Fatalf("Render() error: %v", err)
			}

			parsed, err := Parse(strings.NewReader(string(rendered)))
			if err != nil {
				t.Fatalf("Parse() error: %v", err)
			}

			if parsed.Priority != original.Priority {
				t.Errorf("Priority roundtrip failed: got %q, want %q", parsed.Priority, original.Priority)
			}
		})
	}
}

func TestParseWithEstimate(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		expectedEstimate string
	}{
		{
			name: "with estimate s",
			input: `---
title: Small Task
status: todo
estimate: s
---

A small task.`,
			expectedEstimate: "s",
		},
		{
			name: "with estimate xl",
			input: `---
title: Huge Task
status: todo
estimate: xl
---`,
			expectedEstimate: "xl",
		},
		{
			name: "without estimate",
			input: `---
title: No Estimate
status: todo
---

No estimate specified.`,
			expectedEstimate: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nib, err := Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if nib.Estimate != tt.expectedEstimate {
				t.Errorf("Estimate = %q, want %q", nib.Estimate, tt.expectedEstimate)
			}
		})
	}
}

func TestRenderWithEstimate(t *testing.T) {
	tests := []struct {
		name     string
		nib      *Nib
		contains []string
	}{
		{
			name: "with estimate",
			nib: &Nib{
				Title:    "Estimated Task",
				Status:   "todo",
				Estimate: "l",
			},
			contains: []string{
				"title: Estimated Task",
				"estimate: l",
			},
		},
		{
			name: "without estimate",
			nib: &Nib{
				Title:  "No Estimate",
				Status: "todo",
			},
			contains: []string{
				"title: No Estimate",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered, err := tt.nib.Render()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			content := string(rendered)
			for _, want := range tt.contains {
				if !strings.Contains(content, want) {
					t.Errorf("Render() missing %q in:\n%s", want, content)
				}
			}

			if tt.nib.Estimate == "" && strings.Contains(content, "estimate:") {
				t.Errorf("Render() should not contain 'estimate:' when estimate is empty:\n%s", content)
			}
		})
	}
}

func TestEstimateRoundtrip(t *testing.T) {
	estimates := []string{"s", "m", "l", "xl", ""}

	for _, estimate := range estimates {
		name := estimate
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			original := &Nib{
				Title:    "Test Nib",
				Status:   "todo",
				Estimate: estimate,
			}

			rendered, err := original.Render()
			if err != nil {
				t.Fatalf("Render() error: %v", err)
			}

			parsed, err := Parse(strings.NewReader(string(rendered)))
			if err != nil {
				t.Fatalf("Parse() error: %v", err)
			}

			if parsed.Estimate != original.Estimate {
				t.Errorf("Estimate roundtrip failed: got %q, want %q", parsed.Estimate, original.Estimate)
			}
		})
	}
}

func TestRender(t *testing.T) {
	now := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		nib      *Nib
		contains []string
	}{
		{
			name: "basic nib",
			nib: &Nib{
				Title:  "Test Nib",
				Status: "todo",
			},
			contains: []string{
				"---",
				"title: Test Nib",
				"status: todo",
			},
		},
		{
			name: "with body",
			nib: &Nib{
				Title:  "With Body",
				Status: "completed",
				Body:   "This is content.",
			},
			contains: []string{
				"title: With Body",
				"status: completed",
				"This is content.",
			},
		},
		{
			name: "with timestamps",
			nib: &Nib{
				Title:     "Timed",
				Status:    "todo",
				CreatedAt: &now,
				UpdatedAt: &now,
			},
			contains: []string{
				"title: Timed",
				"created_at:",
				"updated_at:",
			},
		},
		{
			name: "with type",
			nib: &Nib{
				Title:  "Typed Nib",
				Status: "todo",
				Type:   "bug",
			},
			contains: []string{
				"title: Typed Nib",
				"status: todo",
				"type: bug",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := tt.nib.Render()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			result := string(output)
			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf("output missing %q\ngot:\n%s", want, result)
				}
			}
		})
	}
}

func TestParseRenderRoundtrip(t *testing.T) {
	now := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	later := time.Date(2024, 1, 16, 14, 45, 0, 0, time.UTC)

	tests := []struct {
		name string
		nib  *Nib
	}{
		{
			name: "basic",
			nib: &Nib{
				Title:  "Basic Nib",
				Status: "todo",
			},
		},
		{
			name: "with body",
			nib: &Nib{
				Title:  "Nib With Body",
				Status: "in-progress",
				Body:   "This is the body content.\n\nWith multiple paragraphs.",
			},
		},
		{
			name: "with timestamps",
			nib: &Nib{
				Title:     "Timestamped Nib",
				Status:    "completed",
				CreatedAt: &now,
				UpdatedAt: &later,
				Body:      "Some content.",
			},
		},
		{
			name: "with type",
			nib: &Nib{
				Title:  "Typed Nib",
				Status: "todo",
				Type:   "bug",
				Body:   "Bug description.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Render to bytes
			rendered, err := tt.nib.Render()
			if err != nil {
				t.Fatalf("Render error: %v", err)
			}

			// Parse back
			parsed, err := Parse(strings.NewReader(string(rendered)))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			// Compare fields
			if parsed.Title != tt.nib.Title {
				t.Errorf("Title roundtrip: got %q, want %q", parsed.Title, tt.nib.Title)
			}
			if parsed.Status != tt.nib.Status {
				t.Errorf("Status roundtrip: got %q, want %q", parsed.Status, tt.nib.Status)
			}
			if parsed.Type != tt.nib.Type {
				t.Errorf("Type roundtrip: got %q, want %q", parsed.Type, tt.nib.Type)
			}

			// Body comparison (parse adds newline prefix for non-empty body)
			wantBody := tt.nib.Body
			if wantBody != "" {
				wantBody = "\n" + wantBody
			}
			if parsed.Body != wantBody {
				t.Errorf("Body roundtrip: got %q, want %q", parsed.Body, wantBody)
			}

			// Timestamp comparison
			if tt.nib.CreatedAt != nil {
				if parsed.CreatedAt == nil {
					t.Error("CreatedAt: got nil, want non-nil")
				} else if !parsed.CreatedAt.Equal(*tt.nib.CreatedAt) {
					t.Errorf("CreatedAt: got %v, want %v", parsed.CreatedAt, tt.nib.CreatedAt)
				}
			}
			if tt.nib.UpdatedAt != nil {
				if parsed.UpdatedAt == nil {
					t.Error("UpdatedAt: got nil, want non-nil")
				} else if !parsed.UpdatedAt.Equal(*tt.nib.UpdatedAt) {
					t.Errorf("UpdatedAt: got %v, want %v", parsed.UpdatedAt, tt.nib.UpdatedAt)
				}
			}
		})
	}
}

func TestNibJSONSerialization(t *testing.T) {
	t.Run("body omitted when empty", func(t *testing.T) {
		nib := &Nib{
			ID:     "test-123",
			Title:  "Test Nib",
			Status: "todo",
			Body:   "",
		}

		data, err := json.Marshal(nib)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		jsonStr := string(data)
		if strings.Contains(jsonStr, `"body"`) {
			t.Errorf("JSON should not contain 'body' field when empty, got: %s", jsonStr)
		}
	})

	t.Run("body included when non-empty", func(t *testing.T) {
		nib := &Nib{
			ID:     "test-123",
			Title:  "Test Nib",
			Status: "todo",
			Body:   "This is the body content.",
		}

		data, err := json.Marshal(nib)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		jsonStr := string(data)
		if !strings.Contains(jsonStr, `"body":"This is the body content."`) {
			t.Errorf("JSON should contain 'body' field with content, got: %s", jsonStr)
		}
	})
}

func TestParseWithParentAndBlocking(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		expectedParent   string
		expectedBlocking []string
	}{
		{
			name: "with parent only",
			input: `---
title: Test
status: todo
parent: xyz789
---`,
			expectedParent:   "xyz789",
			expectedBlocking: nil,
		},
		{
			name: "with blocking only",
			input: `---
title: Test
status: todo
blocking:
  - abc123
  - def456
---`,
			expectedParent:   "",
			expectedBlocking: []string{"abc123", "def456"},
		},
		{
			name: "with parent and blocking",
			input: `---
title: Test
status: todo
parent: xyz789
blocking:
  - abc123
---`,
			expectedParent:   "xyz789",
			expectedBlocking: []string{"abc123"},
		},
		{
			name: "no relationships",
			input: `---
title: Test
status: todo
---`,
			expectedParent:   "",
			expectedBlocking: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nib, err := Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if nib.Parent != tt.expectedParent {
				t.Errorf("Parent = %q, want %q", nib.Parent, tt.expectedParent)
			}

			if len(tt.expectedBlocking) == 0 && len(nib.Blocking) == 0 {
				return // Both empty, OK
			}

			if len(nib.Blocking) != len(tt.expectedBlocking) {
				t.Errorf("Blocking count = %d, want %d", len(nib.Blocking), len(tt.expectedBlocking))
				return
			}

			for i, expected := range tt.expectedBlocking {
				if nib.Blocking[i] != expected {
					t.Errorf("Blocking[%d] = %q, want %q", i, nib.Blocking[i], expected)
				}
			}
		})
	}
}

func TestRenderWithParentAndBlocking(t *testing.T) {
	tests := []struct {
		name        string
		nib         *Nib
		contains    []string
		notContains []string
	}{
		{
			name: "with parent only",
			nib: &Nib{
				Title:  "Test Nib",
				Status: "todo",
				Parent: "xyz789",
			},
			contains: []string{
				"parent: xyz789",
			},
		},
		{
			// The legacy v0 blocking field IS now rendered (omitempty) so a v0
			// file's on-disk `blocking:` content round-trips and stays visible to
			// the canonical etag. v1+ nibs never set Blocking, so it stays absent
			// for them (see TestRenderNoChurnWithoutExtraOrBlocking).
			name: "legacy blocking field rendered when set",
			nib: &Nib{
				Title:    "Test Nib",
				Status:   "todo",
				Blocking: []string{"abc123", "def456"},
			},
			contains: []string{
				"blocking:",
				"abc123",
				"def456",
			},
		},
		{
			name: "parent and legacy blocking both rendered when set",
			nib: &Nib{
				Title:    "Test Nib",
				Status:   "todo",
				Parent:   "xyz789",
				Blocking: []string{"abc123"},
			},
			contains: []string{
				"parent: xyz789",
				"blocking:",
				"abc123",
			},
		},
		{
			name: "without relationships",
			nib: &Nib{
				Title:  "Test Nib",
				Status: "todo",
			},
			contains: []string{
				"title: Test Nib",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := tt.nib.Render()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			result := string(output)
			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf("output missing %q\ngot:\n%s", want, result)
				}
			}
			for _, unwanted := range tt.notContains {
				if strings.Contains(result, unwanted) {
					t.Errorf("output should not contain %q\ngot:\n%s", unwanted, result)
				}
			}

			// Check that empty parent doesn't appear in output
			if tt.nib.Parent == "" && strings.Contains(result, "parent:") {
				t.Errorf("output should not contain 'parent:' when no parent\ngot:\n%s", result)
			}
		})
	}
}

func TestParentRoundtrip(t *testing.T) {
	tests := []struct {
		name   string
		parent string
	}{
		{name: "with parent", parent: "xyz789"},
		{name: "without parent", parent: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := &Nib{
				Title:  "Test",
				Status: "todo",
				Parent: tt.parent,
			}

			rendered, err := original.Render()
			if err != nil {
				t.Fatalf("Render error: %v", err)
			}

			parsed, err := Parse(strings.NewReader(string(rendered)))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			if parsed.Parent != tt.parent {
				t.Errorf("Parent: got %q, want %q", parsed.Parent, tt.parent)
			}
		})
	}
}

func TestBlockingRoundtrip(t *testing.T) {
	// The legacy v0 `blocking:` field now survives a Render->Parse round-trip
	// (emitted with omitempty). This keeps the canonical render — and thus the
	// stored etag (nibcore.computeStoredETag) — a faithful witness of a v0 file's
	// on-disk `blocking:` content instead of silently stripping it. Normal v1+
	// nibs never set Blocking (it is computed at query time from other nibs'
	// blockedBy), so omitempty keeps it absent for them; see the "no churn"
	// coverage in TestRenderNoChurnWithoutExtraOrBlocking.
	original := &Nib{
		Title:    "Test",
		Status:   "todo",
		Blocking: []string{"abc123", "def456"},
	}

	rendered, err := original.Render()
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if !strings.Contains(string(rendered), "blocking:") {
		t.Errorf("Render should emit the blocking field, got:\n%s", rendered)
	}

	parsed, err := Parse(strings.NewReader(string(rendered)))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if len(parsed.Blocking) != 2 || parsed.Blocking[0] != "abc123" || parsed.Blocking[1] != "def456" {
		t.Errorf("Blocking should survive round-trip, got %v", parsed.Blocking)
	}
}

// TestUnknownKeysRoundtrip verifies that front-matter keys not modeled by any
// named field survive a Parse->Render->Parse cycle, so an external
// edit confined to such a key is visible to the canonical etag rather than being
// silently stripped.
func TestUnknownKeysRoundtrip(t *testing.T) {
	const input = `---
version: 1
title: With Extras
status: todo
type: task
priority: normal
assignee: bob
sprint: 2026-Q1
---

Body text.
`
	parsed, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if got := parsed.Extra["assignee"].Value; got != "bob" {
		t.Errorf("Extra[assignee] = %v, want bob", got)
	}
	if _, ok := parsed.Extra["sprint"]; !ok {
		t.Errorf("Extra missing 'sprint' key: %v", parsed.Extra)
	}

	rendered, err := parsed.Render()
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if !strings.Contains(string(rendered), "assignee: bob") {
		t.Errorf("Render dropped unknown key 'assignee':\n%s", rendered)
	}

	reparsed, err := Parse(strings.NewReader(string(rendered)))
	if err != nil {
		t.Fatalf("re-Parse error: %v", err)
	}
	if reparsed.Extra["assignee"].Value != "bob" {
		t.Errorf("unknown key lost on re-parse: %v", reparsed.Extra)
	}
}

// TestExtraBoolLikeScalarPreserved pins bool-like scalar fidelity: a
// YAML-1.1 bool-like unknown scalar (`y` — the "Norway problem") is captured as a
// raw yaml.v3 string node and re-emitted verbatim as `y`, NOT coerced to a Go
// bool and re-rendered as `true`. yaml.v3 resolves `y` as a string under the YAML
// 1.2 core schema, and the raw node preserves it exactly.
func TestExtraBoolLikeScalarPreserved(t *testing.T) {
	const input = `---
version: 1
title: Norway
status: todo
reviewed: y
---
`
	parsed, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	// The bare `y` is preserved as its original scalar text, not coerced.
	node := parsed.Extra["reviewed"]
	if node.Value != "y" {
		t.Fatalf("Extra[reviewed].Value = %q, want %q (must not coerce)", node.Value, "y")
	}
	if node.Tag != "!!str" {
		t.Errorf("Extra[reviewed].Tag = %q, want !!str (bool-like scalar must stay a string)", node.Tag)
	}

	rendered, err := parsed.Render()
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if !strings.Contains(string(rendered), "reviewed: y") {
		t.Errorf("expected `reviewed: y` to round-trip verbatim, got:\n%s", rendered)
	}
	if strings.Contains(string(rendered), "reviewed: true") {
		t.Errorf("`reviewed: y` was coerced to `reviewed: true`:\n%s", rendered)
	}
}

// TestFrontMatterRenderProjectionSymmetry enforces the load-bearing invariant
// that frontMatter (parse) and renderFrontMatter (render) model the identical
// yaml-key set. If they drift (a key modeled on one side but not the other), a
// pre-existing on-disk file can parse that key into Extra and then collide with
// the modeled render field — historically a yaml.v3 panic. Pinning the two key
// sets here makes a one-sided struct edit fail CI with a clear message.
func TestFrontMatterRenderProjectionSymmetry(t *testing.T) {
	parse := yamlKeyNames(reflect.TypeOf(frontMatter{}))
	render := yamlKeyNames(reflect.TypeOf(renderFrontMatter{}))

	for name := range parse {
		if _, ok := render[name]; !ok {
			t.Errorf("yaml key %q is modeled by frontMatter (parse) but not renderFrontMatter (render); the projections must stay symmetric", name)
		}
	}
	for name := range render {
		if _, ok := parse[name]; !ok {
			t.Errorf("yaml key %q is modeled by renderFrontMatter (render) but not frontMatter (parse); the projections must stay symmetric", name)
		}
	}

	// modeledRenderTags (used by Render's collision drop) must match the render
	// struct's named keys exactly — the inline Extra key ("") is excluded.
	delete(render, "")
	if !reflect.DeepEqual(render, modeledRenderTags) {
		t.Errorf("modeledRenderTags = %v, want the named render keys %v", modeledRenderTags, render)
	}
}

// yamlKeyNames returns the set of yaml key names (the part before the first
// comma) for every field of a struct type, including the empty name of a
// ,inline catch-all field.
func yamlKeyNames(t reflect.Type) map[string]struct{} {
	names := make(map[string]struct{}, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		names[name] = struct{}{}
	}
	return names
}

// TestRenderExtraKeyCollisionDoesNotPanic covers an Extra key that
// collides with a modeled field name (reachable via the "promote an unknown key
// to a modeled field" evolution path) must NOT panic Render/ETag. Render drops
// the colliding Extra key (the modeled field wins) and honors its (,error)
// contract, keeping ETag's "0000000000000000" sentinel reachable on real errors.
func TestRenderExtraKeyCollisionDoesNotPanic(t *testing.T) {
	b := &Nib{
		Title:  "Collision",
		Status: "todo",
		Extra: map[string]yaml.Node{
			"status":   scalarExtraNode("SHOULD-BE-DROPPED"), // collides with modeled Status
			"assignee": scalarExtraNode("bob"),               // genuinely unknown, must survive
		},
	}

	// Neither Render nor ETag may panic (the raw yaml.v3 inline-map panic used to
	// escape both).
	rendered, err := b.Render()
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	if etag := b.ETag(); etag == "0000000000000000" {
		t.Errorf("ETag() returned the render-failure sentinel; Render should have succeeded")
	}

	out := string(rendered)
	if !strings.Contains(out, "status: todo") {
		t.Errorf("modeled Status did not win over the colliding Extra key:\n%s", out)
	}
	if strings.Contains(out, "SHOULD-BE-DROPPED") {
		t.Errorf("colliding Extra key was not dropped:\n%s", out)
	}
	if !strings.Contains(out, "assignee: bob") {
		t.Errorf("non-colliding Extra key was lost:\n%s", out)
	}

	// The colliding key must not have mutated the caller's Extra map.
	if b.Extra["status"].Value != "SHOULD-BE-DROPPED" {
		t.Errorf("Render mutated the caller's Extra map: %v", b.Extra)
	}

	parsed, err := Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("re-Parse error: %v", err)
	}
	if parsed.Status != "todo" {
		t.Errorf("re-parsed Status = %q, want todo", parsed.Status)
	}
}

// TestRenderDeterministicWithExtra guards the etag against Go map-iteration
// nondeterminism: rendering the same nib repeatedly, and rendering
// a fresh clone, must produce byte-identical output despite several unknown keys.
func TestRenderDeterministicWithExtra(t *testing.T) {
	b := &Nib{
		Version:  1,
		Title:    "Deterministic",
		Status:   "todo",
		Type:     "task",
		Priority: "normal",
		Extra: map[string]yaml.Node{
			"zeta":     scalarExtraNode("z"),
			"alpha":    scalarExtraNode("a"),
			"mike":     scalarExtraNode("m"),
			"bravo":    scalarExtraNode("b"),
			"assignee": scalarExtraNode("bob"),
		},
	}
	first, err := b.Render()
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	for i := 0; i < 20; i++ {
		again, err := b.Render()
		if err != nil {
			t.Fatalf("Render error on iteration %d: %v", i, err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("Render not deterministic across unknown keys:\niter %d:\n%s\nfirst:\n%s", i, again, first)
		}
		if b.ETag() == "" || b.Clone().ETag() != b.ETag() {
			t.Fatalf("ETag not stable across Clone on iteration %d", i)
		}
	}
}

// TestRenderFixedPoint verifies parse->render->parse->render is a fixed point
// for a nib carrying unknown keys and a legacy blocking field, so
// the etag settles rather than oscillating.
func TestRenderFixedPoint(t *testing.T) {
	const input = `---
version: 1
title: Fixed Point
status: todo
type: task
priority: normal
assignee: bob
blocking:
  - abc123
custom: value
---

Body.
`
	p1, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	r1, err := p1.Render()
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	p2, err := Parse(strings.NewReader(string(r1)))
	if err != nil {
		t.Fatalf("re-Parse error: %v", err)
	}
	r2, err := p2.Render()
	if err != nil {
		t.Fatalf("re-Render error: %v", err)
	}
	if !bytes.Equal(r1, r2) {
		t.Errorf("render is not a fixed point:\nr1:\n%s\nr2:\n%s", r1, r2)
	}
}

// TestExtraPassthroughFixedPoint verifies that arbitrary unknown front-matter
// keys are a TRUE parse->render fixed point AND that their values are preserved
// verbatim (not re-inferred) — closing the whole yaml-1.1<->1.2 scalar-coercion
// class (bool-like coercion and signed-zero
// oscillation). Because Extra is captured as raw yaml.v3 nodes and re-emitted
// verbatim, no scalar-type re-inference happens on either the parse or the
// render side.
func TestExtraPassthroughFixedPoint(t *testing.T) {
	cases := []struct {
		name string
		// key/value lines injected into the front matter (already indented at col 0)
		fm string
		// substrings the FIRST render must contain (value preserved, not coerced)
		wantContains []string
		// substrings the render must NOT contain (evidence of coercion)
		wantAbsent []string
	}{
		{
			name:         "bool-like y stays a string (#3 Norway problem)",
			fm:           "reviewed: y",
			wantContains: []string{"reviewed: y"},
			wantAbsent:   []string{"reviewed: true"},
		},
		{
			name:         "bool-like no stays a string",
			fm:           "approved: no",
			wantContains: []string{"approved: no"},
			wantAbsent:   []string{"approved: false"},
		},
		{
			name:         "bool-like on stays a string",
			fm:           "draft: on",
			wantContains: []string{"draft: on"},
			wantAbsent:   []string{"draft: true"},
		},
		{
			name:         "signed-zero float stays -0.0 (#4 non-fixed-point)",
			fm:           "offset: -0.0",
			wantContains: []string{"offset: -0.0"},
			wantAbsent:   []string{"offset: 0\n", "offset: -0\n"},
		},
		{
			name:         "nested map preserved",
			fm:           "meta:\n  a: 1\n  b: yes",
			wantContains: []string{"b: yes"},
			wantAbsent:   []string{"b: true"},
		},
		{
			name:         "block sequence preserved",
			fm:           "labels:\n  - x\n  - y",
			wantContains: []string{"- y"},
			wantAbsent:   []string{"- true"},
		},
		{
			name:         "bool-like inside a flow sequence preserved",
			fm:           "flags: [x, y]",
			wantContains: []string{"flags: [x, y]"},
			wantAbsent:   []string{"true"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := "---\nversion: 1\ntitle: Passthrough\nstatus: todo\n" + tc.fm + "\n---\n\nBody.\n"

			p1, err := Parse(strings.NewReader(input))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			r1, err := p1.Render()
			if err != nil {
				t.Fatalf("Render error: %v", err)
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(string(r1), want) {
					t.Errorf("first render missing %q (value was coerced/lost):\n%s", want, r1)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(string(r1), absent) {
					t.Errorf("first render contains coerced form %q:\n%s", absent, r1)
				}
			}

			// parse->render->parse->render must be a byte-stable fixed point.
			p2, err := Parse(strings.NewReader(string(r1)))
			if err != nil {
				t.Fatalf("re-Parse error: %v", err)
			}
			r2, err := p2.Render()
			if err != nil {
				t.Fatalf("re-Render error: %v", err)
			}
			if !bytes.Equal(r1, r2) {
				t.Errorf("not a fixed point:\nr1:\n%s\nr2:\n%s", r1, r2)
			}

			// The etag derived from Render() must be stable across the round-trip,
			// so an ETag()-based if-match never self-conflicts with no writer.
			if p1.ETag() != p2.ETag() {
				t.Errorf("etag diverged across round-trip: %s != %s", p1.ETag(), p2.ETag())
			}
		})
	}
}

// TestExtraAnchorAliasResolvedToFixedPoint covers cross-boundary aliases: a YAML
// anchor on a MODELED field combined with an alias in an unmodeled (Extra) key
// parses fine, but if the alias survives into Render, yaml.Marshal emits a
// DANGLING alias (`*t`) — the modeled field decoded to a plain Go value dropped
// its anchor — which is invalid YAML and permanently corrupts the file. The fix
// resolves aliases to their concrete value at parse time, so Render emits valid
// YAML, the output re-parses, and the round-trip is a fixed point.
func TestExtraAnchorAliasResolvedToFixedPoint(t *testing.T) {
	cases := []struct {
		name string
		// full front matter body (between the --- fences)
		fm string
		// the Extra key whose value is an alias
		aliasKey string
		// substrings the FIRST render must contain (alias resolved to concrete value)
		wantContains []string
		// substrings the render must NOT contain (evidence of a surviving alias/anchor)
		wantAbsent []string
	}{
		{
			name: "scalar anchor on modeled title, alias in Extra",
			fm: `version: 1
title: &t Something
status: todo
mirror: *t`,
			aliasKey:     "mirror",
			wantContains: []string{"mirror: Something"},
			wantAbsent:   []string{"*t", "&t"},
		},
		{
			name: "sequence anchor on modeled tags, alias in Extra",
			fm: `version: 1
title: Anchored Tags
status: todo
tags: &t [a, b]
use: *t`,
			aliasKey:     "use",
			wantContains: []string{"a", "b"},
			wantAbsent:   []string{"*t", "&t"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := "---\n" + tc.fm + "\n---\n\nBody.\n"

			p1, err := Parse(strings.NewReader(input))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			// The captured Extra node must carry no alias/anchor state after parse.
			node := p1.Extra[tc.aliasKey]
			if node.Kind == yaml.AliasNode {
				t.Errorf("Extra[%q] is still an AliasNode after parse; alias must be resolved", tc.aliasKey)
			}
			if node.Anchor != "" {
				t.Errorf("Extra[%q].Anchor = %q, want empty (anchors must be cleared)", tc.aliasKey, node.Anchor)
			}

			r1, err := p1.Render()
			if err != nil {
				t.Fatalf("Render error: %v", err)
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(string(r1), want) {
					t.Errorf("render missing %q (alias not resolved):\n%s", want, r1)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(string(r1), absent) {
					t.Errorf("render still contains anchor/alias token %q (invalid/dangling YAML):\n%s", absent, r1)
				}
			}

			// The render must be valid YAML that re-parses without a dangling-alias
			// error.
			p2, err := Parse(strings.NewReader(string(r1)))
			if err != nil {
				t.Fatalf("re-Parse of rendered output failed (invalid YAML — dangling alias?): %v", err)
			}
			r2, err := p2.Render()
			if err != nil {
				t.Fatalf("re-Render error: %v", err)
			}
			if !bytes.Equal(r1, r2) {
				t.Errorf("render is not a fixed point:\nr1:\n%s\nr2:\n%s", r1, r2)
			}
			if p1.ETag() != p2.ETag() {
				t.Errorf("etag diverged across round-trip: %s != %s", p1.ETag(), p2.ETag())
			}
		})
	}
}

// TestRenderNoChurnWithoutExtraOrBlocking pins the "no churn for normal nibs"
// requirement: a nib with no unknown keys and no legacy blocking
// must render without any `blocking:` line or stray inline keys — i.e. the
// passthrough machinery is invisible unless it carries content.
func TestRenderNoChurnWithoutExtraOrBlocking(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	b := &Nib{
		Version:   1,
		Title:     "Normal",
		Status:    "todo",
		Type:      "task",
		Priority:  "normal",
		CreatedAt: &ts,
		UpdatedAt: &ts,
		Body:      "Body paragraph.",
	}
	rendered, err := b.Render()
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	got := string(rendered)
	if strings.Contains(got, "blocking:") {
		t.Errorf("normal nib rendered a 'blocking:' line:\n%s", got)
	}
	const want = `---
version: 1
title: Normal
status: todo
type: task
priority: normal
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Body paragraph.
`
	if got != want {
		t.Errorf("normal-nib render changed (churn):\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseWithBlockedBy(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		expectedBlockedBy []string
	}{
		{
			name: "with blocked_by",
			input: `---
title: Test
status: todo
blocked_by:
  - abc123
  - def456
---`,
			expectedBlockedBy: []string{"abc123", "def456"},
		},
		{
			name: "no blocked_by",
			input: `---
title: Test
status: todo
---`,
			expectedBlockedBy: nil,
		},
		{
			name: "with blocking and blocked_by",
			input: `---
title: Test
status: todo
blocking:
  - xyz789
blocked_by:
  - abc123
---`,
			expectedBlockedBy: []string{"abc123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nib, err := Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(tt.expectedBlockedBy) == 0 && len(nib.BlockedBy) == 0 {
				return // Both empty, OK
			}

			if len(nib.BlockedBy) != len(tt.expectedBlockedBy) {
				t.Errorf("BlockedBy count = %d, want %d", len(nib.BlockedBy), len(tt.expectedBlockedBy))
				return
			}

			for i, expected := range tt.expectedBlockedBy {
				if nib.BlockedBy[i] != expected {
					t.Errorf("BlockedBy[%d] = %q, want %q", i, nib.BlockedBy[i], expected)
				}
			}
		})
	}
}

func TestRenderWithBlockedBy(t *testing.T) {
	tests := []struct {
		name     string
		nib      *Nib
		contains []string
	}{
		{
			name: "with blocked_by only",
			nib: &Nib{
				Title:     "Test Nib",
				Status:    "todo",
				BlockedBy: []string{"abc123", "def456"},
			},
			contains: []string{
				"blocked_by:",
				"- abc123",
				"- def456",
			},
		},
		{
			name: "with blocking and blocked_by (only blocked_by rendered)",
			nib: &Nib{
				Title:     "Test Nib",
				Status:    "todo",
				Blocking:  []string{"xyz789"},
				BlockedBy: []string{"abc123"},
			},
			contains: []string{
				"blocked_by:",
				"- abc123",
			},
		},
		{
			name: "without blocked_by",
			nib: &Nib{
				Title:  "Test Nib",
				Status: "todo",
			},
			contains: []string{
				"title: Test Nib",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := tt.nib.Render()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			result := string(output)
			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf("output missing %q\ngot:\n%s", want, result)
				}
			}

			// Check that empty blocked_by doesn't appear in output
			if len(tt.nib.BlockedBy) == 0 && strings.Contains(result, "blocked_by:") {
				t.Errorf("output should not contain 'blocked_by:' when no blocked_by\ngot:\n%s", result)
			}
		})
	}
}

func TestBlockedByRoundtrip(t *testing.T) {
	tests := []struct {
		name      string
		blockedBy []string
	}{
		{
			name:      "single blocked_by",
			blockedBy: []string{"abc123"},
		},
		{
			name:      "multiple blocked_by",
			blockedBy: []string{"abc123", "def456"},
		},
		{
			name:      "empty blocked_by",
			blockedBy: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := &Nib{
				Title:     "Test",
				Status:    "todo",
				BlockedBy: tt.blockedBy,
			}

			rendered, err := original.Render()
			if err != nil {
				t.Fatalf("Render error: %v", err)
			}

			parsed, err := Parse(strings.NewReader(string(rendered)))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			if len(parsed.BlockedBy) != len(tt.blockedBy) {
				t.Errorf("BlockedBy count: got %d, want %d", len(parsed.BlockedBy), len(tt.blockedBy))
				return
			}

			for i, expected := range tt.blockedBy {
				if parsed.BlockedBy[i] != expected {
					t.Errorf("BlockedBy[%d] = %q, want %q", i, parsed.BlockedBy[i], expected)
				}
			}
		})
	}
}

func TestNibRelationshipMethods(t *testing.T) {
	t.Run("HasParent", func(t *testing.T) {
		withParent := &Nib{Parent: "xyz789"}
		if !withParent.HasParent() {
			t.Error("expected HasParent() = true when parent is set")
		}

		withoutParent := &Nib{}
		if withoutParent.HasParent() {
			t.Error("expected HasParent() = false when parent is empty")
		}
	})

	t.Run("IsBlocking", func(t *testing.T) {
		b := &Nib{Blocking: []string{"abc", "def"}}
		if !b.IsBlocking("abc") {
			t.Error("expected IsBlocking('abc') = true")
		}
		if !b.IsBlocking("def") {
			t.Error("expected IsBlocking('def') = true")
		}
		if b.IsBlocking("xyz") {
			t.Error("expected IsBlocking('xyz') = false")
		}

		empty := &Nib{}
		if empty.IsBlocking("abc") {
			t.Error("expected IsBlocking('abc') = false for empty blocks")
		}
	})

	t.Run("AddBlocking", func(t *testing.T) {
		b := &Nib{Blocking: []string{"abc"}}
		b.AddBlocking("def")
		if len(b.Blocking) != 2 {
			t.Errorf("AddBlocking new: got len=%d, want 2", len(b.Blocking))
		}
		if !b.IsBlocking("def") {
			t.Error("AddBlocking didn't add the block")
		}

		// Adding duplicate should not add
		b.AddBlocking("abc")
		if len(b.Blocking) != 2 {
			t.Errorf("AddBlocking duplicate: got len=%d, want 2", len(b.Blocking))
		}
	})

	t.Run("RemoveBlocking", func(t *testing.T) {
		b := &Nib{Blocking: []string{"abc", "def", "ghi"}}
		b.RemoveBlocking("def")
		if len(b.Blocking) != 2 {
			t.Errorf("RemoveBlocking existing: got len=%d, want 2", len(b.Blocking))
		}
		if b.IsBlocking("def") {
			t.Error("RemoveBlocking didn't remove the block")
		}

		// Removing non-existent should not change anything
		b.RemoveBlocking("nonexistent")
		if len(b.Blocking) != 2 {
			t.Errorf("RemoveBlocking non-existent: got len=%d, want 2", len(b.Blocking))
		}
	})

	t.Run("IsBlockedBy", func(t *testing.T) {
		b := &Nib{BlockedBy: []string{"abc", "def"}}
		if !b.IsBlockedBy("abc") {
			t.Error("expected IsBlockedBy('abc') = true")
		}
		if !b.IsBlockedBy("def") {
			t.Error("expected IsBlockedBy('def') = true")
		}
		if b.IsBlockedBy("xyz") {
			t.Error("expected IsBlockedBy('xyz') = false")
		}

		empty := &Nib{}
		if empty.IsBlockedBy("abc") {
			t.Error("expected IsBlockedBy('abc') = false for empty blocked_by")
		}
	})

	t.Run("AddBlockedBy", func(t *testing.T) {
		b := &Nib{BlockedBy: []string{"abc"}}
		b.AddBlockedBy("def")
		if len(b.BlockedBy) != 2 {
			t.Errorf("AddBlockedBy new: got len=%d, want 2", len(b.BlockedBy))
		}
		if !b.IsBlockedBy("def") {
			t.Error("AddBlockedBy didn't add the blocker")
		}

		// Adding duplicate should not add
		b.AddBlockedBy("abc")
		if len(b.BlockedBy) != 2 {
			t.Errorf("AddBlockedBy duplicate: got len=%d, want 2", len(b.BlockedBy))
		}
	})

	t.Run("RemoveBlockedBy", func(t *testing.T) {
		b := &Nib{BlockedBy: []string{"abc", "def", "ghi"}}
		b.RemoveBlockedBy("def")
		if len(b.BlockedBy) != 2 {
			t.Errorf("RemoveBlockedBy existing: got len=%d, want 2", len(b.BlockedBy))
		}
		if b.IsBlockedBy("def") {
			t.Error("RemoveBlockedBy didn't remove the blocker")
		}

		// Removing non-existent should not change anything
		b.RemoveBlockedBy("nonexistent")
		if len(b.BlockedBy) != 2 {
			t.Errorf("RemoveBlockedBy non-existent: got len=%d, want 2", len(b.BlockedBy))
		}
	})
}

func TestValidateTag(t *testing.T) {
	tests := []struct {
		tag     string
		wantErr bool
	}{
		{"frontend", false},
		{"backend", false},
		{"tech-debt", false},
		{"v1", false},
		{"a", false},
		{"urgent2", false},
		{"wont-fix", false},
		{"a-b-c", false},
		{"", true},         // empty
		{"Frontend", true}, // uppercase
		{"URGENT", true},   // all uppercase
		{"123", true},      // starts with number
		{"123abc", true},   // starts with number
		{"my tag", true},   // contains space
		{"my_tag", true},   // contains underscore
		{"my--tag", true},  // consecutive hyphens
		{"-tag", true},     // starts with hyphen
		{"tag-", true},     // ends with hyphen
		{"my.tag", true},   // contains dot
		{"my/tag", true},   // contains slash
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			err := ValidateTag(tt.tag)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateTag(%q) = nil, want error", tt.tag)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateTag(%q) = %v, want nil", tt.tag, err)
			}
		})
	}
}

func TestNormalizeTag(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"frontend", "frontend"},
		{"FRONTEND", "frontend"},
		{"FrontEnd", "frontend"},
		{"  frontend  ", "frontend"},
		{"  FRONTEND  ", "frontend"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeTag(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeTag(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNibTagMethods(t *testing.T) {
	t.Run("HasTag", func(t *testing.T) {
		b := &Nib{Tags: []string{"frontend", "urgent"}}
		if !b.HasTag("frontend") {
			t.Error("expected HasTag('frontend') = true")
		}
		if !b.HasTag("urgent") {
			t.Error("expected HasTag('urgent') = true")
		}
		if b.HasTag("backend") {
			t.Error("expected HasTag('backend') = false")
		}
		// Case insensitive lookup
		if !b.HasTag("FRONTEND") {
			t.Error("expected HasTag('FRONTEND') = true (case insensitive)")
		}
	})

	t.Run("AddTag", func(t *testing.T) {
		b := &Nib{Tags: []string{"frontend"}}

		// Add new valid tag
		if err := b.AddTag("backend"); err != nil {
			t.Errorf("AddTag('backend') error: %v", err)
		}
		if len(b.Tags) != 2 {
			t.Errorf("expected 2 tags, got %d", len(b.Tags))
		}

		// Adding duplicate should not add
		if err := b.AddTag("frontend"); err != nil {
			t.Errorf("AddTag('frontend') error: %v", err)
		}
		if len(b.Tags) != 2 {
			t.Errorf("expected 2 tags (no duplicate), got %d", len(b.Tags))
		}

		// Adding invalid tag should error
		if err := b.AddTag("Invalid Tag"); err == nil {
			t.Error("expected AddTag('Invalid Tag') to error")
		}
	})

	t.Run("RemoveTag", func(t *testing.T) {
		b := &Nib{Tags: []string{"frontend", "backend", "urgent"}}

		b.RemoveTag("backend")
		if len(b.Tags) != 2 {
			t.Errorf("expected 2 tags after remove, got %d", len(b.Tags))
		}
		if b.HasTag("backend") {
			t.Error("expected backend tag to be removed")
		}

		// Case insensitive removal
		b.RemoveTag("FRONTEND")
		if len(b.Tags) != 1 {
			t.Errorf("expected 1 tag after remove, got %d", len(b.Tags))
		}
		if b.HasTag("frontend") {
			t.Error("expected frontend tag to be removed")
		}

		// Remove non-existent tag (should not error)
		b.RemoveTag("nonexistent")
		if len(b.Tags) != 1 {
			t.Errorf("expected 1 tag (no change), got %d", len(b.Tags))
		}
	})
}

func TestParseWithTags(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedTags []string
	}{
		{
			name: "single tag",
			input: `---
title: Test
status: todo
tags:
  - frontend
---`,
			expectedTags: []string{"frontend"},
		},
		{
			name: "multiple tags",
			input: `---
title: Test
status: todo
tags:
  - frontend
  - urgent
  - tech-debt
---`,
			expectedTags: []string{"frontend", "urgent", "tech-debt"},
		},
		{
			name: "inline tags syntax",
			input: `---
title: Test
status: todo
tags: [frontend, backend]
---`,
			expectedTags: []string{"frontend", "backend"},
		},
		{
			name: "no tags",
			input: `---
title: Test
status: todo
---`,
			expectedTags: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nib, err := Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(tt.expectedTags) == 0 && len(nib.Tags) == 0 {
				return // Both empty, OK
			}

			if len(nib.Tags) != len(tt.expectedTags) {
				t.Errorf("Tags count = %d, want %d", len(nib.Tags), len(tt.expectedTags))
				return
			}

			for i, expected := range tt.expectedTags {
				if nib.Tags[i] != expected {
					t.Errorf("Tags[%d] = %q, want %q", i, nib.Tags[i], expected)
				}
			}
		})
	}
}

func TestRenderWithTags(t *testing.T) {
	tests := []struct {
		name     string
		nib      *Nib
		contains []string
	}{
		{
			name: "with single tag",
			nib: &Nib{
				Title:  "Test Nib",
				Status: "todo",
				Tags:   []string{"frontend"},
			},
			contains: []string{
				"tags:",
				"- frontend",
			},
		},
		{
			name: "with multiple tags",
			nib: &Nib{
				Title:  "Test Nib",
				Status: "todo",
				Tags:   []string{"frontend", "urgent", "tech-debt"},
			},
			contains: []string{
				"tags:",
				"- frontend",
				"- urgent",
				"- tech-debt",
			},
		},
		{
			name: "without tags",
			nib: &Nib{
				Title:  "Test Nib",
				Status: "todo",
			},
			contains: []string{
				"title: Test Nib",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := tt.nib.Render()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			result := string(output)
			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf("output missing %q\ngot:\n%s", want, result)
				}
			}

			// Check that empty tags don't appear in output
			if len(tt.nib.Tags) == 0 && strings.Contains(result, "tags:") {
				t.Errorf("output should not contain 'tags:' when no tags\ngot:\n%s", result)
			}
		})
	}
}

func TestTagsRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		tags []string
	}{
		{
			name: "single tag",
			tags: []string{"frontend"},
		},
		{
			name: "multiple tags",
			tags: []string{"frontend", "backend", "urgent"},
		},
		{
			name: "hyphenated tags",
			tags: []string{"tech-debt", "wont-fix", "needs-review"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := &Nib{
				Title:  "Test",
				Status: "todo",
				Tags:   tt.tags,
			}

			rendered, err := original.Render()
			if err != nil {
				t.Fatalf("Render error: %v", err)
			}

			parsed, err := Parse(strings.NewReader(string(rendered)))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			if len(parsed.Tags) != len(tt.tags) {
				t.Errorf("Tags count: got %d, want %d", len(parsed.Tags), len(tt.tags))
				return
			}

			for i, expected := range tt.tags {
				if parsed.Tags[i] != expected {
					t.Errorf("Tags[%d] = %q, want %q", i, parsed.Tags[i], expected)
				}
			}
		})
	}
}

func TestRenderWithIDComment(t *testing.T) {
	tests := []struct {
		name          string
		nib           *Nib
		expectComment string
	}{
		{
			name: "with ID",
			nib: &Nib{
				ID:     "nibs-abc123",
				Title:  "Test Nib",
				Status: "todo",
			},
			expectComment: "# nibs-abc123",
		},
		{
			name: "without ID",
			nib: &Nib{
				Title:  "Test Nib",
				Status: "todo",
			},
			expectComment: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := tt.nib.Render()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			result := string(output)

			if tt.expectComment != "" {
				// Check that comment appears right after opening ---
				expectedStart := "---\n" + tt.expectComment + "\n"
				if !strings.HasPrefix(result, expectedStart) {
					t.Errorf("expected output to start with %q\ngot:\n%s", expectedStart, result)
				}
			} else {
				// When no ID, should not have a comment line
				lines := strings.Split(result, "\n")
				if len(lines) > 1 && strings.HasPrefix(lines[1], "#") {
					t.Errorf("expected no comment line when ID is empty\ngot:\n%s", result)
				}
			}
		})
	}
}

func TestRenderWithIDCommentRoundtrip(t *testing.T) {
	// Verify that the ID comment doesn't interfere with parsing
	original := &Nib{
		ID:     "nibs-xyz789",
		Title:  "Test Nib",
		Status: "in-progress",
		Body:   "Some body content.",
	}

	rendered, err := original.Render()
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	// Verify the comment is present
	if !strings.Contains(string(rendered), "# nibs-xyz789") {
		t.Errorf("rendered output should contain ID comment\ngot:\n%s", rendered)
	}

	// Parse should work correctly (comment is ignored)
	parsed, err := Parse(strings.NewReader(string(rendered)))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if parsed.Title != original.Title {
		t.Errorf("Title roundtrip: got %q, want %q", parsed.Title, original.Title)
	}
	if parsed.Status != original.Status {
		t.Errorf("Status roundtrip: got %q, want %q", parsed.Status, original.Status)
	}
}

func TestRenderTrailingNewline(t *testing.T) {
	tests := []struct {
		name string
		nib  *Nib
	}{
		{
			name: "with body",
			nib: &Nib{
				Title:  "Test Nib",
				Status: "todo",
				Body:   "Some content without trailing newline",
			},
		},
		{
			name: "with body ending in newline",
			nib: &Nib{
				Title:  "Test Nib",
				Status: "todo",
				Body:   "Some content with trailing newline\n",
			},
		},
		{
			name: "without body",
			nib: &Nib{
				Title:  "Test Nib",
				Status: "todo",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered, err := tt.nib.Render()
			if err != nil {
				t.Fatalf("Render error: %v", err)
			}
			if !strings.HasSuffix(string(rendered), "\n") {
				t.Errorf("rendered output should end with newline\ngot: %q", rendered)
			}
		})
	}
}

func TestETag(t *testing.T) {
	t.Run("consistent hash", func(t *testing.T) {
		b := &Nib{
			Title:  "Test",
			Status: "todo",
			Body:   "content",
		}
		etag1 := b.ETag()
		etag2 := b.ETag()
		if etag1 != etag2 {
			t.Errorf("ETag not consistent: %s != %s", etag1, etag2)
		}
	})

	t.Run("16 hex characters", func(t *testing.T) {
		b := &Nib{
			Title:  "Test",
			Status: "todo",
		}
		etag := b.ETag()
		if len(etag) != 16 {
			t.Errorf("ETag length = %d, want 16", len(etag))
		}
		// Verify it's valid hex
		for _, c := range etag {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				t.Errorf("ETag contains non-hex char: %c", c)
			}
		}
	})

	t.Run("changes when title changes", func(t *testing.T) {
		b := &Nib{
			Title:  "Test",
			Status: "todo",
		}
		etag1 := b.ETag()

		b.Title = "Changed"
		etag2 := b.ETag()

		if etag1 == etag2 {
			t.Error("ETag should change when title changes")
		}
	})

	t.Run("changes when status changes", func(t *testing.T) {
		b := &Nib{
			Title:  "Test",
			Status: "todo",
		}
		etag1 := b.ETag()

		b.Status = "in-progress"
		etag2 := b.ETag()

		if etag1 == etag2 {
			t.Error("ETag should change when status changes")
		}
	})

	t.Run("changes when body changes", func(t *testing.T) {
		b := &Nib{
			Title:  "Test",
			Status: "todo",
			Body:   "original",
		}
		etag1 := b.ETag()

		b.Body = "modified"
		etag2 := b.ETag()

		if etag1 == etag2 {
			t.Error("ETag should change when body changes")
		}
	})

	t.Run("changes when metadata changes", func(t *testing.T) {
		b := &Nib{
			Title:    "Test",
			Status:   "todo",
			Priority: "normal",
		}
		etag1 := b.ETag()

		b.Priority = "high"
		etag2 := b.ETag()

		if etag1 == etag2 {
			t.Error("ETag should change when priority changes")
		}
	})

	t.Run("same content same etag", func(t *testing.T) {
		b1 := &Nib{
			Title:  "Test",
			Status: "todo",
			Body:   "content",
		}
		b2 := &Nib{
			Title:  "Test",
			Status: "todo",
			Body:   "content",
		}

		if b1.ETag() != b2.ETag() {
			t.Error("Same content should produce same ETag")
		}
	})

	t.Run("different order of tags produces different etag", func(t *testing.T) {
		b1 := &Nib{
			Title:  "Test",
			Status: "todo",
			Tags:   []string{"a", "b"},
		}
		b2 := &Nib{
			Title:  "Test",
			Status: "todo",
			Tags:   []string{"b", "a"},
		}

		// Tag order matters in rendered output, so ETags will differ
		if b1.ETag() == b2.ETag() {
			t.Error("Different tag order should produce different ETag")
		}
	})

	t.Run("etag is empty on render error", func(t *testing.T) {
		// This is a defensive test - Render() shouldn't fail in practice,
		// but ETag() handles it gracefully by returning empty string
		b := &Nib{
			Title:  "Test",
			Status: "todo",
		}
		etag := b.ETag()
		if etag == "" {
			t.Error("ETag should not be empty for valid nib")
		}
	})
}

func TestMarshalJSONIncludesETag(t *testing.T) {
	b := &Nib{
		ID:     "test-123",
		Title:  "Test Nib",
		Status: "todo",
		Body:   "Some content",
	}

	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Check etag field is present
	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"etag"`) {
		t.Errorf("JSON should contain 'etag' field, got: %s", jsonStr)
	}

	// Parse and verify etag value
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	etag, ok := result["etag"].(string)
	if !ok {
		t.Error("etag should be a string")
	}
	if len(etag) != 16 {
		t.Errorf("etag length = %d, want 16", len(etag))
	}

	// Verify it matches the computed ETag
	if etag != b.ETag() {
		t.Errorf("JSON etag = %s, want %s", etag, b.ETag())
	}

	// MarshalJSON must project the PRESENTATION defaults: this fixture leaves
	// Type/Priority empty, so the JSON surface must serialize "task"/"normal"
	// (matching EffectiveType()/EffectivePriority() and the GraphQL field
	// resolvers). Guards the effective-value projection against a revert to a raw
	// (*NibAlias)(b) marshal that would re-emit empty type/priority in `nibs show
	// --json`.
	if got, _ := result["type"].(string); got != "task" {
		t.Errorf("JSON type = %q, want %q (effective default for empty Type)", got, "task")
	}
	if got, _ := result["priority"].(string); got != "normal" {
		t.Errorf("JSON priority = %q, want %q (effective default for empty Priority)", got, "normal")
	}
}

func TestETagChangesAfterModification(t *testing.T) {
	// Verify that ETag changes reflect actual content changes
	// (this is important for optimistic concurrency control)
	b := &Nib{
		Title:  "Original",
		Status: "todo",
		Body:   "Original body",
	}

	etag1 := b.ETag()

	// Modify the nib
	b.Title = "Modified"
	b.Body = "Modified body"

	etag2 := b.ETag()

	if etag1 == etag2 {
		t.Error("ETag should change after modification")
	}

	// Verify JSON serialization reflects the change
	data1, _ := json.Marshal(&Nib{Title: "Original", Status: "todo", Body: "Original body"})
	data2, _ := json.Marshal(b)

	var result1, result2 map[string]interface{}
	if err := json.Unmarshal(data1, &result1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data2, &result2); err != nil {
		t.Fatal(err)
	}

	if result1["etag"] == result2["etag"] {
		t.Error("JSON etag should differ after modification")
	}
}

func TestRenderIncludesVersion(t *testing.T) {
	tests := []struct {
		name    string
		version int
		want    string
	}{
		{
			name:    "version 1",
			version: 1,
			want:    "version: 1",
		},
		{
			name:    "version 0",
			version: 0,
			want:    "version: 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &Nib{
				Title:   "Test",
				Status:  "todo",
				Version: tt.version,
			}
			rendered, err := b.Render()
			if err != nil {
				t.Fatalf("Render error: %v", err)
			}
			if !strings.Contains(string(rendered), tt.want) {
				t.Errorf("Rendered output does not contain %q:\n%s", tt.want, rendered)
			}
		})
	}
}

func TestVersionJSON(t *testing.T) {
	b := &Nib{
		Title:   "Test",
		Status:  "todo",
		Version: 1,
	}
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	version, ok := result["version"]
	if !ok {
		t.Fatal("JSON output missing 'version' field")
	}
	if version.(float64) != 1 {
		t.Errorf("JSON version = %v, want 1", version)
	}

	// Also test version 0 is included (not omitted)
	b.Version = 0
	data, _ = json.Marshal(b)
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	version, ok = result["version"]
	if !ok {
		t.Fatal("JSON output missing 'version' field when version is 0")
	}
	if version.(float64) != 0 {
		t.Errorf("JSON version = %v, want 0", version)
	}
}

func TestVersionRoundtrip(t *testing.T) {
	tests := []struct {
		name    string
		version int
	}{
		{"version 0", 0},
		{"version 1", 1},
		{"version 5", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := &Nib{
				Title:   "Test",
				Status:  "todo",
				Version: tt.version,
			}
			rendered, err := original.Render()
			if err != nil {
				t.Fatalf("Render error: %v", err)
			}
			parsed, err := Parse(strings.NewReader(string(rendered)))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			if parsed.Version != tt.version {
				t.Errorf("Round-trip Version = %d, want %d", parsed.Version, tt.version)
			}
		})
	}
}

func TestParseWithVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		version int
	}{
		{
			name: "explicit version 1",
			input: `---
title: Test
status: todo
version: 1
---
`,
			version: 1,
		},
		{
			name: "no version field defaults to 0",
			input: `---
title: Test
status: todo
---
`,
			version: 0,
		},
		{
			name: "explicit version 0",
			input: `---
title: Test
status: todo
version: 0
---
`,
			version: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			if b.Version != tt.version {
				t.Errorf("Version = %d, want %d", b.Version, tt.version)
			}
		})
	}
}

func TestBlockingNotPersistedForV1Nibs(t *testing.T) {
	// Single-side blocking: a normal v1 nib never has the legacy Blocking field
	// set (resolvers store the relationship on targets' blocked_by, and
	// migrateV0ToV1 clears Blocking before persisting), so with omitempty it is
	// absent from the render. (When Blocking IS explicitly set — only a legacy v0
	// file parsed straight from disk — it now round-trips; see
	// TestBlockingRoundtrip.)
	b := &Nib{
		Version:   1,
		Title:     "Test Nib",
		Status:    "todo",
		BlockedBy: []string{"blocker-1"},
	}

	output, err := b.Render()
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	result := string(output)
	if strings.Contains(result, "blocking:") {
		t.Errorf("v1 nib with no Blocking set should not render 'blocking:'\ngot:\n%s", result)
	}
	if !strings.Contains(result, "blocked_by:") {
		t.Errorf("expected single-side blocked_by to be persisted\ngot:\n%s", result)
	}
}

func TestDocumentsField(t *testing.T) {
	t.Run("parse documents from frontmatter", func(t *testing.T) {
		input := `---
title: Test
status: todo
documents:
    - docs/prd.md
    - research/notes.md
---
`
		b, err := Parse(strings.NewReader(input))
		if err != nil {
			t.Fatalf("Parse error: %v", err)
		}
		if len(b.Documents) != 2 {
			t.Fatalf("Documents count = %d, want 2", len(b.Documents))
		}
		if b.Documents[0] != "docs/prd.md" {
			t.Errorf("Documents[0] = %q, want %q", b.Documents[0], "docs/prd.md")
		}
		if b.Documents[1] != "research/notes.md" {
			t.Errorf("Documents[1] = %q, want %q", b.Documents[1], "research/notes.md")
		}
	})

	t.Run("render documents to frontmatter", func(t *testing.T) {
		b := &Nib{
			Title:     "Test",
			Status:    "todo",
			Documents: []string{"docs/prd.md", "research/notes.md"},
		}
		output, err := b.Render()
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}
		result := string(output)
		if !strings.Contains(result, "documents:") {
			t.Errorf("output missing 'documents:'\ngot:\n%s", result)
		}
		if !strings.Contains(result, "- docs/prd.md") {
			t.Errorf("output missing '- docs/prd.md'\ngot:\n%s", result)
		}
	})

	t.Run("round-trip preserves documents", func(t *testing.T) {
		original := &Nib{
			Title:     "Test",
			Status:    "todo",
			Documents: []string{"docs/prd.md", "research/notes.md"},
		}
		rendered, err := original.Render()
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}
		parsed, err := Parse(strings.NewReader(string(rendered)))
		if err != nil {
			t.Fatalf("Parse error: %v", err)
		}
		if len(parsed.Documents) != 2 {
			t.Fatalf("round-trip Documents count = %d, want 2", len(parsed.Documents))
		}
		if parsed.Documents[0] != "docs/prd.md" || parsed.Documents[1] != "research/notes.md" {
			t.Errorf("round-trip Documents = %v, want [docs/prd.md research/notes.md]", parsed.Documents)
		}
	})

	t.Run("empty documents not rendered", func(t *testing.T) {
		b := &Nib{Title: "Test", Status: "todo"}
		output, err := b.Render()
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}
		if strings.Contains(string(output), "documents:") {
			t.Errorf("output should not contain 'documents:' when empty\ngot:\n%s", string(output))
		}
	})

	t.Run("JSON includes documents", func(t *testing.T) {
		b := &Nib{
			ID:        "test-1",
			Title:     "Test",
			Status:    "todo",
			Documents: []string{"docs/prd.md"},
		}
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("Marshal error: %v", err)
		}
		if !strings.Contains(string(data), `"documents":["docs/prd.md"]`) {
			t.Errorf("JSON missing documents field\ngot: %s", string(data))
		}
	})
}

func TestNibClone(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	original := &Nib{
		ID:        "test-1",
		Slug:      "test-nib",
		Path:      "test-1--test-nib.md",
		Version:   1,
		Title:     "Original Title",
		Status:    "todo",
		Type:      "task",
		Priority:  "high",
		Estimate:  "2h",
		Tags:      []string{"frontend", "urgent"},
		CreatedAt: &now,
		UpdatedAt: &now,
		Body:      "Original body",
		Parent:    "parent-1",
		Blocking:  []string{"block-1"},
		BlockedBy: []string{"blocker-1", "blocker-2"},
		Documents: []string{"docs/spec.md"},
		Order:     "a0",
	}

	clone := original.Clone()

	t.Run("clone has same values", func(t *testing.T) {
		if clone.ID != original.ID {
			t.Errorf("ID = %q, want %q", clone.ID, original.ID)
		}
		if clone.Title != original.Title {
			t.Errorf("Title = %q, want %q", clone.Title, original.Title)
		}
		if clone.Status != original.Status {
			t.Errorf("Status = %q, want %q", clone.Status, original.Status)
		}
		if clone.Body != original.Body {
			t.Errorf("Body = %q, want %q", clone.Body, original.Body)
		}
		if clone.Parent != original.Parent {
			t.Errorf("Parent = %q, want %q", clone.Parent, original.Parent)
		}
		if len(clone.Tags) != len(original.Tags) {
			t.Fatalf("Tags len = %d, want %d", len(clone.Tags), len(original.Tags))
		}
		if len(clone.BlockedBy) != len(original.BlockedBy) {
			t.Fatalf("BlockedBy len = %d, want %d", len(clone.BlockedBy), len(original.BlockedBy))
		}
		if len(clone.Documents) != len(original.Documents) {
			t.Fatalf("Documents len = %d, want %d", len(clone.Documents), len(original.Documents))
		}
	})

	t.Run("modifying clone does not affect original", func(t *testing.T) {
		clone.Title = "Modified Title"
		clone.Status = "completed"
		clone.Tags = append(clone.Tags, "modified")
		clone.BlockedBy = append(clone.BlockedBy, "extra-blocker")
		clone.Documents = append(clone.Documents, "docs/extra.md")
		clone.Body = "Modified body"

		if original.Title != "Original Title" {
			t.Errorf("original.Title was modified to %q", original.Title)
		}
		if original.Status != "todo" {
			t.Errorf("original.Status was modified to %q", original.Status)
		}
		if len(original.Tags) != 2 {
			t.Errorf("original.Tags was modified: %v", original.Tags)
		}
		if len(original.BlockedBy) != 2 {
			t.Errorf("original.BlockedBy was modified: %v", original.BlockedBy)
		}
		if len(original.Documents) != 1 {
			t.Errorf("original.Documents was modified: %v", original.Documents)
		}
		if original.Body != "Original body" {
			t.Errorf("original.Body was modified to %q", original.Body)
		}
	})

	t.Run("clone preserves nil slices", func(t *testing.T) {
		minimal := &Nib{ID: "min-1", Title: "Minimal", Status: "todo"}
		minClone := minimal.Clone()
		if minClone.Tags != nil {
			t.Errorf("Tags should be nil, got %v", minClone.Tags)
		}
		if minClone.BlockedBy != nil {
			t.Errorf("BlockedBy should be nil, got %v", minClone.BlockedBy)
		}
		if minClone.Documents != nil {
			t.Errorf("Documents should be nil, got %v", minClone.Documents)
		}
		if minClone.CreatedAt != nil {
			t.Errorf("CreatedAt should be nil")
		}
		if minClone.UpdatedAt != nil {
			t.Errorf("UpdatedAt should be nil")
		}
	})

	t.Run("clone timestamps are independent", func(t *testing.T) {
		newTime := now.Add(time.Hour)
		clone.UpdatedAt = &newTime
		if !original.UpdatedAt.Equal(now) {
			t.Errorf("original.UpdatedAt was modified")
		}
	})

	t.Run("clone clears the priorityMigrated flag", func(t *testing.T) {
		// Parsing a legacy `priority: deferred` nib normalizes it to `low` and
		// sets the load-boundary-only priorityMigrated flag. Clone() must clear
		// that flag so a stale `true` never rides along through Update cycles
		// (it is meaningful only on a freshly-parsed nib, consumed by the loader).
		parsed, err := Parse(strings.NewReader(`---
version: 1
title: Legacy Deferred
status: todo
priority: deferred
---
`))
		if err != nil {
			t.Fatalf("Parse() error: %v", err)
		}
		if !parsed.PriorityMigrated() {
			t.Fatal("parsed.PriorityMigrated() = false, want true after parsing priority: deferred")
		}

		cloned := parsed.Clone()
		if cloned.PriorityMigrated() {
			t.Error("cloned.PriorityMigrated() = true, want false (Clone must clear the flag)")
		}
		// The normalized value itself must survive the clone.
		if cloned.Priority != "low" {
			t.Errorf("cloned.Priority = %q, want %q", cloned.Priority, "low")
		}
	})

	t.Run("clone deep-copies the Extra map", func(t *testing.T) {
		// Clone must deep-copy the unknown-key passthrough so a mutated clone can't
		// alias (and corrupt) the original's Extra. Without the copy the clone would
		// share the same underlying map and these mutations would leak.
		orig := &Nib{
			ID:     "extra-1",
			Title:  "With Extra",
			Status: "todo",
			Extra:  map[string]yaml.Node{"assignee": scalarExtraNode("bob"), "sprint": scalarExtraNode("2026-Q1")},
		}
		cl := orig.Clone()

		cl.Extra["assignee"] = scalarExtraNode("alice") // mutate an existing key
		cl.Extra["reviewer"] = scalarExtraNode("carol") // add a new key
		delete(cl.Extra, "sprint")                      // remove a key

		if orig.Extra["assignee"].Value != "bob" {
			t.Errorf("original Extra[assignee] mutated via clone: %v", orig.Extra["assignee"])
		}
		if _, added := orig.Extra["reviewer"]; added {
			t.Errorf("adding a key to the clone leaked into the original: %v", orig.Extra)
		}
		if _, present := orig.Extra["sprint"]; !present {
			t.Errorf("deleting a key on the clone removed it from the original: %v", orig.Extra)
		}
	})

	t.Run("clone preserves a nil Extra", func(t *testing.T) {
		minimal := &Nib{ID: "min-extra", Title: "Minimal", Status: "todo"}
		if minimal.Clone().Extra != nil {
			t.Errorf("nil Extra should stay nil after Clone")
		}
	})
}
