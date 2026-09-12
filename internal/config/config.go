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

// DefaultStatuses is the hardcoded status vocabulary, in RANK order: the open
// statuses first, then the closed ones. That order is the primary sort key of
// nib.SortByStatusPriorityAndType, so reordering it re-sorts the lists.
// Pickers read a different order — see workflowStatusOrder.
var DefaultStatuses = []StatusConfig{
	{Name: "in-progress", Color: "yellow", Role: RoleOpen, Description: "Currently being worked on"},
	{Name: "todo", Color: "green", Role: RoleStartable, Description: "Ready to be worked on"},
	{Name: "draft", Color: "blue", Role: RoleOpen, Description: "Needs refinement before it can be worked on"},
	{Name: "deferred", Color: "magenta", Role: RoleParked, Description: "Set aside — a good idea at the wrong time; closed, but kept as a seed rather than a dead end"},
	{Name: "completed", Color: "lightgray", Role: RoleDone, Description: "Finished successfully"},
	{Name: "scrapped", Color: "dimgray", Role: RoleDropped, Description: "Will not be done"},
}

// Status group names, for every surface that accepts a group where a concrete
// status goes — `-s open` on the CLI, `status:open` in the web. Only the NAMES
// live here; each surface derives the membership from the roles.
const (
	StatusGroupOpen   = "open"
	StatusGroupClosed = "closed"
)

// workflowStatusOrder lists the statuses in transition order, which is what
// choosers offer; DefaultStatuses is the rank order. Read it through
// WorkflowStatuses/WorkflowStatusNames.
var workflowStatusOrder = []string{"draft", "todo", "in-progress", "completed", "deferred", "scrapped"}

// DefaultTypes is the hardcoded type vocabulary. Its order is the tertiary
// sort key of nib.SortByStatusPriorityAndType.
var DefaultTypes = []TypeConfig{
	{Name: "milestone", Color: "cyan", Description: "A target release or checkpoint; group work that should ship together"},
	{Name: "epic", Color: "purple", Description: "A deliverable that tops the work tree; should have child nibs, not be worked on directly"},
	{Name: "bug", Color: "red", Description: "Something that is broken and needs fixing"},
	{Name: "feature", Color: "green", Description: "A user-facing capability or enhancement"},
	{Name: "task", Color: "blue", Description: "A concrete piece of work to complete (eg. a chore, or a sub-task for a feature)"},
	{Name: "research", Color: "yellow", Description: "Exploratory work whose output is knowledge or decisions, not code"},
}

// DefaultPriorities is the hardcoded priority vocabulary, ordered from highest
// to lowest urgency. PriorityRank is the index into it.
var DefaultPriorities = []PriorityConfig{
	{Name: "critical", Color: "red", Description: "Urgent, blocking work. When possible, address immediately"},
	{Name: "high", Color: "yellow", Description: "Important, should be done before normal work"},
	{Name: "normal", Color: "white", Description: "Standard priority"},
	{Name: "low", Color: "gray", Description: "Less important, can be delayed"},
}

// DefaultEstimates is the hardcoded estimate vocabulary: t-shirt sizes ordered
// from smallest to largest.
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

// StatusConfig defines a single status with its display color and its Role.
//
// The Role is the status's whole classification: the closed,
// releases-dependents and startable predicates all derive from it, and it
// carries the done/dropped split none of them can see — the distinction
// progress arithmetic keys on.
//
// The three sets are not interchangeable. Deferred is closed and still blocks;
// startable is narrower than "not closed", excluding draft and in-progress.
// Read them through IsClosedStatus/ClosedStatusNames/OpenStatusNames,
// StatusReleasesDependents/ReleasingStatusNames, HoldingStatusNames,
// IsStartableStatus/StartableStatusNames and StatusRole. The web derives its
// copy from the generated vocabulary (internal/webvocab), pinned by
// TestGeneratedVocabularyIsFresh; TestStatusRoles and
// TestStatusRoleGroupsAreNonEmpty guard the roles here, and say why.
//
// Renaming a status means visiting the sites that name one: internal/progress
// exposes `scrapped` and `deferred` as JSON field names, and internal/ui
// abbreviates "deferred" to F so it does not collide with draft.
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
type Config struct {
	Nibs NibsConfig `yaml:"nibs"`

	// The `.nibs` directory this config was read from. Everything positional
	// about a project derives from it: the config file's own location, the data
	// and archive directories, and the project name (its PARENT directory's).
	storeDir string `yaml:"-"`

	// See LoadedFromFile.
	fromFile bool `yaml:"-"`
}

