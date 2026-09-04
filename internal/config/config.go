package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/alphaleonis/nibs/internal/fsutil"
	"github.com/alphaleonis/nibs/internal/store"
	"gopkg.in/yaml.v3"
)

// DefaultStatuses defines the hardcoded status configuration.
//
// The three closed statuses carry distinct colours: they were all "gray", which
// left deferred, completed and scrapped indistinguishable in the TUI and the CLI
// even though they mean different things. Deferred is magenta because it is the
// odd one out — the work is coming back — while completed and scrapped share a
// gray ramp with scrapped the dimmer of the two. See the closed-status ramp in
// internal/ui/styles.go for why those two grays sit where they do.
// Statuses are not configurable - they are hardcoded like types.
// Order determines sort priority: the open statuses first (in-progress, todo,
// draft), then the closed ones (deferred, completed, scrapped) last. Pickers
// list statuses in a different order — see workflowStatusOrder.
var DefaultStatuses = []StatusConfig{
	{Name: "in-progress", Color: "yellow", Role: RoleOpen, Description: "Currently being worked on"},
	{Name: "todo", Color: "green", Role: RoleStartable, Description: "Ready to be worked on"},
	{Name: "draft", Color: "blue", Role: RoleOpen, Description: "Needs refinement before it can be worked on"},
	{Name: "deferred", Color: "magenta", Role: RoleParked, Description: "Set aside — a good idea at the wrong time; closed, but kept as a seed rather than a dead end"},
	{Name: "completed", Color: "lightgray", Role: RoleDone, Description: "Finished successfully"},
	{Name: "scrapped", Color: "dimgray", Role: RoleDropped, Description: "Will not be done"},
}

// Status group names — one value vocabulary across every surface that accepts
// a group where a concrete status goes: the CLI's `-s open` (cmd/statusfilter.go)
// and the web filter box's `status:open` (via the generated vocabulary,
// internal/webvocab). Membership is not declared here — each surface derives
// it from the roles (OpenStatusNames/ClosedStatusNames); the names are the one
// part that cannot be derived, so they live once.
const (
	StatusGroupOpen   = "open"
	StatusGroupClosed = "closed"
)

// workflowStatusOrder lists the statuses in transition order — the path work
// actually takes, from draft through to a closed state. It exists because the
// two ways a status list gets shown want opposite orders: a *chooser* reads
// best as the flow (what comes next?), while a *list* reads best with the work
// that is underway at the top. DefaultStatuses is the second one, and its order
// is the primary sort key of nib.SortByStatusPriorityAndType, so reordering it
// into a workflow would push in-progress work off the top of every list,
// archive and roadmap. Two orders, one vocabulary.
//
// Read through WorkflowStatuses/WorkflowStatusNames by the TUI status picker
// (internal/tui/statuspicker.go) and, via the generated vocabulary
// (internal/webvocab), by the web's status select and row context menu.
//
// Membership is not restated: TestWorkflowStatusOrderCoversEveryStatus requires
// this list and DefaultStatuses to hold the same names, and orderStatusesBy
// appends anything missing rather than dropping it, so a status added to
// DefaultStatuses and forgotten here is still offered by every picker.
var workflowStatusOrder = []string{"draft", "todo", "in-progress", "completed", "deferred", "scrapped"}

// DefaultTypes defines the default type configuration.
var DefaultTypes = []TypeConfig{
	{Name: "milestone", Color: "cyan", Description: "A target release or checkpoint; group work that should ship together"},
	{Name: "epic", Color: "purple", Description: "A deliverable that tops the work tree; should have child nibs, not be worked on directly"},
	{Name: "bug", Color: "red", Description: "Something that is broken and needs fixing"},
	{Name: "feature", Color: "green", Description: "A user-facing capability or enhancement"},
	{Name: "task", Color: "blue", Description: "A concrete piece of work to complete (eg. a chore, or a sub-task for a feature)"},
	{Name: "research", Color: "yellow", Description: "Exploratory work whose output is knowledge or decisions, not code"},
}

// DefaultPriorities defines the hardcoded priority configuration.
// Priorities are ordered from highest to lowest urgency.
var DefaultPriorities = []PriorityConfig{
	{Name: "critical", Color: "red", Description: "Urgent, blocking work. When possible, address immediately"},
	{Name: "high", Color: "yellow", Description: "Important, should be done before normal work"},
	{Name: "normal", Color: "white", Description: "Standard priority"},
	{Name: "low", Color: "gray", Description: "Less important, can be delayed"},
}

// DefaultEstimates defines the hardcoded estimate configuration.
// Estimates are t-shirt sizes ordered from smallest to largest.
var DefaultEstimates = []EstimateConfig{
	{Name: "s", Color: "blue", Description: "Small (1 point)"},
	{Name: "m", Color: "white", Description: "Medium (3 points)"},
	{Name: "l", Color: "yellow", Description: "Large (5 points)"},
	{Name: "xl", Color: "red", Description: "Extra Large (8 points)"},
}

