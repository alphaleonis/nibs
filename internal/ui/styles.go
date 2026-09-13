package ui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/alphaleonis/nibs/internal/config"
)

// Color palette
var (
	ColorPrimary   = lipgloss.Color("#7C3AED") // Purple
	ColorSecondary = lipgloss.Color("#6B7280") // Gray
	ColorSuccess   = lipgloss.Color("#10B981") // Green
	ColorWarning   = lipgloss.Color("#F59E0B") // Amber
	ColorDanger    = lipgloss.Color("#EF4444") // Red
	ColorMuted     = lipgloss.Color("#9CA3AF") // Light gray
	ColorSubtle    = lipgloss.Color("#555555") // Dark gray (for tree lines)
	ColorBlue      = lipgloss.Color("#3B82F6") // Blue
	ColorCyan      = lipgloss.Color("14")      // Bright Cyan (ANSI)

	// The closed-status ramp: one color per status for dark and light terminals
	// alike. Neutral grays clearing 3:1 against #1c1c1c, #000000, #fdfdfd and
	// #ffffff span #67 to #93; completed and scrapped sit near its ends, with
	// completed the lighter, as on the web.
	ColorMagenta   = lipgloss.Color("#AA6693") // Deferred — set aside, not finished
	ColorGrayLight = lipgloss.Color("#90939B") // Completed
	ColorGrayDim   = lipgloss.Color("#64676F") // Scrapped — dimmer than completed
)

// NamedColors maps color names to colors.
var NamedColors = map[string]color.Color{
	"green":  ColorSuccess,
	"yellow": ColorWarning,
	"red":    ColorDanger,
	"gray":   ColorSecondary,
	"grey":   ColorSecondary,
	"blue":   ColorBlue,
	"purple": ColorPrimary,
	"cyan":   ColorCyan,
	// The low priority uses "gray"; the closed-status ramp has its own names.
	"magenta":   ColorMagenta,
	"lightgray": ColorGrayLight,
	"dimgray":   ColorGrayDim,
}

// ResolveColor converts a color name or hex code to a color.
func ResolveColor(name string) color.Color {
	if strings.HasPrefix(name, "#") {
		return lipgloss.Color(name)
	}
	if c, ok := NamedColors[strings.ToLower(name)]; ok {
		return c
	}
	return ColorMuted
}

// IsValidColor reports whether color is a named color, or starts with "#" and
// is 4 or 7 bytes long. The characters after "#" are not checked.
func IsValidColor(color string) bool {
	if strings.HasPrefix(color, "#") {
		return len(color) == 4 || len(color) == 7
	}
	_, ok := NamedColors[strings.ToLower(color)]
	return ok
}

var TagBadge = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#000")).
	Background(ColorMuted).
	Padding(0, 1)

// RenderTag renders a single tag as a badge
func RenderTag(tag string) string {
	return TagBadge.Render(tag)
}

// RenderTags renders multiple tags as badges separated by spaces
func RenderTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	rendered := make([]string, len(tags))
	for i, tag := range tags {
		rendered[i] = RenderTag(tag)
	}
	return strings.Join(rendered, " ")
}

// RenderTagsCompact renders up to maxTags badges, then "+N" for the rest. A tag
// over 12 bytes is cut to its first 10 bytes plus "..".
func RenderTagsCompact(tags []string, maxTags int) string {
	if len(tags) == 0 {
		return ""
	}
	if maxTags <= 0 {
		maxTags = 1
	}

	showTags := tags
	var extra int
	if len(tags) > maxTags {
		showTags = tags[:maxTags]
		extra = len(tags) - maxTags
	}

	rendered := make([]string, len(showTags))
	for i, tag := range showTags {
		displayTag := tag
		if len(displayTag) > 12 {
			displayTag = displayTag[:10] + ".."
		}
		rendered[i] = RenderTag(displayTag)
	}

	result := strings.Join(rendered, " ")
	if extra > 0 {
		result += Muted.Render(fmt.Sprintf(" +%d", extra))
	}
	return result
}

// RenderDocuments renders document paths as a labeled line.
// Returns empty string when docs is empty.
func RenderDocuments(docs []string) string {
	if len(docs) == 0 {
		return ""
	}
	label := Muted.Render("Docs:")
	rendered := make([]string, len(docs))
	for i, d := range docs {
		rendered[i] = Path.Render(d)
	}
	return label + " " + strings.Join(rendered, "  ")
}

var (
	Bold      = lipgloss.NewStyle().Bold(true)
	Muted     = lipgloss.NewStyle().Foreground(ColorMuted)
	Primary   = lipgloss.NewStyle().Foreground(ColorPrimary)
	Success   = lipgloss.NewStyle().Foreground(ColorSuccess)
	Warning   = lipgloss.NewStyle().Foreground(ColorWarning)
	Danger    = lipgloss.NewStyle().Foreground(ColorDanger)
	Secondary = lipgloss.NewStyle().Foreground(ColorSecondary)
)

