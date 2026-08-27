package tui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

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
	okFooter := m.renderFooter(paintedFrame{})
	m.statusKind = statusWarn
	warnFooter := m.renderFooter(paintedFrame{})

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

// b1 is what makes ms1's detail view draw the links box, which is the tallest
// thing in that frame and has its own floor. Without a link the geometry sweeps
// measured a detail view the links box was never part of.
//
// It blocks on the milestone rather than parenting under it because a milestone
// is not a legal parent here; the queue membership t1 already carries is not a
// link the detail view resolves.
func queueRefusalNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "ms1", Title: "Wave one", Type: "milestone", Status: "todo"},
		{ID: "t1", Title: "Still open", Type: "task", Status: "todo", Milestone: "ms1", MilestoneOrder: "a0"},
		{ID: "b1", Title: "Waiting on the wave", Type: "task", Status: "todo", BlockedBy: []string{"ms1"}},
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

// selectParent drives the open parent picker down to the named nib and
// confirms it, the way a user does.
func selectParent(t *testing.T, app *App, parentID string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		if item, ok := app.parentPicker.list.SelectedItem().(parentItem); ok && item.nib.ID == parentID {
			sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
			return
		}
		sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	t.Fatalf("could not reach parent %q in the picker", parentID)
}

// toggleBlockingTarget drives the open blocking picker down to the named nib
// and marks it, which is what the picker later confirms as a diff.
func toggleBlockingTarget(t *testing.T, app *App, targetID string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		if item, ok := app.blockingPicker.list.SelectedItem().(blockingItem); ok && item.nib.ID == targetID {
			sendKey(app, tea.KeyPressMsg{Code: ' ', Text: " "})
			return
		}
		sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	t.Fatalf("could not reach target %q in the picker", targetID)
}

// pickerHostView is one of the two views a picker is opened over. Each has its
// own footer, and the report has to reach the one on screen.
type pickerHostView struct {
	name   string
	enter  func(t *testing.T, app *App)
	footer func(app *App) (string, statusKind)
}

func pickerHostViews() []pickerHostView {
	return []pickerHostView{
		{
			name:   "list",
			footer: func(app *App) (string, statusKind) { return app.list.statusMessage, app.list.statusKind },
		},
		{
			name: "detail",
			enter: func(t *testing.T, app *App) {
				t.Helper()
				sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
				if app.state != viewDetail {
					t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
				}
			},
			footer: func(app *App) (string, statusKind) { return app.detail.statusMessage, app.detail.statusKind },
		},
	}
}

// expandHelp opens the help panel, which replaces the footer's help keys and
// takes rows the message has to be budgeted around.
func expandHelp(t *testing.T, app *App) {
	t.Helper()
	sendKey(app, tea.KeyPressMsg{Code: '?', Text: "?"})
	if !app.helpExpanded {
		t.Fatal("premise failed: ? did not expand the help panel")
	}
}

// reparentRefusalNibs is a move the parent picker offers and the write path
// refuses. The picker filters candidates by ValidParentTypes, and a task under
// a feature is a legal shape, so nothing there can pre-empt it; assignment
// exclusivity is what refuses, because t1 and f1 both hold a place in ms1's
// queue and a nib is never assigned alongside its own ancestor.
func reparentRefusalNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "ms1", Title: "Wave one", Type: "milestone", Status: "todo"},
		{ID: "f1", Title: "Sync engine", Type: "feature", Status: "todo", Milestone: "ms1", MilestoneOrder: "a0"},
		{ID: "t1", Title: "Wire the queue", Type: "task", Status: "todo", Milestone: "ms1", MilestoneOrder: "a1"},
	}
}

// Spelled out rather than built by calling the resolver: an expectation
// produced by re-running the production code asserts only that it is
// deterministic.
const reparentRefusal = "cannot move t1 under f1: t1 is assigned to milestone ms1 and f1 is assigned to milestone ms1 (a nib and its ancestor are never both assigned)"