// EstimateConfig defines a single estimate size with its display color.
type EstimateConfig struct {
	Name        string `yaml:"name"`
	Color       string `yaml:"color"`
	Description string `yaml:"description,omitempty"`
}

// StatusConfig defines a single status with its display color.
//
// A status's Role is its whole classification (see Role): which of the five
// lifecycle positions the status occupies. The three group predicates —
// closed, releases-dependents, startable — derive from the role, so of the
// eight states three independent booleans could express, only the legal
// combinations are representable: every role IS one legal row of that table,
// and the illegal rows (an open status releasing its dependents, a startable
// closed status) have no role to express them. What the role adds over the
// flags is the done/dropped split: completed and scrapped share every
// predicate and differ only in whether the work counts as accomplished, which
// is the distinction progress arithmetic keys on.
//
// TestStatusRoles pins each status's role, and TestStatusRoleGroupsAreNonEmpty
// requires each derived group to be NON-EMPTY, which is not a stylistic
// nicety: a derived set that empties out fails open rather than closed.
// Emptying Startable made `nibs list --ready` widen from "only startable"
// to every unblocked nib — 86 of 89 on the sample fixture, including completed
// and scrapped work — because an empty include-list filters nothing.
//
// None of the derived sets is interchangeable with another. Deferred is closed
// and still blocks, so collapsing Closed and ReleasesDependents into one
// answer would silently unblock deferred work. Startable is strictly narrower
// than "not closed": draft and in-progress are open and not startable, so
// reading Startable off the Closed answer would put work that is already
// underway or not yet refined into the ready queue.
//
// In Go the roles are the only definitions of their sets — consumers read
// them through IsClosedStatus/ClosedStatusNames/OpenStatusNames,
// StatusReleasesDependents/ReleasingStatusNames, HoldingStatusNames for the
// closed-but-still-blocking difference,
// IsStartableStatus/StartableStatusNames for the ready queue, and StatusRole
// for the done/dropped split the group predicates cannot see. The web UI
// derives its copy from the generated vocabulary (internal/webvocab), pinned
// fresh by TestGeneratedVocabularyIsFresh. README.md's Data Model section is a
// hand-written copy — there is no render step behind it — held to these roles
// by cmd/readme_test.go rather than by derivation.
//
// Sites that name one specific status are not rival definitions of these sets,
// because a group predicate cannot single a member out — but renaming a status
// means visiting them. cmd/dedup.go names "scrapped" to attach the
// scrap-reason snippet; internal/ui abbreviates "deferred" to F so it does not
// collide with draft.
type StatusConfig struct {
	Name        string `yaml:"name"`
	Color       string `yaml:"color"`
	Role        Role   `yaml:"-"`
	Description string `yaml:"description,omitempty"`
}

// TypeConfig defines a single nib type with its display color.
type TypeConfig struct {
	Name        string `yaml:"name"`
	Color       string `yaml:"color"`
	Description string `yaml:"description,omitempty"`
}

// PriorityConfig defines a single priority level with its display color.
type PriorityConfig struct {
	Name        string `yaml:"name"`
	Color       string `yaml:"color"`
	Description string `yaml:"description,omitempty"`
}

// Config holds the nibs configuration.
// Note: Statuses are no longer stored in config - they are hardcoded like types.
type Config struct {
	Nibs NibsConfig `yaml:"nibs"`

	// storeDir is the `.nibs` directory this config was read from (not
	// serialized). Everything positional about a project derives from it: the
	// config file's own location, the data and archive directories, and the
	// project name (its PARENT directory's name).
	storeDir string `yaml:"-"`

	// fromFile records that a config FILE was read to produce these values,
	// rather than them being the defaults a store without one gets. See
	// LoadedFromFile for the distinction this exists to keep answerable.
	fromFile bool `yaml:"-"`
}

// NibsConfig defines settings for nib creation.
type NibsConfig struct {
	Prefix         string       `yaml:"prefix"`
	IDLength       int          `yaml:"id_length"`
	DefaultStatus  string       `yaml:"default_status,omitempty"`
	DefaultType    string       `yaml:"default_type,omitempty"`
	RequireIfMatch bool         `yaml:"require_if_match,omitempty"`
	AutoActivation bool         `yaml:"auto_activation,omitempty"`
	HideCompleted  *bool        `yaml:"hide_completed,omitempty"`
	WideMode       *bool        `yaml:"wide_mode,omitempty"`
	Server         ServerConfig `yaml:"server,omitempty"`
}

// ServerConfig defines settings for the HTTP server.
type ServerConfig struct {
	Port        *int  `yaml:"port,omitempty"`
	OpenBrowser *bool `yaml:"open_browser,omitempty"`
}

// The system defaults: what an unset key means. Named rather than repeated,
// because they have to be identical in two places — Default(), which nibs init
// persists into a new project config, and applySystemDefaults, which fills the
// gaps in a config that omits them. Those two disagreed about the type: the
// fallback read DefaultTypes[0], and that list is ordered by HIERARCHY DEPTH, not
// by which entry is a sensible default, so it answered "milestone" while every
// config nibs init wrote said "task".
const (
	defaultIDLength   = 4
	defaultStatusName = "todo"
	defaultTypeName   = "task"
)

