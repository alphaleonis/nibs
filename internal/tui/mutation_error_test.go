package tui

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/ui"
)

// refusalReason stands in for a real refusal's message, shaped like the queue
// guard's. The stub-backend tests assert it reaches the footer verbatim; the
// real guard's own text is checked against the resolver further down.
const refusalReason = "cannot close tnib-mile: 3 open nibs are still assigned to its queue"

func pickerTestNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "nib-1", Title: "First", Type: "milestone", Status: "todo", Order: "1"},
		{ID: "nib-2", Title: "Second", Type: "task", Status: "todo", Order: "2"},
		{ID: "nib-3", Title: "Third", Type: "task", Status: "todo", Order: "3"},
	}
}

// selectStatus drives the open status picker down to the named status and
// confirms it, the way a user does.
func selectStatus(t *testing.T, app *App, status string) {
	t.Helper()
	for i := 0; i < len(config.DefaultStatuses); i++ {
		item, ok := app.statusPicker.list.SelectedItem().(statusItem)
		if ok && item.name == status {
			sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
			return
		}
		sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	t.Fatalf("could not reach status %q in the picker", status)
}

// A refused status change must not vanish with the closing picker: the whole
// point of the queue guard is the reason it gives.
func TestStatusRefusalReachesTheListFooter(t *testing.T) {
	app, stub := setupTestApp(t, pickerTestNibs())
	stub.UpdateErr = errors.New(refusalReason)

	sendKey(app, tea.KeyPressMsg{Code: 's', Text: "s"})
	if app.state != viewStatusPicker {
		t.Fatalf("premise failed: expected the status picker to open, got state %d", app.state)
	}
	selectStatus(t, app, "completed")

	want := "Status change failed: " + refusalReason
	if got := app.list.statusMessage; got != want {
		t.Errorf("list footer message = %q, want %q", got, want)
	}
	if app.list.statusKind != statusWarn {
		t.Errorf("statusKind = %v, want statusWarn — it would render in the success color", app.list.statusKind)
	}
	if content := app.View().Content; !strings.Contains(content, refusalReason) {
		t.Errorf("rendered view does not carry the reason:\n%s", content)
	}
}

// The same refusal raised from the detail view has to land in the detail
// footer — the list footer is not on screen there.
func TestStatusRefusalReachesTheDetailFooter(t *testing.T) {
	app, stub := setupTestApp(t, pickerTestNibs())
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.state != viewDetail {
		t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
	}
	stub.UpdateErr = errors.New(refusalReason)

	sendKey(app, tea.KeyPressMsg{Code: 's', Text: "s"})
	if app.state != viewStatusPicker {
		t.Fatalf("premise failed: expected the status picker to open, got state %d", app.state)
	}
	selectStatus(t, app, "completed")

	if app.state != viewDetail {
		t.Fatalf("expected to return to the detail view, got state %d", app.state)
	}
	want := "Status change failed: " + refusalReason
	if got := app.detail.statusMessage; got != want {
		t.Errorf("detail footer message = %q, want %q", got, want)
	}
	if app.detail.statusKind != statusWarn {
		t.Errorf("detail statusKind = %v, want statusWarn — it would render in the success color", app.detail.statusKind)
	}
	if content := app.View().Content; !strings.Contains(content, refusalReason) {
		t.Errorf("rendered detail view does not carry the reason:\n%s", content)
	}
}

// Every picker that writes through UpdateNib reports its failure; none of them
// may swallow it.
func TestPickerMutationFailureIsReported(t *testing.T) {
	ids := []string{"nib-1"}
	tests := []struct {
		name     string
		open     tea.Msg
		selected tea.Msg
		want     string
	}{
		{
			name:     "status",
			open:     openStatusPickerMsg{nibIDs: ids, nibTitle: "First", currentStatus: "todo"},
			selected: statusSelectedMsg{nibIDs: ids, status: "completed"},
			want:     "Status change failed: " + refusalReason,
		},
		{
			name:     "type",
			open:     openTypePickerMsg{nibIDs: ids, nibTitle: "First", currentType: "milestone"},
			selected: typeSelectedMsg{nibIDs: ids, nibType: "epic"},
			want:     "Type change failed: " + refusalReason,
		},
		{
			name:     "priority",
			open:     openPriorityPickerMsg{nibIDs: ids, nibTitle: "First", currentPriority: "normal"},
			selected: prioritySelectedMsg{nibIDs: ids, priority: "high"},
			want:     "Priority change failed: " + refusalReason,
		},
		{
			name:     "estimate",
			open:     openEstimatePickerMsg{nibIDs: ids, nibTitle: "First", currentEstimate: "m"},
			selected: estimateSelectedMsg{nibIDs: ids, estimate: "xl"},
			want:     "Estimate change failed: " + refusalReason,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, stub := setupTestApp(t, pickerTestNibs())
			_, cmd := app.Update(tt.open)
			processCmd(app, cmd)
			stub.UpdateErr = errors.New(refusalReason)

			_, cmd = app.Update(tt.selected)
			processCmd(app, cmd)

			if got := app.list.statusMessage; got != tt.want {
				t.Errorf("list footer message = %q, want %q", got, tt.want)
			}
			if app.list.statusKind != statusWarn {
				t.Errorf("statusKind = %v, want statusWarn", app.list.statusKind)
			}
		})
	}
}

