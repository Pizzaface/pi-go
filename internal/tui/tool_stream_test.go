package tui

import (
	"testing"

	"github.com/dimetron/pi-go/pkg/piapi"
)

func TestExtensionToolStreamMsgUpdatesRow(t *testing.T) {
	m := newTestModel(t)

	m2, _ := m.Update(ExtensionToolStreamMsg{
		ToolCallID: "c1",
		Partial:    piapi.ToolResult{Content: []piapi.ContentPart{{Type: "text", Text: "ping 1/3"}}},
	})
	m = m2.(*model)

	m3, _ := m.Update(ExtensionToolStreamMsg{
		ToolCallID: "c1",
		Partial:    piapi.ToolResult{Content: []piapi.ContentPart{{Type: "text", Text: "ping 2/3"}}},
	})
	m = m3.(*model)

	row, ok := m.chatModel.ToolDisplay.streamingRows["c1"]
	if !ok {
		t.Fatal("streaming row c1 not created")
	}
	if row.Updates != 2 {
		t.Fatalf("Updates = %d; want 2", row.Updates)
	}
	if len(row.Content) != 1 || row.Content[0].Text != "ping 2/3" {
		t.Fatalf("row content = %+v", row.Content)
	}
}
