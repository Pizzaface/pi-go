// Package main is the pi-go hosted-hello example extension, rewritten
// against the v2 host_call protocol. It registers a single slash
// command (/hello) and pushes a status line entry via the ui service.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
	"github.com/dimetron/pi-go/internal/extension/sdk"
	commandstypes "github.com/dimetron/pi-go/internal/extension/services/commands"
	uitypes "github.com/dimetron/pi-go/internal/extension/services/ui"
)

func main() {
	client := sdk.NewClient(os.Stdin, os.Stdout)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	err := client.Serve(ctx, sdk.ServeOptions{
		ExtensionID: "hosted-hello",
		Mode:        "hosted_stdio",
		RequestedServices: []hostproto.ServiceRequest{
			{Service: "ui", Version: 1, Methods: []string{"status"}},
			{Service: "commands", Version: 1, Methods: []string{"register"}},
		},
		OnReady: func(ready sdk.HandshakeReady) error {
			// Register a slash command.
			if _, err := ready.Client.HostCall(ctx, "commands", "register", 1, commandstypes.RegisterPayload{
				Name:        "hello",
				Description: "Say hello from the hosted-hello extension",
				Prompt:      "Say hello from the hosted-hello extension. Extra args: {{args}}",
				Kind:        "prompt",
			}); err != nil {
				log.Printf("hosted-hello: commands.register failed: %v", err)
			}

			// Push a status line entry.
			if _, err := ready.Client.HostCall(ctx, "ui", "status", 1, uitypes.StatusPayload{
				Text:  "hosted-hello connected",
				Color: "cyan",
			}); err != nil {
				log.Printf("hosted-hello: ui.status failed: %v", err)
			}
			return nil
		},
	})
	if err != nil && err != context.Canceled {
		log.Printf("hosted-hello: Serve exited: %v", err)
		os.Exit(1)
	}
}
