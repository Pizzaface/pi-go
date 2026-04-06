package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dimetron/pi-go/internal/provider"
)

func waitForRestart(ch chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-ch
		return restartMsg{}
	}
}

func waitForProviderDebug(ch <-chan provider.DebugEvent) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return providerDebugMsg{event: ev}
	}
}

func (m *model) handleProviderDebug(msg providerDebugMsg) (tea.Model, tea.Cmd) {
	summary, detail := provider.FormatDebugEvent(msg.event)
	kind := msg.event.Kind
	m.chatModel.TraceLog = append(m.chatModel.TraceLog, traceEntry{
		time:    msg.event.Time,
		kind:    kind,
		summary: summary,
		detail:  detail,
	})
	return m, waitForProviderDebug(m.cfg.DebugTracer.Channel())
}

func drainTerminalResponses() {
	f := os.Stdin
	if err := setNonBlock(f); err != nil {
		return
	}
	defer setBlock(f) //nolint:errcheck

	buf := make([]byte, 256)
	deadline := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
		n, _ := f.Read(buf)
		if n == 0 {
			break
		}
	}
}

func (m *model) refreshDiffStats() {
	cwd := m.cwd()
	if cwd == "" {
		return
	}
	cmd := exec.Command("git", "diff", "--numstat", "HEAD")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return
	}
	var added, removed int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var a, r int
		if _, err := fmt.Sscanf(line, "%d\t%d\t", &a, &r); err == nil {
			added += a
			removed += r
		}
	}
	added += countUntrackedLines(cwd)
	m.diffAdded = added
	m.diffRemoved = removed
}

func countUntrackedLines(cwd string) int {
	cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	total := 0
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		wc := exec.Command("wc", "-l", f)
		wc.Dir = cwd
		wcOut, err := wc.Output()
		if err != nil {
			continue
		}
		var lines int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(wcOut)), "%d", &lines); err == nil {
			total += lines
		}
	}
	return total
}

func detectBranch(workDir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	if workDir != "" {
		cmd.Dir = workDir
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func resetCtrlCCount(m *model) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(2 * time.Second)
		return resetCtrlCCountMsg{}
	}
}

type resetCtrlCCountMsg struct{}

func (m *model) handleResetCtrlCCount() (tea.Model, tea.Cmd) {
	m.ctrlCCount = 0
	return m, nil
}
