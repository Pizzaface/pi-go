package api

import "testing"

func TestSessionBridge_UIInterfaceIsSatisfiedByNoopBridge(t *testing.T) {
	var b SessionBridge = NoopBridge{}
	_ = b.SetExtensionStatus("ext-a", "hi", "")
	_ = b.ClearExtensionStatus("ext-a")
	_ = b.SetExtensionWidget("ext-a", ExtensionWidget{ID: "w1"})
	_ = b.ClearExtensionWidget("ext-a", "w1")
	_ = b.EnqueueNotify("ext-a", "info", "hello", 0)
	_, _ = b.ShowDialog("ext-a", DialogSpec{Title: "ok"})
	m := b.GetSessionMetadata()
	_ = m
	_ = b.SetSessionName("n")
	_ = b.SetSessionTags([]string{"a"})
}
