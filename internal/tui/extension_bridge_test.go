package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/extension"

	tea "charm.land/bubbletea/v2"
)

func TestExtensionBridge_EmitsTeaMsgsWithoutBlocking(t *testing.T) {
	manager := extension.NewManager(extension.ManagerOptions{})
	bridge := newExtensionBridge(manager)
	if bridge == nil {
		t.Fatal("expected bridge")
	}
	t.Cleanup(bridge.Close)

	msgCh := make(chan tea.Msg, 1)
	cmd := waitForExtensionIntent(bridge.Channel())
	go func() {
		msgCh <- cmd()
	}()

	start := time.Now()
	for i := 0; i < 512; i++ {
		err := manager.EmitUIIntent("ext.demo", extension.UIIntent{
			Type:   extension.UIIntentStatus,
			Status: &extension.StatusIntent{Text: "syncing"},
		})
		if err != nil {
			t.Fatalf("EmitUIIntent() error: %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("expected non-blocking emit fanout, took %s", elapsed)
	}

	select {
	case msg := <-msgCh:
		intentMsg, ok := msg.(extensionIntentMsg)
		if !ok {
			t.Fatalf("expected extensionIntentMsg, got %T", msg)
		}
		if intentMsg.envelope.Intent.Type != extension.UIIntentStatus {
			t.Fatalf("expected status intent, got %q", intentMsg.envelope.Intent.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bridge tea message")
	}
}

func TestTUI_AppliesStatusIntentFromExtensionMsg(t *testing.T) {
	m := &model{
		cfg:       Config{},
		chatModel: ChatModel{Messages: make([]message, 0)},
	}

	newM, _ := m.handleExtensionIntent(extensionIntentMsg{
		envelope: extension.UIIntentEnvelope{
			ExtensionID: "ext.demo",
			Intent: extension.UIIntent{
				Type:   extension.UIIntentStatus,
				Status: &extension.StatusIntent{Text: "indexing..."},
			},
		},
	})
	mm := newM.(*model)
	if mm.statusModel.ExtensionStatus != "indexing..." {
		t.Fatalf("expected extension status to be applied, got %q", mm.statusModel.ExtensionStatus)
	}
}

func TestTUI_AppliesWidgetIntentAboveEditor(t *testing.T) {
	m := &model{
		cfg:       Config{},
		chatModel: ChatModel{Messages: make([]message, 0)},
	}
	newM, _ := m.handleExtensionIntent(extensionIntentMsg{
		envelope: extension.UIIntentEnvelope{
			ExtensionID: "ext.demo",
			Intent: extension.UIIntent{
				Type: extension.UIIntentWidget,
				Widget: &extension.WidgetIntent{
					Placement: extension.WidgetPlacementAboveEditor,
					Content: extension.RenderContent{
						Kind:    extension.RenderKindText,
						Content: "widget-above",
					},
				},
			},
		},
	})
	mm := newM.(*model)
	if mm.extensionWidgetAbove == nil {
		t.Fatal("expected above-editor widget state")
	}
	if got := mm.renderExtensionWidget(mm.extensionWidgetAbove); !strings.Contains(got, "widget-above") {
		t.Fatalf("expected rendered widget content, got %q", got)
	}
}

func TestTUI_AppliesWidgetIntentBelowEditor(t *testing.T) {
	m := &model{
		cfg:       Config{},
		chatModel: ChatModel{Messages: make([]message, 0)},
	}
	newM, _ := m.handleExtensionIntent(extensionIntentMsg{
		envelope: extension.UIIntentEnvelope{
			ExtensionID: "ext.demo",
			Intent: extension.UIIntent{
				Type: extension.UIIntentWidget,
				Widget: &extension.WidgetIntent{
					Placement: extension.WidgetPlacementBelowEditor,
					Content: extension.RenderContent{
						Kind:    extension.RenderKindText,
						Content: "widget-below",
					},
				},
			},
		},
	})
	mm := newM.(*model)
	if mm.extensionWidgetBelow == nil {
		t.Fatal("expected below-editor widget state")
	}
	if got := mm.renderExtensionWidget(mm.extensionWidgetBelow); !strings.Contains(got, "widget-below") {
		t.Fatalf("expected rendered widget content, got %q", got)
	}
}

func TestTUI_AppliesNotificationIntent(t *testing.T) {
	m := &model{
		cfg:       Config{},
		chatModel: ChatModel{Messages: make([]message, 0)},
	}
	newM, _ := m.handleExtensionIntent(extensionIntentMsg{
		envelope: extension.UIIntentEnvelope{
			ExtensionID: "ext.demo",
			Intent: extension.UIIntent{
				Type: extension.UIIntentNotification,
				Notification: &extension.NotificationIntent{
					Content: extension.RenderContent{
						Kind:    extension.RenderKindText,
						Content: "build complete",
					},
					Level: "info",
				},
			},
		},
	})
	mm := newM.(*model)
	if len(mm.chatModel.Messages) == 0 {
		t.Fatal("expected notification message appended to chat")
	}
	last := mm.chatModel.Messages[len(mm.chatModel.Messages)-1]
	if !strings.Contains(last.content, "build complete") {
		t.Fatalf("expected notification content, got %q", last.content)
	}
	if last.extensionOwner != "ext.demo" {
		t.Fatalf("expected extensionOwner to be set, got %q", last.extensionOwner)
	}
}

func TestTUI_AppliesDialogIntentAsModal(t *testing.T) {
	m := &model{
		cfg:       Config{},
		chatModel: ChatModel{Messages: make([]message, 0)},
	}
	newM, _ := m.handleExtensionIntent(extensionIntentMsg{
		envelope: extension.UIIntentEnvelope{
			ExtensionID: "ext.demo",
			Intent: extension.UIIntent{
				Type: extension.UIIntentDialog,
				Dialog: &extension.DialogIntent{
					Title: "Confirm",
					Content: extension.RenderContent{
						Kind:    extension.RenderKindText,
						Content: "Apply migration?",
					},
					Modal: true,
				},
			},
		},
	})
	mm := newM.(*model)
	if mm.extensionDialog == nil {
		t.Fatal("expected extension dialog state")
	}
	rendered := mm.renderExtensionDialog(100)
	if !strings.Contains(rendered, "Confirm") || !strings.Contains(rendered, "Apply migration?") {
		t.Fatalf("expected rendered dialog content, got %q", rendered)
	}
}

func TestTUI_IgnoresUIIntentInNonInteractiveMode(t *testing.T) {
	m := &model{
		cfg:       Config{DisableExtensionUI: true},
		chatModel: ChatModel{Messages: make([]message, 0)},
	}
	newM, _ := m.handleExtensionIntent(extensionIntentMsg{
		envelope: extension.UIIntentEnvelope{
			ExtensionID: "ext.demo",
			Intent: extension.UIIntent{
				Type:   extension.UIIntentStatus,
				Status: &extension.StatusIntent{Text: "should-ignore"},
			},
		},
	})
	mm := newM.(*model)
	if mm.statusModel.ExtensionStatus != "" {
		t.Fatalf("expected status intent to be ignored, got %q", mm.statusModel.ExtensionStatus)
	}
	if len(mm.chatModel.Messages) != 0 {
		t.Fatalf("expected no chat messages, got %d", len(mm.chatModel.Messages))
	}
}

func TestRenderer_RejectsConflictingOwnershipOnSameSurface(t *testing.T) {
	manager := extension.NewManager(extension.ManagerOptions{})
	err := manager.RegisterRenderer(
		"ext.a",
		extension.RenderSurfaceToolResult,
		[]extension.RenderKind{extension.RenderKindText},
		func(_ context.Context, _ extension.RenderRequest) (extension.RenderResult, error) {
			return extension.RenderResult{Kind: extension.RenderKindText, Content: "a"}, nil
		},
	)
	if err != nil {
		t.Fatalf("first RegisterRenderer() error: %v", err)
	}

	err = manager.RegisterRenderer(
		"ext.b",
		extension.RenderSurfaceToolResult,
		[]extension.RenderKind{extension.RenderKindText},
		func(_ context.Context, _ extension.RenderRequest) (extension.RenderResult, error) {
			return extension.RenderResult{Kind: extension.RenderKindText, Content: "b"}, nil
		},
	)
	if err == nil {
		t.Fatal("expected ownership conflict for same renderer surface")
	}
}

func TestRenderer_CleansUpOnExtensionUnload(t *testing.T) {
	manager := extension.NewManager(extension.ManagerOptions{})
	err := manager.RegisterRenderer(
		"ext.a",
		extension.RenderSurfaceToolResult,
		[]extension.RenderKind{extension.RenderKindText},
		func(_ context.Context, _ extension.RenderRequest) (extension.RenderResult, error) {
			return extension.RenderResult{Kind: extension.RenderKindText, Content: "a"}, nil
		},
	)
	if err != nil {
		t.Fatalf("RegisterRenderer() error: %v", err)
	}
	if _, ok := manager.RendererOwner(extension.RenderSurfaceToolResult); !ok {
		t.Fatal("expected renderer owner before cleanup")
	}

	manager.UnregisterExtension("ext.a")
	if _, ok := manager.RendererOwner(extension.RenderSurfaceToolResult); ok {
		t.Fatal("expected renderer ownership to be removed on unload")
	}
}

func TestChat_UsesCustomMessageRendererForSupportedType(t *testing.T) {
	manager := extension.NewManager(extension.ManagerOptions{})
	err := manager.RegisterRenderer(
		"ext.chat",
		extension.RenderSurfaceChatMessage,
		[]extension.RenderKind{extension.RenderKindText},
		func(_ context.Context, req extension.RenderRequest) (extension.RenderResult, error) {
			content, _ := req.Payload["content"].(string)
			return extension.RenderResult{
				Kind:    extension.RenderKindText,
				Content: "custom:" + content,
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("RegisterRenderer() error: %v", err)
	}

	chat := NewChatModel(nil)
	chat.ExtensionManager = manager
	chat.Messages = []message{
		{
			role:           "assistant",
			content:        "hello",
			extensionOwner: "ext.chat",
		},
	}
	output := chat.RenderMessages(false)
	if !strings.Contains(output, "custom:hello") {
		t.Fatalf("expected custom chat renderer output, got %q", output)
	}
}
