package cli

import (
	"github.com/pizzaface/go-pi/internal/agent"
)

func baseInstruction() string {
	if flagSystem != "" {
		return flagSystem
	}
	return agent.LoadInstruction(agent.SystemInstruction)
}
