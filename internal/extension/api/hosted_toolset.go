package api

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
)

// HostedToolset exposes the live contents of a HostedToolRegistry as an
// ADK Toolset. Tools(ctx) is queried per LLM invocation, so add/remove in
// the registry is visible without rebuilding the agent.
type HostedToolset struct {
	reg *HostedToolRegistry
}

// NewHostedToolset returns a Toolset whose contents are the live registry.
func NewHostedToolset(reg *HostedToolRegistry) *HostedToolset {
	return &HostedToolset{reg: reg}
}

// Name is stable and used by ADK for logging/identification.
func (t *HostedToolset) Name() string { return "go-pi-hosted-extensions" }

// Tools snapshots the registry and wraps each entry in a hosted tool
// adapter. Adapter construction errors and nil returns are dropped.
func (t *HostedToolset) Tools(_ agent.ReadonlyContext) ([]tool.Tool, error) {
	snap := t.reg.Snapshot()
	out := make([]tool.Tool, 0, len(snap))
	for _, e := range snap {
		adapter, err := NewHostedToolAdapter(e)
		if err != nil || adapter == nil {
			continue
		}
		out = append(out, adapter)
	}
	return out, nil
}

// NewHostedToolAdapter: STUB — real implementation arrives in Task 5.
// Returns (nil, nil) so Tools() filters it out; tests of the registry
// mechanics still exercise snapshot semantics without depending on the
// adapter. Task 5 removes this stub.
func NewHostedToolAdapter(_ HostedToolEntry) (tool.Tool, error) {
	return nil, nil
}