// A batch is reported by count: several wrapped reasons would take a quarter of
// the screen from the list they are about, and the count is what tells the user
// the batch was not applied whole.
func TestBatchMutationFailureIsReportedByCount(t *testing.T) {
	app, stub := setupTestApp(t, pickerTestNibs())
	ids := []string{"nib-1", "nib-2", "nib-3"}
	_, cmd := app.Update(openStatusPickerMsg{nibIDs: ids, nibTitle: "3 selected nibs"})
	processCmd(app, cmd)

	stub.UpdateErrByID = map[string]error{
		"nib-1": errors.New(refusalReason),
		"nib-3": errors.New("some other refusal"),
	}

	_, cmd = app.Update(statusSelectedMsg{nibIDs: ids, status: "completed"})
	processCmd(app, cmd)

	want := "Status change failed for 2 nib(s)"
	if got := app.list.statusMessage; got != want {
		t.Errorf("list footer message = %q, want %q", got, want)
	}
	if app.list.statusKind != statusWarn {
		t.Errorf("statusKind = %v, want statusWarn", app.list.statusKind)
	}
	// The nib that was not refused must still have been written.
	var wrote []string
	for _, call := range stub.UpdateCalls {
		wrote = append(wrote, call.ID)
	}
	if len(wrote) != 1 || wrote[0] != "nib-2" {
		t.Errorf("applied updates = %v, want only nib-2 — a refusal must not abort the rest", wrote)
	}
}

// A change that succeeds leaves the footer alone, so the report cannot degrade
// into a message shown on every edit.
func TestSuccessfulStatusChangeReportsNothing(t *testing.T) {
	app, _ := setupTestApp(t, pickerTestNibs())
	sendKey(app, tea.KeyPressMsg{Code: 's', Text: "s"})
	selectStatus(t, app, "completed")

	if got := app.list.statusMessage; got != "" {
		t.Errorf("list footer message = %q, want empty after a successful change", got)
	}
	if app.list.statusKind != statusOK {
		t.Errorf("statusKind = %v, want statusOK", app.list.statusKind)
	}
}

// The detail footer renders every status message in the success color unless
// the kind reaches the style, which would paint a refusal as though the change
// went through.
func TestDetailFooterColorsARefusalAsAWarning(t *testing.T) {
	n := &nib.Nib{ID: "nib-1", Title: "First", Type: "milestone", Status: "todo"}
	m := newDetailModel(n, &StubBackend{}, config.Default(), 80, 24)
	m.statusMessage = "Status change failed: " + refusalReason

	m.statusKind = statusOK
	okFooter := m.renderFooter()
	m.statusKind = statusWarn
	warnFooter := m.renderFooter()

	if warnFooter == okFooter {
		t.Fatal("the detail footer renders a warning identically to a success — statusKind never reaches the style")
	}
	// Spelled from the palette entry and the SGR grammar rather than from the
	// style expression the footer uses: an expectation built by re-running the
	// production code asserts only that the production code is deterministic.
	if got := fgSGRParams(ui.ColorWarning); !strings.Contains(warnFooter, got) {
		t.Errorf("detail footer does not paint the message amber (%s):\n%q", got, warnFooter)
	}
	if got := fgSGRParams(ui.ColorSuccess); strings.Contains(warnFooter, got) {
		t.Errorf("detail footer still paints the refusal in the success color (%s):\n%q", got, warnFooter)
	}
	// The success color is what the same message reads as without the kind, so
	// the two are distinguishable and the check above is not vacuous.
	if got := fgSGRParams(ui.ColorSuccess); !strings.Contains(okFooter, got) {
		t.Errorf("premise failed: an ok message is not painted in the success color (%s):\n%q", got, okFooter)
	}
}

