package host

import (
	"io"
	"sync/atomic"
	"testing"
	"time"
)

func TestRPCConn_CloseCallback(t *testing.T) {
	r, w := io.Pipe()
	conn := NewRPCConn(r, w, nil)
	var fired atomic.Int32
	conn.OnClose(func() { fired.Add(1) })
	conn.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && fired.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatalf("callback not fired: %d", fired.Load())
	}

	// Registering after close fires immediately.
	var late atomic.Int32
	conn.OnClose(func() { late.Add(1) })
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) && late.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if late.Load() != 1 {
		t.Fatalf("late callback not fired: %d", late.Load())
	}
}

func TestManager_OnClose_FiresOnDisconnect(t *testing.T) {
	mgr := newTestManager(t, "")
	reg := &Registration{ID: "ext-a", Mode: "hosted-go", Trust: TrustThirdParty}
	if err := mgr.Register(reg); err != nil {
		t.Fatal(err)
	}
	var fired atomic.Int32
	mgr.OnClose("ext-a", func() { fired.Add(1) })

	r, w := io.Pipe()
	reg.Conn = NewRPCConn(r, w, nil)
	// Wire up the callback manually the same way the production launcher does.
	reg.Conn.OnClose(func() { mgr.FireOnClose("ext-a") })
	reg.Conn.Close()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && fired.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatal("OnClose did not fire")
	}
}
