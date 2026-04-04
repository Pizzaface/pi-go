package cli

import (
	"github.com/dimetron/pi-go/internal/agent"
)

func baseInstruction() string {
	if flagSystem != "" {
		return flagSystem
	}
	return agent.LoadInstruction(agent.SystemInstruction)
}