// fgSGRParams is the ECMA-48 SGR parameter run that selects c as a 24-bit
// foreground. It is matched as a substring so the assertion does not depend on
// which other attributes share the escape or in what order.
func fgSGRParams(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

// A refusal's warning color must not outlive its message. The next message the
// detail footer shows — a successful copy, say — sets no kind of its own and
// would inherit the stale warning.
func TestDetailRefusalKindIsClearedOnTheNextKeypress(t *testing.T) {
	app, stub := setupTestApp(t, pickerTestNibs())
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.state != viewDetail {
		t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
	}
	stub.UpdateErr = errors.New(refusalReason)
	sendKey(app, tea.KeyPressMsg{Code: 's', Text: "s"})
	selectStatus(t, app, "completed")
	if app.detail.statusKind != statusWarn {
		t.Fatalf("premise failed: expected the refusal to be marked as a warning, got %v", app.detail.statusKind)
	}

	sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})

	if got := app.detail.statusMessage; got != "" {
		t.Errorf("detail footer message = %q, want it cleared", got)
	}
	if app.detail.statusKind != statusOK {
		t.Errorf("detail statusKind = %v, want statusOK — the next message would inherit the warning color", app.detail.statusKind)
	}
}

// setupRealBackendApp wires the TUI to the real resolver and core the way
// cmd/tui.go does, so a guard's own refusal can be read where the user reads
// it rather than through a stand-in error.
func setupRealBackendApp(t *testing.T, nibs []*nib.Nib) *App {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(store.NewLayout(nibsDir).DataDir(), 0755); err != nil {
		t.Fatalf("create test store: %v", err)
	}
	cfg := config.Default()
	core := nibcore.New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("load core: %v", err)
	}
	for _, b := range nibs {
		if err := core.Create(b); err != nil {
			t.Fatalf("create %s: %v", b.ID, err)
		}
	}
	resolver := &graph.Resolver{
		Reader:    core,
		Writer:    core,
		Validator: core,
		Blocking:  core,
		Orderer:   graph.NewOrderer(core, core),
	}
	app := New(NewRealBackend(core, resolver), cfg, "dev")
	initCmd := app.Init()
	app.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	processCmd(app, initCmd)
	return app
}