// Default returns a Config with default values.
func Default() *Config {
	return &Config{
		Nibs: NibsConfig{
			Prefix:        "",
			IDLength:      defaultIDLength,
			DefaultStatus: defaultStatusName,
			DefaultType:   defaultTypeName,
			HideCompleted: boolPtr(true),
			WideMode:      boolPtr(true),
		},
	}
}

// boolPtr returns a pointer to the given bool value.
func boolPtr(b bool) *bool {
	return &b
}

// DefaultWithPrefix returns a Config with the given prefix.
func DefaultWithPrefix(prefix string) *Config {
	cfg := Default()
	cfg.Nibs.Prefix = prefix
	return cfg
}

// Load reads configuration from the given config file path, taking the file's
// containing directory as the store. Returns a default config if the file
// doesn't exist.
func Load(configPath string) (*Config, error) {
	cfg, err := loadRaw(configPath)
	if err != nil {
		return nil, err
	}
	applySystemDefaults(cfg)
	return cfg, nil
}

// LoadFromStore reads the config that lives INSIDE the store directory
// (<store>/config.yml). This is the derivation every command uses: the store
// is located first, and its config is read from within it.
func LoadFromStore(storeDir string) (*Config, error) {
	return Load(store.NewLayout(storeDir).ConfigPath())
}

// retiredPathProbe detects a `nibs.path:` key, which the store layout retired.
// The key used to point the config at a data directory somewhere else; the
// store directory now IS the data directory's parent, so a config still
// carrying it describes a layout this build cannot honor. Refusing loudly
// beats silently reading the key's value as decoration and operating on a
// different directory than the user wrote down.
//
// It is also what RetiredNibsPath reads on behalf of the CLI, so the shape of
// the retired key is declared once rather than re-transcribed per caller.
type retiredPathProbe struct {
	Nibs struct {
		Path string `yaml:"path"`
	} `yaml:"nibs"`
}

// RetiredNibsPath returns the retired `nibs.path` value a pre-layout config
// carries. The answer is THREE-WAY, because one of its callers decides whether
// `nibs migrate` may rewrite a directory and "I could not read the evidence" is
// not the same authorization answer as "there is no evidence":
//
//   - ("", nil)      the file is absent, or present and simply does not set the key;
//   - (value, nil)   the key is set;
//   - ("", err)      the file EXISTS but its content could not be established —
//     unreadable, over MaxConfigBytes, or not YAML at all.
//
// A caller that only sharpens a message may discard the error; a caller making a
// decision from the answer must report "cannot determine" instead of guessing.
func RetiredNibsPath(path string) (string, error) {
	data, err := ReadConfigFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	var probe retiredPathProbe
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return "", fmt.Errorf("parsing %s: %w", path, err)
	}
	return probe.Nibs.Path, nil
}

// MaxConfigBytes bounds how many bytes any config file read may consume — the
// project config, the user config and the pre-layout config probe alike, because
// every one of them is read on the ORDINARY, always-successful path of every
// command. A nibs config is a few dozen lines; the ceiling exists because an
// unbounded os.ReadFile there turns one oversized file into several times its
// size in resident memory (a 50 MB config.yml drove a plain `nibs list` to
// 334 MB RSS). It is the same posture nib.MaxFrontMatterBytes takes for a nib
// file's header.
const MaxConfigBytes = 1 << 20 // 1 MiB

// ReadConfigFile reads a config file, refusing one that is not a regular file
// and one larger than MaxConfigBytes.
//
// THE REGULARITY CHECK IS ABOUT LIVENESS. Opening a FIFO for reading blocks
// inside open(2) until a writer arrives, so a `.nibs.yml` or a config.yml that
// is a named pipe made every command hang forever instead of failing — and
// nothing downstream can bound that, because the process never reaches
// downstream. Statting first answers before the open, and it is the shared
// reader that has to do it: this is the one point all four config reads — the
// project config, the user config, the pre-layout probe and the store-evidence
// probe — pass through.
//
// It is also what makes the answer DETERMINATE. The discovery route reads the
// same pre-layout `.nibs.yml` twice, and a FIFO can serve different bytes to
// each read: a valid `nibs.path` first and malformed YAML second made one
// refusal say a config names the store while the next said no config names it.
// A regular file cannot diverge that way for free.
//
// os.O_RDONLY|syscall.O_NONBLOCK is the other way to avoid the block, and it is
// worse. On a writerless FIFO it opens and then reads clean EOF (measured: 0
// bytes, nil error), so the caller is handed an EMPTY config — a project with
// no prefix, whose next nib is written under a different id — where it should
// be handed an error. A determinate refusal beats a silent misreading.
//
// The stat races the filesystem by construction. That is acceptable because
// this guard bounds a hang and a divergence, not an attacker: nothing decided
// downstream treats "was regular a moment ago" as a security property.
//
// The ceiling is enforced by reading one byte PAST it and erroring, never by
// truncating: a silently shortened config would parse as a different project —
// a missing prefix re-prefixes every new nib — which is worse than not opening
// at all. A missing file is returned as an ordinary os.IsNotExist error so
// callers can keep treating absence as "use the defaults".
func ReadConfigFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is %s, not a regular file; a nibs config is an ordinary file, and reading a pipe or a device here would block the command instead of failing — remove or replace it",
			path, describeFileKind(info.Mode()))
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxConfigBytes {
		return nil, fmt.Errorf("%s is larger than the %d-byte configuration limit; a nibs config is a few dozen lines, so this is either not a config or is corrupt",
			path, MaxConfigBytes)
	}
	return data, nil
}

