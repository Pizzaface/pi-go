package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dimetron/pi-go/internal/extension"
)

// extensionsPanelState holds the TUI state for the /extensions panel.
type extensionsPanelState struct {
	rows      []extensionPanelRow
	cursor    int
	subDialog *extensionApprovalDialogState
}

type extensionPanelRow struct {
	info    extension.ExtensionInfo
	isGroup bool
	label   string
}

type extensionApprovalDialogState struct {
	id           string
	trustClass   extension.TrustClass
	runtime      extension.RuntimeSpec
	capabilities []extension.Capability
	action       extensionDialogAction
}

type extensionDialogAction string

const (
	extensionDialogApprove extensionDialogAction = "approve"
	extensionDialogRevoke  extensionDialogAction = "revoke"
)

// extensionLifecycleResultMsg reports the outcome of a background lifecycle op.
type extensionLifecycleResultMsg struct {
	id  string
	op  string
	err error
}

func buildExtensionPanelRows(infos []extension.ExtensionInfo) []extensionPanelRow {
	groups := map[extension.ExtensionState][]extension.ExtensionInfo{}
	for _, info := range infos {
		groups[info.State] = append(groups[info.State], info)
	}
	order := []struct {
		state extension.ExtensionState
		label string
	}{
		{extension.StatePending, "Pending approval"},
		{extension.StateRunning, "Running"},
		{extension.StateStopped, "Stopped"},
		{extension.StateErrored, "Errored"},
		{extension.StateDenied, "Denied"},
		{extension.StateReady, "Ready (not running)"},
	}
	var rows []extensionPanelRow
	for _, g := range order {
		infos := groups[g.state]
		if len(infos) == 0 {
			continue
		}
		rows = append(rows, extensionPanelRow{isGroup: true, label: g.label})
		for _, info := range infos {
			rows = append(rows, extensionPanelRow{info: info})
		}
	}
	return rows
}

func renderExtensionsPanel(state *extensionsPanelState, width int) string {
	if state == nil {
		return ""
	}
	if state.subDialog != nil {
		return renderApprovalSubDialog(state.subDialog, width)
	}

	bg := lipgloss.Color("236")
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("244")).
		Padding(0, 1).
		Background(bg).
		Width(width - 4)

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("252")).Bold(true).Render("Extensions"))
	b.WriteString("\n")
	if len(state.rows) == 0 {
		b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("246")).Render("(no extensions registered)"))
		b.WriteString("\n")
	}
	for i, row := range state.rows {
		if row.isGroup {
			b.WriteString("\n")
			b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("39")).Render(row.label))
			b.WriteString("\n")
			continue
		}
		prefix := "  "
		if i == state.cursor {
			prefix = "> "
		}
		line := fmt.Sprintf("%s%s  [%s]", prefix, row.info.ID, row.info.TrustClass)
		if row.info.LastError != "" {
			errStr := row.info.LastError
			if len(errStr) > 40 {
				errStr = errStr[:37] + "..."
			}
			line += "  — " + errStr
		}
		style := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("252"))
		if i == state.cursor {
			style = style.Bold(true)
		}
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}
	b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("246")).Render(
		"[a]pprove [d]eny [r]estart [s]top [x]revoke [R]eload [Esc] close",
	))
	return border.Render(b.String())
}

func renderApprovalSubDialog(d *extensionApprovalDialogState, width int) string {
	bg := lipgloss.Color("236")
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(0, 1).
		Background(bg).
		Width(width - 6)

	title := "Approve extension"
	if d.action == extensionDialogRevoke {
		title = "Revoke extension"
	}
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("214")).Bold(true).Render(title + ": " + d.id))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  Trust class:    %s\n", d.trustClass))
	b.WriteString(fmt.Sprintf("  Runtime:        %s %s\n", d.runtime.Command, strings.Join(d.runtime.Args, " ")))
	caps := make([]string, 0, len(d.capabilities))
	for _, c := range d.capabilities {
		caps = append(caps, string(c))
	}
	b.WriteString(fmt.Sprintf("  Requested caps: %s\n\n", strings.Join(caps, ", ")))
	if d.action == extensionDialogApprove {
		b.WriteString("  [Enter] approve and start   [Esc] cancel")
	} else {
		b.WriteString("  [Enter] revoke              [Esc] cancel")
	}
	return border.Render(b.String())
}

// --- Cmd helpers ---

func extensionLifecycleCmd(mgr *extension.Manager, workDir, id, op string, fn func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*extension.HostedHandshakeTimeout)
		defer cancel()
		err := fn(ctx)
		return extensionLifecycleResultMsg{id: id, op: op, err: err}
	}
}

func extensionGrantAndStartCmd(mgr *extension.Manager, workDir, id string, trust extension.TrustClass, caps []extension.Capability) tea.Cmd {
	return extensionLifecycleCmd(mgr, workDir, id, "grant", func(ctx context.Context) error {
		if err := mgr.GrantApproval(extension.GrantInput{
			ExtensionID:  id,
			TrustClass:   trust,
			Capabilities: caps,
		}); err != nil {
			return err
		}
		return mgr.StartExtension(ctx, id)
	})
}

func extensionDenyCmd(mgr *extension.Manager, id string) tea.Cmd {
	return func() tea.Msg {
		err := mgr.DenyApproval(id)
		return extensionLifecycleResultMsg{id: id, op: "deny", err: err}
	}
}

func extensionStopCmd(mgr *extension.Manager, id string) tea.Cmd {
	return extensionLifecycleCmd(mgr, "", id, "stop", func(ctx context.Context) error {
		return mgr.StopExtension(ctx, id)
	})
}

func extensionRestartCmd(mgr *extension.Manager, id string) tea.Cmd {
	return extensionLifecycleCmd(mgr, "", id, "restart", func(ctx context.Context) error {
		return mgr.RestartExtension(ctx, id)
	})
}

func extensionRevokeCmd(mgr *extension.Manager, id string) tea.Cmd {
	return extensionLifecycleCmd(mgr, "", id, "revoke", func(ctx context.Context) error {
		return mgr.RevokeApproval(ctx, id)
	})
}

func extensionReloadCmd(mgr *extension.Manager, workDir string) tea.Cmd {
	return extensionLifecycleCmd(mgr, workDir, "", "reload", func(_ context.Context) error {
		dirs := extension.DiscoverResourceDirs(workDir).ExtensionDirs
		_, _, err := mgr.ReloadManifests(dirs...)
		return err
	})
}