// The acceptance case the nib was written for, driven end to end: closing a
// milestone that still holds open queue entries is refused by the model, and
// the TUI must show that refusal rather than close the picker over it.
func TestMilestoneQueueRefusalReachesTheFooter(t *testing.T) {
	t.Run("one milestone names the reason", func(t *testing.T) {
		app := setupRealBackendApp(t, []*nib.Nib{
			{ID: "ms1", Title: "Wave one", Type: "milestone", Status: "todo"},
			{ID: "t1", Title: "Still open", Type: "task", Status: "todo", Milestone: "ms1", MilestoneOrder: "a0"},
		})
		if !focusOn(app, "ms1") {
			t.Fatal("could not focus the milestone")
		}

		sendKey(app, tea.KeyPressMsg{Code: 's', Text: "s"})
		selectStatus(t, app, "completed")

		// The holding status is spelled out rather than read back from the
		// config the app was built with: asking the same lookup the code asks
		// would assert whatever it returned.
		want := (&graph.MilestoneQueueOpenError{
			MilestoneID: "ms1",
			Status:      "completed",
			Open:        []string{"t1"},
			Holding:     []string{"deferred"},
		}).Error()
		if got := app.list.statusMessage; got != "Status change failed: "+want {
			t.Errorf("list footer message = %q, want %q", got, "Status change failed: "+want)
		}
		if app.list.statusKind != statusWarn {
			t.Errorf("statusKind = %v, want statusWarn", app.list.statusKind)
		}
		if content := app.View().Content; !strings.Contains(content, "Status change failed") {
			t.Errorf("rendered view does not show the refusal:\n%s", content)
		}
	})

	t.Run("a refused batch reports how many failed", func(t *testing.T) {
		app := setupRealBackendApp(t, []*nib.Nib{
			{ID: "ms1", Title: "Wave one", Type: "milestone", Status: "todo"},
			{ID: "ms2", Title: "Wave two", Type: "milestone", Status: "todo"},
			{ID: "t1", Title: "Still open", Type: "task", Status: "todo", Milestone: "ms1", MilestoneOrder: "a0"},
			{ID: "t2", Title: "Also open", Type: "task", Status: "todo", Milestone: "ms2", MilestoneOrder: "a0"},
		})
		if !focusOn(app, "ms1") {
			t.Fatal("could not focus the first milestone")
		}
		sendKey(app, tea.KeyPressMsg{Code: ' ', Text: " "})
		if !focusOn(app, "ms2") {
			t.Fatal("could not focus the second milestone")
		}
		sendKey(app, tea.KeyPressMsg{Code: ' ', Text: " "})
		if !app.list.selectedNibs["ms1"] || !app.list.selectedNibs["ms2"] || len(app.list.selectedNibs) != 2 {
			t.Fatalf("premise failed: expected ms1 and ms2 marked, got %v", app.list.selectedNibs)
		}

		sendKey(app, tea.KeyPressMsg{Code: 's', Text: "s"})
		selectStatus(t, app, "completed")

		want := "Status change failed for 2 nib(s)"
		if got := app.list.statusMessage; got != want {
			t.Errorf("list footer message = %q, want %q", got, want)
		}
		if app.list.statusKind != statusWarn {
			t.Errorf("statusKind = %v, want statusWarn", app.list.statusKind)
		}
		if content := app.View().Content; !strings.Contains(content, want) {
			t.Errorf("rendered view does not show the count:\n%s", content)
		}
	})
}

// The type picker reaches a refusal of its own: stripping milestone-hood from a
// nib that still holds assignments. The picker offers "epic" — the queue is not
// a parent/child constraint, so nothing filters it out — which makes this a
// refusal the TUI can actually raise, and therefore one it must report.
func TestMilestoneRetypeRefusalReachesTheFooter(t *testing.T) {
	app := setupRealBackendApp(t, []*nib.Nib{
		{ID: "ms1", Title: "Wave one", Type: "milestone", Status: "todo"},
		{ID: "t1", Title: "Assigned work", Type: "task", Status: "todo", Milestone: "ms1", MilestoneOrder: "a0"},
	})
	if !focusOn(app, "ms1") {
		t.Fatal("could not focus the milestone")
	}

	sendKey(app, tea.KeyPressMsg{Code: 't', Text: "t"})
	if app.state != viewTypePicker {
		t.Fatalf("premise failed: expected the type picker to open, got state %d", app.state)
	}
	// "e" is the type picker's first-letter shortcut for epic.
	sendKey(app, tea.KeyPressMsg{Code: 'e', Text: "e"})

	want := "Type change failed: " + (&graph.MilestoneRetypeError{
		MilestoneID: "ms1",
		NewType:     "epic",
		Held:        []string{"t1"},
	}).Error()
	if got := app.list.statusMessage; got != want {
		t.Errorf("list footer message = %q, want %q", got, want)
	}
	if app.list.statusKind != statusWarn {
		t.Errorf("statusKind = %v, want statusWarn", app.list.statusKind)
	}
	if content := app.View().Content; !strings.Contains(content, "Type change failed") {
		t.Errorf("rendered view does not show the refusal:\n%s", content)
	}
}

// ansiSGR matches the color escapes a rendered frame carries, so the frame can
// be read back as the text a reader sees.
var ansiSGR = regexp.MustCompile("\x1b\\[[0-9;]*m")

// screen reproduces what the alt screen actually paints. Bubbletea draws the
// frame into a fixed width x height cell grid with wrapping off and drops every
// cell outside it — no wrap, no ellipsis. Reading View().Content directly would
// credit the app with text the terminal never shows.
func screen(content string, width, height int) string {
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	clip := lipgloss.NewStyle().MaxWidth(width)
	for i, line := range lines {
		lines[i] = clip.Render(line)
	}
	return strings.Join(lines, "\n")
}

