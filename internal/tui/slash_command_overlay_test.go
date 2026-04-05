package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dimetron/pi-go/internal/extension"
)

func TestSlashCommandInventory_DedupesByPrecedenceAndPreservesOrder(t *testing.T) {
	im := NewInputModel(nil, nil, nil, "")
	im.ExtensionCommands = []extension.SlashCommand{
		{Name: "theme", Description: "extension theme override"},
		{Name: "demo", Description: "extension demo"},
		{Name: "/alpha", Description: "extension alpha"},
	}
	im.Skills = []extension.Skill{
		{Name: "demo", Description: "skill demo"},
		{Name: "help", Description: "skill help override"},
		{Name: "zeta", Description: "skill zeta"},
	}

	inventory := im.slashCommandInventory()

	if got := inventory.BuiltIns[0].Name; got != slashCommands[0] {
		t.Fatalf("expected first built-in command %q, got %q", slashCommands[0], got)
	}
	if got := inventory.BuiltIns[0].Description; got != slashCommandDesc(slashCommands[0]) {
		t.Fatalf("expected built-in description from slashCommandDesc, got %q", got)
	}

	gotExtensions := []string{inventory.Extensions[0].Name, inventory.Extensions[1].Name}
	wantExtensions := []string{"/demo", "/alpha"}
	if !reflect.DeepEqual(gotExtensions, wantExtensions) {
		t.Fatalf("expected extension commands %v, got %v", wantExtensions, gotExtensions)
	}

	gotSkills := []string{inventory.Skills[0].Name}
	wantSkills := []string{"/zeta"}
	if !reflect.DeepEqual(gotSkills, wantSkills) {
		t.Fatalf("expected skill commands %v, got %v", wantSkills, gotSkills)
	}

	orderedSkills := []extension.Skill{
		{Name: "beta", Description: "skill beta"},
		{Name: "gamma", Description: "skill gamma"},
		{Name: "theta", Description: "skill theta"},
	}
	orderedInput := NewInputModel(nil, orderedSkills, nil, "")
	orderedInventory := orderedInput.slashCommandInventory()
	gotOrderedSkills := []string{
		orderedInventory.Skills[0].Name,
		orderedInventory.Skills[1].Name,
		orderedInventory.Skills[2].Name,
	}
	wantOrderedSkills := []string{"/beta", "/gamma", "/theta"}
	if !reflect.DeepEqual(gotOrderedSkills, wantOrderedSkills) {
		t.Fatalf("expected skill order %v, got %v", wantOrderedSkills, gotOrderedSkills)
	}

	gotAll := im.AllCommandNames()
	wantSuffix := []string{"/demo", "/alpha", "/zeta"}
	if len(gotAll) < len(slashCommands)+len(wantSuffix) {
		t.Fatalf("expected at least %d commands, got %d", len(slashCommands)+len(wantSuffix), len(gotAll))
	}
	if !reflect.DeepEqual(gotAll[len(gotAll)-len(wantSuffix):], wantSuffix) {
		t.Fatalf("expected trailing commands %v, got %v", wantSuffix, gotAll[len(gotAll)-len(wantSuffix):])
	}
}

func TestBuildSlashCommandOverlayRows_InsertsOnlyNonEmptySectionHeaders(t *testing.T) {
	im := NewInputModel(nil, nil, nil, "")
	im.ExtensionCommands = []extension.SlashCommand{{Name: "demo", Description: "extension demo"}}

	rows := buildSlashCommandOverlayRows(im.slashCommandInventory())
	if len(rows) == 0 {
		t.Fatal("expected overlay rows")
	}

	if rows[0].Kind != slashCommandOverlayRowHeader {
		t.Fatalf("expected first row to be a header, got %+v", rows[0])
	}
	if rows[0].Selectable() {
		t.Fatal("expected header row to be non-selectable")
	}
	if rows[0].Header != "Built-in Commands" {
		t.Fatalf("expected built-in header, got %q", rows[0].Header)
	}

	headers := []string{}
	for _, row := range rows {
		if row.Kind == slashCommandOverlayRowHeader {
			headers = append(headers, row.Header)
		}
	}
	wantHeaders := []string{"Built-in Commands", "Extension Commands"}
	if !reflect.DeepEqual(headers, wantHeaders) {
		t.Fatalf("expected headers %v, got %v", wantHeaders, headers)
	}

	foundDemo := false
	for _, row := range rows {
		if row.Kind == slashCommandOverlayRowCommand && row.Name == "/demo" {
			foundDemo = true
			if row.Description != "extension demo" {
				t.Fatalf("expected extension description, got %q", row.Description)
			}
		}
		if row.Kind == slashCommandOverlayRowCommand && row.Name == "/help" {
			if row.Description != slashCommandDesc("/help") {
				t.Fatalf("expected built-in description from slashCommandDesc, got %q", row.Description)
			}
		}
	}
	if !foundDemo {
		t.Fatal("expected /demo command row")
	}
}

