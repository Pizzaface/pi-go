package tui

import (
	"fmt"
	"testing"
)

func TestRenderMarkdownCachesOutput(t *testing.T) {
	c := NewChatModel(nil)
	c.UpdateRenderer(80)

	input := "# Hello\n\nworld"
	first := c.RenderMarkdown(input)
	if first == "" || first == input {
		t.Fatalf("glamour did not render; got %q", first)
	}

	// Nil the renderer. Without a cache, RenderMarkdown would now
	// return the raw input. With a cache, we get the prior output.
	c.Renderer = nil
	second := c.RenderMarkdown(input)
	if second != first {
		t.Fatalf("cache miss: want %q got %q", first, second)
	}
}

func TestUpdateRendererInvalidatesMarkdownCache(t *testing.T) {
	c := NewChatModel(nil)
	c.UpdateRenderer(80)

	input := "# hello"
	_ = c.RenderMarkdown(input)

	c.UpdateRenderer(40)
	c.Renderer = nil
	got := c.RenderMarkdown(input)
	if got != input {
		t.Fatalf("expected cache cleared after UpdateRenderer; got %q", got)
	}
}

func TestClearInvalidatesMarkdownCache(t *testing.T) {
	c := NewChatModel(nil)
	c.UpdateRenderer(80)

	input := "# hello"
	_ = c.RenderMarkdown(input)

	c.Clear()
	c.Renderer = nil
	got := c.RenderMarkdown(input)
	if got != input {
		t.Fatalf("expected cache cleared after Clear; got %q", got)
	}
}

func TestRenderMarkdownCacheBounded(t *testing.T) {
	c := NewChatModel(nil)
	c.UpdateRenderer(80)

	for i := 0; i < 600; i++ {
		c.RenderMarkdown(fmt.Sprintf("message %d", i))
	}
	if got := len(c.mdCache); got > 512 {
		t.Fatalf("cache exceeded cap: %d entries", got)
	}
}

func agentMessage(content string) message {
	return message{
		role:         "tool",
		tool:         "agent",
		toolIn:       `{"task":"t"}`,
		agentGroupID: 1,
		content:      content,
		collapsed:    true, // render response inline, no deferral
	}
}

func TestRenderAgentRespCachesOutput(t *testing.T) {
	c := NewChatModel(nil)
	c.UpdateRenderer(80)
	c.ToolDisplay.Width = 80
	c.Messages = []message{agentMessage("hello world")}

	_ = c.RenderMessages(false)
	if len(c.agentRespCache) == 0 {
		t.Fatalf("expected agentRespCache populated; got empty")
	}
}

func TestClearInvalidatesAgentRespCache(t *testing.T) {
	c := NewChatModel(nil)
	c.UpdateRenderer(80)
	c.ToolDisplay.Width = 80
	c.Messages = []message{agentMessage("hello world")}
	_ = c.RenderMessages(false)

	c.Clear()
	if len(c.agentRespCache) != 0 {
		t.Fatalf("expected agentRespCache cleared; got %d entries", len(c.agentRespCache))
	}
}

func TestUpdateRendererInvalidatesAgentRespCache(t *testing.T) {
	c := NewChatModel(nil)
	c.UpdateRenderer(80)
	c.ToolDisplay.Width = 80
	c.Messages = []message{agentMessage("hello world")}
	_ = c.RenderMessages(false)

	c.UpdateRenderer(40)
	if len(c.agentRespCache) != 0 {
		t.Fatalf("expected agentRespCache cleared on UpdateRenderer; got %d entries", len(c.agentRespCache))
	}
}
