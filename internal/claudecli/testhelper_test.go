package claudecli

import (
	"github.com/dimetron/pi-go/internal/agent"
	"google.golang.org/adk/model"
)

// newTestAgent creates a real ADK agent wired to the given model.LLM provider.
// This exercises the full ADK stack: runner → flow → GenerateContent → session events.
func newTestAgent(llm model.LLM) (*agent.Agent, error) {
	return agent.New(agent.Config{
		Model:       llm,
		Instruction: "You are a test agent.",
	})
}