// frameText reads a painted frame back as one whitespace-collapsed line, which
// is how the eye reads a message wrapped across several of them.
func frameText(content string, width, height int) string {
	return strings.Join(strings.Fields(ansiSGR.ReplaceAllString(screen(content, width, height), "")), " ")
}

// queueRefusal is the acceptance case's own refusal, 207 columns of it — 229
// once the footer names the action — so wider than any terminal these tests run
// at, with the remedy (the half that says how to proceed) at the far end.
func queueRefusal() string {
	return (&graph.MilestoneQueueOpenError{
		MilestoneID: "ms1",
		Status:      "completed",
		Open:        []string{"t1"},
		Holding:     []string{"deferred"},
	}).Error()
}

func queueRefusalNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "ms1", Title: "Wave one", Type: "milestone", Status: "todo"},
		{ID: "t1", Title: "Still open", Type: "task", Status: "todo", Milestone: "ms1", MilestoneOrder: "a0"},
	}
}

// A refusal the terminal cannot fit is not one the user can act on. Bubbletea
// draws the alt screen into a fixed cell grid with wrapping off, so every cell
// outside it is dropped with no ellipsis and nothing on screen to say anything
// is missing — which cost the acceptance case its remedy at every width tried.
// Reaching app.detail.statusMessage is therefore not the property; being inside
// the frame is.
func TestRefusalIsLegibleAtEveryTerminalWidth(t *testing.T) {
	const termHeight = 24
	want := "Status change failed: " + queueRefusal()

	views := []struct {
		name  string
		enter func(t *testing.T, app *App, width int)
	}{
		{name: "list"},
		{
			name: "detail",
			enter: func(t *testing.T, app *App, _ int) {
				sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
				if app.state != viewDetail {
					t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
				}
			},
		},
		{
			// W leaves wide mode, which is what puts the list beside the
			// preview pane — a different composition, with its own footer
			// arithmetic.
			name: "two-column",
			enter: func(t *testing.T, app *App, width int) {
				sendKey(app, tea.KeyPressMsg{Code: 'W', Text: "W"})
				if want := width >= TwoColumnMinWidth; app.isTwoColumnMode() != want {
					t.Fatalf("premise failed: two-column mode = %v at width %d, want %v", app.isTwoColumnMode(), width, want)
				}
			},
		},
	}

	for _, width := range []int{80, 100, 120, 160} {
		for _, view := range views {
			t.Run(fmt.Sprintf("%s/%d", view.name, width), func(t *testing.T) {
				app := setupRealBackendApp(t, queueRefusalNibs())
				app.Update(tea.WindowSizeMsg{Width: width, Height: termHeight})
				if !focusOn(app, "ms1") {
					t.Fatal("premise failed: could not focus the milestone")
				}
				if view.enter != nil {
					view.enter(t, app, width)
				}

				sendKey(app, tea.KeyPressMsg{Code: 's', Text: "s"})
				selectStatus(t, app, "completed")

				content := app.View().Content
				if got := frameText(content, width, termHeight); !strings.Contains(got, want) {
					t.Errorf("the frame does not carry the whole refusal.\nwant: %q\ngot:  %q", want, got)
				}
				if got := lipgloss.Height(content); got > termHeight {
					t.Errorf("frame is %d lines tall for a %d-line terminal — the bottom is clipped, the footer's last line first", got, termHeight)
				}
				if got := lipgloss.Width(content); got > width {
					t.Errorf("frame is %d columns wide for a %d-column terminal — the right edge is cut", got, width)
				}
			})
		}
	}
}

// The detail footer's help row names the keys the reader acts with, so it has
// to survive a refusal being shown. Writing the message in front of it pushed
// the whole row off the right edge for as long as the message was up.
func TestDetailHelpRowSurvivesARefusal(t *testing.T) {
	app := setupRealBackendApp(t, queueRefusalNibs())
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !focusOn(app, "ms1") {
		t.Fatal("premise failed: could not focus the milestone")
	}
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.state != viewDetail {
		t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
	}

	sendKey(app, tea.KeyPressMsg{Code: 's', Text: "s"})
	selectStatus(t, app, "completed")

	got := frameText(app.View().Content, 80, 24)
	for _, key := range []string{"e edit", "s status", "esc back", "q quit"} {
		if !strings.Contains(got, key) {
			t.Errorf("the help row lost %q while a refusal was up:\n%s", key, got)
		}
	}
}