// NibsConfig holds the `nibs:` block of a project config.
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

// The system defaults: what an unset key means. Default() persists them into a
// new project config and applySystemDefaults fills them into one that omits
// them; the two must answer alike.
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
// (<store>/config.yml), without user-config defaults. Commands reach their
// config through LoadStoreWithUserConfig.
func LoadFromStore(storeDir string) (*Config, error) {
	return Load(store.NewLayout(storeDir).ConfigPath())
}

// retiredPathProbe detects a `nibs.path:` key, which the store layout retired:
// it points the config at a data directory somewhere else, and the store
// directory IS that directory's parent. The shape is declared once here, for
// loadRaw and RetiredNibsPath both.
type retiredPathProbe struct {
	Nibs struct {
		Path string `yaml:"path"`
	} `yaml:"nibs"`
}

// RetiredNibsPath returns the retired `nibs.path` value a pre-layout config
// carries. The answer is THREE-WAY, because a caller deciding whether
// `nibs migrate` may rewrite a directory must not read "I could not tell" as
// "there is no evidence":
//
//   - ("", nil)      the file is absent, or does not set the key;
//   - (value, nil)   the key is set;
//   - ("", err)      the file EXISTS and its content could not be established —
//     unreadable, over MaxConfigBytes, or not YAML at all.
//
// A caller that only sharpens a message may discard the error; one deciding
// from the answer must report "cannot determine".
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

// MaxConfigBytes bounds every config file read. A nibs config is a few dozen
// lines, and several of these reads sit on the ordinary path of an everyday
// command, where an unbounded os.ReadFile would turn one oversized file into
// several times its size in resident memory. Same posture as
// nib.MaxFrontMatterBytes for a nib's header.
const MaxConfigBytes = 1 << 20 // 1 MiB

// ReadConfigFile reads a config file, refusing one that is not a regular file
// and one larger than MaxConfigBytes. Read every config file through it: it is
// the one point they all pass through.
//
// THE REGULARITY CHECK IS ABOUT LIVENESS. Opening a FIFO for reading blocks
// inside open(2) until a writer arrives, so a `.nibs.yml` or config.yml that is
// a named pipe hangs the command instead of failing it, and nothing downstream
// can bound that — the process never reaches downstream. Statting first answers
// before the open. It also makes the answer DETERMINATE: the discovery route
// reads the same pre-layout `.nibs.yml` twice (cmd/root.go), and a FIFO can
// serve different bytes to each read.
//
// The stat races the filesystem by construction. This guard bounds a hang and a
// divergence, not an attacker — do not treat "was regular a moment ago" as a
// security property.
//
// The ceiling is enforced by reading one byte PAST it and erroring. Never
// truncate instead: a shortened config parses as a different project, and a
// missing prefix re-prefixes every new nib. A missing file comes back as an
// ordinary os.IsNotExist error, so callers can keep treating absence as "use
// the defaults".
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

// describeFileKind names what sits at a path a config was expected at. A stray
// FIFO and a directory called config.yml are different mistakes with different
// fixes, so the refusal quotes this rather than saying "not a regular file".
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

	// Reword freely, but keep the config PATH and the backticked `nibs migrate`.
	// cmd's resolveCLIStore wraps this error with a `%w`-only format, so this is
	// the only place either reaches the user; TestLoadRejectsRetiredNibsPath is
	// what catches their loss.
	var probe retiredPathProbe
	if err := yaml.Unmarshal(data, &probe); err == nil && probe.Nibs.Path != "" {
		return nil, fmt.Errorf("%s sets the retired `nibs.path` key (%q); the store directory now holds the config, the data and the archive together — remove the key, and run `nibs migrate` if this project still uses the old layout",
			configPath, probe.Nibs.Path)
	}

	// An `areas:` block here is refused, not ignored: ignoring it leaves a block
	// that still reads like a declaration while authorizing nothing, which
	// undeclares every `area:` a nib carries and refuses every write to it.
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
// values. False means the store holds no config.yml, or the Config was built in
// memory by Default and friends, so every field here is a default rather than
// something the store declares.
//
// Load returns a fully-defaulted Config either way, which serves a reader that
// only needs values. A reader comparing what the store declares against what
// this process loaded needs this instead — see nibcore.Core.mintingVocabulary.
func (c *Config) LoadedFromFile() bool {
	return c.fromFile
}