// Unlike the type picker — which pre-filters through validTypesForNib, so a
// hierarchy-illegal flip is never offered — the parent picker can present a
// target the write path rejects. A refusal it drops leaves the picker closed,
// the list refreshed, and nothing said.
func TestReparentRefusalReachesTheFooter(t *testing.T) {
	const termWidth, termHeight = 160, 40
	want := "Parent change failed: " + reparentRefusal

	for _, helpOpen := range []bool{false, true} {
		for _, view := range pickerHostViews() {
			name := view.name
			if helpOpen {
				name += "/help-open"
			}
			t.Run(name, func(t *testing.T) {
				app := setupRealBackendApp(t, reparentRefusalNibs())
				if !focusOn(app, "t1") {
					t.Fatal("premise failed: could not focus the task")
				}
				if view.enter != nil {
					view.enter(t, app)
				}
				if helpOpen {
					expandHelp(t, app)
				}

				sendKey(app, tea.KeyPressMsg{Code: 'p', Text: "p"})
				if app.state != viewParentPicker {
					t.Fatalf("premise failed: expected the parent picker to open, got state %d", app.state)
				}
				selectParent(t, app, "f1")

				got, kind := view.footer(app)
				if got != want {
					t.Errorf("footer message = %q, want %q", got, want)
				}
				if kind != statusWarn {
					t.Errorf("statusKind = %v, want statusWarn — the refusal would render in the success color", kind)
				}
				if painted := frameText(app.View().Content, termWidth, termHeight); !strings.Contains(painted, want) {
					t.Errorf("the painted frame does not carry the refusal.\nwant: %q\ngot:  %q", want, painted)
				}
			})
		}
	}
}

// t2 is already blocked by t1, so marking t1 in t2's picker asks for the edge
// back. The picker offers every nib but the subject, which is what lets it
// present this refusal.
func blockingCycleNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "t1", Title: "Ship the parser", Type: "task", Status: "todo"},
		{ID: "t2", Title: "Ship the writer", Type: "task", Status: "todo", BlockedBy: []string{"t1"}},
	}
}

const blockingCycleRefusal = "would create cycle: [t1 t2 t1]"

func TestBlockingRefusalReachesTheFooter(t *testing.T) {
	const termWidth, termHeight = 160, 40
	want := "Blocking change failed: " + blockingCycleRefusal

	for _, helpOpen := range []bool{false, true} {
		for _, view := range pickerHostViews() {
			name := view.name
			if helpOpen {
				name += "/help-open"
			}
			t.Run(name, func(t *testing.T) {
				app := setupRealBackendApp(t, blockingCycleNibs())
				if !focusOn(app, "t2") {
					t.Fatal("premise failed: could not focus the blocked nib")
				}
				if view.enter != nil {
					view.enter(t, app)
				}
				if helpOpen {
					expandHelp(t, app)
				}

				sendKey(app, tea.KeyPressMsg{Code: 'b', Text: "b"})
				if app.state != viewBlockingPicker {
					t.Fatalf("premise failed: expected the blocking picker to open, got state %d", app.state)
				}
				toggleBlockingTarget(t, app, "t1")
				sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})

				got, kind := view.footer(app)
				if got != want {
					t.Errorf("footer message = %q, want %q", got, want)
				}
				if kind != statusWarn {
					t.Errorf("statusKind = %v, want statusWarn — the refusal would render in the success color", kind)
				}
				if painted := frameText(app.View().Content, termWidth, termHeight); !strings.Contains(painted, want) {
					t.Errorf("the painted frame does not carry the refusal.\nwant: %q\ngot:  %q", want, painted)
				}
			})
		}
	}
}

