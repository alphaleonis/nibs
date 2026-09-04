package graph

import (
	"context"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// stubSubscriber implements NibSubscriber for testing.
// It returns a channel that the test controls directly.
type stubSubscriber struct {
	ch chan []nibcore.NibEvent
	// areasCh carries vocabulary-change ticks, the second stream a subscriber
	// serves.
	areasCh chan struct{}
	// unsubscribed is closed by the returned unsubscribe func. Closing (rather
	// than setting a bool) gives the test a happens-before to wait on before
	// asserting that unsubscribe ran, avoiding a data race with the resolver's
	// background cleanup goroutine.
	unsubscribed chan struct{}
}

func newStubSubscriber() *stubSubscriber {
	return &stubSubscriber{
		ch:           make(chan []nibcore.NibEvent, 16),
		areasCh:      make(chan struct{}, 16),
		unsubscribed: make(chan struct{}),
	}
}

func (s *stubSubscriber) Subscribe() (<-chan []NibEvent, func()) {
	return s.ch, func() { close(s.unsubscribed) }
}

func (s *stubSubscriber) SubscribeAreas() (<-chan struct{}, func()) {
	return s.areasCh, func() {}
}

func TestNibChangedSubscription(t *testing.T) {
	t.Run("emits events from Core", func(t *testing.T) {
		sub := newStubSubscriber()
		reader := &stubReader{nibs: map[string]*nib.Nib{}}
		writer := &stubWriter{}
		resolver := &Resolver{
			Reader:     reader,
			Writer:     writer,
			Validator:  &stubValidator{},
			Blocking:   &stubBlockingChecker{},
			Subscriber: sub,
			Orderer:    NewOrderer(reader, writer),
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ch, err := resolver.Subscription().NibChanged(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		testNib := &nib.Nib{ID: "test-1", Title: "Hello", Status: "todo", Type: "task"}
		sub.ch <- []nibcore.NibEvent{
			{Type: nibcore.EventCreated, Nib: testNib, NibID: "test-1"},
		}

		select {
		case evt := <-ch:
			if evt == nil {
				t.Fatal("got nil event")
				return
			}
			if evt.Type != "created" {
				t.Errorf("type = %q, want %q", evt.Type, "created")
			}
			if evt.NibID != "test-1" {
				t.Errorf("nibId = %q, want %q", evt.NibID, "test-1")
			}
			if evt.Nib == nil || evt.Nib.ID != "test-1" {
				t.Errorf("nib = %v, want nib with ID test-1", evt.Nib)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	})

	t.Run("flattens batched events", func(t *testing.T) {
		sub := newStubSubscriber()
		reader := &stubReader{nibs: map[string]*nib.Nib{}}
		writer := &stubWriter{}
		resolver := &Resolver{
			Reader:     reader,
			Writer:     writer,
			Validator:  &stubValidator{},
			Blocking:   &stubBlockingChecker{},
			Subscriber: sub,
			Orderer:    NewOrderer(reader, writer),
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ch, err := resolver.Subscription().NibChanged(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		nib1 := &nib.Nib{ID: "n-1", Title: "First"}
		nib2 := &nib.Nib{ID: "n-2", Title: "Second"}
		sub.ch <- []nibcore.NibEvent{
			{Type: nibcore.EventCreated, Nib: nib1, NibID: "n-1"},
			{Type: nibcore.EventUpdated, Nib: nib2, NibID: "n-2"},
		}

		// Should receive two individual events from one batch
		var events []*model.NibChangeEvent
		for i := 0; i < 2; i++ {
			select {
			case evt := <-ch:
				events = append(events, evt)
			case <-time.After(time.Second):
				t.Fatalf("timed out after %d events", len(events))
			}
		}

		if events[0].NibID != "n-1" || events[0].Type != "created" {
			t.Errorf("event[0] = {%s, %s}, want {n-1, created}", events[0].NibID, events[0].Type)
		}
		if events[1].NibID != "n-2" || events[1].Type != "updated" {
			t.Errorf("event[1] = {%s, %s}, want {n-2, updated}", events[1].NibID, events[1].Type)
		}
	})

	t.Run("deleted event has nil nib", func(t *testing.T) {
		sub := newStubSubscriber()
		reader := &stubReader{nibs: map[string]*nib.Nib{}}
		writer := &stubWriter{}
		resolver := &Resolver{
			Reader:     reader,
			Writer:     writer,
			Validator:  &stubValidator{},
			Blocking:   &stubBlockingChecker{},
			Subscriber: sub,
			Orderer:    NewOrderer(reader, writer),
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ch, err := resolver.Subscription().NibChanged(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		sub.ch <- []nibcore.NibEvent{
			{Type: nibcore.EventDeleted, Nib: nil, NibID: "gone-1"},
		}

		select {
		case evt := <-ch:
			if evt.Type != "deleted" {
				t.Errorf("type = %q, want %q", evt.Type, "deleted")
			}
			if evt.NibID != "gone-1" {
				t.Errorf("nibId = %q, want %q", evt.NibID, "gone-1")
			}
			if evt.Nib != nil {
				t.Errorf("nib = %v, want nil for deleted event", evt.Nib)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	})

	t.Run("filters by nib ID", func(t *testing.T) {
		sub := newStubSubscriber()
		reader := &stubReader{nibs: map[string]*nib.Nib{}}
		writer := &stubWriter{}
		resolver := &Resolver{
			Reader:     reader,
			Writer:     writer,
			Validator:  &stubValidator{},
			Blocking:   &stubBlockingChecker{},
			Subscriber: sub,
			Orderer:    NewOrderer(reader, writer),
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		filterID := "target-1"
		ch, err := resolver.Subscription().NibChanged(ctx, &filterID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		target := &nib.Nib{ID: "target-1", Title: "Target"}
		other := &nib.Nib{ID: "other-1", Title: "Other"}
		sub.ch <- []nibcore.NibEvent{
			{Type: nibcore.EventUpdated, Nib: other, NibID: "other-1"},
			{Type: nibcore.EventUpdated, Nib: target, NibID: "target-1"},
		}

		// Should only receive the target event, not the other
		select {
		case evt := <-ch:
			if evt.NibID != "target-1" {
				t.Errorf("nibId = %q, want %q", evt.NibID, "target-1")
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for filtered event")
		}

		// No more events should be pending
		select {
		case evt := <-ch:
			t.Errorf("unexpected extra event: %v", evt)
		case <-time.After(100 * time.Millisecond):
			// Good — no extra events
		}
	})

	t.Run("unsubscribes on context cancel", func(t *testing.T) {
		sub := newStubSubscriber()
		reader := &stubReader{nibs: map[string]*nib.Nib{}}
		writer := &stubWriter{}
		resolver := &Resolver{
			Reader:     reader,
			Writer:     writer,
			Validator:  &stubValidator{},
			Blocking:   &stubBlockingChecker{},
			Subscriber: sub,
			Orderer:    NewOrderer(reader, writer),
		}

		ctx, cancel := context.WithCancel(context.Background())

		ch, err := resolver.Subscription().NibChanged(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cancel()

		// Channel should close after context cancellation
		select {
		case _, ok := <-ch:
			// ok == true means we got an event before close (fine, drain it)
			// ok == false means channel closed (expected)
			_ = ok
		case <-time.After(time.Second):
			t.Fatal("channel not closed after context cancel")
		}

		// Wait for the resolver goroutine's deferred cleanup to call unsubscribe.
		// Receiving from the closed channel establishes a happens-before with the
		// stub's write, so the assertion is race-free without a sleep.
		select {
		case <-sub.unsubscribed:
			// unsubscribe was called (channel closed) — expected.
		case <-time.After(time.Second):
			t.Error("unsubscribe was not called")
		}
	})
}

// A vocabulary tick delivers the CURRENT config, read at delivery time. The
// stream carries no payload of its own — the point is that a client re-renders
// from the same shape the initial `config` query gave it.
func TestConfigChangedSubscriptionDeliversTheReloadedVocabulary(t *testing.T) {
	sub := newStubSubscriber()
	reader := &stubReader{nibs: map[string]*nib.Nib{}, areas: areaCfg(config.AreaConfig{Name: "web"})}
	resolver := &Resolver{Reader: reader, Subscriber: sub}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := resolver.Subscription().ConfigChanged(ctx)
	if err != nil {
		t.Fatalf("ConfigChanged: %v", err)
	}

	// The reload: the store now declares `frontend`, and the tick follows it —
	// the order the watcher publishes in.
	reader.areas = areaCfg(config.AreaConfig{Name: "frontend"})
	sub.areasCh <- struct{}{}

	select {
	case got := <-ch:
		if len(got.Areas) != 1 || got.Areas[0].Path != "frontend" {
			t.Errorf("areas = %v, want the reloaded vocabulary [frontend]", pathsOf(got.Areas))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no config delivered after the vocabulary changed")
	}
}

// The stream closes when the client goes away, so a browser that navigated
// off does not leave a subscription attached to the core for the server's life.
func TestConfigChangedSubscriptionClosesOnContextCancel(t *testing.T) {
	sub := newStubSubscriber()
	resolver := &Resolver{Reader: &stubReader{nibs: map[string]*nib.Nib{}}, Subscriber: sub}

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := resolver.Subscription().ConfigChanged(ctx)
	if err != nil {
		t.Fatalf("ConfigChanged: %v", err)
	}
	cancel()

	select {
	case _, open := <-ch:
		if open {
			t.Error("channel delivered a value after cancel, want it closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the subscription channel stayed open after the context was canceled")
	}
}
