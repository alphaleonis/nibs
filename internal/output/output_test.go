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
		{ErrTextNotFound, ExitValidation},  // 2
		{ErrTextAmbiguous, ExitValidation}, // 2
		{ErrConflict, ExitConflict},        // 4
		{ErrFileError, ExitIO},             // 5
		{ErrNoNibsDir, ExitIO},             // 5
		{ErrUncategorized, ExitError},      // 1
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

// generalCodeExpectations is every code this package declares, with the general
// member of its class. TestGeneralCode asserts the mapping and FuzzGeneralCode
// seeds its corpus from it.
//
// Which class a code generalizes to is a DECISION, not a property, so it is
// written down. What is not written down twice is the vocabulary: TestGeneralCode
// ranges over exitCodes and requires a row here for each member, so a code added
// to the vocabulary arrives with no row and fails rather than passing unnoticed.
var generalCodeExpectations = map[string]string{
	ErrValidation:    ErrValidation,
	ErrInvalidStatus: ErrValidation,
	ErrHierarchy:     ErrValidation,
	ErrTextNotFound:  ErrValidation,
	ErrTextAmbiguous: ErrValidation,
	ErrNotFound:      ErrNotFound,
	ErrConflict:      ErrConflict,
	ErrFileError:     ErrFileError,
	ErrNoNibsDir:     ErrFileError,
	ErrUncategorized: ErrUncategorized,
}

// TestGeneralCode pins the code a caller reports when it knows a failure's exit
// class but not its kind — the answer cmd/errors.go's graphQLResponseCode needs
// for a response holding several failures that share an exit status.
//
// The named expectations are the load-bearing half: the general member of a
// class must be the code whose meaning is the class's own, never a
// specialization. HIERARCHY generalizing to VALIDATION_ERROR is the case the
// aggregation rule depends on — reporting HIERARCHY for a mixed exit-2 response
// would assert an illegal parent type about a failure that is not one — and
// NO_BEANS_DIR generalizing to FILE_ERROR is the same shape one class over.
//
// Two properties are then asserted over every row, because they are what the
// caller relies on and neither is visible from a single mapping:
//
//   - It stays inside the class: ExitCode(GeneralCode(c)) == ExitCode(c). A
//     general member that exited elsewhere would change the answer of a
//     response every one of whose failures agreed on the exit.
//   - It is idempotent: generalizing an already-general code is a no-op, so a
//     caller cannot drift further from the class by asking twice.
//
// generalCodeExpectations is the table, and FuzzGeneralCode seeds its corpus
// from the same rows — one vocabulary, so the named mapping and the property
// check cannot come to describe different sets of codes.
//
// Its completeness is checked against exitCodes, the production map that IS the
// vocabulary, so a code declared and classified without anyone deciding what it
// generalizes to fails here.
func TestGeneralCode(t *testing.T) {
	general := generalCodeExpectations
	// A string ExitCode does not know exits 1, so its class is the one that
	// makes no claim.
	unknown := map[string]string{
		"SOMETHING_ELSE": ErrUncategorized,
		"":               ErrUncategorized,
	}

	for code := range exitCodes {
		if _, ok := general[code]; !ok {
			t.Errorf("%q is in the exit-code vocabulary but has no row here, so nothing says "+
				"which class it generalizes to — add one", code)
		}
	}
	for code := range general {
		if _, ok := exitCodes[code]; !ok {
			t.Errorf("%q has a row here but is not in the exit-code vocabulary, so this row "+
				"describes a code ExitCode does not classify", code)
		}
	}

	for code, want := range general {
		t.Run(code, func(t *testing.T) {
			assertGeneralCode(t, code, want)
		})
	}
	for code, want := range unknown {
		t.Run("unknown "+code, func(t *testing.T) {
			assertGeneralCode(t, code, want)
		})
	}
}

func assertGeneralCode(t *testing.T, code, want string) {
	t.Helper()
	got := GeneralCode(code)
	if got != want {
		t.Errorf("GeneralCode(%q) = %q, want %q", code, got, want)
	}
	if ExitCode(got) != ExitCode(code) {
		t.Errorf("GeneralCode(%q) = %q, which exits %d while %q exits %d — the general "+
			"member must stay inside its own class",
			code, got, ExitCode(got), code, ExitCode(code))
	}
	if again := GeneralCode(got); again != got {
		t.Errorf("GeneralCode(%q) = %q, but GeneralCode(%q) = %q — generalizing must be "+
			"idempotent", code, got, got, again)
	}
}

// FuzzGeneralCode asserts the two properties GeneralCode's callers rely on,
// over arbitrary strings rather than over a list of them. GeneralCode is total
// on strings — it switches on ExitCode, which answers for every input — so both
// properties are universally quantified and hold for a code this package never
// declared as readily as for one it did:
//
//   - It stays inside the class: ExitCode(GeneralCode(s)) == ExitCode(s).
//   - It is idempotent: GeneralCode(GeneralCode(s)) == GeneralCode(s).
//
// Quantifying over the input rather than over a hand-listed vocabulary is the
// point: a code declared as a var, as a typed value, or without the Err prefix
// is an ordinary string here and is covered on the same terms as any other.
//
// The corpus is seeded from generalCodeExpectations so an ordinary `go test`
// run — which executes the seeds without fuzzing — exercises every code that
// table names, plus two strings ExitCode does not know.
//
// What this does NOT do is notice a code added to the vocabulary later: an
// input the corpus never holds and the fuzzer never synthesizes is not tested.
// TestGeneralCode's named rows are what say which class each declared code
// belongs to, and that claim is not a property — it is a decision.
func FuzzGeneralCode(f *testing.F) {
	for code := range generalCodeExpectations {
		f.Add(code)
	}
	f.Add("")
	f.Add("SOMETHING_ELSE")

	f.Fuzz(func(t *testing.T, code string) {
		general := GeneralCode(code)
		if got, want := ExitCode(general), ExitCode(code); got != want {
			t.Errorf("GeneralCode(%q) = %q, which exits %d while %q exits %d — the general "+
				"member must stay inside its own class", code, general, got, code, want)
		}
		if again := GeneralCode(general); again != general {
			t.Errorf("GeneralCode(%q) = %q, but GeneralCode(%q) = %q — generalizing must be "+
				"idempotent", code, general, general, again)
		}
	})
}