// One picker confirmation carries both an add list and a remove list, and the
// footer holds one message — so the two loops report through a single call
// rather than the second overwriting the first. The stub is what makes the
// remove side refusable at all: RemoveBlocking's only refusal is a write
// failure, so the real backend cannot be driven into one from the picker.
func TestBlockingFailuresFromBothLoopsAreReportedTogether(t *testing.T) {
	addRefusal := errors.New("would create cycle: [nib-2 nib-1 nib-2]")
	removeRefusal := errors.New("write refused: etag mismatch")

	tests := []struct {
		name      string
		addErr    error
		removeErr error
		want      string
	}{
		{
			name:   "the add loop names its reason",
			addErr: addRefusal,
			want:   "Blocking change failed: " + addRefusal.Error(),
		},
		{
			name:      "the remove loop names its reason",
			removeErr: removeRefusal,
			want:      "Blocking change failed: " + removeRefusal.Error(),
		},
		{
			name:      "both loops feed one count",
			addErr:    addRefusal,
			removeErr: removeRefusal,
			want:      "Blocking change failed for 2 link(s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, stub := setupTestApp(t, pickerTestNibs())
			_, cmd := app.Update(openBlockingPickerMsg{nibID: "nib-1", nibTitle: "First", currentBlocking: []string{"nib-3"}})
			processCmd(app, cmd)
			if app.state != viewBlockingPicker {
				t.Fatalf("premise failed: expected the blocking picker to open, got state %d", app.state)
			}
			stub.AddBlockingErr = tt.addErr
			stub.RemoveBlockingErr = tt.removeErr

			_, cmd = app.Update(blockingConfirmedMsg{nibID: "nib-1", toAdd: []string{"nib-2"}, toRemove: []string{"nib-3"}})
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

// A refused batch of moves is reported by count, and the nibs that were not
// refused are still moved — a refusal must not abort the rest.
func TestBatchReparentFailureIsReportedByCount(t *testing.T) {
	app, stub := setupTestApp(t, []*nib.Nib{
		{ID: "epic-1", Title: "Umbrella", Type: "epic", Status: "todo", Order: "1"},
		{ID: "nib-1", Title: "First", Type: "task", Status: "todo", Order: "2"},
		{ID: "nib-2", Title: "Second", Type: "task", Status: "todo", Order: "3"},
		{ID: "nib-3", Title: "Third", Type: "task", Status: "todo", Order: "4"},
	})
	ids := []string{"nib-1", "nib-2", "nib-3"}
	_, cmd := app.Update(openParentPickerMsg{
		nibIDs:   ids,
		nibTitle: "3 selected nibs",
		nibTypes: []string{"task", "task", "task"},
	})
	processCmd(app, cmd)
	if app.state != viewParentPicker {
		t.Fatalf("premise failed: expected the parent picker to open, got state %d", app.state)
	}
	stub.SetParentErrByID = map[string]error{
		"nib-1": errors.New(reparentRefusal),
		"nib-3": errors.New("some other refusal"),
	}

	_, cmd = app.Update(parentSelectedMsg{nibIDs: ids, parentID: "epic-1"})
	processCmd(app, cmd)

	want := "Parent change failed for 2 nib(s)"
	if got := app.list.statusMessage; got != want {
		t.Errorf("list footer message = %q, want %q", got, want)
	}
	if app.list.statusKind != statusWarn {
		t.Errorf("statusKind = %v, want statusWarn", app.list.statusKind)
	}
	var moved []string
	for _, call := range stub.SetParentCalls {
		moved = append(moved, call.ID)
	}
	if len(moved) != 1 || moved[0] != "nib-2" {
		t.Errorf("applied moves = %v, want only nib-2 — a refusal must not abort the rest", moved)
	}
}

// A re-parent that goes through leaves the footer alone, so the report cannot
// degrade into a message shown on every move. The move is read back afterwards
// so this cannot pass by nothing having happened.
func TestSuccessfulReparentReportsNothing(t *testing.T) {
	app := setupRealBackendApp(t, []*nib.Nib{
		{ID: "e1", Title: "Umbrella", Type: "epic", Status: "todo"},
		{ID: "t1", Title: "Wire the queue", Type: "task", Status: "todo"},
	})
	if !focusOn(app, "t1") {
		t.Fatal("premise failed: could not focus the task")
	}
	sendKey(app, tea.KeyPressMsg{Code: 'p', Text: "p"})
	if app.state != viewParentPicker {
		t.Fatalf("premise failed: expected the parent picker to open, got state %d", app.state)
	}
	selectParent(t, app, "e1")

	moved, err := app.backend.GetNib(context.Background(), "t1")
	if err != nil || moved == nil || moved.Parent != "e1" {
		t.Fatalf("premise failed: the move did not land (parent=%v, err=%v)", moved, err)
	}
	if got := app.list.statusMessage; got != "" {
		t.Errorf("list footer message = %q, want empty after an accepted move", got)
	}
	if app.list.statusKind != statusOK {
		t.Errorf("statusKind = %v, want statusOK", app.list.statusKind)
	}
}

// editorSession puts the app in the state an $EDITOR exit lands in: a nib whose
// file exists under the store root with an mtime later than the one recorded
// when the editor launched, which is what makes the app take the edit in.
func editorSession(t *testing.T, app *App, stub *StubBackend, id string) {
	t.Helper()
	root := t.TempDir()
	stub.RootDir = root
	rel := filepath.Join("data", id+".md")
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("creating the store's data directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, rel), []byte("edited by hand\n"), 0o644); err != nil {
		t.Fatalf("writing the nib file: %v", err)
	}
	n, ok := stub.Nibs[id]
	if !ok {
		t.Fatalf("premise failed: %s is not in the stub store", id)
	}
	n.Path = filepath.ToSlash(rel)
	app.editingNibID = id
	app.editingNibModTime = time.Now().Add(-time.Hour)
}

// The write that follows an $EDITOR session is a write, and a refused one has
// to say so. The user's text is already on disk, so the message must name the
// store as what turned it down rather than reporting a failed "reload" — which
// would read as the edit being safely in and merely not shown.
func TestRefusedEditorWriteReachesTheListFooter(t *testing.T) {
	app, stub := setupTestApp(t, pickerTestNibs())
	editorSession(t, app, stub, "nib-1")
	stub.ReloadErr = errors.New("nib-1: updating /store/data/nib-1.md: file does not exist")

	_, cmd := app.Update(editorFinishedMsg{})
	processCmd(app, cmd)

	want := editorTextSafeLead +
		" The store did not accept the edit to nib-1: nib-1: updating /store/data/nib-1.md: file does not exist"
	if got := app.list.statusMessage; got != want {
		t.Errorf("list footer message = %q, want %q", got, want)
	}
	if app.list.statusKind != statusWarn {
		t.Errorf("statusKind = %v, want statusWarn — it would render in the success color", app.list.statusKind)
	}
	if got := frameText(app.View().Content, 120, 40); !strings.Contains(got, want) {
		t.Errorf("the frame does not carry the refusal:\n%s", got)
	}
}

// The same refusal raised while the detail view is up has to land in the detail
// footer; the list footer is not on screen there.
func TestRefusedEditorWriteReachesTheDetailFooter(t *testing.T) {
	app, stub := setupTestApp(t, pickerTestNibs())
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.state != viewDetail {
		t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
	}
	editorSession(t, app, stub, "nib-1")
	stub.ReloadErr = errors.New(refusalReason)

	_, cmd := app.Update(editorFinishedMsg{})
	processCmd(app, cmd)

	want := editorTextSafeLead + " The store did not accept the edit to nib-1: " + refusalReason
	if got := app.detail.statusMessage; got != want {
		t.Errorf("detail footer message = %q, want %q", got, want)
	}
	if app.detail.statusKind != statusWarn {
		t.Errorf("detail statusKind = %v, want statusWarn", app.detail.statusKind)
	}
}

// The read that locates the nib is the first thing `nibs config set-prefix`
// running in another process breaks: the id this session opened the editor with
// no longer answers. Swallowing that leaves the edit unrecorded with nothing on
// screen — the same silence, one call earlier — so it is reported too.
func TestEditedNibMissingFromTheStoreIsReported(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(stub *StubBackend)
		want    string
	}{
		{
			name:    "lookup refused",
			arrange: func(stub *StubBackend) { stub.GetErr = errors.New("store is unreadable") },
			want:    editorTextSafeLead + " The store did not accept the edit to nib-1: store is unreadable",
		},
		{
			name:    "id no longer resolves",
			arrange: func(stub *StubBackend) { delete(stub.Nibs, "nib-1") },
			want:    editorTextSafeLead + " The store did not accept the edit to nib-1: nib-1 is no longer in the store",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, stub := setupTestApp(t, pickerTestNibs())
			editorSession(t, app, stub, "nib-1")
			tt.arrange(stub)

			_, cmd := app.Update(editorFinishedMsg{})
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

// A warning that is always up says nothing. An accepted write must leave the
// footer alone, and an untouched file must not be written back at all — the
// mtime taken before the editor launched is what tells the two apart.
func TestAcceptedEditorSessionSaysNothing(t *testing.T) {
	tests := []struct {
		name      string
		touch     bool
		wantWrite bool
	}{
		{name: "file saved", touch: true, wantWrite: true},
		{name: "editor quit without saving", touch: false, wantWrite: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, stub := setupTestApp(t, pickerTestNibs())
			editorSession(t, app, stub, "nib-1")
			if !tt.touch {
				// The recorded mtime is the file's own, so nothing looks newer.
				info, err := os.Stat(filepath.Join(stub.RootDir, "data", "nib-1.md"))
				if err != nil {
					t.Fatalf("stat: %v", err)
				}
				app.editingNibModTime = info.ModTime()
			}

			_, cmd := app.Update(editorFinishedMsg{})
			processCmd(app, cmd)

			if got := len(stub.ReloadCalls) > 0; got != tt.wantWrite {
				t.Errorf("write attempted = %v, want %v (calls: %v)", got, tt.wantWrite, stub.ReloadCalls)
			}
			if app.list.statusMessage != "" {
				t.Errorf("footer message = %q, want none", app.list.statusMessage)
			}
		})
	}
}

// The editor state is a one-shot guard for the session that just ended. Holding
// it past a refusal would make the NEXT editor exit write this nib again,
// against a timestamp from a different session.
func TestEditorStateIsClearedAfterARefusal(t *testing.T) {
	app, stub := setupTestApp(t, pickerTestNibs())
	editorSession(t, app, stub, "nib-1")
	stub.ReloadErr = errors.New(refusalReason)

	_, cmd := app.Update(editorFinishedMsg{})
	processCmd(app, cmd)

	if app.editingNibID != "" {
		t.Errorf("editingNibID = %q after a refusal, want it cleared", app.editingNibID)
	}
	if !app.editingNibModTime.IsZero() {
		t.Errorf("editingNibModTime = %v after a refusal, want the zero time", app.editingNibModTime)
	}
}

// The file the store says the nib lives in is the one the mtime comparison asks
// about, so a nib whose file is gone cannot be compared and cannot be taken in.
// That is the third way an editor session ends without the store recording it,
// and it is as silent as the other two were.
func TestUnreadableNibFileAfterEditingIsReported(t *testing.T) {
	app, stub := setupTestApp(t, pickerTestNibs())
	editorSession(t, app, stub, "nib-1")
	if err := os.Remove(filepath.Join(stub.RootDir, "data", "nib-1.md")); err != nil {
		t.Fatalf("removing the nib file: %v", err)
	}

	_, cmd := app.Update(editorFinishedMsg{})
	processCmd(app, cmd)

	// The file is gone, so the reassurance the other legs carry would be a lie
	// here: this leg gets a lead of its own.
	prefix := editorFileUnreadableLead + " The file recorded for nib-1 could not be read: stat "
	got := app.list.statusMessage
	if !strings.HasPrefix(got, prefix) {
		t.Errorf("list footer message = %q, want it to start %q", got, prefix)
	}
	// Neither claim about the user's text is this leg's to make. A stat that
	// failed cannot promise the text is sitting in the file — and it cannot say
	// the text is gone either: a concurrent `nibs config set-prefix` renames the
	// file while the TUI is suspended, and the stale Path the store hands back
	// stats ENOENT over a file holding every word the user wrote.
	for _, unwanted := range []string{"Your text is still in the file", "Nothing was saved"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("list footer message = %q — %q settles what became of the user's text, which a failed stat does not establish", got, unwanted)
		}
	}
	if app.list.statusKind != statusWarn {
		t.Errorf("statusKind = %v, want statusWarn", app.list.statusKind)
	}
}

// failedEditorLaunch produces the error a launch that never happened hands back,
// by attempting one. Building it rather than hand-rolling an error keeps the
// message these tests assert on the one a user would actually be shown.
func failedEditorLaunch(t *testing.T) (*exec.Cmd, error) {
	t.Helper()
	c := exec.Command(filepath.Join(t.TempDir(), "nibs-no-such-editor"))
	c.Stdout, c.Stderr = io.Discard, io.Discard
	err := c.Run()
	if err == nil {
		t.Fatal("premise failed: running a nonexistent editor succeeded")
	}
	if c.ProcessState != nil {
		t.Fatalf("premise failed: ProcessState = %v after a launch that never happened, want nil", c.ProcessState)
	}
	return c, err
}

// nonZeroEditorExit produces the error a process that ran and chose its own
// status hands back — `vi` quit with :cq. A bogus flag makes the test binary
// itself the stand-in, which needs no shell and so works on every platform.
func nonZeroEditorExit(t *testing.T) (*exec.Cmd, error) {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.nibs-no-such-flag")
	c.Stdout, c.Stderr = io.Discard, io.Discard
	err := c.Run()
	if err == nil {
		t.Fatal("premise failed: the stand-in editor exited zero")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("premise failed: err = %T %v, want an *exec.ExitError", err, err)
	}
	if c.ProcessState == nil {
		t.Fatal("premise failed: ProcessState = nil after a process that ran")
	}
	return c, err
}

// The handler reads whether the editor RAN, not what kind of error came back,
// and this pins both halves of that: the observation it rests on — exec.Cmd
// records a ProcessState only for a process it managed to start — and the
// callback that reads it, which tea.ExecProcess invokes out of reach of any test
// driving Update.
//
// The error alone cannot carry the distinction. tea.ExecProcess hands the
// callback the terminal's release and restore errors too, so "not an
// *exec.ExitError" would also cover a restore that failed after the editor ran
// perfectly well — reporting a finished editing session, whose file is now
// waiting to be taken back in, as a launch that never happened.
func TestTheEditorCallbackReadsWhetherTheProcessRan(t *testing.T) {
	tests := []struct {
		name        string
		arrange     func(t *testing.T) (*exec.Cmd, error)
		wantStarted bool
	}{
		{name: "the editor never started", arrange: failedEditorLaunch, wantStarted: false},
		{name: "the editor ran and exited non-zero", arrange: nonZeroEditorExit, wantStarted: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := tt.arrange(t)

			msg, ok := editorFinished(c)(err).(editorFinishedMsg)
			if !ok {
				t.Fatalf("callback returned %T, want an editorFinishedMsg", editorFinished(c)(err))
			}
			if msg.started != tt.wantStarted {
				t.Errorf("started = %v, want %v", msg.started, tt.wantStarted)
			}
			if msg.err != err {
				t.Errorf("err = %v, want %v", msg.err, err)
			}
		})
	}
}

// An $EDITOR that never started is a screen flicker and nothing else unless it
// is reported: no editor opened, so the user has no error text of their own to
// read, and nothing on screen changed to say the keypress landed.
func TestFailedEditorLaunchIsReported(t *testing.T) {
	app, stub := setupTestApp(t, pickerTestNibs())
	editorSession(t, app, stub, "nib-1")
	_, launchErr := failedEditorLaunch(t)

	_, cmd := app.Update(editorFinishedMsg{err: launchErr})
	processCmd(app, cmd)

	want := editorNotStartedLead + " The editor could not be started: " + launchErr.Error()
	if got := app.list.statusMessage; got != want {
		t.Errorf("list footer message = %q, want %q", got, want)
	}
	if app.list.statusKind != statusWarn {
		t.Errorf("statusKind = %v, want statusWarn — it would render in the success color", app.list.statusKind)
	}
	if got := frameText(app.View().Content, 120, 40); !strings.Contains(got, want) {
		t.Errorf("the frame does not carry the launch failure:\n%s", got)
	}
	// Nothing was edited, so nothing is taken back in: a write here would bump
	// updated_at for an editing session that never happened.
	if len(stub.ReloadCalls) > 0 {
		t.Errorf("write attempted after a launch that never happened: %v", stub.ReloadCalls)
	}
	// The id/mtime pair is a one-shot guard for the launch this msg ends. Held
	// past a failed launch it would make the NEXT editor exit write this nib.
	if app.editingNibID != "" {
		t.Errorf("editingNibID = %q after a failed launch, want it cleared", app.editingNibID)
	}
	if !app.editingNibModTime.IsZero() {
		t.Errorf("editingNibModTime = %v after a failed launch, want the zero time", app.editingNibModTime)
	}
}

// The same failure raised while the detail view is up has to land in the detail
// footer; the list footer is not on screen there.
func TestFailedEditorLaunchReachesTheDetailFooter(t *testing.T) {
	app, stub := setupTestApp(t, pickerTestNibs())
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.state != viewDetail {
		t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
	}
	editorSession(t, app, stub, "nib-1")
	_, launchErr := failedEditorLaunch(t)

	_, cmd := app.Update(editorFinishedMsg{err: launchErr})
	processCmd(app, cmd)

	want := editorNotStartedLead + " The editor could not be started: " + launchErr.Error()
	if got := app.detail.statusMessage; got != want {
		t.Errorf("detail footer message = %q, want %q", got, want)
	}
	if app.detail.statusKind != statusWarn {
		t.Errorf("detail statusKind = %v, want statusWarn", app.detail.statusKind)
	}
}

// The launch failure makes none of the claims the write-back's two leads make,
// because none of them is true here: there is no text of the user's to account
// for, no file was opened, and the store was never asked for anything — so
// restarting nibs to re-read it, which both of those leads prescribe, changes
// nothing about why the keypress did not open an editor.
func TestTheLaunchFailureBorrowsNeitherWriteBackLead(t *testing.T) {
	app, stub := setupTestApp(t, pickerTestNibs())
	editorSession(t, app, stub, "nib-1")
	_, launchErr := failedEditorLaunch(t)

	_, cmd := app.Update(editorFinishedMsg{err: launchErr})
	processCmd(app, cmd)

	got := app.list.statusMessage
	if got == "" {
		t.Fatal("premise failed: the failed launch was not reported at all")
	}
	for _, unwanted := range []string{editorTextSafeLead, editorFileUnreadableLead, "Restart nibs"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("list footer message = %q — %q speaks for a finished editor session, which this is not", got, unwanted)
		}
	}
}

// An editor that RAN chose its own exit status, and the user is usually the one
// who chose it: quitting `vi` with :cq exits non-zero on purpose, so a warning
// there would fire on an ordinary keystroke. Either way the file it may have
// saved is still taken back into the store.
func TestAnEditorThatRanIsTakenInWhateverItsExitStatus(t *testing.T) {
	_, exitErr := nonZeroEditorExit(t)
	tests := []struct {
		name string
		msg  editorFinishedMsg
	}{
		{name: "exited zero", msg: editorFinishedMsg{}},
		{name: "quit with :cq", msg: editorFinishedMsg{err: exitErr, started: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, stub := setupTestApp(t, pickerTestNibs())
			editorSession(t, app, stub, "nib-1")

			_, cmd := app.Update(tt.msg)
			processCmd(app, cmd)

			if app.list.statusMessage != "" {
				t.Errorf("footer message = %q, want none", app.list.statusMessage)
			}
			if len(stub.ReloadCalls) == 0 {
				t.Error("the saved file was not taken back into the store")
			}
		})
	}
}

// editorRefusalGeometries is where an $EDITOR refusal has to survive being cut.
// renderStatusMessage caps the footer at height/3 rows and drops the overflow
// with an ellipsis, so the narrow-and-short corner is where a sentence that ends
// with its remedy ends up saying only that something went wrong.
var (
	editorRefusalWidths  = []int{30, 34, 38, 80}
	editorRefusalHeights = heightSweep(16, 24)
)

// statusRegionIsOneLine reports the geometries where the footer grants the
// status message a single row: height/3 is one, or — with the help panel open —
// the region is two rows and the panel keeps one of them for itself.
func statusRegionIsOneLine(panelOpen bool, height int) bool {
	if !panelOpen {
		return maxStatusFooterLines(height) == 1
	}
	return min(maxStatusFooterLines(height), max(1, max(1, height-listBoxFloor)-1)) == 1
}

// An $EDITOR session the store did not record has two things to say — where the
// user's text is, and what to do next — and reaching app.list.statusMessage is
// not either of them. The message is cut to the footer's row budget with a
// trailing ellipsis, so a remedy at the end of the sentence is exactly what the
// user does not get: at eight, twelve and sixteen rows the old wording left the
// store's error text on screen and nothing at all about the file.
//
// Below forty columns a single-row region cannot carry both clauses whatever
// they say — twenty-five cells survive the wrap there — so those geometries pin
// the weaker property that still separates this wording from the old one: the
// row spends itself on the LEAD rather than on the store's error text.
//
// The launch failure is swept alongside them because it is cut hardest: the
// error it ends with is a whole filesystem path, so a lead that came second
// would leave the user reading the tail of a temp path and nothing else.
func TestTheEditorRefusalKeepsItsLeadOnScreen(t *testing.T) {
	legs := []struct {
		name     string
		lead     string
		arrange  func(t *testing.T, stub *StubBackend)
		finished func(t *testing.T) editorFinishedMsg
		want     []string
		floor    []string
	}{
		{
			name:    "the store refused the write",
			lead:    editorTextSafeLead,
			arrange: func(_ *testing.T, stub *StubBackend) { stub.ReloadErr = errors.New(refusalReason) },
			want:    []string{"Your text is still in the file", "Restart nibs"},
			floor:   []string{"Your text is still in"},
		},
		{
			name:    "the lookup was refused",
			lead:    editorTextSafeLead,
			arrange: func(_ *testing.T, stub *StubBackend) { stub.GetErr = errors.New("store is unreadable") },
			want:    []string{"Your text is still in the file", "Restart nibs"},
			floor:   []string{"Your text is still in"},
		},
		{
			name:    "the id no longer resolves",
			lead:    editorTextSafeLead,
			arrange: func(_ *testing.T, stub *StubBackend) { delete(stub.Nibs, "nib-1") },
			want:    []string{"Your text is still in the file", "Restart nibs"},
			floor:   []string{"Your text is still in"},
		},
		{
			name: "the file cannot be read",
			lead: editorFileUnreadableLead,
			arrange: func(t *testing.T, stub *StubBackend) {
				if err := os.Remove(filepath.Join(stub.RootDir, "data", "nib-1.md")); err != nil {
					t.Fatalf("removing the nib file: %v", err)
				}
			},
			want:  []string{"Nothing was recorded", "the file cannot be read", "Restart nibs"},
			floor: []string{"Nothing was recorded"},
		},
		{
			name:    "the editor never started",
			lead:    editorNotStartedLead,
			arrange: func(_ *testing.T, _ *StubBackend) {},
			finished: func(t *testing.T) editorFinishedMsg {
				_, err := failedEditorLaunch(t)
				return editorFinishedMsg{err: err}
			},
			want:  []string{"Nothing was opened and nothing changed", "Check $VISUAL"},
			floor: []string{"Nothing was opened"},
		},
	}

	for _, leg := range legs {
		for _, panelOpen := range []bool{false, true} {
			for _, height := range editorRefusalHeights {
				for _, width := range editorRefusalWidths {
					name := fmt.Sprintf("%s/%dx%d", leg.name, width, height)
					if panelOpen {
						name += "/help"
					}
					t.Run(name, func(t *testing.T) {
						app, stub := setupTestApp(t, pickerTestNibs())
						app.Update(tea.WindowSizeMsg{Width: width, Height: height})
						if panelOpen {
							sendKey(app, tea.KeyPressMsg{Code: '?', Text: "?"})
							if !app.helpExpanded {
								t.Fatal("premise failed: ? did not expand the help panel")
							}
						}
						editorSession(t, app, stub, "nib-1")
						leg.arrange(t, stub)
						finished := editorFinishedMsg{}
						if leg.finished != nil {
							finished = leg.finished(t)
						}

						_, cmd := app.Update(finished)
						processCmd(app, cmd)
						if app.list.statusMessage == "" {
							t.Fatal("premise failed: the editor session was not reported at all")
						}

						content := app.View().Content
						assertFrameFitsTerminal(t, content, width, height)
						painted := frameText(content, width, height)

						want := leg.want
						if statusRegionIsOneLine(panelOpen, height) && width < lipgloss.Width(leg.lead) {
							want = leg.floor
						}
						for _, fragment := range want {
							if !strings.Contains(painted, fragment) {
								t.Errorf("the frame does not carry %q — what reaches the user is the error text and nothing about what became of their edit:\n%s",
									fragment, painted)
							}
						}
					})
				}
			}
		}
	}
}

// editorLaunchDetailHeights are the heights the detail frame's arithmetic turns
// on: every short one individually, because the thresholds are boundaries, then
// the ordinary sizes.
var editorLaunchDetailHeights = heightSweep(16, 20, 24, 30)

// detailStatusRegionRows is how many rows the detail view grants its status
// message at a geometry — maxStatusFooterLines above the help row, and with the
// panel expanded the region's own budget, which keeps a row for the panel and is
// floored against the header plus a one-row body box.
//
// It is the message's budget, not the frame's: what the region cuts is already
// gone before the frame is composed. That separation is what keeps the sweep
// below an assertion about COMPOSITION — everything the region granted has to
// reach the painted frame — rather than a restatement of the wrap.
func detailStatusRegionRows(m detailModel, panelOpen bool) int {
	if !panelOpen {
		return maxStatusFooterLines(m.height)
	}
	return min(maxStatusFooterLines(m.height), max(1, max(1, m.height-m.contentFloor())-1))
}

// The launch message is raised while the detail view is up as readily as from
// the list, and the detail frame budgets its rows differently: the body gives
// its rows to the region, the header is taller, and the expanded panel is held
// back against a floor of its own. Neither sweep covered the composition —
// TestTheEditorRefusalKeepsItsLeadOnScreen never enters the detail view, and
// the detail geometry sweep carries the status-change refusal instead — so a
// change to either side could lose the remedy with the suite green.
//
// What is guaranteed, and what is not:
//
//   - the opening clause reaches the reader at every one of these geometries;
//   - so does the remedy, wherever the status region had the rows to grant it,
//     which is every geometry at eleven rows and taller;
//   - below that the region cuts the sentence before the frame sees it. Measured
//     today: with the panel open the region is one row at eight and nine, so the
//     remedy is gone at 48 columns and narrower, and two rows at ten, gone at 34
//     and narrower; with the footer it is two rows at eight, gone at 34 and
//     narrower. That is renderStatusMessage's documented cut on a 66-column
//     sentence, not a frame defect — the pre-existing editorTextSafeLead loses
//     "Restart nibs" the same way at eight and nine, at 40 columns and narrower,
//     the threshold differing only because what follows its lead is shorter.
func TestTheLaunchFailureKeepsItsRemedyInTheDetailFrame(t *testing.T) {
	const (
		floor  = "Nothing was opened"
		remedy = "Check $VISUAL and $EDITOR"
	)
	for _, panelOpen := range []bool{false, true} {
		for _, height := range editorLaunchDetailHeights {
			for _, width := range sweepWidths {
				name := fmt.Sprintf("%dx%d", width, height)
				if panelOpen {
					name += "/help"
				}
				t.Run(name, func(t *testing.T) {
					app, stub := setupTestApp(t, pickerTestNibs())
					app.Update(tea.WindowSizeMsg{Width: width, Height: height})
					sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
					if app.state != viewDetail {
						t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
					}
					if panelOpen {
						sendKey(app, tea.KeyPressMsg{Code: '?', Text: "?"})
						if !app.helpExpanded {
							t.Fatal("premise failed: ? did not expand the help panel")
						}
					}
					editorSession(t, app, stub, "nib-1")
					_, launchErr := failedEditorLaunch(t)

					_, cmd := app.Update(editorFinishedMsg{err: launchErr})
					processCmd(app, cmd)
					if app.detail.statusMessage == "" {
						t.Fatal("premise failed: the failed launch was not reported to the detail view at all")
					}

					content := app.View().Content
					assertFrameFitsTerminal(t, content, width, height)
					painted := frameText(content, width, height)

					if !strings.Contains(painted, floor) {
						t.Fatalf("the frame does not carry %q — the reader is left with a keypress that did nothing and no word about it:\n%s",
							floor, painted)
					}

					rows := detailStatusRegionRows(app.detail, panelOpen)
					granted := frameText(statusBlock(app.detail.statusMessage, app.detail.statusKind, width, rows), width, height)
					if strings.Contains(granted, remedy) {
						if !strings.Contains(painted, remedy) {
							t.Errorf("the status region granted %q at %d row(s) and the frame did not paint it — the composition lost what the message is for:\n%s",
								remedy, rows, painted)
						}
						return
					}
					// The region cut it. That is only ever the shortest
					// terminals; anywhere else it is a budget that shrank.
					if height > 10 {
						t.Errorf("the status region granted only %d row(s) at %dx%d and cut %q — the remedy is guaranteed from eleven rows up:\n%s",
							rows, width, height, remedy, painted)
					}
				})
			}
		}
	}
}