// describeFileKind names what sits at a path a config was expected at. The
// refusal quotes it because "not a regular file" alone sends the reader hunting:
// a stray FIFO and a directory called config.yml are different mistakes with
// different fixes.
func describeFileKind(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "a directory"
	case mode&fs.ModeNamedPipe != 0:
		return "a named pipe (FIFO)"
	case mode&fs.ModeSocket != 0:
		return "a socket"
	case mode&fs.ModeCharDevice != 0:
		return "a character device"
	case mode&fs.ModeDevice != 0:
		return "a block device"
	default:
		return "of type " + mode.Type().String()
	}
}

// loadRaw reads and unmarshals the config file without applying system defaults.
// Returns an empty Config if the file doesn't exist (callers apply defaults);
// LoadedFromFile is what tells that answer apart from a file that declares
// nothing.
func loadRaw(configPath string) (*Config, error) {
	data, err := ReadConfigFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := &Config{}
			cfg.storeDir = filepath.Dir(configPath)
			return cfg, nil
		}
		return nil, err
	}

	// The CONFIG PATH and the backticked `nibs migrate` are load-bearing OUTSIDE
	// this package. cmd's resolveCLIStore wraps this error with a `%w`-only
	// format that contributes no path and no command of its own, so what the
	// user reads — and what cmd/refusal_invariant_test.go's rows "--config
	// naming a store config that still sets nibs.path" and "a store config that
	// still sets nibs.path" assert on — comes from here. That test parses
	// cmd/root.go alone and cannot see this string: dropping the path from it
	// leaves the composed message with nothing but an unresolvable command, and
	// the `want` list in TestLoadRejectsRetiredNibsPath is what catches it.
	// Reword freely, but keep both.
	var probe retiredPathProbe
	if err := yaml.Unmarshal(data, &probe); err == nil && probe.Nibs.Path != "" {
		return nil, fmt.Errorf("%s sets the retired `nibs.path` key (%q); the store directory now holds the config, the data and the archive together — remove the key, and run `nibs migrate` if this project still uses the old layout",
			configPath, probe.Nibs.Path)
	}

	// The areas vocabulary moved to its own file, and a config still declaring
	// one is refused rather than ignored. Ignoring it would be the quiet
	// failure this whole split exists to avoid: the block would keep reading
	// like a declaration while authorizing nothing, so every `area:` a nib
	// already carries would become undeclared and every write to it refused,
	// with the file that appears to declare the vocabulary sitting right there.
	var areasProbe struct {
		Areas []AreaConfig `yaml:"areas"`
	}
	if err := yaml.Unmarshal(data, &areasProbe); err == nil && len(areasProbe.Areas) > 0 {
		return nil, fmt.Errorf("%s declares an `areas:` block; the areas vocabulary now lives in its own file so it can be reloaded while `nibs serve` runs — move the block to %s and remove it here",
			configPath, AreasFileFor(configPath))
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// The config lives inside the store, so its directory IS the store.
	cfg.storeDir = filepath.Dir(configPath)
	cfg.fromFile = true

	return &cfg, nil
}

// applySystemDefaults fills in zero-value fields with system defaults.
func applySystemDefaults(cfg *Config) {
	if cfg.Nibs.IDLength == 0 {
		cfg.Nibs.IDLength = defaultIDLength
	}
	if cfg.Nibs.DefaultStatus == "" {
		cfg.Nibs.DefaultStatus = defaultStatusName
	}
	if cfg.Nibs.DefaultType == "" {
		cfg.Nibs.DefaultType = defaultTypeName
	}
}

// StoreDir returns the `.nibs` directory this config belongs to.
func (c *Config) StoreDir() string {
	return c.storeDir
}

// SetStoreDir sets the store directory (for testing, or when creating a config
// for a store that does not exist yet).
func (c *Config) SetStoreDir(dir string) {
	c.storeDir = dir
}

// LoadedFromFile reports whether a config FILE was read to produce these
// values. False means the store holds no config.yml (or the Config was built in
// memory by Default and friends), so every field here is a default rather than
// something the store declares.
//
// Load returns a fully-defaulted Config either way, which is right for the
// readers that only need values. It is wrong for the one reader that compares
// what the store declares against what this process loaded: there "the store
// declares nothing" and "the store declares the empty string" are different
// answers, and only one of them is evidence that something changed on disk. See
// nibcore.Core.mintingVocabulary.
func (c *Config) LoadedFromFile() bool {
	return c.fromFile
}