var ID = lipgloss.NewStyle().
	Foreground(ColorPrimary).
	Bold(true)

var TreeLine = lipgloss.NewStyle().Foreground(ColorSubtle)

var Title = lipgloss.NewStyle().Bold(true)

var Path = lipgloss.NewStyle().Foreground(ColorMuted)

var Header = lipgloss.NewStyle().
	Foreground(ColorPrimary).
	Bold(true).
	MarginBottom(1)

// RenderStatusWithColor returns a styled status badge using the specified color.
func RenderStatusWithColor(status, color string, isClosedStatus bool) string {
	c := ResolveColor(color)
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#fff")).
		Background(c).
		Padding(0, 1)

	if !isClosedStatus {
		style = style.Bold(true)
	}

	return style.Render(status)
}

// RenderStatusTextWithColor returns styled status text using the specified color.
func RenderStatusTextWithColor(status, color string, isClosedStatus bool) string {
	c := ResolveColor(color)
	style := lipgloss.NewStyle().Foreground(c)

	if !isClosedStatus {
		style = style.Bold(true)
	}

	return style.Render(status)
}

// RenderTypeText returns styled type text using the specified color.
// If color is empty, uses muted styling.
func RenderTypeText(typeName, color string) string {
	if typeName == "" {
		return ""
	}
	if color == "" {
		return Muted.Render(typeName)
	}
	c := ResolveColor(color)
	return lipgloss.NewStyle().Foreground(c).Render(typeName)
}

// RenderTypeWithColor returns a styled type badge with colored background.
func RenderTypeWithColor(typeName, color string) string {
	if typeName == "" {
		return ""
	}
	c := ResolveColor(color)
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#fff")).
		Background(c).
		Bold(true).
		Padding(0, 1)
	return style.Render(typeName)
}

// RenderEstimateWithColor returns a styled estimate badge using the specified color.
func RenderEstimateWithColor(estimate, color string) string {
	if estimate == "" {
		return ""
	}
	c := ResolveColor(color)
	style := lipgloss.NewStyle().
		Foreground(c)
	return style.Render("[" + strings.ToUpper(estimate) + "]")
}

// RenderPriorityWithColor returns a styled priority badge using the specified color.
func RenderPriorityWithColor(priority, color string) string {
	if priority == "" {
		return ""
	}
	c := ResolveColor(color)
	style := lipgloss.NewStyle().
		Foreground(c).
		Bold(priority == "critical" || priority == "high")
	return style.Render("[" + priority + "]")
}

// RenderPriorityText returns styled priority text.
func RenderPriorityText(priority, color string) string {
	if priority == "" {
		return ""
	}
	c := ResolveColor(color)
	style := lipgloss.NewStyle().Foreground(c)
	if priority == "critical" || priority == "high" {
		style = style.Bold(true)
	}
	return style.Render(priority)
}

// ShortType returns the uppercased first letter of a type in
// config.DefaultTypes, or "?".
func ShortType(t string) string {
	if t == "" {
		return "?"
	}
	for _, def := range config.DefaultTypes {
		if def.Name == t {
			return strings.ToUpper(def.Name[:1])
		}
	}
	return "?"
}

// ShortStatus returns the uppercased first letter of a status in
// config.DefaultStatuses, or "?". deferred is "F", since draft has "D".
func ShortStatus(s string) string {
	if s == "" {
		return "?"
	}
	if s == "deferred" {
		return "F"
	}
	for _, def := range config.DefaultStatuses {
		if def.Name == s {
			return strings.ToUpper(def.Name[:1])
		}
	}
	return "?"
}

// GetPrioritySymbol returns the raw symbol for a priority without styling.
// Returns empty string for normal/empty priority.
func GetPrioritySymbol(priority string) string {
	switch priority {
	case "critical":
		return glyphCritical()
	case "high":
		return glyphHigh()
	case "low":
		return glyphLow()
	default:
		return ""
	}
}

// RenderPrioritySymbol returns the styled priority symbol, or "" for
// normal/empty priority.
func RenderPrioritySymbol(priority, color string) string {
	symbol := GetPrioritySymbol(priority)
	if symbol == "" {
		return ""
	}

	c := ResolveColor(color)
	style := lipgloss.NewStyle().Foreground(c)
	if priority == "critical" || priority == "high" {
		style = style.Bold(true)
	}
	return style.Render(symbol)
}

