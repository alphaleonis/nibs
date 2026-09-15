package yamlfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/testskip"
	"gopkg.in/yaml.v3"
)

// TestReadFileRefusesAnIrregularFile pins that something other than a regular
// file produces a DETERMINATE ERROR rather than blocking the command forever:
// open(2) on a FIFO blocks until a writer arrives.
//
// The error must also not read as ABSENCE, which callers take as "use the
// defaults".
//
// The read runs under a deadline in a goroutine that never touches t, so a
// regression fails this test instead of hanging the package's run. The directory
// row keeps the guard meaningful where FIFOs cannot be created, because it
// reaches the same refusal through the same branch.
func TestReadFileRefusesAnIrregularFile(t *testing.T) {
	kinds := []struct {
		name   string
		create func(t *testing.T, path string)
	}{
		{
			name: "a named pipe",
			create: func(t *testing.T, path string) {
				if err := mkfifo(path); err != nil {
					testskip.Unavailable(t, testskip.NamedPipes, "mkfifo(%s): %v", path, err)
				}
			},
		},
		{
			name: "a directory",
			create: func(t *testing.T, path string) {
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatalf("mkdir %s: %v", path, err)
				}
			},
		},
	}

	const deadline = 10 * time.Second

	for _, kind := range kinds {
		t.Run(kind.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yml")
			kind.create(t, path)

			done := make(chan error, 1)
			go func() {
				_, err := ReadFile(path)
				done <- err
			}()

			var err error
			select {
			case err = <-done:
			case <-time.After(deadline):
				t.Fatalf("ReadFile did not return within %s with %s at the path; the read is blocked", deadline, kind.name)
			}
			if err == nil {
				t.Fatalf("ReadFile accepted %s", kind.name)
			}
			if os.IsNotExist(err) {
				t.Fatalf("ReadFile reported %s as an absent file (%v)", kind.name, err)
			}
			if !strings.Contains(err.Error(), "not a regular file") {
				t.Errorf("ReadFile refused %s with %v, which does not come from the regularity check", kind.name, err)
			}
		})
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()

	t.Run("an absent file is os.IsNotExist", func(t *testing.T) {
		_, err := ReadFile(filepath.Join(dir, "missing.yml"))
		if !os.IsNotExist(err) {
			t.Fatalf("err = %v, want an os.IsNotExist error", err)
		}
	})

	t.Run("a file at the cap is read whole", func(t *testing.T) {
		path := filepath.Join(dir, "at-cap.yml")
		if err := os.WriteFile(path, []byte(strings.Repeat("x", MaxBytes)), 0o644); err != nil {
			t.Fatal(err)
		}
		data, err := ReadFile(path)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if len(data) != MaxBytes {
			t.Errorf("read %d bytes, want %d", len(data), MaxBytes)
		}
	})

	t.Run("a file past the cap is refused, not truncated", func(t *testing.T) {
		path := filepath.Join(dir, "over-cap.yml")
		if err := os.WriteFile(path, []byte(strings.Repeat("x", MaxBytes+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		data, err := ReadFile(path)
		if err == nil {
			t.Fatalf("read %d bytes and no error, want a refusal", len(data))
		}
		if !strings.Contains(err.Error(), "configuration limit") {
			t.Errorf("err = %v, want the size refusal", err)
		}
	})
}

func TestSoleDocument(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		wantErr  error
		wantAny  bool // any non-nil error
		wantZero bool
	}{
		{name: "empty input is a zero node", data: "", wantZero: true},
		{name: "one document", data: "nibs:\n  prefix: x-\n"},
		{name: "a leading marker is still one document", data: "---\nnibs: {}\n"},
		{name: "two documents", data: "a: 1\n---\nb: 2\n", wantErr: ErrMultipleDocuments},
		{name: "a marker with nothing after it is still two", data: "a: 1\n---\n", wantErr: ErrMultipleDocuments},
		{name: "unparseable input", data: "a: [\n", wantAny: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := SoleDocument([]byte(tt.data))
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			case tt.wantAny:
				if err == nil {
					t.Fatal("err = nil, want a decode error")
				}
				return
			case err != nil:
				t.Fatalf("err = %v, want nil", err)
			}
			if got := doc.Kind == 0; got != tt.wantZero {
				t.Errorf("zero node = %v, want %v", got, tt.wantZero)
			}
		})
	}
}

func TestMappingValue(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("a: 1\nb: two\n"), &doc); err != nil {
		t.Fatal(err)
	}
	root := doc.Content[0]
	if got := MappingValue(root, "b"); got == nil || got.Value != "two" {
		t.Errorf("MappingValue(b) = %v, want the node holding two", got)
	}
	if got := MappingValue(root, "c"); got != nil {
		t.Errorf("MappingValue(c) = %v, want nil", got)
	}
	if got := MappingValue(nil, "a"); got != nil {
		t.Errorf("MappingValue(nil) = %v, want nil", got)
	}
	if got := MappingValue(root.Content[1], "a"); got != nil {
		t.Errorf("MappingValue on a scalar = %v, want nil", got)
	}
}