// Layout returns the store layout this config belongs to — the one place the
// data and archive directories are derived from.
func (c *Config) Layout() store.Layout {
	return store.NewLayout(c.storeDir)
}

// GetProjectName returns the project name: the name of the directory
// CONTAINING the store, since the store itself is always called `.nibs`.
// Falls back to "Nibs" when no store directory is set.
func (c *Config) GetProjectName() string {
	if c.storeDir == "" {
		return "Nibs"
	}
	name := filepath.Base(filepath.Dir(c.storeDir))
	if name == "." || name == "" || name == string(filepath.Separator) {
		return "Nibs"
	}
	return name
}

// errMultipleConfigDocuments reports a config file that holds more than one YAML
// document. Both in-place config editors refuse such a file, and each renders its
// own remedy, because the sentence has to name the edit that would otherwise have
// rewritten the whole file from the first document alone.
var errMultipleConfigDocuments = errors.New("more than one YAML document")

// soleConfigDocument decodes data as the single YAML document a nibs config is,
// which is what makes an in-place edit of one key safe to write back: yaml.Marshal
// re-emits the file from one node tree, so a second document would be deleted by
// the write that carries the edit.
//
// An empty file comes back as a zero node rather than an error, because the two
// editors answer that case differently — the prefix editor creates the document,
// the areas editor refuses. Anything else the decoder objects to is returned as
// it came, for the caller to word.
func soleConfigDocument(data []byte) (yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	switch err := decoder.Decode(&doc); {
	case errors.Is(err, io.EOF):
		return yaml.Node{}, nil
	case err != nil:
		return yaml.Node{}, err
	}
	var next yaml.Node
	switch err := decoder.Decode(&next); {
	case err == nil:
		return yaml.Node{}, errMultipleConfigDocuments
	case !errors.Is(err, io.EOF):
		return yaml.Node{}, err
	}
	return doc, nil
}

// mappingValueNode returns the value node for key in a YAML mapping, or nil.
func mappingValueNode(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// writeConfigPreservingMode writes data over the config at path, keeping the
// existing file's permissions and reporting a symlink at that path the way Save
// does — the two writers must not differ about either.
func writeConfigPreservingMode(path string, data []byte) (staleLinkTarget string, err error) {
	if link, lstatErr := os.Lstat(path); lstatErr == nil && link.Mode()&os.ModeSymlink != 0 {
		if target, readErr := os.Readlink(path); readErr == nil {
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
			staleLinkTarget = target
		} else {
			staleLinkTarget = path
		}
	}
	perm := os.FileMode(0644)
	info, statErr := os.Stat(path)
	switch {
	case statErr == nil:
		perm = info.Mode().Perm()
	case !errors.Is(statErr, fs.ErrNotExist):
		return "", fmt.Errorf("reading the current mode of %s: %w", path, statErr)
	}
	if err := fsutil.AtomicWriteFile(path, data, perm); err != nil {
		return "", err
	}
	return staleLinkTarget, nil
}

// Save writes the configuration to <store>/config.yml. If the config has no
// store directory, the given directory is taken as the store.
//
// The write is ATOMIC and MODE-PRESERVING, the same contract the migration engine's
// relocation of this file holds it to (fsutil.AtomicWriteFile). One file with two
// writers and two contracts is not a contract: a plain os.WriteFile here widened a
// 0600 config the relocation had deliberately kept private, and left a torn file
// possible for a resume path that assumes config.yml is only ever absent or
// complete.
//
// A SYMLINK at config.yml is REPLACED with a regular file rather than written
// through, because the rename is what makes the write atomic. That is a contract of
// Save and not an implementation detail of fsutil: a config.yml symlinked into a
// dotfile manager becomes an ordinary file holding the new settings while the
// manager's copy keeps the old ones, so the next `chezmoi apply` (or equivalent)
// restores a stale prefix and short-id resolution stops finding nibs created since.
//
// It is REPORTED rather than silent — the first return value is the path the link
// pointed at, non-empty only when a link was replaced, and the caller must tell the
// user which file is now stale. Refusing instead was rejected because this policy
// is not Save's alone — writeConfigPreservingMode carries the same sequence for
// the in-place editors, which reach it instead of Save — and there a refusal is
// worse: `nibs config set-prefix` has already renamed every nib file by the time
// its config write runs, so a refusal there leaves the store half-changed.
func (c *Config) Save(storeDir string) (staleLinkTarget string, err error) {
	targetDir := c.storeDir
	if targetDir == "" {
		targetDir = storeDir
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", err
	}
	path := store.NewLayout(targetDir).ConfigPath()

	data, err := yaml.Marshal(c)
	if err != nil {
		return "", err
	}

	if link, lstatErr := os.Lstat(path); lstatErr == nil && link.Mode()&os.ModeSymlink != 0 {
		if target, readErr := os.Readlink(path); readErr == nil {
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
			staleLinkTarget = target
		} else {
			staleLinkTarget = path
		}
	}

	// Keep the existing file's permissions; a config that has never existed gets
	// the ordinary 0644 this used to hardcode for every case. A stat failure that is
	// not "absent" is reported rather than answered with 0644 — that fallback could
	// only widen a config whose real mode was narrower.
	perm := os.FileMode(0644)
	info, statErr := os.Stat(path)
	switch {
	case statErr == nil:
		perm = info.Mode().Perm()
	case !errors.Is(statErr, fs.ErrNotExist):
		return "", fmt.Errorf("reading the current mode of %s: %w", path, statErr)
	}
	if err := fsutil.AtomicWriteFile(path, data, perm); err != nil {
		return "", err
	}
	return staleLinkTarget, nil
}

// IsValidStatus returns true if the status is a valid hardcoded status.
func (c *Config) IsValidStatus(status string) bool {
	return IsKnownStatus(status)
}

// IsKnownStatus reports whether status is one of the hardcoded statuses. The
// package-level form exists for callers that have no Config yet — store
// resolution runs before any config is loaded and uses a nibs status as the
// evidence that a file was written by nibs.
func IsKnownStatus(status string) bool {
	for _, s := range DefaultStatuses {
		if s.Name == status {
			return true
		}
	}
	return false
}

// StatusList returns a comma-separated list of valid statuses.
// Statuses are hardcoded and not configurable.
func (c *Config) StatusList() string {
	names := make([]string, len(DefaultStatuses))
	for i, s := range DefaultStatuses {
		names[i] = s.Name
	}
	return strings.Join(names, ", ")
}

// StatusNames returns a slice of valid status names.
// Statuses are hardcoded and not configurable.
func (c *Config) StatusNames() []string {
	names := make([]string, len(DefaultStatuses))
	for i, s := range DefaultStatuses {
		names[i] = s.Name
	}
	return names
}

// WorkflowStatuses returns every hardcoded status in transition order — what a
// status picker offers, and in what sequence. Same members as DefaultStatuses,
// different order; see workflowStatusOrder for why the two differ.
func (c *Config) WorkflowStatuses() []StatusConfig {
	return orderStatusesBy(workflowStatusOrder)
}

// WorkflowStatusNames returns the status names in transition order — the name
// half of WorkflowStatuses, and the order the web's STATUS_WORKFLOW is pinned
// against.
func (c *Config) WorkflowStatusNames() []string {
	statuses := orderStatusesBy(workflowStatusOrder)
	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = s.Name
	}
	return names
}