// NibRowConfig holds configuration for rendering a nib row
type NibRowConfig struct {
	StatusColor   string
	TypeColor     string
	PriorityColor string
	Priority      string // Priority value (critical, high, normal, low)
	IsClosed      bool
	MaxTitleWidth int  // 0 means no truncation
	ShowCursor    bool // Show selection cursor
	IsSelected    bool
	IsMarked      bool     // Marked for multi-select batch operations
	Tags          []string // Tags to display (optional)
	ShowTags      bool     // Whether to show tags column
	TagsColWidth  int      // Width of tags column (0 = default)
	MaxTags       int      // Max tags to show (0 = default of 1)
	TreePrefix    string   // Tree prefix to prepend to the ID, e.g. "│  └─ "
	Dimmed        bool     // Render row dimmed (for unmatched ancestor nibs in tree)
	IDColWidth    int      // Width of ID column (0 = default of ColWidthID)
	UseFullNames  bool     // Use full type/status names instead of single-char abbreviations
	IsBlocked     bool     // Show blocked indicator (red dot)
	IsBlocking    bool     // Show blocking indicator (amber diamond)

}

// Base column widths for nib lists (minimum sizes)
const (
	ColWidthID     = 12
	ColWidthStatus = 3
	ColWidthType   = 3
	ColWidthTags   = 24
)

// ResponsiveColumns holds calculated column widths based on available space
type ResponsiveColumns struct {
	ID                int
	Status            int
	Type              int
	Tags              int
	MaxTags           int // How many tags to show
	ShowTags          bool
	UseFullTypeStatus bool // Use full names instead of single-char abbreviations
}

// CalculateResponsiveColumns determines column widths based on available width.
// Tags are shown only when the title keeps at least 50 cells.
func CalculateResponsiveColumns(totalWidth int, hasTags bool) ResponsiveColumns {
	cols := ResponsiveColumns{
		ID:       ColWidthID,
		Status:   ColWidthStatus,
		Type:     ColWidthType,
		Tags:     0,
		MaxTags:  0,
		ShowTags: false,
	}

	const minWidthForFullNames = 120
	if totalWidth >= minWidthForFullNames {
		cols.UseFullTypeStatus = true
		cols.Status = 12 // "in-progress" needs 11 chars
		cols.Type = 10   // "milestone" needs 9 chars
	}

	const minWidthForTags = 140

	if !hasTags || totalWidth < minWidthForTags {
		return cols
	}

	cursorWidth := 2
	baseWidth := cursorWidth + cols.ID + cols.Status + cols.Type
	available := totalWidth - baseWidth

	minTitleWidth := 50
	spaceForTags := available - minTitleWidth

	if spaceForTags >= ColWidthTags {
		cols.ShowTags = true

		if spaceForTags >= 80 {
			cols.Tags = 70
			cols.MaxTags = 5
		} else if spaceForTags >= 60 {
			cols.Tags = 55
			cols.MaxTags = 4
		} else if spaceForTags >= 45 {
			cols.Tags = 42
			cols.MaxTags = 3
		} else if spaceForTags >= 35 {
			cols.Tags = 32
			cols.MaxTags = 2
		} else {
			cols.Tags = ColWidthTags
			cols.MaxTags = 1
		}
	}

	return cols
}

