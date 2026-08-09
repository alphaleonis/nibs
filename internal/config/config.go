package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// ConfigFileName is the name of the config file at project root
	ConfigFileName = ".nibs.yml"
	// DefaultNibsPath is the default directory for storing nibs
	DefaultNibsPath = ".nibs"
	// LegacyConfigFile is the old config file location (deprecated)
	LegacyConfigFile = "config.yaml"
)

// DefaultStatuses defines the hardcoded status configuration.
// Statuses are not configurable - they are hardcoded like types.
// Order determines sort priority: the open statuses first (in-progress, todo,
// draft), then the closed ones (deferred, completed, scrapped) last. Pickers
// list statuses in a different order — see workflowStatusOrder.
var DefaultStatuses = []StatusConfig{
	{Name: "in-progress", Color: "yellow", Description: "Currently being worked on"},
	{Name: "todo", Color: "green", Startable: true, Description: "Ready to be worked on"},
	{Name: "draft", Color: "blue", Description: "Needs refinement before it can be worked on"},
	{Name: "deferred", Color: "gray", Closed: true, Description: "Set aside — a good idea at the wrong time; closed, but kept as a seed rather than a dead end"},
	{Name: "completed", Color: "gray", Closed: true, ReleasesDependents: true, Description: "Finished successfully"},
	{Name: "scrapped", Color: "gray", Closed: true, ReleasesDependents: true, Description: "Will not be done"},
}

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
// (internal/tui/statuspicker.go) and, via the hand-written STATUS_WORKFLOW copy
// in web/src/lib/constants.ts, by the web's status select and row context menu.
// The web copy is pinned to this order by TestWebConstantsMatchConfig — unlike
// the STATUSES/DefaultStatuses pair, where only membership is pinned because
// the orders differ on purpose.
//
// Membership is not restated: TestWorkflowStatusOrderCoversEveryStatus requires
// this list and DefaultStatuses to hold the same names, and orderStatusesBy
// appends anything missing rather than dropping it, so a status added to
// DefaultStatuses and forgotten here is still offered by every picker.
var workflowStatusOrder = []string{"draft", "todo", "in-progress", "completed", "deferred", "scrapped"}