// orderStatusesBy returns DefaultStatuses rearranged into the given name order.
// A declared status the order forgets is appended (keeping its
// DefaultStatuses-relative position), and a name that is not a declared status
// is skipped, so the result always holds every status exactly once whatever the
// order says. That is deliberate: an ordering mistake should make a picker read
// oddly, never hide a status a nib can be set to.
func orderStatusesBy(order []string) []StatusConfig {
	out := make([]StatusConfig, 0, len(DefaultStatuses))
	taken := make(map[string]bool, len(DefaultStatuses))
	for _, name := range order {
		if taken[name] {
			continue
		}
		for _, s := range DefaultStatuses {
			if s.Name == name {
				out = append(out, s)
				taken[name] = true
				break
			}
		}
	}
	for _, s := range DefaultStatuses {
		if !taken[s.Name] {
			out = append(out, s)
			taken[s.Name] = true
		}
	}
	return out
}

// GetStatus returns the StatusConfig for a given status name, or nil if not found.
// Statuses are hardcoded and not configurable.
func (c *Config) GetStatus(name string) *StatusConfig {
	for i := range DefaultStatuses {
		if DefaultStatuses[i].Name == name {
			return &DefaultStatuses[i]
		}
	}
	return nil
}

// GetDefaultStatus returns the default status name for new nibs.
func (c *Config) GetDefaultStatus() string {
	if c.Nibs.DefaultStatus == "" {
		return "todo"
	}
	return c.Nibs.DefaultStatus
}

// GetDefaultType returns the default type name for new nibs.
func (c *Config) GetDefaultType() string {
	return c.Nibs.DefaultType
}

// IsClosedStatus returns true if the given status is closed (terminal) — the
// canonical answer to "is this nib finished", used by every package instead of
// a local status list. Unknown statuses are open.
// Statuses are hardcoded and not configurable, so the receiver is currently
// never dereferenced — but callers should not depend on that: hand it a real
// *Config (config.Default() if nothing better is in reach).
func (c *Config) IsClosedStatus(name string) bool {
	if s := c.GetStatus(name); s != nil {
		return s.Role.Closed()
	}
	return false
}

// ClosedStatusNames returns the names of all closed statuses, derived from
// DefaultStatuses (the single source of truth). Every returned name satisfies
// IsClosedStatus. Today this is {deferred, completed, scrapped}; deriving it
// here keeps the set correct if the Closed flags ever change. This is the
// "closed" status group, and the exact complement of OpenStatusNames.
func (c *Config) ClosedStatusNames() []string {
	var names []string
	for _, s := range DefaultStatuses {
		if s.Role.Closed() {
			names = append(names, s.Name)
		}
	}
	return names
}

