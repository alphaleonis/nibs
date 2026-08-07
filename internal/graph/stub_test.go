package graph

import (
	"context"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// stubReader implements NibReader for testing.
type stubReader struct {
	nibs    map[string]*nib.Nib
	allNibs []*nib.Nib
	cfg     *config.Config
	// prefix, when set, makes NormalizeID resolve short IDs by prepending the
	// prefix — mirroring nibcore.Core.NormalizeID's exact-first, then
	// prefix-prepended behavior.
	prefix string
	// mentionsOut, when populated, is returned by FindMentions keyed on the
	// source (from) nib ID. Tests that need the mention filter paths to
	// return meaningful data seed this directly.
	mentionsOut map[string][]*nib.Nib
	// mentionsIn, when populated, is returned by FindMentionedBy keyed on the
	// target nib ID.
	mentionsIn map[string][]*nib.Nib
	// searchOut, when populated, is returned by Search keyed on the query
	// string. Tests that need queryResolver.Nibs to take its search branch
	// (and therefore its includeAncestors step) seed this directly.
	searchOut map[string][]*nib.Nib
	// searchErr, when set, is what Search reports for every query — the
	// index failure Core.Search surfaces when Bleve cannot answer.
	searchErr error
	// searchCalls counts Search invocations. Core.Search is the expensive read
	// on these paths (a write lock for lazy init, a Bleve query, then a full
	// scan of the store for id matches), so a test can pin how many of them one
	// resolver call costs.
	searchCalls int
}

// Get mirrors nibcore.Core.Get: exact id first, then — if a prefix is
// configured and the input does not already carry it — the prefix-prepended
// form. Resolving here rather than doing a bare map lookup is what lets tests
// hand the filters a short-form stored link (`parent: e1`) directly, without
// the loader pass that would have canonicalized it first.
func (s *stubReader) Get(id string) (*nib.Nib, error) {
	if b, ok := s.nibs[id]; ok {
		return b, nil
	}
	if s.prefix != "" && !strings.HasPrefix(id, s.prefix) {
		if b, ok := s.nibs[s.prefix+id]; ok {
			return b, nil
		}
	}
	return nil, nib.ErrNotFound
}

// GetForUpdate mirrors nibcore.Core.GetForUpdate: return an owned Clone of the
// shared nib (or the not-found error), so mutation sites under test never touch
// the stub's shared pointers.
func (s *stubReader) GetForUpdate(id string) (*nib.Nib, error) {
	b, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	return b.Clone(), nil
}

// GetSnapshot mirrors nibcore.Core.GetSnapshot: return a detached Clone of the
// nib (exact id, then the configured prefix prepended), or ok=false when absent.
func (s *stubReader) GetSnapshot(id string) (*nib.Nib, bool) {
	if b, ok := s.nibs[id]; ok {
		return b.Clone(), true
	}
	if s.prefix != "" && !strings.HasPrefix(id, s.prefix) {
		if b, ok := s.nibs[s.prefix+id]; ok {
			return b.Clone(), true
		}
	}
	return nil, false
}

func (s *stubReader) All() []*nib.Nib {
	return s.allNibs
}

func (s *stubReader) Search(query string) ([]*nib.Nib, error) {
	s.searchCalls++
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	return s.searchOut[query], nil
}

// NormalizeID resolves an id to its full form. It mirrors
// nibcore.Core.NormalizeID: exact match first, then — if a prefix is
// configured and the input does not already carry it — try the
// prefix-prepended form. On miss, it echoes the original id back with
// ok=false (matching Core.NormalizeID's "echo on miss" convention).
func (s *stubReader) NormalizeID(id string) (string, bool) {
	if _, ok := s.nibs[id]; ok {
		return id, true
	}
	if s.prefix != "" && !strings.HasPrefix(id, s.prefix) {
		full := s.prefix + id
		if _, ok := s.nibs[full]; ok {
			return full, true
		}
	}
	return id, false
}

func (s *stubReader) FindIncomingLinks(targetID string) []nib.IncomingLink {
	return nil
}

func (s *stubReader) FindMentions(fromID string) []*nib.Nib {
	return s.mentionsOut[fromID]
}

func (s *stubReader) FindMentionedBy(targetID string) []*nib.Nib {
	return s.mentionsIn[targetID]
}

func (s *stubReader) Config() *config.Config {
	if s.cfg != nil {
		return s.cfg
	}
	return config.Default()
}

// CurrentETag returns the in-memory etag for the requested nib so resolver
// tests that don't exercise on-disk content still compile against the
// extended NibReader interface.
func (s *stubReader) CurrentETag(id string) (string, error) {
	if b, ok := s.nibs[id]; ok {
		return b.ETag(), nil
	}
	if s.prefix != "" && !strings.HasPrefix(id, s.prefix) {
		if b, ok := s.nibs[s.prefix+id]; ok {
			return b.ETag(), nil
		}
	}
	return "", nib.ErrNotFound
}

// stubWriter implements NibWriter for testing.
//
// When store is set, Create/Update reflect the write into the shared stubReader
// map — Create assigns an ID exactly as nibcore.Core.Create does — so a
// resolver's GetSnapshot read-after-write sees the just-written nib, mirroring
// the real store where the reader and writer are the same object. When store is
// nil the writer only records calls (the legacy record-only behavior for tests
// that don't exercise the nib-returning resolver path).
type stubWriter struct {
	store   *stubReader
	created []*nib.Nib
	updated []*nib.Nib
	deleted []string
}

func (s *stubWriter) Create(b *nib.Nib) error {
	if s.store != nil {
		if b.ID == "" {
			prefix, length := "", 4
			if cfg := s.store.Config(); cfg != nil {
				prefix = cfg.Nibs.Prefix
				if cfg.Nibs.IDLength > 0 {
					length = cfg.Nibs.IDLength
				}
			}
			b.ID = nib.NewID(prefix, length)
		}
		if s.store.nibs == nil {
			s.store.nibs = map[string]*nib.Nib{}
		}
		s.store.nibs[b.ID] = b
	}
	s.created = append(s.created, b)
	return nil
}

func (s *stubWriter) Update(b *nib.Nib, ifMatch *string) error {
	if s.store != nil {
		if s.store.nibs == nil {
			s.store.nibs = map[string]*nib.Nib{}
		}
		s.store.nibs[b.ID] = b
	}
	s.updated = append(s.updated, b)
	return nil
}

func (s *stubWriter) Delete(id string) error {
	s.deleted = append(s.deleted, id)
	return nil
}

func (s *stubWriter) Archive(id string) error {
	return nil
}

func (s *stubWriter) RemoveLinksTo(targetID string) (int, error) {
	return 0, nil
}

// stubValidator implements NibValidator for testing.
type stubValidator struct {
	validateParentErr error
	detectCycleResult []string
	validateEnumsErr  error
}

func (s *stubValidator) ValidateParent(b *nib.Nib, parentID string) error {
	return s.validateParentErr
}

func (s *stubValidator) DetectCycle(fromID, linkType, toID string) []string {
	return s.detectCycleResult
}

func (s *stubValidator) ValidateEnums(b *nib.Nib) error {
	return s.validateEnumsErr
}

// stubBlockingChecker implements BlockingChecker for testing.
type stubBlockingChecker struct {
	blocked  map[string]bool
	blocking map[string]bool
}

func (s *stubBlockingChecker) IsBlocked(nibID string) bool {
	return s.blocked[nibID]
}

func (s *stubBlockingChecker) IsBlocking(nibID string) bool {
	return s.blocking[nibID]
}

// TestQueryNibWithStub verifies the Nib query resolver works through the NibReader interface.
func TestQueryNibWithStub(t *testing.T) {
	testNib := &nib.Nib{ID: "test-1", Title: "Test Nib", Status: "todo"}
	reader := &stubReader{
		nibs:    map[string]*nib.Nib{"test-1": testNib},
		allNibs: []*nib.Nib{testNib},
	}

	writer := &stubWriter{}
	resolver := &Resolver{
		Reader:    reader,
		Writer:    writer,
		Validator: &stubValidator{},
		Blocking:  &stubBlockingChecker{},
		Orderer:   NewOrderer(reader, writer),
	}

	ctx := context.Background()

	t.Run("found", func(t *testing.T) {
		got, err := resolver.Query().Nib(ctx, "test-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil || got.ID != "test-1" {
			t.Errorf("got %v, want nib with ID test-1", got)
		}
	})

	t.Run("not found returns nil", func(t *testing.T) {
		got, err := resolver.Query().Nib(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
}

// TestQueryNibsWithStub verifies the Nibs query resolver works through interfaces.
func TestQueryNibsWithStub(t *testing.T) {
	nib1 := &nib.Nib{ID: "n-1", Title: "First", Status: "todo", Type: "task"}
	nib2 := &nib.Nib{ID: "n-2", Title: "Second", Status: "completed", Type: "bug"}
	reader := &stubReader{
		nibs:    map[string]*nib.Nib{"n-1": nib1, "n-2": nib2},
		allNibs: []*nib.Nib{nib1, nib2},
	}
	writer := &stubWriter{}

	resolver := &Resolver{
		Reader:    reader,
		Writer:    writer,
		Validator: &stubValidator{},
		Blocking:  &stubBlockingChecker{},
		Orderer:   NewOrderer(reader, writer),
	}

	ctx := context.Background()

	t.Run("no filter returns all", func(t *testing.T) {
		got, err := resolver.Query().Nibs(ctx, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("got %d nibs, want 2", len(got))
		}
	})

	t.Run("status filter", func(t *testing.T) {
		filter := &model.NibFilter{Status: []string{"todo"}}
		got, err := resolver.Query().Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != "n-1" {
			t.Errorf("got %v, want [n-1]", got)
		}
	})
}

// TestArchiveNibWithStub verifies the archiveNib resolver works through interfaces.
func TestArchiveNibWithStub(t *testing.T) {
	testNib := &nib.Nib{ID: "test-1", Title: "Test Nib", Status: "todo"}
	reader := &stubReader{
		nibs: map[string]*nib.Nib{"test-1": testNib},
	}
	writer := &stubWriter{}

	resolver := &Resolver{
		Reader:    reader,
		Writer:    writer,
		Validator: &stubValidator{},
		Blocking:  &stubBlockingChecker{},
		Orderer:   NewOrderer(reader, writer),
	}

	t.Run("archives existing nib", func(t *testing.T) {
		got, err := resolver.Mutation().ArchiveNib(context.Background(), "test-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Error("expected true, got false")
		}
	})

	t.Run("returns error for nonexistent nib", func(t *testing.T) {
		_, err := resolver.Mutation().ArchiveNib(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent nib")
		}
	})
}

// TestCreateNibWithStub verifies mutation orchestration through interfaces.
func TestCreateNibWithStub(t *testing.T) {
	reader := &stubReader{
		nibs: map[string]*nib.Nib{},
	}
	writer := &stubWriter{store: reader}

	resolver := &Resolver{
		Reader:    reader,
		Writer:    writer,
		Validator: &stubValidator{},
		Blocking:  &stubBlockingChecker{},
		Orderer:   NewOrderer(reader, writer),
	}

	ctx := context.Background()

	t.Run("creates with title and defaults", func(t *testing.T) {
		got, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title: "New Task",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Title != "New Task" {
			t.Errorf("title = %q, want %q", got.Title, "New Task")
		}
		if got.Type != "task" {
			t.Errorf("type = %q, want %q", got.Type, "task")
		}
		if len(writer.created) != 1 {
			t.Fatalf("expected 1 create call, got %d", len(writer.created))
		}
	})
}

// TestStubReaderNormalizeIDMirrorsCoreContract pins the stub's NormalizeID
// against the real nibcore.Core.NormalizeID over a representative input
// table. The stub duplicates Core's "exact-first, then prefix-prepended"
// resolution rule; this test fires if the two diverge on any of the
// inputs below — covering the short/full/unknown/empty/prefix-only shapes.
//
// Scope: the guarantee is "no drift on the patterns this table exercises."
// If Core ever gains a branch that fires only on a genuinely new input
// shape (e.g. case-insensitive fallback on mixed-case IDs, UUID lookup on
// UUID-formatted IDs, archive resolution on archived-nib IDs), the input
// list here must be extended to detect it. Treat this as a representative
// contract test, not an exhaustive one.
func TestStubReaderNormalizeIDMirrorsCoreContract(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DefaultWithPrefix("test-")
	core := nibcore.New(tmpDir, cfg)
	if err := core.Load(); err != nil {
		t.Fatalf("core.Load: %v", err)
	}
	// Seed a couple of nibs so the maps have content to resolve against.
	for _, id := range []string{"test-abc", "test-def"} {
		if err := core.Create(&nib.Nib{ID: id, Title: id, Status: "todo"}); err != nil {
			t.Fatalf("core.Create(%q): %v", id, err)
		}
	}
	// Mirror Core's nib set into the stub.
	allNibs := core.All()
	nibMap := make(map[string]*nib.Nib, len(allNibs))
	for _, b := range allNibs {
		nibMap[b.ID] = b
	}
	stub := &stubReader{nibs: nibMap, prefix: "test-"}

	inputs := []string{
		"abc",           // short form → resolves to test-abc
		"test-abc",      // exact full form
		"def",           // short form → resolves to test-def
		"test-def",      // exact full form
		"nope",          // unknown short
		"test-nope",     // unknown full
		"",              // empty
		"test-",         // prefix-only, no id body
		"test-test-abc", // double-prefixed, not in map
	}

	for _, input := range inputs {
		t.Run("input="+input, func(t *testing.T) {
			coreID, coreOk := core.NormalizeID(input)
			stubID, stubOk := stub.NormalizeID(input)
			if coreID != stubID || coreOk != stubOk {
				t.Errorf("NormalizeID(%q) drift: core=(%q,%t), stub=(%q,%t) — update stubReader.NormalizeID to match Core",
					input, coreID, coreOk, stubID, stubOk)
			}
		})
	}
}
