package graph

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/store"
)

// readRecorder serves every store read a resolver can make from a real Core and
// records the name of each one. Config and ValidateEnums read only the fixed
// config, so they are not store reads and are not recorded.
type readRecorder struct {
	core  *nibcore.Core
	reads []string
}

func (r *readRecorder) note(name string) { r.reads = append(r.reads, name) }

func (r *readRecorder) AwaitWriters() { r.note("AwaitWriters"); r.core.AwaitWriters() }
func (r *readRecorder) Get(id string) (*nib.Nib, error) {
	r.note("Get")
	return r.core.Get(id)
}
func (r *readRecorder) GetForUpdate(id string) (*nib.Nib, error) {
	r.note("GetForUpdate")
	return r.core.GetForUpdate(id)
}
func (r *readRecorder) GetSnapshot(id string) (*nib.Nib, bool) {
	r.note("GetSnapshot")
	return r.core.GetSnapshot(id)
}
func (r *readRecorder) All() []*nib.Nib { r.note("All"); return r.core.All() }
func (r *readRecorder) Search(q string) ([]*nib.Nib, error) {
	r.note("Search")
	return r.core.Search(q)
}
func (r *readRecorder) SearchAll(q string) ([]*nib.Nib, error) {
	r.note("SearchAll")
	return r.core.SearchAll(q)
}
func (r *readRecorder) NormalizeID(id string) (string, bool) {
	r.note("NormalizeID")
	return r.core.NormalizeID(id)
}
func (r *readRecorder) FindIncomingLinks(id string) []nib.IncomingLink {
	r.note("FindIncomingLinks")
	return r.core.FindIncomingLinks(id)
}
func (r *readRecorder) FindMentions(id string) []*nib.Nib {
	r.note("FindMentions")
	return r.core.FindMentions(id)
}
func (r *readRecorder) FindMentionedBy(id string) []*nib.Nib {
	r.note("FindMentionedBy")
	return r.core.FindMentionedBy(id)
}
func (r *readRecorder) Config() *config.Config { return r.core.Config() }
func (r *readRecorder) Areas() *area.Vocabulary {
	r.note("Areas")
	return r.core.Areas()
}
func (r *readRecorder) CurrentETag(id string) (string, error) {
	r.note("CurrentETag")
	return r.core.CurrentETag(id)
}
func (r *readRecorder) ValidateParent(b *nib.Nib, parentID string) error {
	r.note("ValidateParent")
	return r.core.ValidateParent(b, parentID)
}
func (r *readRecorder) DetectCycle(fromID, linkType, toID string) []string {
	r.note("DetectCycle")
	return r.core.DetectCycle(fromID, linkType, toID)
}
func (r *readRecorder) ValidateEnums(b *nib.Nib) error { return r.core.ValidateEnums(b) }
func (r *readRecorder) ValidateArea(b *nib.Nib) error {
	r.note("ValidateArea")
	return r.core.ValidateArea(b)
}
func (r *readRecorder) IsBlocked(id string) bool  { r.note("IsBlocked"); return r.core.IsBlocked(id) }
func (r *readRecorder) IsBlocking(id string) bool { r.note("IsBlocking"); return r.core.IsBlocking(id) }

// TestEveryMutationWaitsForWritersBeforeItsFirstRead calls every mutation
// resolver, found by reflection, with zero arguments except where reachRead
// supplies the ones it needs to read at all. Most refuse early, but whatever
// they read first must be a wait. See NibReader.AwaitWriters.
func TestEveryMutationWaitsForWritersBeforeItsFirstRead(t *testing.T) {
	nibsDir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(store.NewLayout(nibsDir).DataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	core := nibcore.New(nibsDir, config.Default())
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	rec := &readRecorder{core: core}
	r := &Resolver{Reader: rec, Writer: core, Validator: rec, Blocking: rec, Orderer: NewOrderer(rec, core), AreaWriter: core}

	// The area edits read nothing before Core takes c.mu, so they need no wait.
	readsNothing := map[string]bool{"AddArea": true, "UpdateArea": true, "RemoveArea": true}
	first := true
	reachRead := map[string]func(args []reflect.Value){
		"ReorderSiblings": func(args []reflect.Value) {
			args[1] = reflect.ValueOf([]string{"absent"})
			args[4] = reflect.ValueOf(&first)
		},
	}

	mutations := reflect.ValueOf(r.Mutation())
	contract := reflect.TypeOf((*MutationResolver)(nil)).Elem()
	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	for i := range contract.NumMethod() {
		method := contract.Method(i)
		t.Run(method.Name, func(t *testing.T) {
			fn := mutations.MethodByName(method.Name)
			args := make([]reflect.Value, fn.Type().NumIn())
			for a := range args {
				if in := fn.Type().In(a); in == ctxType {
					args[a] = reflect.ValueOf(context.Background())
				} else {
					args[a] = reflect.Zero(in)
				}
			}
			if fill := reachRead[method.Name]; fill != nil {
				fill(args)
			}
			rec.reads = nil
			fn.Call(args)
			switch {
			case len(rec.reads) == 0 && !readsNothing[method.Name]:
				t.Errorf("%s read nothing, so this check cannot see its wait; give it arguments in reachRead", method.Name)
			case len(rec.reads) > 0 && rec.reads[0] != "AwaitWriters" && rec.reads[0] != "GetForUpdate":
				t.Errorf("%s read %v before waiting for writers", method.Name, rec.reads[0])
			}
		})
	}
}