// Layout returns the store layout this config belongs to. Derive the data and
// archive directories from it.
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
// document. Both in-place editors refuse it and each words its own remedy, which
// has to name the edit that would have rewritten the file from the first
// document alone.
var errMultipleConfigDocuments = errors.New("more than one YAML document")

// soleConfigDocument decodes data as the single YAML document a nibs config is.
// That is what makes an in-place edit of one key safe to write back: yaml.Marshal
// re-emits the file from one node tree, so a second document would be deleted by
// the write carrying the edit.
//
// An empty file comes back as a ZERO NODE rather than an error — callers differ
// on it, so each decides. Anything else the decoder objects to is returned as it
// came, for the caller to word.
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
// existing file's permissions and reporting a replaced symlink. Save holds the
// same contract; keep the two in step.
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
// The write is ATOMIC and MODE-PRESERVING (fsutil.AtomicWriteFile), the same
// contract the migration engine's relocation of this file holds it to.
//
// A SYMLINK at config.yml is REPLACED with a regular file, because the rename is
// what makes the write atomic. That is a contract of Save, not a detail of
// fsutil: a config.yml symlinked into a dotfile manager leaves the manager's copy
// holding the old settings, and the next apply restores a stale prefix.
//
// The replacement is REPORTED, not silent: staleLinkTarget is the path the link
// pointed at, non-empty only when one was replaced, and the caller must tell the
// user which file is now stale.
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

	// A config that has never existed gets the ordinary 0644. A stat failure that
	// is not "absent" is reported rather than defaulted, which could only widen a
	// config whose real mode was narrower.
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
// package-level form exists for callers with no Config in hand — `nibs migrate`
// classifies a pre-layout file partly on its status before any config loads.
func IsKnownStatus(status string) bool {
	for _, s := range DefaultStatuses {
		if s.Name == status {
			return true
		}
	}
	return false
}

// StatusList returns a comma-separated list of valid statuses.
func (c *Config) StatusList() string {
	names := make([]string, len(DefaultStatuses))
	for i, s := range DefaultStatuses {
		names[i] = s.Name
	}
	return strings.Join(names, ", ")
}

// StatusNames returns the valid status names, in DefaultStatuses rank order.
func (c *Config) StatusNames() []string {
	names := make([]string, len(DefaultStatuses))
	for i, s := range DefaultStatuses {
		names[i] = s.Name
	}
	return names
}

// WorkflowStatuses returns every status in transition order — what a picker
// offers, and in what sequence. Same members as DefaultStatuses, different
// order; see workflowStatusOrder.
func (c *Config) WorkflowStatuses() []StatusConfig {
	return orderStatusesBy(workflowStatusOrder)
}

// WorkflowStatusNames returns the status names in transition order — the name
// half of WorkflowStatuses, generated into the web as STATUS_WORKFLOW_ORDER.
func (c *Config) WorkflowStatusNames() []string {
	statuses := orderStatusesBy(workflowStatusOrder)
	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = s.Name
	}
	return names
}

// orderStatusesBy returns DefaultStatuses rearranged into the given name order.
// A status the order forgets is appended and a name that is no status is
// skipped, so the result holds every status exactly once whatever the order
// says: an ordering mistake makes a picker read oddly, never hide a status.
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

