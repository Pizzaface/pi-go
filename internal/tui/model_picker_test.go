package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pizzaface/go-pi/internal/provider"
)

func TestBuildPickerEntries(t *testing.T) {
	models := []provider.ModelEntry{
		{ID: "claude-sonnet-4", Provider: "anthropic", DisplayName: "Claude Sonnet 4"},
		{ID: "claude-opus-4", Provider: "anthropic", DisplayName: "Claude Opus 4"},
		{ID: "gpt-4o", Provider: "openai", DisplayName: "gpt-4o"},
	}

	entries := buildPickerEntries(models)

	// Expect: header(anthropic), model, model, header(openai), model = 5 entries.
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
	if entries[0].providerHeader != "anthropic" {
		t.Errorf("entry 0: expected anthropic header, got %q", entries[0].providerHeader)
	}
	if entries[1].model == nil || entries[1].model.ID != "claude-sonnet-4" {
		t.Error("entry 1: expected claude-sonnet-4 model")
	}
	if entries[2].model == nil || entries[2].model.ID != "claude-opus-4" {
		t.Error("entry 2: expected claude-opus-4 model")
	}
	if entries[3].providerHeader != "openai" {
		t.Errorf("entry 3: expected openai header, got %q", entries[3].providerHeader)
	}
	if entries[4].model == nil || entries[4].model.ID != "gpt-4o" {
		t.Error("entry 4: expected gpt-4o model")
	}
}

