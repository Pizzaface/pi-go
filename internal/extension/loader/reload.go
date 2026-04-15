package loader

import (
	"context"

	"github.com/dimetron/pi-go/internal/extension/host"
)

// Reload orchestrates a full re-scan of the extension directories. The
// manager shuts down any running extensions, the capability gate reloads
// from disk, and Discover walks the four conventional directories again.
// Spec #5 will wire this into the user-facing ctx.Reload().
func Reload(ctx context.Context, manager *host.Manager, cwd string) ([]Candidate, error) {
	manager.Shutdown(ctx)
	if err := manager.Gate().Reload(); err != nil {
		return nil, err
	}
	return Discover(cwd)
}
