// Package uitypes contains shared UI types used by the extension API bridge
// and test helpers without creating import cycles.
package uitypes

// ExtensionWidget is one extension-owned widget placed in the TUI.
type ExtensionWidget struct {
	ID       string
	Title    string
	Lines    []string
	Style    string
	Position Position
}

// Position describes how a widget is placed in the TUI.
type Position struct {
	Mode    string
	Anchor  string
	OffsetX int
	OffsetY int
	Z       int
}

// DialogField is a single input field within a dialog.
type DialogField struct {
	Name    string
	Kind    string
	Label   string
	Default string
	Choices []string
}

// DialogButton is a button in a dialog.
type DialogButton struct {
	ID    string
	Label string
	Style string
}

// DialogSpec describes a dialog to be shown to the user.
type DialogSpec struct {
	Title   string
	Fields  []DialogField
	Buttons []DialogButton
}

// DialogResolution records how a dialog was dismissed.
type DialogResolution struct {
	DialogID  string
	Values    map[string]any
	Cancelled bool
	ButtonID  string
}

// SessionMetadata holds observable session-level metadata.
type SessionMetadata struct {
	Name      string
	Title     string
	Tags      []string
	CreatedAt string
	UpdatedAt string
}