// A refusal has to outlive a file-watcher tick. Agents and the CLI write nibs
// while a session is open, so a tick lands within seconds of the refusal — and
// rebuilding the detail model from scratch would take the message away before
// the user had acted on it, which is the swallow this whole change removed.
func TestDetailRefusalSurvivesAWatcherTick(t *testing.T) {
	app, stub := setupTestApp(t, pickerTestNibs())
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.state != viewDetail {
		t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
	}
	stub.UpdateErr = errors.New(refusalReason)
	sendKey(app, tea.KeyPressMsg{Code: 's', Text: "s"})
	selectStatus(t, app, "completed")

	want := "Status change failed: " + refusalReason
	if got := app.detail.statusMessage; got != want {
		t.Fatalf("premise failed: detail footer message = %q, want %q", got, want)
	}

	// Cleared so nothing can re-raise the refusal: what survives has to be the
	// message that was already up.
	stub.UpdateErr = nil
	_, cmd := app.Update(nibsChangedMsg{})
	processCmd(app, cmd)

	if got := app.detail.statusMessage; got != want {
		t.Errorf("detail footer message = %q after a watcher tick, want %q", got, want)
	}
	if app.detail.statusKind != statusWarn {
		t.Errorf("detail statusKind = %v after a watcher tick, want statusWarn — the refusal would turn green", app.detail.statusKind)
	}
	if got := frameText(app.View().Content, 120, 40); !strings.Contains(got, want) {
		t.Errorf("the frame does not carry the refusal after a watcher tick:\n%s", got)
	}
}

// The wrapped block must not be padded out to the width it wrapped at: callers
// append to it — the list its update indicator, the detail view its help row —
// and padding would push those past the edge the wrapping exists to stay
// inside.
func TestWrappedStatusMessageIsNotPaddedOut(t *testing.T) {
	const width = 40
	tests := []struct {
		name string
		msg  string
	}{
		{name: "short", msg: "Status change failed: refused"},
		{name: "wrapped", msg: "Status change failed: " + queueRefusal()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := strings.Split(renderStatusMessage(tt.msg, statusWarn, width, 0), "\n")
			for i, line := range lines {
				if got := lipgloss.Width(line); got > width {
					t.Errorf("line %d is %d columns wide, want at most %d: %q", i, got, width, line)
				}
			}
			last := lines[len(lines)-1]
			if got := lipgloss.Width(last); got >= width {
				t.Errorf("the last line is padded out to %d columns, leaving no room for what follows it: %q", got, last)
			}
		})
	}
}

// A refusal is allowed to take rows from the view above it, but not to take the
// view. A queue refusal naming hundreds of nibs wraps to more lines than the
// terminal has, and an uncapped footer would push the list off the screen and
// then be clipped itself — leaving neither the message nor what it is about.
func TestAnOverlongRefusalIsCappedRatherThanEatingTheView(t *testing.T) {
	const termWidth, termHeight = 80, 24
	huge := "cannot close ms1: " + strings.Repeat("tnib-abcd, ", 300) + "are still assigned to its queue"

	tests := []struct {
		name  string
		enter func(t *testing.T, app *App)
	}{
		{name: "list"},
		{
			name: "detail",
			enter: func(t *testing.T, app *App) {
				sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
				if app.state != viewDetail {
					t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, stub := setupTestApp(t, pickerTestNibs())
			app.Update(tea.WindowSizeMsg{Width: termWidth, Height: termHeight})
			if tt.enter != nil {
				tt.enter(t, app)
			}
			stub.UpdateErr = errors.New(huge)
			sendKey(app, tea.KeyPressMsg{Code: 's', Text: "s"})
			selectStatus(t, app, "completed")

			content := app.View().Content
			if got := lipgloss.Height(content); got > termHeight {
				t.Errorf("frame is %d lines tall for a %d-line terminal — the footer ate the view and was clipped with it", got, termHeight)
			}
			if got := lipgloss.Width(content); got > termWidth {
				t.Errorf("frame is %d columns wide for a %d-column terminal", got, termWidth)
			}
			// The cut has to be visible: a message that just stops reads as the
			// whole reason.
			if got := frameText(content, termWidth, termHeight); !strings.Contains(got, "…") {
				t.Errorf("the truncated refusal carries no ellipsis, so nothing says it was cut:\n%s", got)
			}
		})
	}
}
