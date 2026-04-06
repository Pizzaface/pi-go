package hostruntime

import (
	"context"
	"encoding/json"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

func TestHostedClient_PerformsHandshake(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	serverErr := make(chan error, 1)
	go func() {
		defer close(serverErr)
		decoder := json.NewDecoder(serverConn)
		encoder := json.NewEncoder(serverConn)
		var request hostproto.RPCRequest
		if err := decoder.Decode(&request); err != nil {
			serverErr <- err
			return
		}
		if request.Method != hostproto.MethodHandshake {
			serverErr <- context.Canceled
			return
		}
		var handshake hostproto.HandshakeRequest
		if err := json.Unmarshal(request.Params, &handshake); err != nil {
			serverErr <- err
			return
		}
		if handshake.Mode != "interactive" || len(handshake.CapabilityMask) != 2 {
			serverErr <- context.Canceled
			return
		}
		result, err := json.Marshal(hostproto.HandshakeResponse{
			ProtocolVersion: hostproto.ProtocolVersion,
			Accepted:        true,
		})
		if err != nil {
			serverErr <- err
			return
		}
		serverErr <- encoder.Encode(hostproto.RPCResponse{
			JSONRPC: hostproto.JSONRPCVersion,
			ID:      request.ID,
			Result:  result,
		})
	}()

	client := NewClient(clientConn, clientConn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := client.Handshake(ctx, hostproto.HandshakeRequest{
		ExtensionID:    "ext.demo",
		Mode:           "interactive",
		CapabilityMask: []string{"commands.register", "tools.register"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !response.Accepted {
		t.Fatalf("expected accepted handshake response, got %+v", response)
	}
	if !client.IsHealthy() {
		t.Fatal("expected client to be healthy after handshake")
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server side failed: %v", err)
	}
}

func TestHostedClient_TimeoutsSlowHandshake(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		decoder := json.NewDecoder(serverConn)
		var request hostproto.RPCRequest
		_ = decoder.Decode(&request)
		time.Sleep(500 * time.Millisecond)
	}()

	client := NewClient(clientConn, clientConn)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := client.Handshake(ctx, hostproto.HandshakeRequest{
		ExtensionID: "ext.slow",
		Mode:        "interactive",
	})
	if err == nil {
		t.Fatal("expected handshake timeout to fail")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
}

func TestHostedClient_MarksUnhealthyOnCrash(t *testing.T) {
	command, args := immediateExitCommand()
	process, err := StartProcess(context.Background(), ProcessConfig{
		Command: command,
		Args:    args,
	})
	if err != nil {
		t.Fatal(err)
	}

	client := NewClientFromProcess(process)
	waitUntil(t, 2*time.Second, func() bool { return !client.IsHealthy() })
}

func immediateExitCommand() (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", "exit 0"}
	}
	return "sh", []string{"-c", "exit 0"}
}

func waitUntil(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
