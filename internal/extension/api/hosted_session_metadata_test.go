package api

import (
	"encoding/json"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
)

type recordingBridge struct {
	NoopBridge
	meta SessionMetadata
	name string
	tags []string
}

func (b *recordingBridge) GetSessionMetadata() SessionMetadata { return b.meta }
func (b *recordingBridge) SetSessionName(n string) error {
	b.name = n
	b.meta.Name = n
	return nil
}
func (b *recordingBridge) SetSessionTags(t []string) error {
	b.tags = append([]string{}, t...)
	b.meta.Tags = b.tags
	return nil
}

func TestHandleHostCall_Session_Metadata(t *testing.T) {
	gate, _ := host.NewGate("")
	mgr := host.NewManager(gate)
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustCompiledIn}
	_ = mgr.Register(reg)
	br := &recordingBridge{meta: SessionMetadata{Title: "T", CreatedAt: "2026-04-20T00:00:00Z"}}
	h := NewHostedHandler(mgr, reg, br)

	call := func(method string, payload any) (json.RawMessage, error) {
		pb, _ := json.Marshal(payload)
		params, _ := json.Marshal(hostproto.HostCallParams{
			Service: hostproto.ServiceSession, Method: method, Payload: pb,
		})
		res, err := h.Handle(hostproto.MethodHostCall, params)
		if err != nil {
			return nil, err
		}
		rb, _ := json.Marshal(res)
		return rb, nil
	}

	if _, err := call(hostproto.MethodSessionSetName,
		hostproto.SessionSetNameParams{Name: "my-branch"}); err != nil {
		t.Fatalf("set_name: %v", err)
	}
	if br.name != "my-branch" {
		t.Fatalf("bridge.name = %q", br.name)
	}
	if _, err := call(hostproto.MethodSessionSetTags,
		hostproto.SessionSetTagsParams{Tags: []string{"a", "b"}}); err != nil {
		t.Fatalf("set_tags: %v", err)
	}
	getRes, err := call(hostproto.MethodSessionGetMetadata, struct{}{})
	if err != nil {
		t.Fatalf("get_metadata: %v", err)
	}
	var m hostproto.SessionGetMetadataResult
	_ = json.Unmarshal(getRes, &m)
	if m.Name != "my-branch" || m.Title != "T" || len(m.Tags) != 2 {
		t.Fatalf("metadata = %+v", m)
	}
}