// IsClosedStatus reports whether a status is closed (terminal) — the answer to
// "is this nib finished". Ask it rather than keeping a local status list.
// Unknown statuses are open. The receiver is not dereferenced today; do not
// depend on that — hand it a real *Config (config.Default() will do).
//
// CANONICAL INVARIANT (the closed-status answer): cmd, internal/graph,
// internal/nibcore and internal/nibcontext all defer here; do not re-derive it.
func (c *Config) IsClosedStatus(name string) bool {
	if s := c.GetStatus(name); s != nil {
		return s.Role.Closed()
	}
	return false
}

// ClosedStatusNames returns the closed statuses, derived from the roles. Every
// returned name satisfies IsClosedStatus. This is the "closed" status group, and
// the exact complement of OpenStatusNames.
func (c *Config) ClosedStatusNames() []string {
	var names []string
	for _, s := range DefaultStatuses {
		if s.Role.Closed() {
			names = append(names, s.Name)
		}
	}
	return names
}

// StatusReleasesDependents reports whether closing a blocker with this status
// satisfies the dependency. Ask it rather than IsClosedStatus: deferred is
// closed and still blocks, because the set-aside work is coming back. Unknown
// statuses do not release, so an unrecognized blocker keeps blocking. Hand it a
// real *Config, as with IsClosedStatus.
//
// CANONICAL INVARIANT (the blocker-release answer, distinct from closed): the
// blocking graph in internal/graph and the CLI's readiness surface defer here.
func (c *Config) StatusReleasesDependents(name string) bool {
	if s := c.GetStatus(name); s != nil {
		return s.Role.ReleasesDependents()
	}
	return false
}

// ReleasingStatusNames returns the statuses that release their dependents,
// derived from the roles. Every returned name satisfies
// StatusReleasesDependents. A strict subset of ClosedStatusNames.
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
// dependents — what a blocker can carry while still holding up everything that
// depends on it, the set difference ClosedStatusNames \ ReleasingStatusNames.
// The agent-facing docs (cmd/cheat.go, cmd/prime.go) word the "closed but still
// blocks" rule from this set, and drop the rule when it comes back empty.
func (c *Config) HoldingStatusNames() []string {
	var names []string
	for _, s := range DefaultStatuses {
		if s.Role.Closed() && !s.Role.ReleasesDependents() {
			names = append(names, s.Name)
		}
	}
	return names
}

// OpenStatusNames returns the non-closed statuses, derived from the roles — the
// "open" status group, and the exact complement of ClosedStatusNames.
func (c *Config) OpenStatusNames() []string {
	var names []string
	for _, s := range DefaultStatuses {
		if !s.Role.Closed() {
			names = append(names, s.Name)
		}
	}
	return names
}

// IsStartableStatus reports whether work can be picked up from a status — the
// status half of "can I start this?", shared by the projected `ready` field and
// `nibs list --ready`. It says nothing about blockers, which are the other half.
// Unknown statuses are not startable, so a nib outside the declared vocabulary
// stays out of the work queue. Hand it a real *Config, as with IsClosedStatus.
func (c *Config) IsStartableStatus(name string) bool {
	if s := c.GetStatus(name); s != nil {
		return s.Role.Startable()
	}
	return false
}

// StartableStatusNames returns the startable statuses, derived from the roles.
// Every returned name satisfies IsStartableStatus; narrower than
// OpenStatusNames. `nibs list --ready` builds its status filter from this set
// and the agent guides word the --ready rule from it, so a new status reaches
// the ready queue only by declaring itself startable.
func (c *Config) StartableStatusNames() []string {
	var names []string
	for _, s := range DefaultStatuses {
		if s.Role.Startable() {
			names = append(names, s.Name)
		}
	}
	return names
}

// DoneStatusNames returns the statuses in the done role — the close reasons
// that count as an accomplishment, in DefaultStatuses order. `nibs close` takes
// its default and completion reasons from the FIRST of them, so the set must
// never be empty; TestStatusRoleGroupsAreNonEmpty enforces that. Strictly narrower
// than ReleasingStatusNames: dropped work releases its dependents too.
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
func (c *Config) GetType(name string) *TypeConfig {
	for i := range DefaultTypes {
		if DefaultTypes[i].Name == name {
			return &DefaultTypes[i]
		}
	}
	return nil
}

// TypeNames returns the valid type names.
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

// NibColors holds resolved color information for rendering a nib.
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
