package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/alphaleonis/nibs/internal/fsutil"
	"github.com/alphaleonis/nibs/internal/store"
	"gopkg.in/yaml.v3"
)

// Areas is a store's declared area vocabulary, read from <store>/areas.yml.
//
// It lives in its own file rather than in config.yml because it has its own
// LIFETIME. Everything in config.yml is read once at construction and fixed for
// the life of the process — Core.ValidateArea's off-lock read and
// requireIfMatch's beside it both rest on that — while this vocabulary is
// reloaded whenever the file changes, so a `nibs area rename` reaches a running
// `nibs serve` without a restart. Splitting the FILE makes that difference
// something a reader can see, where splitting only the field would have left one
// struct half-live and half-frozen with nothing to mark which half is which.
//
// A nil *Areas is a valid empty vocabulary and every method answers from it: a
// store with no areas.yml declares no areas, which is a normal project.
//
// The value is IMMUTABLE once loaded. A reload builds a new one and swaps the
// pointer, so a reader holding the old one keeps a coherent vocabulary for the
// whole of its decision rather than observing a tree change under it.
type Areas struct {
	// Nodes is the declared forest, in the file's own declaration order.
	Nodes []AreaConfig `yaml:"areas,omitempty"`

	// path is the file this vocabulary was read from — set even when that file
	// does not exist, so a message about a MISSING vocabulary can still name
	// where one belongs. Not serialized.
	path string `yaml:"-"`

	// fromFile records that an areas FILE was read to produce this value,
	// rather than it being the empty vocabulary a store without one gets. It
	// separates a store that never declared areas from one whose areas.yml
	// vanished, which are the same empty tree and different situations: only
	// the second names a file to restore.
	fromFile bool `yaml:"-"`
}

// LoadedFromFile reports that this vocabulary came from an areas file that
// existed, as opposed to the empty one a store without the file gets.
func (a *Areas) LoadedFromFile() bool {
	return a != nil && a.fromFile
}

// Path returns the areas file this vocabulary belongs to, whether or not that
// file exists. Messages that tell a user where to declare or restore a
// vocabulary use it.
func (a *Areas) Path() string {
	if a == nil {
		return ""
	}
	return a.path
}

// StoreDir returns the store directory this vocabulary belongs to.
func (a *Areas) StoreDir() string {
	if a == nil || a.path == "" {
		return ""
	}
	return filepath.Dir(a.path)
}

// LoadAreas reads a vocabulary from an explicit areas.yml path. An absent file
// is an empty vocabulary, not an error — a project that has not declared areas
// is a normal project.
//
// A file that exists and cannot be honored is refused rather than read as
// empty: the vocabulary is AUTHORIZATION data, so silently narrowing it to
// nothing would make every `area:` value a store already carries undeclared,
// and every write to those nibs would be refused with "this store declares no
// areas".
func LoadAreas(path string) (*Areas, error) {
	data, err := ReadConfigFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Areas{path: path}, nil
		}
		return nil, err
	}

	var areas Areas
	if err := yaml.Unmarshal(data, &areas); err != nil {
		return nil, fmt.Errorf("%s is not readable as an areas vocabulary: %w", path, err)
	}
	if err := areas.Validate(); err != nil {
		return nil, fmt.Errorf("%s declares a malformed area vocabulary: %w", path, err)
	}
	areas.fromFile = true
	areas.path = path
	return &areas, nil
}

// LoadAreasFromStore reads the vocabulary that lives inside the store directory
// (<store>/areas.yml). This is the derivation every command uses.
func LoadAreasFromStore(storeDir string) (*Areas, error) {
	return LoadAreas(store.NewLayout(storeDir).AreasPath())
}

// Save writes the vocabulary to <store>/areas.yml, keeping an existing file's
// permission bits the way every other config writer does.
//
// A vocabulary with no nodes REMOVES the file rather than writing an empty
// `areas:` key, so "declares nothing" has one representation on disk instead of
// two that read alike but differ to a reader of the directory.
func (a *Areas) Save(storeDir string) error {
	path := store.NewLayout(storeDir).AreasPath()

	if !a.Declared() {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}

	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return err
	}

	data, err := yaml.Marshal(a)
	if err != nil {
		return err
	}

	perm := os.FileMode(0o644)
	info, statErr := os.Stat(path)
	switch {
	case statErr == nil:
		perm = info.Mode().Perm()
	case !errors.Is(statErr, fs.ErrNotExist):
		return fmt.Errorf("reading the current mode of %s: %w", path, statErr)
	}
	return fsutil.AtomicWriteFile(path, data, perm)
}

// Equal reports whether two vocabularies declare the same forest, node for
// node and field for field.
//
// It is what keeps a reload from waking every consumer over a file that was
// rewritten without changing: an editor that saves unchanged text, a `touch`, or
// a rewrite that only reorders whitespace all produce a file event and no new
// vocabulary. Order IS significant — declaration order is what the areas surface
// renders in — so a reordered file is a genuine change.
func (a *Areas) Equal(other *Areas) bool {
	return slices.EqualFunc(a.Roots(), other.Roots(), equalAreaNode)
}

func equalAreaNode(x, y AreaConfig) bool {
	return x.Name == y.Name &&
		x.Description == y.Description &&
		x.Color == y.Color &&
		x.Order == y.Order &&
		slices.EqualFunc(x.Children, y.Children, equalAreaNode)
}

// AreasFileFor names the areas file belonging to the store a config path sits
// in. Messages that tell a user where to declare a vocabulary use it, so the
// path they read is the one their store actually has.
func AreasFileFor(configPath string) string {
	return store.NewLayout(filepath.Dir(configPath)).AreasPath()
}