func TestNewSlashCommandOverlayState_SelectsFirstCommandRow(t *testing.T) {
	state := newSlashCommandOverlayState([]slashCommandOverlayRow{
		{Kind: slashCommandOverlayRowHeader, Header: "Built-in Commands"},
		{Kind: slashCommandOverlayRowCommand, Name: "/help", Description: slashCommandDesc("/help")},
		{Kind: slashCommandOverlayRowCommand, Name: "/clear", Description: slashCommandDesc("/clear")},
	})

	if state.SelectedIndex != 1 {
		t.Fatalf("expected first selectable row index 1, got %d", state.SelectedIndex)
	}
	if state.ScrollOffset != 0 {
		t.Fatalf("expected zero scroll offset, got %d", state.ScrollOffset)
	}
	if state.Height != 0 {
		t.Fatalf("expected zero height, got %d", state.Height)
	}
	if got, ok := state.SelectedRow(); !ok || got.Name != "/help" {
		t.Fatalf("expected selected row /help, got %+v ok=%v", got, ok)
	}
}

func TestBuildSlashCommandOverlayRows_EmptyInventory(t *testing.T) {
	rows := buildSlashCommandOverlayRows(slashCommandInventory{})
	if len(rows) != 0 {
		t.Fatalf("expected no rows, got %d", len(rows))
	}

	state := newSlashCommandOverlayState(rows)
	if state.SelectedIndex != -1 {
		t.Fatalf("expected no selection, got %d", state.SelectedIndex)
	}
	if _, ok := state.SelectedRow(); ok {
		t.Fatal("expected no selected row")
	}
}

func TestSlashCommandOverlaySelection_SkipsHeaders(t *testing.T) {
	state := newSlashCommandOverlayState([]slashCommandOverlayRow{
		{Kind: slashCommandOverlayRowHeader, Header: "Built-in Commands"},
		{Kind: slashCommandOverlayRowCommand, Name: "/help"},
		{Kind: slashCommandOverlayRowHeader, Header: "Extension Commands"},
		{Kind: slashCommandOverlayRowCommand, Name: "/demo"},
		{Kind: slashCommandOverlayRowCommand, Name: "/alpha"},
	})
	state.Height = 2

	state.Move(1)
	if state.SelectedIndex != 3 {
		t.Fatalf("expected selection to skip header and land on index 3, got %d", state.SelectedIndex)
	}

	state.Move(-1)
	if state.SelectedIndex != 1 {
		t.Fatalf("expected selection to move back to index 1, got %d", state.SelectedIndex)
	}
}

func TestSlashCommandOverlayScroll_ClampsAndKeepsSelectionVisible(t *testing.T) {
	state := newSlashCommandOverlayState([]slashCommandOverlayRow{
		{Kind: slashCommandOverlayRowHeader, Header: "Built-in Commands"},
		{Kind: slashCommandOverlayRowCommand, Name: "/help"},
		{Kind: slashCommandOverlayRowCommand, Name: "/clear"},
		{Kind: slashCommandOverlayRowHeader, Header: "Extension Commands"},
		{Kind: slashCommandOverlayRowCommand, Name: "/demo"},
		{Kind: slashCommandOverlayRowCommand, Name: "/alpha"},
	})
	state.Height = 2

	state.Move(1) // /clear
	state.Move(1) // /demo, should scroll
	if state.SelectedIndex != 4 {
		t.Fatalf("expected selected index 4, got %d", state.SelectedIndex)
	}
	if state.ScrollOffset != 3 {
		t.Fatalf("expected scroll offset 3, got %d", state.ScrollOffset)
	}

	state.Move(1) // /alpha
	if state.ScrollOffset != 4 {
		t.Fatalf("expected scroll offset 4, got %d", state.ScrollOffset)
	}

	state.Move(1) // clamp at bottom
	if state.SelectedIndex != 5 {
		t.Fatalf("expected selection to stay at last command, got %d", state.SelectedIndex)
	}
	if state.ScrollOffset != 4 {
		t.Fatalf("expected scroll offset to stay clamped at 4, got %d", state.ScrollOffset)
	}

	state.Move(-10)
	if state.SelectedIndex != 1 {
		t.Fatalf("expected selection to clamp to first command, got %d", state.SelectedIndex)
	}
	if state.ScrollOffset != 1 {
		t.Fatalf("expected scroll offset to keep selection visible at 1, got %d", state.ScrollOffset)
	}
}