// DefaultTypes defines the default type configuration.
var DefaultTypes = []TypeConfig{
	{Name: "milestone", Color: "cyan", Description: "A target release or checkpoint; group work that should ship together"},
	{Name: "epic", Color: "purple", Description: "A thematic container for related work; should have child nibs, not be worked on directly"},
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
// Three booleans classify a status, because "is this nib finished", "does this
// nib still hold up the work that depends on it" and "can this nib be picked
// up" are different questions:
//
//   - Closed marks the status as terminal — the work is no longer on the board,
//     whether it was finished (completed), abandoned (scrapped) or set aside
//     (deferred); everything else is open. Open and closed partition the
//     declared statuses. A status outside that vocabulary — a hand-edited nib
//     with no `status:` carries "" — is in neither group, and IsClosedStatus
//     reads it as open.
//   - ReleasesDependents marks a status that *satisfies* a dependency: closing a
//     blocker this way frees everything it was gating. True for completed (the
//     work happened) and scrapped (it never will), false for deferred — the
//     set-aside work is coming back, so the dependency is still unmet. Every
//     open status is false too: an unfinished blocker blocks.
//   - Startable marks a status work can be picked up from. It is the status half
//     of "can I start this?"; the other half is having no active blockers, and
//     the two are applied together by the `ready` projection and by
//     `nibs list --ready`. True for todo alone: in-progress work is already
//     underway, draft needs refinement first, and the closed statuses are off
//     the board.
//
// The three questions are independent; the flags are not. Of the eight states
// three booleans can express, four are legal, and every declared status is one
// of them:
//
//	Closed  Releases  Startable  meaning                       members
//	false   false     false      open, not yet pickable        in-progress, draft
//	false   false     true       open and pickable             todo
//	true    false     false      closed, still blocking        deferred
//	true    true      false      closed, dependency settled    completed, scrapped
//
// The other four are illegal. ReleasesDependents is a strict subset of Closed,
// since an open status that released its dependents would hand out work that is
// still blocked. Startable and Closed are disjoint, because a startable closed
// status would offer finished work as the next thing to pick up.
//
// **Nothing in the type enforces this**, and that is a deliberate choice rather
// than an oversight. Statuses are hardcoded in DefaultStatuses below and are not
// user-configurable (see the note on Config), so an illegal combination can only
// be written by a developer editing this file — and
// TestStatusFlagCombinationsAreLegal fails in that same commit, naming the
// offending status and the rule it broke. Making the states unrepresentable
// would mean migrating every consumer of these three predicates to prevent a
// state no user can reach.
//
// The same test also requires each of the four groups to be NON-EMPTY, which is
// not a stylistic nicety: a derived set that empties out fails open rather than
// closed. Emptying Startable made `nibs list --ready` widen from "only startable"
// to every unblocked nib — 86 of 89 on the sample fixture, including completed
// and scrapped work — because an empty include-list filters nothing.
//
// None of the three sets is interchangeable with another. Deferred is closed
// and still blocks, so collapsing Closed and ReleasesDependents back into one
// flag would silently unblock deferred work. Startable is strictly narrower
// than "not closed": draft and in-progress are open and not startable, so
// reading Startable off the Closed flag would put work that is already underway
// or not yet refined into the ready queue.
//
// In Go these flags are the only definitions of their sets — consumers read
// them through IsClosedStatus/ClosedStatusNames/OpenStatusNames,
// StatusReleasesDependents/ReleasingStatusNames, HoldingStatusNames for the
// closed-but-still-blocking difference, and
// IsStartableStatus/StartableStatusNames for the ready queue. The web UI keeps
// a hand-written copy in web/src/lib/constants.ts as CLOSED_STATUSES
// (nibs-nv05). README.md's Data Model section is another hand-written copy —
// there is no render step behind it — held to these flags by cmd/readme_test.go
// rather than by derivation.
//
// Sites that name one specific status are not rival definitions of these sets,
// because a group predicate cannot single a member out — but renaming a status
// means visiting them. The progress rollups in internal/graph and
// internal/nibcontext give "completed", "scrapped" and "deferred" three
// different treatments (see graph.ProgressRollup); cmd/dedup.go names
// "scrapped" to attach the scrap-reason snippet; cmd/close.go writes
// "completed"; internal/ui abbreviates "deferred" to F so it does not collide
// with draft.
type StatusConfig struct {
	Name               string `yaml:"name"`
	Color              string `yaml:"color"`
	Closed             bool   `yaml:"closed,omitempty"`
	ReleasesDependents bool   `yaml:"releases_dependents,omitempty"`
	Startable          bool   `yaml:"startable,omitempty"`
	Description        string `yaml:"description,omitempty"`
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

	// configDir is the directory containing the config file (not serialized)
	// Used to resolve relative paths
	configDir string `yaml:"-"`
}

// NibsConfig defines settings for nib creation.
type NibsConfig struct {
	// Path is the path to the nibs directory (relative to config file location)
	Path           string       `yaml:"path,omitempty"`
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

// Default returns a Config with default values.
func Default() *Config {
	return &Config{
		Nibs: NibsConfig{
			Path:          DefaultNibsPath,
			Prefix:        "",
			IDLength:      4,
			DefaultStatus: "todo",
			DefaultType:   "task",
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

// FindConfig searches upward from the given directory for a .nibs.yml config file.
// Returns the absolute path to the config file, or empty string if not found.
//
// The NIBS_CONFIG_ROOT environment variable, when set to a non-empty path,
// bounds the upward walk: each directory up to and including that ceiling is
// checked for .nibs.yml, but the walk never ascends above it. Comparison is
// done on absolute paths (via filepath.Abs), so a ceiling that isn't an
// ancestor of startDir simply never triggers and the walk proceeds to the
// filesystem root as usual. When unset, behavior is unchanged (walk to root).
// This is mainly a sandboxing/test-isolation knob — it keeps a stray ancestor
// .nibs.yml (e.g. /tmp/.nibs.yml) from leaking into tests that expect no
// config to be found.
func FindConfig(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	// NIBS_CONFIG_ROOT caps how far the upward walk may climb.
	var ceiling string
	if raw := os.Getenv("NIBS_CONFIG_ROOT"); raw != "" {
		ceiling, err = filepath.Abs(raw)
		if err != nil {
			return "", err
		}
	}

	for {
		configPath := filepath.Join(dir, ConfigFileName)
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}

		// Stop at the ceiling: this dir was checked, but do not ascend above it.
		if ceiling != "" && dir == ceiling {
			return "", nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			return "", nil
		}
		dir = parent
	}
}

// Load reads configuration from the given config file path.
// Returns default config if the file doesn't exist.
func Load(configPath string) (*Config, error) {
	cfg, err := loadRaw(configPath)
	if err != nil {
		return nil, err
	}
	applySystemDefaults(cfg)
	return cfg, nil
}

// loadRaw reads and unmarshals the config file without applying system defaults.
// Returns an empty Config if the file doesn't exist (callers apply defaults).
func loadRaw(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := &Config{}
			cfg.configDir = filepath.Dir(configPath)
			return cfg, nil
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Store the config directory for resolving relative paths
	cfg.configDir = filepath.Dir(configPath)

	return &cfg, nil
}

// applySystemDefaults fills in zero-value fields with system defaults.
func applySystemDefaults(cfg *Config) {
	if cfg.Nibs.Path == "" {
		cfg.Nibs.Path = DefaultNibsPath
	}
	if cfg.Nibs.IDLength == 0 {
		cfg.Nibs.IDLength = 4
	}
	if cfg.Nibs.DefaultStatus == "" {
		cfg.Nibs.DefaultStatus = "todo"
	}
	if cfg.Nibs.DefaultType == "" {
		cfg.Nibs.DefaultType = DefaultTypes[0].Name
	}
}

// LoadFromDirectory finds and loads the config file by searching upward from the given directory.
// If no config file is found, returns a default config anchored at the given directory.
func LoadFromDirectory(startDir string) (*Config, error) {
	configPath, err := FindConfig(startDir)
	if err != nil {
		return nil, err
	}

	if configPath == "" {
		// No config found, return default anchored at startDir
		cfg := Default()
		cfg.configDir = startDir
		return cfg, nil
	}

	return Load(configPath)
}

// ResolveNibsPath returns the absolute path to the nibs directory.
func (c *Config) ResolveNibsPath() string {
	if filepath.IsAbs(c.Nibs.Path) {
		return c.Nibs.Path
	}
	if c.configDir == "" {
		// Fallback: use current directory
		cwd, _ := os.Getwd()
		return filepath.Join(cwd, c.Nibs.Path)
	}
	return filepath.Join(c.configDir, c.Nibs.Path)
}

// ConfigDir returns the directory containing the config file.
func (c *Config) ConfigDir() string {
	return c.configDir
}

// SetConfigDir sets the config directory (for testing or when creating new configs).
func (c *Config) SetConfigDir(dir string) {
	c.configDir = dir
}

// GetProjectName returns the project name derived from the config directory name.
// Falls back to "Nibs" when no config directory is set.
func (c *Config) GetProjectName() string {
	name := filepath.Base(c.configDir)
	if name == "." || name == "" {
		return "Nibs"
	}
	return name
}

// Save writes the configuration to the config file.
// If configDir is set, saves to that directory; otherwise saves to the given directory.
func (c *Config) Save(dir string) error {
	targetDir := c.configDir
	if targetDir == "" {
		targetDir = dir
	}
	path := filepath.Join(targetDir, ConfigFileName)

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// IsValidStatus returns true if the status is a valid hardcoded status.
func (c *Config) IsValidStatus(status string) bool {
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
		return s.Closed
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
		if s.Closed {
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
		return s.ReleasesDependents
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
		if s.ReleasesDependents {
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
		if s.Closed && !s.ReleasesDependents {
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
		if !s.Closed {
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
		return s.Startable
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
		if s.Startable {
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
