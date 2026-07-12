package input

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failingReader always fails, used to exercise the stdin read-error path.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestProseStdin(t *testing.T) {
	got, err := Prose("-", strings.NewReader("## Heading\n`code` \"quoted\"\nline\n"))
	if err != nil {
		t.Fatalf("Prose(-) unexpected error: %v", err)
	}
	want := "## Heading\n`code` \"quoted\"\nline\n"
	if got != want {
		t.Errorf("Prose(-) = %q, want %q", got, want)
	}
}

func TestProseStdinReadError(t *testing.T) {
	_, err := Prose("-", failingReader{})
	if err == nil {
		t.Fatal("Prose(-) with failing reader: expected error, got nil")
	}
	var ioErr *IOError
	if !errors.As(err, &ioErr) {
		t.Errorf("Prose(-) read error = %T, want *IOError", err)
	}
}

func TestProseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.md")
	content := "# Title\n\nSome **prose** with `backticks`.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	got, err := Prose("@"+path, strings.NewReader("SHOULD NOT BE READ"))
	if err != nil {
		t.Fatalf("Prose(@file) unexpected error: %v", err)
	}
	if got != content {
		t.Errorf("Prose(@file) = %q, want %q", got, content)
	}
}

func TestProseMissingFileIsIOError(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.md")

	_, err := Prose("@"+missing, strings.NewReader(""))
	if err == nil {
		t.Fatal("Prose(@missing) expected error, got nil")
	}
	var ioErr *IOError
	if !errors.As(err, &ioErr) {
		t.Errorf("Prose(@missing) error = %T, want *IOError", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Prose(@missing) should wrap os.ErrNotExist, got %v", err)
	}
}

func TestProseRejectsInline(t *testing.T) {
	_, err := Prose("just some inline text", strings.NewReader("stdin"))
	if !errors.Is(err, ErrInlineProse) {
		t.Errorf("Prose(inline) error = %v, want ErrInlineProse", err)
	}
	// A bare inline value must not be reported as an I/O failure.
	var ioErr *IOError
	if errors.As(err, &ioErr) {
		t.Errorf("Prose(inline) should not be an *IOError, got %v", err)
	}
}

func TestProseEmptyPath(t *testing.T) {
	_, err := Prose("@", strings.NewReader(""))
	if !errors.Is(err, ErrEmptyPath) {
		t.Errorf("Prose(@) error = %v, want ErrEmptyPath", err)
	}
}

func TestProseEmptyValue(t *testing.T) {
	// An empty value resolves to "" and does NOT consume stdin.
	sentinel := &trackingReader{r: strings.NewReader("data")}
	got, err := Prose("", sentinel)
	if err != nil {
		t.Fatalf("Prose(\"\") unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("Prose(\"\") = %q, want empty", got)
	}
	if sentinel.readCalled {
		t.Error("Prose(\"\") must not read stdin")
	}
}

// trackingReader records whether Read was called.
type trackingReader struct {
	r          io.Reader
	readCalled bool
}

func (tr *trackingReader) Read(p []byte) (int, error) {
	tr.readCalled = true
	return tr.r.Read(p)
}
