package tui

import (
	"context"

	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/safetext"
)

// displayBackend is the TUI's rendering boundary for the text of a nib. Every
// nib it returns is a display copy whose title, status, type, priority,
// estimate, tags, documents and body went through safetext. It implements each
// Backend method itself, so a method added to Backend does not compile until it
// is written here too.
//
// The id, the link fields and the area stay as stored, because lookups and
// mutations key on them. The views strip them where they draw them: fitText for
// list cells, displayID for ids, and renderStatusMessage for status lines. The
// TUI writes by id and sends only the values the user picked, so no write
// carries a display copy's text to disk.
type displayBackend struct{ inner Backend }

// displayNib returns n when none of its free text needs sanitizing, or else a
// sanitized clone. It never modifies n, which belongs to the inner Backend.
func displayNib(n *nib.Nib) *nib.Nib {
	if n == nil {
		return nil
	}
	title, status, typ := safetext.StripLine(n.Title), safetext.StripLine(n.Status), safetext.StripLine(n.Type)
	priority, estimate := safetext.StripLine(n.Priority), safetext.StripLine(n.Estimate)
	body := safetext.StripText(n.Body)
	tags, tagsChanged := stripEach(n.Tags)
	documents, documentsChanged := stripEach(n.Documents)
	if title == n.Title && status == n.Status && typ == n.Type && priority == n.Priority &&
		estimate == n.Estimate && body == n.Body && !tagsChanged && !documentsChanged {
		return n
	}
	d := n.Clone()
	d.Title, d.Status, d.Type, d.Priority, d.Estimate, d.Body = title, status, typ, priority, estimate, body
	if tagsChanged {
		d.Tags = tags
	}
	if documentsChanged {
		d.Documents = documents
	}
	return d
}

// stripEach returns ss with StripLine applied to each element, and whether
// that changed any. It returns ss itself when nothing changed.
func stripEach(ss []string) ([]string, bool) {
	var out []string
	for i, s := range ss {
		stripped := safetext.StripLine(s)
		if stripped == s && out == nil {
			continue
		}
		if out == nil {
			out = append(make([]string, 0, len(ss)), ss[:i]...)
		}
		out = append(out, stripped)
	}
	if out == nil {
		return ss, false
	}
	return out, true
}

// displayID returns id as the TUI draws it. Copy, write and look up a nib by
// the stored id.
func displayID(id string) string { return safetext.StripLine(id) }

func displayNibs(ns []*nib.Nib, err error) ([]*nib.Nib, error) {
	if ns == nil {
		return nil, err
	}
	out := make([]*nib.Nib, len(ns))
	for i, n := range ns {
		out[i] = displayNib(n)
	}
	return out, err
}

func displayOne(n *nib.Nib, err error) (*nib.Nib, error) { return displayNib(n), err }

func (d displayBackend) GetNib(ctx context.Context, id string) (*nib.Nib, error) {
	return displayOne(d.inner.GetNib(ctx, id))
}

func (d displayBackend) ListNibs(ctx context.Context, filter *model.NibFilter) ([]*nib.Nib, error) {
	return displayNibs(d.inner.ListNibs(ctx, filter))
}

func (d displayBackend) GetParent(ctx context.Context, obj *nib.Nib) (*nib.Nib, error) {
	return displayOne(d.inner.GetParent(ctx, obj))
}

func (d displayBackend) GetChildren(ctx context.Context, obj *nib.Nib, filter *model.NibFilter) ([]*nib.Nib, error) {
	return displayNibs(d.inner.GetChildren(ctx, obj, filter))
}

func (d displayBackend) GetBlockedBy(ctx context.Context, obj *nib.Nib, filter *model.NibFilter) ([]*nib.Nib, error) {
	return displayNibs(d.inner.GetBlockedBy(ctx, obj, filter))
}

func (d displayBackend) GetBlocking(ctx context.Context, obj *nib.Nib, filter *model.NibFilter) ([]*nib.Nib, error) {
	return displayNibs(d.inner.GetBlocking(ctx, obj, filter))
}

func (d displayBackend) GetMentions(ctx context.Context, obj *nib.Nib, filter *model.NibFilter) ([]*nib.Nib, error) {
	return displayNibs(d.inner.GetMentions(ctx, obj, filter))
}

func (d displayBackend) GetMentionedBy(ctx context.Context, obj *nib.Nib, filter *model.NibFilter) ([]*nib.Nib, error) {
	return displayNibs(d.inner.GetMentionedBy(ctx, obj, filter))
}

func (d displayBackend) IsBlocked(nibID string) bool  { return d.inner.IsBlocked(nibID) }
func (d displayBackend) IsBlocking(nibID string) bool { return d.inner.IsBlocking(nibID) }

func (d displayBackend) CreateNib(ctx context.Context, input model.CreateNibInput) (*nib.Nib, error) {
	return displayOne(d.inner.CreateNib(ctx, input))
}

func (d displayBackend) UpdateNib(ctx context.Context, id string, input model.UpdateNibInput) (*nib.Nib, error) {
	return displayOne(d.inner.UpdateNib(ctx, id, input))
}

func (d displayBackend) SetParent(ctx context.Context, id string, parentID *string, ifMatch *string) (*nib.Nib, error) {
	return displayOne(d.inner.SetParent(ctx, id, parentID, ifMatch))
}

func (d displayBackend) AddBlocking(ctx context.Context, id string, targetID string) (*nib.Nib, error) {
	return displayOne(d.inner.AddBlocking(ctx, id, targetID))
}

func (d displayBackend) RemoveBlocking(ctx context.Context, id string, targetID string) (*nib.Nib, error) {
	return displayOne(d.inner.RemoveBlocking(ctx, id, targetID))
}

func (d displayBackend) ArchiveNib(ctx context.Context, id string) error {
	return d.inner.ArchiveNib(ctx, id)
}

func (d displayBackend) DeleteNib(ctx context.Context, id string) error {
	return d.inner.DeleteNib(ctx, id)
}

func (d displayBackend) ReorderNib(ctx context.Context, id string, afterID, beforeID *string, first *bool, scope model.OrderScope) (*nib.Nib, error) {
	return displayOne(d.inner.ReorderNib(ctx, id, afterID, beforeID, first, scope))
}

func (d displayBackend) Areas() *area.Vocabulary              { return d.inner.Areas() }
func (d displayBackend) Root() string                         { return d.inner.Root() }
func (d displayBackend) ReloadAfterEdit(id string) error      { return d.inner.ReloadAfterEdit(id) }
func (d displayBackend) StartWatching() error                 { return d.inner.StartWatching() }
func (d displayBackend) StopWatching()                        { d.inner.StopWatching() }
func (d displayBackend) Subscribe() (<-chan struct{}, func()) { return d.inner.Subscribe() }

func (d displayBackend) SubscribeAreas() (<-chan struct{}, func()) {
	return d.inner.SubscribeAreas()
}
