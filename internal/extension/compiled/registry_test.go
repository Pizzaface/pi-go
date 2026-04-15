package compiled

import (
	"testing"

	"github.com/dimetron/pi-go/pkg/piapi"
)

func TestAppendAndSnapshot(t *testing.T) {
	defer Reset()
	Reset()
	Append(Entry{Name: "alpha", Metadata: piapi.Metadata{Name: "alpha", Version: "0.1"}})
	Append(Entry{Name: "beta", Metadata: piapi.Metadata{Name: "beta", Version: "0.2"}})
	list := Compiled()
	if len(list) != 2 {
		t.Fatalf("expected 2 entries; got %d", len(list))
	}
	if list[0].Name != "alpha" || list[1].Name != "beta" {
		t.Fatalf("order not preserved: %+v", list)
	}
}

func TestSnapshotIsCopy(t *testing.T) {
	defer Reset()
	Reset()
	Append(Entry{Name: "x"})
	snap := Compiled()
	snap[0].Name = "mutated"
	list := Compiled()
	if list[0].Name != "x" {
		t.Fatalf("snapshot mutation leaked into registry: %+v", list)
	}
}
