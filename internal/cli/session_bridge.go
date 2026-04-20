package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	extapi "github.com/pizzaface/go-pi/internal/extension/api"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

type cliSessionBridge struct {
	stderr   io.Writer
	title    string
	mu       sync.Mutex
	logFile  *os.File
	logPath  string
	reloadFn func(context.Context) error
}

func NewSessionBridge(stderr io.Writer, logPath string, reloadFn func(context.Context) error) extapi.SessionBridge {
	if stderr == nil {
		stderr = os.Stderr
	}
	return &cliSessionBridge{stderr: stderr, logPath: logPath, reloadFn: reloadFn}
}

func (b *cliSessionBridge) AppendEntry(extID, kind string, payload any) error {
	body, _ := json.Marshal(payload)
	fmt.Fprintf(b.stderr, "[%s/%s] %s\n", extID, kind, string(body))
	return nil
}

func (b *cliSessionBridge) SendCustomMessage(extID string, msg piapi.CustomMessage, _ piapi.SendOptions) error {
	if !msg.Display {
		return nil
	}
	fmt.Fprintf(b.stderr, "[%s:%s] %s\n", extID, msg.CustomType, msg.Content)
	return nil
}

func (b *cliSessionBridge) SendUserMessage(extID string, msg piapi.UserMessage, _ piapi.SendOptions) error {
	var text string
	for _, c := range msg.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	fmt.Fprintf(b.stderr, "[%s:user] %s\n", extID, text)
	return nil
}

func (b *cliSessionBridge) SetSessionTitle(title string) error {
	b.mu.Lock()
	b.title = title
	b.mu.Unlock()
	return nil
}

func (b *cliSessionBridge) GetSessionTitle() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.title
}

func (b *cliSessionBridge) SetEntryLabel(string, string) error { return nil }

func (b *cliSessionBridge) WaitForIdle(context.Context) error {
	return piapi.ErrSessionControlUnsupportedInCLI{Method: "WaitForIdle"}
}

func (b *cliSessionBridge) NewSession(piapi.NewSessionOptions) (piapi.NewSessionResult, error) {
	return piapi.NewSessionResult{}, piapi.ErrSessionControlUnsupportedInCLI{Method: "NewSession"}
}

func (b *cliSessionBridge) Fork(string) (piapi.ForkResult, error) {
	return piapi.ForkResult{}, piapi.ErrSessionControlUnsupportedInCLI{Method: "Fork"}
}

func (b *cliSessionBridge) NavigateBranch(string) (piapi.NavigateResult, error) {
	return piapi.NavigateResult{}, piapi.ErrSessionControlUnsupportedInCLI{Method: "NavigateBranch"}
}

func (b *cliSessionBridge) SwitchSession(string) (piapi.SwitchResult, error) {
	return piapi.SwitchResult{}, piapi.ErrSessionControlUnsupportedInCLI{Method: "SwitchSession"}
}

func (b *cliSessionBridge) Reload(ctx context.Context) error {
	if b.reloadFn == nil {
		return nil
	}
	return b.reloadFn(ctx)
}

func (b *cliSessionBridge) EmitToolUpdate(_ string, partial piapi.ToolResult) error {
	for _, c := range partial.Content {
		if c.Type == "text" {
			fmt.Fprintln(b.stderr, c.Text)
		}
	}
	return nil
}

func (b *cliSessionBridge) AppendExtensionLog(extID, level, msg string, fields map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.logFile == nil && b.logPath != "" {
		f, err := os.OpenFile(b.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			b.logFile = f
		}
	}
	if b.logFile != nil {
		entry := map[string]any{"ext": extID, "level": level, "msg": msg, "fields": fields}
		data, _ := json.Marshal(entry)
		_, _ = fmt.Fprintln(b.logFile, string(data))
	}
	fmt.Fprintf(b.stderr, "[%s %s] %s\n", extID, level, msg)
	return nil
}
