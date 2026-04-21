package api

import (
	"testing"
)

func TestUIService_StatusPerExtension(t *testing.T) {
	svc := NewUIService()
	_ = svc.SetStatus("ext-a", "A", "")
	_ = svc.SetStatus("ext-b", "B", "")
	if svc.Status("ext-a") != "A" || svc.Status("ext-b") != "B" {
		t.Fatalf("status isolation broken: a=%q b=%q", svc.Status("ext-a"), svc.Status("ext-b"))
	}
	_ = svc.ClearStatus("ext-a")
	if svc.Status("ext-a") != "" {
		t.Fatalf("clear failed")
	}
}

func TestUIService_WidgetStoreAndClear(t *testing.T) {
	svc := NewUIService()
	_ = svc.SetWidget("ext-a", ExtensionWidget{ID: "w1", Lines: []string{"hello"}})
	_ = svc.SetWidget("ext-a", ExtensionWidget{ID: "w2", Lines: []string{"world"}})
	if ws := svc.Widgets("ext-a"); len(ws) != 2 {
		t.Fatalf("widgets = %d", len(ws))
	}
	_ = svc.ClearWidget("ext-a", "w1")
	ws := svc.Widgets("ext-a")
	if len(ws) != 1 || ws[0].ID != "w2" {
		t.Fatalf("after clear: %+v", ws)
	}
}

func TestUIService_DialogQueueAndResolve(t *testing.T) {
	svc := NewUIService()
	id1, _ := svc.EnqueueDialog("ext-a", DialogSpec{Title: "one"})
	id2, _ := svc.EnqueueDialog("ext-a", DialogSpec{Title: "two"})
	if id1 == "" || id2 == "" || id1 == id2 {
		t.Fatalf("dialog IDs = %q %q", id1, id2)
	}
	active := svc.ActiveDialog()
	if active == nil || active.ID != id1 {
		t.Fatalf("active = %+v", active)
	}
	if _, ok := svc.ResolveDialog(id1, map[string]any{"ok": true}, false, "ok"); !ok {
		t.Fatalf("resolve id1 failed")
	}
	active = svc.ActiveDialog()
	if active == nil || active.ID != id2 {
		t.Fatalf("active after resolve = %+v", active)
	}
}

func TestUIService_RemoveAllByOwner(t *testing.T) {
	svc := NewUIService()
	_ = svc.SetStatus("ext-a", "A", "")
	_ = svc.SetWidget("ext-a", ExtensionWidget{ID: "w1"})
	_, _ = svc.EnqueueDialog("ext-a", DialogSpec{Title: "d"})
	_ = svc.SetStatus("ext-b", "B", "")

	cancelled := svc.RemoveAllByOwner("ext-a")
	if svc.Status("ext-a") != "" || len(svc.Widgets("ext-a")) != 0 {
		t.Fatalf("ext-a state should be cleared")
	}
	if len(cancelled) != 1 {
		t.Fatalf("expected 1 cancelled dialog: %+v", cancelled)
	}
	if svc.Status("ext-b") != "B" {
		t.Fatalf("ext-b should survive")
	}
}
