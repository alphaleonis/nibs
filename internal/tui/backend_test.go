package tui

import (
	"context"
	"sync"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

// Compile-time checks: both implementations must satisfy Backend.
var (
	_ Backend = (*RealBackend)(nil)
	_ Backend = (*StubBackend)(nil)
)

// StubBackend implements Backend for testing.
type StubBackend struct {
	Nibs    map[string]*nib.Nib
	AllNibs []*nib.Nib
	RootDir string

	// Call recording
	UpdateCalls         []stubUpdateCall
	CreateCalls         []model.CreateNibInput
	SetParentCalls      []stubSetParentCall
	AddBlockingCalls    []stubBlockingCall
	RemoveBlockingCalls []stubBlockingCall
	ReorderCalls        []stubReorderCall
	ArchiveCalls        []string
	DeleteCalls         []string

	// Blocking status
	Blocked  map[string]bool
	Blocking map[string]bool

	// Error injection
	ListErr    error
	GetErr     error
	UpdateErr  error
	CreateErr  error
	ReorderErr error
	ArchiveErr error
	DeleteErr  error
}

type stubUpdateCall struct {
	ID    string
	Input model.UpdateNibInput
}

type stubSetParentCall struct {
	ID       string
	ParentID *string
	IfMatch  *string
}

type stubBlockingCall struct {
	ID       string
	TargetID string
}

func (s *StubBackend) GetNib(_ context.Context, id string) (*nib.Nib, error) {
	if s.GetErr != nil {
		return nil, s.GetErr
	}
	if n, ok := s.Nibs[id]; ok {
		return n, nil
	}
	return nil, nil // matches resolver behavior: not found -> nil, nil
}

func (s *StubBackend) ListNibs(_ context.Context, _ *model.NibFilter) ([]*nib.Nib, error) {
	if s.ListErr != nil {
		return nil, s.ListErr
	}
	return s.AllNibs, nil
}

func (s *StubBackend) GetParent(_ context.Context, obj *nib.Nib) (*nib.Nib, error) {
	if obj.Parent == "" {
		return nil, nil
	}
	if n, ok := s.Nibs[obj.Parent]; ok {
		return n, nil
	}
	return nil, nil
}

func (s *StubBackend) GetChildren(_ context.Context, obj *nib.Nib, _ *model.NibFilter) ([]*nib.Nib, error) {
	var children []*nib.Nib
	for _, n := range s.AllNibs {
		if n.Parent == obj.ID {
			children = append(children, n)
		}
	}
	return children, nil
}

func (s *StubBackend) GetBlockedBy(_ context.Context, _ *nib.Nib, _ *model.NibFilter) ([]*nib.Nib, error) {
	return nil, nil
}

func (s *StubBackend) GetBlocking(_ context.Context, _ *nib.Nib, _ *model.NibFilter) ([]*nib.Nib, error) {
	return nil, nil
}

func (s *StubBackend) IsBlocked(nibID string) bool {
	return s.Blocked[nibID]
}

func (s *StubBackend) IsBlocking(nibID string) bool {
	return s.Blocking[nibID]
}

func (s *StubBackend) CreateNib(_ context.Context, input model.CreateNibInput) (*nib.Nib, error) {
	if s.CreateErr != nil {
		return nil, s.CreateErr
	}
	s.CreateCalls = append(s.CreateCalls, input)
	b := &nib.Nib{ID: "stub-new", Title: input.Title, Type: "task", Status: "draft"}
	return b, nil
}

func (s *StubBackend) UpdateNib(_ context.Context, id string, input model.UpdateNibInput) (*nib.Nib, error) {
	if s.UpdateErr != nil {
		return nil, s.UpdateErr
	}
	s.UpdateCalls = append(s.UpdateCalls, stubUpdateCall{ID: id, Input: input})
	if n, ok := s.Nibs[id]; ok {
		return n, nil
	}
	return &nib.Nib{ID: id}, nil
}

func (s *StubBackend) SetParent(_ context.Context, id string, parentID *string, ifMatch *string) (*nib.Nib, error) {
	s.SetParentCalls = append(s.SetParentCalls, stubSetParentCall{ID: id, ParentID: parentID, IfMatch: ifMatch})
	if n, ok := s.Nibs[id]; ok {
		return n, nil
	}
	return &nib.Nib{ID: id}, nil
}

func (s *StubBackend) AddBlocking(_ context.Context, id string, targetID string) (*nib.Nib, error) {
	s.AddBlockingCalls = append(s.AddBlockingCalls, stubBlockingCall{ID: id, TargetID: targetID})
	if n, ok := s.Nibs[id]; ok {
		return n, nil
	}
	return &nib.Nib{ID: id}, nil
}

func (s *StubBackend) RemoveBlocking(_ context.Context, id string, targetID string) (*nib.Nib, error) {
	s.RemoveBlockingCalls = append(s.RemoveBlockingCalls, stubBlockingCall{ID: id, TargetID: targetID})
	if n, ok := s.Nibs[id]; ok {
		return n, nil
	}
	return &nib.Nib{ID: id}, nil
}

func (s *StubBackend) ArchiveNib(_ context.Context, id string) error {
	if s.ArchiveErr != nil {
		return s.ArchiveErr
	}
	s.ArchiveCalls = append(s.ArchiveCalls, id)
	return nil
}

func (s *StubBackend) DeleteNib(_ context.Context, id string) error {
	if s.DeleteErr != nil {
		return s.DeleteErr
	}
	s.DeleteCalls = append(s.DeleteCalls, id)
	return nil
}

type stubReorderCall struct {
	ID       string
	AfterID  *string
	BeforeID *string
	First    *bool
}

func (s *StubBackend) ReorderNib(_ context.Context, id string, afterID, beforeID *string, first *bool) (*nib.Nib, error) {
	if s.ReorderErr != nil {
		return nil, s.ReorderErr
	}
	s.ReorderCalls = append(s.ReorderCalls, stubReorderCall{ID: id, AfterID: afterID, BeforeID: beforeID, First: first})
	if n, ok := s.Nibs[id]; ok {
		return n, nil
	}
	return &nib.Nib{ID: id}, nil
}

func (s *StubBackend) Root() string {
	return s.RootDir
}

func (s *StubBackend) ReloadAfterEdit(_ string) (*nib.Nib, error) {
	return nil, nil
}

func (s *StubBackend) StartWatching() error {
	return nil
}

func (s *StubBackend) StopWatching() {}

func (s *StubBackend) Subscribe() (events <-chan struct{}, cancel func()) {
	ch := make(chan struct{})
	var once sync.Once
	return ch, func() { once.Do(func() { close(ch) }) }
}

// TestNewAppWithBackend verifies the App can be constructed with a stub Backend.
func TestNewAppWithBackend(t *testing.T) {
	stub := &StubBackend{
		Nibs: map[string]*nib.Nib{
			"test-1": {ID: "test-1", Title: "First", Status: "todo", Type: "task"},
		},
		AllNibs: []*nib.Nib{
			{ID: "test-1", Title: "First", Status: "todo", Type: "task"},
		},
	}

	cfg := config.Default()
	app := New(stub, cfg, "dev")

	if app == nil {
		t.Fatal("New() returned nil")
	}

	// Verify the app initializes and renders without panicking
	cmd := app.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil command; expected initial nib load")
	}

	view := app.View()
	if view.Content == "" {
		t.Error("View() returned empty content")
	}
}
