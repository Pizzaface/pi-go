// hosted-surface-fixture exercises every v2.2 hosted service so e2e tests
// can assert end-to-end behavior. The mode is driven by environment
// variable PI_SURFACE_MODE, which each e2e test sets before launching.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/pizzaface/go-pi/pkg/piapi"
	"github.com/pizzaface/go-pi/pkg/piext"
)

var Metadata = piapi.Metadata{
	Name:        "hosted-surface-fixture",
	Version:     "0.1.0",
	Description: "Exercises state/commands/ui/sigils/session-metadata services for e2e coverage.",
	RequestedCapabilities: []string{
		"state.get", "state.set", "state.patch", "state.delete",
		"commands.register", "commands.unregister", "commands.list",
		"ui.status", "ui.clear_status", "ui.widget", "ui.clear_widget",
		"ui.notify", "ui.dialog",
		"sigils.register", "sigils.unregister", "sigils.list",
		"session.get_metadata", "session.set_name", "session.set_tags",
		"events.session_start",
	},
}

func register(pi piapi.API) error {
	ctx := context.Background()
	mode := os.Getenv("PI_SURFACE_MODE")
	fmt.Fprintln(piext.Log(), "hosted-surface-fixture: mode=", mode)

	switch mode {
	case "state":
		if err := pi.StateSet(ctx, map[string]any{"count": 1}); err != nil {
			return fmt.Errorf("StateSet: %w", err)
		}
		if err := pi.StatePatch(ctx, json.RawMessage(`{"count":2,"note":"hi"}`)); err != nil {
			return fmt.Errorf("StatePatch: %w", err)
		}
	case "commands":
		if err := pi.CommandsRegister(ctx, "fixture-cmd", "Fixture command", "", ""); err != nil {
			return fmt.Errorf("CommandsRegister: %w", err)
		}
		pi.OnCommandInvoke(func(ev piapi.CommandsInvokeEvent) piapi.CommandsInvokeResult {
			return piapi.CommandsInvokeResult{Handled: true, Message: "invoked:" + ev.Args}
		})
	case "ui":
		if err := pi.UIStatus(ctx, "fixture-status", ""); err != nil {
			return fmt.Errorf("UIStatus: %w", err)
		}
		if err := pi.UIWidget(ctx, "w1", "Title", []string{"line"}, piapi.Position{Mode: "sticky", Anchor: "top"}); err != nil {
			return fmt.Errorf("UIWidget: %w", err)
		}
		if err := pi.UINotify(ctx, "info", "hello", 0); err != nil {
			return fmt.Errorf("UINotify: %w", err)
		}
		if _, err := pi.UIDialog(ctx, "confirm", nil, []piapi.DialogButton{{ID: "ok", Label: "OK"}}); err != nil {
			return fmt.Errorf("UIDialog: %w", err)
		}
	case "sigils":
		if err := pi.SigilsRegister(ctx, []string{"fix", "fixture"}); err != nil {
			return fmt.Errorf("SigilsRegister: %w", err)
		}
		pi.OnSigilResolve(func(ev piapi.SigilResolveEvent) piapi.SigilResolveResult {
			return piapi.SigilResolveResult{Display: ev.Prefix + "->" + ev.ID}
		})
	case "session":
		if err := pi.SessionSetName(ctx, "fixture-branch"); err != nil {
			return fmt.Errorf("SessionSetName: %w", err)
		}
		if err := pi.SessionSetTags(ctx, []string{"one", "two"}); err != nil {
			return fmt.Errorf("SessionSetTags: %w", err)
		}
	default:
		// Register nothing; just signal ready.
	}
	return pi.Ready()
}

func main() {
	if err := piext.Run(Metadata, register); err != nil {
		fmt.Fprintln(piext.Log(), "hosted-surface-fixture: fatal:", err)
		os.Exit(1)
	}
}
