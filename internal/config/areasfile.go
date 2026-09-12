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
// This vocabulary is re-read whenever the file changes; config.yml is loaded
// once. That is why it has its own file.
//
// A nil *Areas answers as the empty vocabulary, so call any method on it
// without a nil check. LoadAreas never returns one: a store with no areas.yml
// gets a non-nil value with no nodes, which LoadedFromFile tells apart.
//
// Treat a loaded value as immutable. A reload builds a new one and swaps the
// pointer; see nibcore.Core.ValidateArea for what reads one off-lock.
type Areas struct {
	// The declared forest, in the file's declaration order.
	Nodes []AreaConfig `yaml:"areas,omitempty"`

	path     string `yaml:"-"`
	fromFile bool   `yaml:"-"`
}

// LoadedFromFile reports whether an areas file was read to produce this
// vocabulary, as opposed to it being the empty one a store without the file
// gets. Compare it before and after a reload to catch a vanished areas.yml.
func (a *Areas) LoadedFromFile() bool {
	return a != nil && a.fromFile
}

// Path returns the areas file this vocabulary belongs to, whether or not that
// file exists.
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
// is an empty vocabulary, not an error.
//
// A file that exists and cannot be honored is refused. Never fall back to an
// empty vocabulary here: that undeclares every `area:` the store already
// carries, and every write to those nibs is then refused.
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
// (<store>/areas.yml).
func LoadAreasFromStore(storeDir string) (*Areas, error) {
	return LoadAreas(store.NewLayout(storeDir).AreasPath())
}

// Save writes the vocabulary to <store>/areas.yml, preserving an existing
// file's permission bits.
//
// A vocabulary with no nodes REMOVES the file, so "declares nothing" has one
// representation on disk.
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

// Equal reports whether two vocabularies declare the same forest, in order. It
// compares the declared tree alone: two equal vocabularies can still differ in
// the file they were read from.
func (a *Areas) Equal(other *Areas) bool {
	return slices.EqualFunc(a.Roots(), other.Roots(), equalAreaNode)
}

// equalAreaNode compares every AreaConfig field; extend it when one is added.
func equalAreaNode(x, y AreaConfig) bool {
	return x.Name == y.Name &&
		x.Description == y.Description &&
		x.Color == y.Color &&
		x.Order == y.Order &&
		slices.EqualFunc(x.Children, y.Children, equalAreaNode)
}

// AreasFileFor names the areas file belonging to the store a config path sits in.
func AreasFileFor(configPath string) string {
	return store.NewLayout(filepath.Dir(configPath)).AreasPath()
}