// StatusReleasesDependents returns true if closing a blocker with this status
// satisfies the dependency — the canonical answer to "does this blocker still
// count", used by the blocking graph instead of IsClosedStatus. Today this is
// {completed, scrapped}: deferred is closed but still blocks, because the
// set-aside work is coming back. Unknown statuses do not release, so an
// unrecognized blocker keeps blocking rather than silently freeing its
// dependents.
// Like IsClosedStatus the receiver is currently never dereferenced, but callers
// should hand it a real *Config anyway (config.Default() if nothing better).
func (c *Config) StatusReleasesDependents(name string) bool {
	if s := c.GetStatus(name); s != nil {
		return s.Role.ReleasesDependents()
	}
	return false
}

// ReleasingStatusNames returns the names of the statuses that release their
// dependents, derived from DefaultStatuses (the single source of truth). Every
// returned name satisfies StatusReleasesDependents. Today this is {completed,
// scrapped} — a strict subset of ClosedStatusNames, since deferred is closed
// but keeps blocking.
func (c *Config) ReleasingStatusNames() []string {
	var names []string
	for _, s := range DefaultStatuses {
		if s.Role.ReleasesDependents() {
			names = append(names, s.Name)
		}
	}
	return names
}

// HoldingStatusNames returns the closed statuses that do NOT release their
// dependents — the statuses a blocker can carry while still holding up
// everything that depends on it. It is the set difference ClosedStatusNames \
// ReleasingStatusNames, derived from the same flags. Today this is {deferred}.
// The agent-facing docs (cmd/cheat.go and the prime templates) state the
// "closed but still blocks" rule from this set instead of naming a status in
// prose; an empty result means no such rule exists and the docs drop it.
func (c *Config) HoldingStatusNames() []string {
	var names []string
	for _, s := range DefaultStatuses {
		if s.Role.Closed() && !s.Role.ReleasesDependents() {
			names = append(names, s.Name)
		}
	}
	return names
}

// OpenStatusNames returns the names of all non-closed statuses — the "open"
// status group, and the exact complement of ClosedStatusNames. Derived from
// DefaultStatuses (Closed == false), so it stays correct if the Closed flags
// ever change. Today this is {in-progress, todo, draft}.
func (c *Config) OpenStatusNames() []string {
	var names []string
	for _, s := range DefaultStatuses {
		if !s.Role.Closed() {
			names = append(names, s.Name)
		}
	}
	return names
}

// IsStartableStatus returns true if work can be picked up from the given status
// — the status half of "can I start this?", read by both the projected `ready`
// field and `nibs list --ready` so the two answer it from one definition rather
// than two. The other half is having no active blockers; this predicate says
// nothing about blockers.
// Unknown statuses are not startable, so a nib carrying a status outside the
// declared vocabulary stays out of the work queue rather than being offered as
// the next thing to do.
// Like IsClosedStatus the receiver is currently never dereferenced, but callers
// should hand it a real *Config anyway (config.Default() if nothing better).
func (c *Config) IsStartableStatus(name string) bool {
	if s := c.GetStatus(name); s != nil {
		return s.Role.Startable()
	}
	return false
}

// StartableStatusNames returns the names of all startable statuses, derived from
// DefaultStatuses (the single source of truth). Every returned name satisfies
// IsStartableStatus. Today this is {todo} alone — narrower than OpenStatusNames,
// which also holds draft and in-progress.
// `nibs list --ready` builds its status filter from this set and the agent
// guides state the --ready rule from it, so a status added to DefaultStatuses
// reaches the ready queue only by declaring itself startable.
func (c *Config) StartableStatusNames() []string {
	var names []string
	for _, s := range DefaultStatuses {
		if s.Role.Startable() {
			names = append(names, s.Name)
		}
	}
	return names
}

// DoneStatusNames returns the names of the statuses in the done role — the
// close reasons that count as an accomplishment, in DefaultStatuses order.
// Today this is {completed}. `nibs close` derives its default reason and its
// completion reason from the FIRST of them, so the set must never be empty —
// which TestStatusRoleGroupsAreNonEmpty enforces for the declared vocabulary.
// Strictly narrower than ReleasingStatusNames: dropped work also releases its
// dependents, but nothing was accomplished.
func (c *Config) DoneStatusNames() []string {
	var names []string
	for _, s := range DefaultStatuses {
		if s.Role == RoleDone {
			names = append(names, s.Name)
		}
	}
	return names
}

// GetType returns the TypeConfig for a given type name, or nil if not found.
// Types are hardcoded and not configurable.
func (c *Config) GetType(name string) *TypeConfig {
	for i := range DefaultTypes {
		if DefaultTypes[i].Name == name {
			return &DefaultTypes[i]
		}
	}
	return nil
}

// TypeNames returns a slice of valid type names.
// Types are hardcoded and not configurable.
func (c *Config) TypeNames() []string {
	names := make([]string, len(DefaultTypes))
	for i, t := range DefaultTypes {
		names[i] = t.Name
	}
	return names
}

// IsValidType returns true if the type is a valid hardcoded type.
func (c *Config) IsValidType(typeName string) bool {
	for _, t := range DefaultTypes {
		if t.Name == typeName {
			return true
		}
	}
	return false
}