// truncateCells cuts s to at most width display cells, keeping or dropping each
// rune whole.
func truncateCells(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

// RenderNibRow renders a nib as a single row with ID, Type, Status, Tags (optional), Title
func RenderNibRow(id, status, typeName, title string, cfg NibRowConfig) string {
	idColWidth := ColWidthID
	if cfg.IDColWidth > 0 {
		idColWidth = cfg.IDColWidth
	}
	typeStyle := lipgloss.NewStyle().Width(ColWidthType)
	statusStyle := lipgloss.NewStyle().Width(ColWidthStatus)

	tagsColWidth := ColWidthTags
	if cfg.TagsColWidth > 0 {
		tagsColWidth = cfg.TagsColWidth
	}
	tagsStyle := lipgloss.NewStyle().Width(tagsColWidth)

	maxTags := 1
	if cfg.MaxTags > 0 {
		maxTags = cfg.MaxTags
	}

	highlightStyle := lipgloss.NewStyle().Foreground(ColorWarning)

	var idCol string
	// Runes, not bytes: each tree glyph is one cell.
	visualWidth := len([]rune(cfg.TreePrefix)) + len(id)
	padding := ""
	if idColWidth > visualWidth {
		padding = strings.Repeat(" ", idColWidth-visualWidth)
	}
	if cfg.Dimmed {
		idCol = Muted.Render(cfg.TreePrefix) + Muted.Render(id) + padding
	} else if cfg.IsMarked {
		// Marking highlights the ID column only.
		idCol = highlightStyle.Render(cfg.TreePrefix) + highlightStyle.Render(id) + padding
	} else {
		idCol = TreeLine.Render(cfg.TreePrefix) + ID.Render(id) + padding
	}

	var typeStr string
	if cfg.UseFullNames {
		typeStr = typeName
		typeStyle = typeStyle.Width(12)
	} else {
		typeStr = ShortType(typeName)
	}
	var typeCol string
	if cfg.Dimmed {
		typeCol = typeStyle.Render(Muted.Render(typeStr))
	} else {
		typeCol = typeStyle.Render(RenderTypeText(typeStr, cfg.TypeColor))
	}

	var statusStr string
	if cfg.UseFullNames {
		statusStr = status
		statusStyle = statusStyle.Width(12)
	} else {
		statusStr = ShortStatus(status)
	}
	var statusCol string
	if cfg.Dimmed {
		statusCol = statusStyle.Render(Muted.Render(statusStr))
	} else {
		statusCol = statusStyle.Render(RenderStatusTextWithColor(statusStr, cfg.StatusColor, cfg.IsClosed))
	}

	var tagsCol string
	if cfg.ShowTags {
		if cfg.Dimmed {
			if len(cfg.Tags) > 0 {
				tagsCol = tagsStyle.Render(Muted.Render(cfg.Tags[0]))
			} else {
				tagsCol = tagsStyle.Render("")
			}
		} else {
			tagsCol = tagsStyle.Render(RenderTagsCompact(cfg.Tags, maxTags))
		}
	}

	// The indicator column is indicatorColWidth cells, right-aligned: a 2-cell
	// slot for blocked or blocking (blocked wins), then a 2-cell priority slot.
	const indicatorColWidth = 4

	// padSlot pads a one-cell glyph to fill its 2-cell slot.
	padSlot := func(rendered, raw string) string {
		if lipgloss.Width(raw) >= 2 {
			return rendered
		}
		return rendered + " "
	}

	var slot1, slot2 string
	slot1Width := 0
	slot2Width := 0

	if !cfg.Dimmed && cfg.IsBlocked {
		raw := glyphBlocked()
		slot1 = padSlot(lipgloss.NewStyle().Foreground(ColorDanger).Bold(true).Render(raw), raw)
		slot1Width = 2
	} else if !cfg.Dimmed && cfg.IsBlocking {
		raw := glyphBlocking()
		slot1 = padSlot(lipgloss.NewStyle().Foreground(ColorWarning).Bold(true).Render(raw), raw)
		slot1Width = 2
	}

	if !cfg.Dimmed {
		raw := GetPrioritySymbol(cfg.Priority)
		if raw != "" {
			slot2 = padSlot(RenderPrioritySymbol(cfg.Priority, cfg.PriorityColor), raw)
			slot2Width = 2
		}
	}

	indicatorCol := slot1 + slot2
	indicatorWidth := slot1Width + slot2Width

	if indicatorWidth < indicatorColWidth {
		indicatorCol = strings.Repeat(" ", indicatorColWidth-indicatorWidth) + indicatorCol
	}

	// The title shares MaxTitleWidth with the indicator column. A budget the
	// indicators use up yields an empty title, not an unlimited one. Cuts are
	// measured in display cells.
	displayTitle := title
	titleColWidth := cfg.MaxTitleWidth
	if cfg.MaxTitleWidth > 0 {
		maxWidth := cfg.MaxTitleWidth - indicatorColWidth
		switch {
		case maxWidth <= 0:
			displayTitle = ""
		case maxWidth <= 3:
			// No room for the "..." marker, so the cut goes unmarked.
			displayTitle = truncateCells(title, maxWidth)
		case lipgloss.Width(title) > maxWidth:
			displayTitle = truncateCells(title, maxWidth-3) + "..."
		}
	}

	var cursor string
	var titleStyled string
	if cfg.ShowCursor {
		if cfg.IsSelected {
			cursor = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render(glyphCursor())
			titleStyled = lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render(displayTitle)
		} else {
			cursor = " "
			if cfg.Dimmed {
				titleStyled = Muted.Render(displayTitle)
			} else {
				titleStyled = displayTitle
			}
		}
	} else {
		cursor = ""
		if cfg.Dimmed {
			titleStyled = Muted.Render(displayTitle)
		} else {
			titleStyled = displayTitle
		}
	}

	if cfg.ShowTags {
		// Pad the title column so the tags line up.
		titleLen := lipgloss.Width(displayTitle) + indicatorColWidth
		padding := ""
		if titleColWidth > titleLen {
			padding = strings.Repeat(" ", titleColWidth-titleLen)
		}
		return cursor + idCol + " " + typeCol + " " + statusCol + " " + indicatorCol + titleStyled + padding + " " + tagsCol
	}
	return cursor + idCol + " " + typeCol + " " + statusCol + " " + indicatorCol + titleStyled
}
