package extension

import (
	"fmt"
	"strings"
)

// IntentType identifies an action requested by an extension.
type IntentType string

const (
	IntentCommand IntentType = "command"
	IntentTool    IntentType = "tool"
	IntentUI      IntentType = "ui"
)

// CommandIntent represents a request to invoke a command.
type CommandIntent struct {
	Name string         `json:"name"`
	Args []string       `json:"args,omitempty"`
	Meta map[string]any `json:"meta,omitempty"`
}

// ToolIntent represents a request to register or invoke a tool.
type ToolIntent struct {
	Name      string         `json:"name"`
	Intercept bool           `json:"intercept,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

type UIIntentType string

const (
	UIIntentStatus       UIIntentType = "status"
	UIIntentWidget       UIIntentType = "widget"
	UIIntentNotification UIIntentType = "notification"
	UIIntentDialog       UIIntentType = "dialog"
)

type WidgetPlacement string

const (
	WidgetPlacementAboveEditor WidgetPlacement = "above_editor"
	WidgetPlacementBelowEditor WidgetPlacement = "below_editor"
)

type RenderKind string

const (
	RenderKindText     RenderKind = "text"
	RenderKindMarkdown RenderKind = "markdown"
)

func normalizeRenderKind(kind RenderKind) RenderKind {
	if strings.TrimSpace(string(kind)) == "" {
		return RenderKindText
	}
	return kind
}

type RenderContent struct {
	Kind    RenderKind `json:"kind,omitempty"`
	Content string     `json:"content"`
}

func (r RenderContent) Validate() error {
	kind := normalizeRenderKind(r.Kind)
	switch kind {
	case RenderKindText, RenderKindMarkdown:
		return nil
	default:
		return fmt.Errorf("unsupported render kind %q", r.Kind)
	}
}

type StatusIntent struct {
	Text string `json:"text"`
}

type WidgetIntent struct {
	Placement WidgetPlacement `json:"placement"`
	Content   RenderContent   `json:"content"`
}

type NotificationIntent struct {
	Content RenderContent `json:"content"`
	Level   string        `json:"level,omitempty"`
}

type DialogIntent struct {
	Title   string        `json:"title,omitempty"`
	Content RenderContent `json:"content"`
	Modal   bool          `json:"modal,omitempty"`
}

type UIIntent struct {
	Type         UIIntentType        `json:"type"`
	Status       *StatusIntent       `json:"status,omitempty"`
	Widget       *WidgetIntent       `json:"widget,omitempty"`
	Notification *NotificationIntent `json:"notification,omitempty"`
	Dialog       *DialogIntent       `json:"dialog,omitempty"`
	Meta         map[string]any      `json:"meta,omitempty"`
}

func (i UIIntent) Validate() error {
	switch i.Type {
	case UIIntentStatus:
		if i.Status == nil {
			return fmt.Errorf("status payload is required")
		}
		return nil
	case UIIntentWidget:
		if i.Widget == nil {
			return fmt.Errorf("widget payload is required")
		}
		switch i.Widget.Placement {
		case WidgetPlacementAboveEditor, WidgetPlacementBelowEditor:
		default:
			return fmt.Errorf("unsupported widget placement %q", i.Widget.Placement)
		}
		return i.Widget.Content.Validate()
	case UIIntentNotification:
		if i.Notification == nil {
			return fmt.Errorf("notification payload is required")
		}
		return i.Notification.Content.Validate()
	case UIIntentDialog:
		if i.Dialog == nil {
			return fmt.Errorf("dialog payload is required")
		}
		return i.Dialog.Content.Validate()
	default:
		return fmt.Errorf("unsupported ui intent type %q", i.Type)
	}
}

func (i UIIntent) RequiredCapability() Capability {
	switch i.Type {
	case UIIntentWidget:
		return CapabilityUIWidget
	case UIIntentDialog:
		return CapabilityUIDialog
	case UIIntentStatus, UIIntentNotification:
		return CapabilityUIStatus
	default:
		return ""
	}
}

type UIIntentEnvelope struct {
	ExtensionID string   `json:"extension_id"`
	Intent      UIIntent `json:"intent"`
}

type RenderSurface string

const (
	RenderSurfaceToolCallRow RenderSurface = "tool.call_row"
	RenderSurfaceToolResult  RenderSurface = "tool.result"
	RenderSurfaceChatMessage RenderSurface = "chat.message"
)

type RenderRequest struct {
	Surface RenderSurface  `json:"surface"`
	Payload map[string]any `json:"payload,omitempty"`
}

type RenderResult struct {
	Kind    RenderKind `json:"kind"`
	Content string     `json:"content"`
}

func (r RenderResult) Validate() error {
	kind := normalizeRenderKind(r.Kind)
	switch kind {
	case RenderKindText, RenderKindMarkdown:
		return nil
	default:
		return fmt.Errorf("unsupported render kind %q", r.Kind)
	}
}