// TypeList returns a comma-separated list of valid types.
func (c *Config) TypeList() string {
	names := make([]string, len(DefaultTypes))
	for i, t := range DefaultTypes {
		names[i] = t.Name
	}
	return strings.Join(names, ", ")
}

// NibColors holds resolved color information for rendering a nib
type NibColors struct {
	StatusColor   string
	TypeColor     string
	PriorityColor string
	IsClosed      bool
}

// GetNibColors returns the resolved colors for a nib based on its status, type, and priority.
func (c *Config) GetNibColors(status, typeName, priority string) NibColors {
	colors := NibColors{
		StatusColor:   "gray",
		TypeColor:     "",
		PriorityColor: "",
		IsClosed:      false,
	}

	if statusCfg := c.GetStatus(status); statusCfg != nil {
		colors.StatusColor = statusCfg.Color
	}
	colors.IsClosed = c.IsClosedStatus(status)

	if typeCfg := c.GetType(typeName); typeCfg != nil {
		colors.TypeColor = typeCfg.Color
	}

	if priorityCfg := c.GetPriority(priority); priorityCfg != nil {
		colors.PriorityColor = priorityCfg.Color
	}

	return colors
}

// GetPriority returns the PriorityConfig for a given priority name, or nil if not found.
func (c *Config) GetPriority(name string) *PriorityConfig {
	for i := range DefaultPriorities {
		if DefaultPriorities[i].Name == name {
			return &DefaultPriorities[i]
		}
	}
	return nil
}

// PriorityNames returns a slice of valid priority names in order from highest to lowest.
func (c *Config) PriorityNames() []string {
	names := make([]string, len(DefaultPriorities))
	for i, p := range DefaultPriorities {
		names[i] = p.Name
	}
	return names
}

// IsValidPriority returns true if the priority is a valid hardcoded priority.
// Empty string is valid (means no priority set).
func (c *Config) IsValidPriority(priority string) bool {
	if priority == "" {
		return true
	}
	for _, p := range DefaultPriorities {
		if p.Name == priority {
			return true
		}
	}
	return false
}

// PriorityList returns a comma-separated list of valid priorities.
func (c *Config) PriorityList() string {
	names := make([]string, len(DefaultPriorities))
	for i, p := range DefaultPriorities {
		names[i] = p.Name
	}
	return strings.Join(names, ", ")
}

// PriorityRank returns the sort rank for a priority string.
// Lower rank = higher priority. Empty string is treated as "normal".
// Unknown priorities return len(DefaultPriorities), sorting last.
func (c *Config) PriorityRank(priority string) int {
	if priority == "" {
		priority = "normal"
	}
	for i, p := range DefaultPriorities {
		if p.Name == priority {
			return i
		}
	}
	return len(DefaultPriorities)
}

// IsValidEstimate returns true if the estimate is a valid hardcoded estimate.
// Empty string is valid (means unestimated).
func (c *Config) IsValidEstimate(estimate string) bool {
	if estimate == "" {
		return true
	}
	for _, e := range DefaultEstimates {
		if e.Name == estimate {
			return true
		}
	}
	return false
}

// EstimateNames returns a slice of valid estimate names in order from smallest to largest.
func (c *Config) EstimateNames() []string {
	names := make([]string, len(DefaultEstimates))
	for i, e := range DefaultEstimates {
		names[i] = e.Name
	}
	return names
}

// EstimateList returns a comma-separated list of valid estimates.
func (c *Config) EstimateList() string {
	names := make([]string, len(DefaultEstimates))
	for i, e := range DefaultEstimates {
		names[i] = e.Name
	}
	return strings.Join(names, ", ")
}

// GetEstimate returns the EstimateConfig for a given estimate name, or nil if not found.
func (c *Config) GetEstimate(name string) *EstimateConfig {
	for i := range DefaultEstimates {
		if DefaultEstimates[i].Name == name {
			return &DefaultEstimates[i]
		}
	}
	return nil
}

// HideCompleted returns whether nibs in a closed status should be hidden.
// Defaults to true when not explicitly set (nil).
func (c *Config) HideCompleted() bool {
	if c.Nibs.HideCompleted != nil {
		return *c.Nibs.HideCompleted
	}
	return true
}

// WideMode returns whether wide mode is enabled.
// Defaults to true when not explicitly set (nil).
func (c *Config) WideMode() bool {
	if c.Nibs.WideMode != nil {
		return *c.Nibs.WideMode
	}
	return true
}

const (
	DefaultServerPort        = 3000
	DefaultServerOpenBrowser = true
)

// ServerPort returns the configured server port, or 3000 if not set.
func (c *Config) ServerPort() int {
	if c.Nibs.Server.Port != nil {
		return *c.Nibs.Server.Port
	}
	return DefaultServerPort
}

// ServerOpenBrowser returns whether to open a browser on serve, or true if not set.
func (c *Config) ServerOpenBrowser() bool {
	if c.Nibs.Server.OpenBrowser != nil {
		return *c.Nibs.Server.OpenBrowser
	}
	return DefaultServerOpenBrowser
}
