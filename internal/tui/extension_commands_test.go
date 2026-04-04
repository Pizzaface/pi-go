package tui

import (
	"strings"
	"testing"

	"github.com/dimetron/pi-go/internal/extension"
)

func TestHandleSlashCommand_ExtensionCommandSubmitsPrompt(t *testing.T) {
	m := &model{
		cfg: Config{
			ExtensionCommands: []extension.SlashCommand{{
				Name:        "demo",
				Description: "Run demo",
				Prompt:      "demo {{args}}",
			}},
		},
		chatModel: ChatModel{},
	}

	newM, cmd := m.handleSlashCommand("/demo ship it")
	if cmd == nil {
		t.Fatal("expected extension command to submit a prompt")
	}
	mm := newM.(*model)
	if !mm.running {
		t.Fatal("expected model to enter running state")
	}
	if len(mm.chatModel.Messages) < 1 || mm.chatModel.Messages[0].role != "user" {
		t.Fatalf("expected a user message, got %+v", mm.chatModel.Messages)
	}
	if mm.chatModel.Messages[0].content != "demo ship it" {
		t.Fatalf("expected rendered prompt, got %q", mm.chatModel.Messages[0].content)
	}
	_ = cmd()
}

func TestFormatHelp_IncludesExtensionCommands(t *testing.T) {
	m := &model{
		cfg: Config{
			ExtensionCommands: []extension.SlashCommand{{
				Name:        "demo",
				Description: "Run demo",
			}},
		},
	}

	help := m.formatHelp()
	if !strings.Contains(help, "/demo") {
		t.Fatalf("expected help to include extension command, got %q", help)
	}
}

func TestAllCommandNames_IncludesExtensionCommands(t *testing.T) {
	im := NewInputModel(nil, nil, nil, "")
	im.ExtensionCommands = []extension.SlashCommand{{Name: "demo", Description: "Run demo"}}

	cmds := im.AllCommandNames()
	found := false
	for _, cmd := range cmds {
		if cmd == "/demo" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected /demo in command list, got %v", cmds)
	}
}