func TestSlashCommandOverlayVisibleRows(t *testing.T) {
	state := newSlashCommandOverlayState([]slashCommandOverlayRow{
		{Kind: slashCommandOverlayRowHeader, Header: "Built-in Commands"},
		{Kind: slashCommandOverlayRowCommand, Name: "/help"},
		{Kind: slashCommandOverlayRowCommand, Name: "/clear"},
		{Kind: slashCommandOverlayRowHeader, Header: "Extension Commands"},
		{Kind: slashCommandOverlayRowCommand, Name: "/demo"},
	})
	state.Height = 2
	state.ScrollOffset = 1

	visible := state.VisibleRows()
	if len(visible) != 2 {
		t.Fatalf("expected 2 visible rows, got %d", len(visible))
	}
	if visible[0].Name != "/help" || visible[1].Name != "/clear" {
		t.Fatalf("unexpected visible rows: %+v", visible)
	}
}

func TestSlashCommandOverlayHasVisibleSelectableRow(t *testing.T) {
	state := newSlashCommandOverlayState([]slashCommandOverlayRow{{Kind: slashCommandOverlayRowHeader, Header: "Only Header"}})
	state.Height = 1
	if state.HasVisibleSelectableRow() {
		t.Fatal("expected no visible selectable rows")
	}

	state = newSlashCommandOverlayState([]slashCommandOverlayRow{
		{Kind: slashCommandOverlayRowHeader, Header: "Built-in Commands"},
		{Kind: slashCommandOverlayRowCommand, Name: "/help"},
	})
	state.Height = 1
	state.ScrollOffset = 1
	if !state.HasVisibleSelectableRow() {
		t.Fatal("expected visible selectable row")
	}
}

func TestRenderSlashCommandOverlay_IncludesViewportRows(t *testing.T) {
	state := newSlashCommandOverlayState([]slashCommandOverlayRow{
		{Kind: slashCommandOverlayRowHeader, Header: "Built-in Commands"},
		{Kind: slashCommandOverlayRowCommand, Name: "/help", Description: "Show help"},
		{Kind: slashCommandOverlayRowCommand, Name: "/clear", Description: "Clear conversation"},
		{Kind: slashCommandOverlayRowHeader, Header: "Extension Commands"},
		{Kind: slashCommandOverlayRowCommand, Name: "/demo", Description: "Demo command"},
	})
	state.Height = 3
	state.ScrollOffset = 1
	state.SelectedIndex = 2

	out := state.render(60)
	if !strings.Contains(out, "Slash Commands") || !strings.Contains(out, "2/3") {
		t.Fatalf("expected selection indicator, got %q", out)
	}
	if !strings.Contains(out, "/help") || !strings.Contains(out, "/clear") {
		t.Fatalf("expected visible command rows, got %q", out)
	}
	if strings.Contains(out, "/demo") {
		t.Fatalf("did not expect offscreen row /demo in %q", out)
	}
}

func TestRenderSlashCommandOverlay_HidesDescriptionsInNarrowWidths(t *testing.T) {
	state := newSlashCommandOverlayState([]slashCommandOverlayRow{
		{Kind: slashCommandOverlayRowHeader, Header: "Built-in Commands"},
		{Kind: slashCommandOverlayRowCommand, Name: "/help", Description: "Show help"},
	})
	state.Height = 2

	out := state.render(30)
	if !strings.Contains(out, "/help") {
		t.Fatalf("expected command name in narrow render, got %q", out)
	}
	if strings.Contains(out, "Show help") {
		t.Fatalf("expected description to be hidden in narrow render, got %q", out)
	}
}