func TestBuildPickerEntriesEmpty(t *testing.T) {
	entries := buildPickerEntries(nil)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestModelPickerFilter(t *testing.T) {
	models := []provider.ModelEntry{
		{ID: "claude-sonnet-4", Provider: "anthropic"},
		{ID: "claude-opus-4", Provider: "anthropic"},
		{ID: "gpt-4o", Provider: "openai"},
		{ID: "gemini-2.5-pro", Provider: "gemini"},
	}
	all := buildPickerEntries(models)

	pk := &modelPickerState{
		entries: all,
		all:     all,
		height:  20,
	}

	// Filter for "claude" should keep only anthropic header + 2 claude models.
	pk.filter = "claude"
	pk.applyFilter()

	if len(pk.entries) != 3 { // header + 2 models
		t.Fatalf("expected 3 entries for 'claude' filter, got %d", len(pk.entries))
	}
	if pk.entries[0].providerHeader != "anthropic" {
		t.Error("expected anthropic header")
	}

	// Filter for "gpt" should match only openai.
	pk.filter = "gpt"
	pk.applyFilter()

	if len(pk.entries) != 2 { // header + 1 model
		t.Fatalf("expected 2 entries for 'gpt' filter, got %d", len(pk.entries))
	}

	// Clear filter restores all.
	pk.filter = ""
	pk.applyFilter()

	if len(pk.entries) != len(all) {
		t.Errorf("expected all entries restored, got %d", len(pk.entries))
	}
}

func TestModelPickerFilterNoMatch(t *testing.T) {
	models := []provider.ModelEntry{
		{ID: "claude-sonnet-4", Provider: "anthropic"},
	}
	all := buildPickerEntries(models)

	pk := &modelPickerState{
		entries: all,
		all:     all,
		height:  20,
	}

	pk.filter = "nonexistent"
	pk.applyFilter()

	if len(pk.entries) != 0 {
		t.Errorf("expected 0 entries for non-matching filter, got %d", len(pk.entries))
	}
}

func TestModelPickerNavigation(t *testing.T) {
	models := []provider.ModelEntry{
		{ID: "model-a", Provider: "prov1"},
		{ID: "model-b", Provider: "prov1"},
		{ID: "model-c", Provider: "prov2"},
	}
	all := buildPickerEntries(models)
	// Layout: header(prov1) idx=0, model-a idx=1, model-b idx=2, header(prov2) idx=3, model-c idx=4

	pk := &modelPickerState{
		entries:  all,
		all:      all,
		selected: 1, // model-a
		height:   20,
	}

	// Move down: should go to model-b (idx 2).
	pk.moveDown()
	if pk.selected != 2 {
		t.Errorf("after moveDown: expected selected=2, got %d", pk.selected)
	}

	// Move down: should skip header (idx 3) and go to model-c (idx 4).
	pk.moveDown()
	if pk.selected != 4 {
		t.Errorf("after moveDown: expected selected=4 (skip header), got %d", pk.selected)
	}

	// Move down at bottom: should stay at 4.
	pk.moveDown()
	if pk.selected != 4 {
		t.Errorf("after moveDown at bottom: expected selected=4, got %d", pk.selected)
	}

	// Move up: should go back to model-b (idx 2), skipping header.
	pk.moveUp()
	if pk.selected != 2 {
		t.Errorf("after moveUp: expected selected=2, got %d", pk.selected)
	}

	// Move up to first model.
	pk.moveUp()
	if pk.selected != 1 {
		t.Errorf("after moveUp: expected selected=1, got %d", pk.selected)
	}

	// Move up at top: should stay at 1.
	pk.moveUp()
	if pk.selected != 1 {
		t.Errorf("after moveUp at top: expected selected=1, got %d", pk.selected)
	}
}

func TestModelPickerSelectCurrent(t *testing.T) {
	models := []provider.ModelEntry{
		{ID: "model-a", Provider: "prov"},
		{ID: "model-b", Provider: "prov"},
		{ID: "model-c", Provider: "prov"},
	}
	all := buildPickerEntries(models)
	// Layout: header idx=0, model-a idx=1, model-b idx=2, model-c idx=3

	pk := &modelPickerState{
		entries: all,
		all:     all,
		current: "model-b",
		height:  20,
	}

	pk.selectCurrent()
	if pk.selected != 2 {
		t.Errorf("expected selected=2 for model-b, got %d", pk.selected)
	}
}

func TestModelPickerSelectedModel(t *testing.T) {
	models := []provider.ModelEntry{
		{ID: "model-a", Provider: "prov"},
		{ID: "model-b", Provider: "prov"},
	}
	all := buildPickerEntries(models)

	pk := &modelPickerState{
		entries:  all,
		all:      all,
		selected: 2, // model-b
		height:   20,
	}

	sel := pk.selectedModel()
	if sel == nil {
		t.Fatal("expected non-nil selected model")
	}
	if sel.ID != "model-b" {
		t.Errorf("expected model-b, got %q", sel.ID)
	}

	// Selected on header returns nil.
	pk.selected = 0
	sel = pk.selectedModel()
	if sel != nil {
		t.Error("expected nil when selected on header")
	}
}

func TestFormatTokenLimits(t *testing.T) {
	tests := []struct {
		maxIn, maxOut int64
		want          string
	}{
		{0, 0, ""},
		{200000, 8192, "[200k in / 8.2k out]"},
		{128000, 4096, "[128k in / 4.1k out]"},
		{1000000, 16384, "[1M in / 16.4k out]"},
		{1048576, 0, "[1.0M in / — out]"},
		{0, 4096, "[— in / 4.1k out]"},
		{500, 200, "[500 in / 200 out]"},
		{2000000, 100000, "[2M in / 100k out]"},
	}
	for _, tt := range tests {
		got := formatTokenLimits(tt.maxIn, tt.maxOut)
		if got != tt.want {
			t.Errorf("formatTokenLimits(%d, %d) = %q, want %q", tt.maxIn, tt.maxOut, got, tt.want)
		}
	}
}

func TestModelPickerHideToggle(t *testing.T) {
	models := []provider.ModelEntry{
		{ID: "model-a", Provider: "prov"},
		{ID: "model-b", Provider: "prov"},
		{ID: "model-c", Provider: "prov"},
	}
	all := buildPickerEntries(models)

	pk := &modelPickerState{
		entries:  all,
		all:      all,
		selected: 2, // model-b
		height:   20,
		hidden:   make(map[string]bool),
	}

	// Hide model-b.
	id := pk.toggleHidden()
	if id != "model-b" {
		t.Fatalf("expected toggled model-b, got %q", id)
	}
	if !pk.hidden["model-b"] {
		t.Error("expected model-b to be hidden")
	}

	// Unhide model-b.
	id = pk.toggleHidden()
	if id != "model-b" {
		t.Fatalf("expected toggled model-b, got %q", id)
	}
	if pk.hidden["model-b"] {
		t.Error("expected model-b to not be hidden")
	}
}

func TestModelPickerFilterRespectsHidden(t *testing.T) {
	models := []provider.ModelEntry{
		{ID: "model-a", Provider: "prov"},
		{ID: "model-b", Provider: "prov"},
		{ID: "model-c", Provider: "prov"},
	}
	all := buildPickerEntries(models)

	pk := &modelPickerState{
		entries: all,
		all:     all,
		height:  20,
		hidden:  map[string]bool{"model-b": true},
	}

	// With showHidden=false, model-b should be excluded.
	pk.applyFilter()
	for _, e := range pk.entries {
		if e.model != nil && e.model.ID == "model-b" {
			t.Error("model-b should be hidden from entries")
		}
	}
	// Count visible models: should be 2 (model-a, model-c).
	modelCount := 0
	for _, e := range pk.entries {
		if e.model != nil {
			modelCount++
		}
	}
	if modelCount != 2 {
		t.Errorf("expected 2 visible models, got %d", modelCount)
	}

	// With showHidden=true, model-b should appear.
	pk.showHidden = true
	pk.applyFilter()
	found := false
	for _, e := range pk.entries {
		if e.model != nil && e.model.ID == "model-b" {
			found = true
		}
	}
	if !found {
		t.Error("model-b should be visible when showHidden is true")
	}
}

func TestModelPickerHiddenList(t *testing.T) {
	pk := &modelPickerState{
		hidden: map[string]bool{"z-model": true, "a-model": true, "m-model": true},
	}
	list := pk.hiddenList()
	if len(list) != 3 {
		t.Fatalf("expected 3 items, got %d", len(list))
	}
	// Should be sorted.
	if list[0] != "a-model" || list[1] != "m-model" || list[2] != "z-model" {
		t.Errorf("expected sorted list, got %v", list)
	}
}

func TestModelPickerHiddenListEmpty(t *testing.T) {
	pk := &modelPickerState{}
	list := pk.hiddenList()
	if list != nil {
		t.Errorf("expected nil, got %v", list)
	}
}

func TestModelPickerHiddenWithTextFilter(t *testing.T) {
	models := []provider.ModelEntry{
		{ID: "claude-sonnet", Provider: "anthropic"},
		{ID: "claude-opus", Provider: "anthropic"},
		{ID: "gpt-4o", Provider: "openai"},
	}
	all := buildPickerEntries(models)

	pk := &modelPickerState{
		entries: all,
		all:     all,
		height:  20,
		hidden:  map[string]bool{"claude-opus": true},
	}

	// Filter for "claude" with claude-opus hidden → only claude-sonnet.
	pk.filter = "claude"
	pk.applyFilter()

	modelCount := 0
	for _, e := range pk.entries {
		if e.model != nil {
			modelCount++
			if e.model.ID == "claude-opus" {
				t.Error("claude-opus should be hidden")
			}
		}
	}
	if modelCount != 1 {
		t.Errorf("expected 1 visible model, got %d", modelCount)
	}
}

func TestModelPickerApplyFilterPreservesSelectionAndScroll(t *testing.T) {
	models := make([]provider.ModelEntry, 0, 15)
	for i := 1; i <= 15; i++ {
		models = append(models, provider.ModelEntry{ID: fmt.Sprintf("model-%02d", i), Provider: "prov"})
	}
	all := buildPickerEntries(models)

	pk := &modelPickerState{
		entries: all,
		all:     all,
		height:  5,
	}

	// Select model-10 and position viewport away from the top.
	if !pk.selectModelByID("model-10") {
		t.Fatal("failed to select model-10")
	}
	pk.scrollOff = 7

	pk.applyFilter()

	if got := pk.selectedModelID(); got != "model-10" {
		t.Fatalf("selected model changed after applyFilter: got %q", got)
	}
	if pk.scrollOff == 0 {
		t.Fatalf("expected scrollOff to stay near selection, got %d", pk.scrollOff)
	}
	if pk.selected < pk.scrollOff || pk.selected >= pk.scrollOff+pk.height {
		t.Fatalf("selection not visible after applyFilter: selected=%d scrollOff=%d height=%d", pk.selected, pk.scrollOff, pk.height)
	}
}

func TestModelPickerFilterCaseInsensitive(t *testing.T) {
	models := []provider.ModelEntry{
		{ID: "Claude-Sonnet-4", Provider: "anthropic"},
		{ID: "GPT-4o", Provider: "openai"},
	}
	all := buildPickerEntries(models)

	pk := &modelPickerState{
		entries: all,
		all:     all,
		height:  20,
	}

	pk.filter = "CLAUDE"
	pk.applyFilter()

	modelCount := 0
	for _, e := range pk.entries {
		if e.model != nil {
			modelCount++
			if !strings.Contains(strings.ToLower(e.model.ID), "claude") {
				t.Errorf("unexpected model in filter results: %q", e.model.ID)
			}
		}
	}
	if modelCount != 1 {
		t.Errorf("expected 1 model for CLAUDE filter, got %d", modelCount)
	}
}
