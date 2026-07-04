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
// Order determines sort priority: in-progress first (active work), then todo, draft, and done states last.
var DefaultStatuses = []StatusConfig{
	{Name: "in-progress", Color: "yellow", Description: "Currently being worked on"},
	{Name: "todo", Color: "green", Description: "Ready to be worked on"},
	{Name: "draft", Color: "blue", Description: "Needs refinement before it can be worked on"},
	{Name: "deferred", Color: "gray", Description: "Parked — not actionable now, but not abandoned (scrapped) or merely unrefined (draft)"},
	{Name: "completed", Color: "gray", Archive: true, Description: "Finished successfully"},
	{Name: "scrapped", Color: "gray", Archive: true, Description: "Will not be done"},
}

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
type StatusConfig struct {
	Name        string `yaml:"name"`
	Color       string `yaml:"color"`
	Archive     bool   `yaml:"archive,omitempty"`
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
func FindConfig(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	for {
		configPath := filepath.Join(dir, ConfigFileName)
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
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

// IsArchiveStatus returns true if the given status is marked for archiving.
// Statuses are hardcoded and not configurable.
func (c *Config) IsArchiveStatus(name string) bool {
	if s := c.GetStatus(name); s != nil {
		return s.Archive
	}
	return false
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
	IsArchive     bool
}

// GetNibColors returns the resolved colors for a nib based on its status, type, and priority.
func (c *Config) GetNibColors(status, typeName, priority string) NibColors {
	colors := NibColors{
		StatusColor:   "gray",
		TypeColor:     "",
		PriorityColor: "",
		IsArchive:     false,
	}

	if statusCfg := c.GetStatus(status); statusCfg != nil {
		colors.StatusColor = statusCfg.Color
	}
	colors.IsArchive = c.IsArchiveStatus(status)

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

// HideCompleted returns whether completed/scrapped nibs should be hidden.
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
