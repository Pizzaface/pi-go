package tui

import (
	"encoding/json"
	"fmt"
	stdlog "log"
	"runtime/debug"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// agentMsg wraps messages coming from the agent goroutine via a channel.
type agentMsg interface{ agentMsg() }

type agentTextMsg struct{ text string }
type agentThinkingMsg struct{ text string }
type agentToolCallMsg struct {
	name string
	args map[string]any
}
type agentToolResultMsg struct {
	name    string
	content string
}
type agentDoneMsg struct{ err error }


func (agentTextMsg) agentMsg()       {}
func (agentThinkingMsg) agentMsg()   {}
func (agentToolCallMsg) agentMsg()   {}
func (agentToolResultMsg) agentMsg() {}
func (agentDoneMsg) agentMsg()       {}

// waitForAgent returns a Cmd that waits for the next message on the agent channel.
func waitForAgent(ch chan agentMsg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return agentDoneMsg{}
		}
		return msg
	}
}


// cancelAgent stops a running agent and drains its channel.
func (m *model) cancelAgent() {
	m.cancel()
	m.running = false
	m.statusModel.ActiveTool = ""
	m.statusModel.ActiveTools = nil
	m.chatModel.Streaming = ""
	m.chatModel.Thinking = ""
	if m.face != nil {
		m.face.SetMood(MoodIdle)
	}
	if m.agentCh != nil {
		go func(ch chan agentMsg) {
			for range ch {
			}
		}(m.agentCh)
		m.agentCh = nil
	}
}

// submitPrompt sends a user prompt to the agent.
func (m *model) submitPrompt(text string, mentions []string) (tea.Model, tea.Cmd) {
	// Append referenced file annotations for @mentions.
	promptText := text
	if len(mentions) > 0 {
		var refs strings.Builder
		refs.WriteString(text)
		refs.WriteString("\n")
		for _, path := range mentions {
			refs.WriteString("\n[Referenced file: ")
			refs.WriteString(path)
			refs.WriteString("]")
		}
		promptText = refs.String()
	}

	if m.cfg.Logger != nil {
		m.cfg.Logger.UserMessage(promptText)
	}

	m.chatModel.Messages = append(m.chatModel.Messages, message{role: "user", content: text})
	m.chatModel.Messages = append(m.chatModel.Messages, message{role: "assistant", content: ""})
	m.chatModel.Streaming = ""
	m.chatModel.Thinking = ""
	m.running = true
	m.chatModel.Scroll = 0
	if m.face != nil {
		m.face.SetMood(MoodThinking)
	}

	m.agentCh = make(chan agentMsg, 64)
	go m.runAgentLoop(promptText)

	return m, waitForAgent(m.agentCh)
}

// runAgentLoop runs the agent and sends events to the channel.
func (m *model) runAgentLoop(prompt string) {
	defer close(m.agentCh)
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			stdlog.Printf("agent loop panicked: %v\n%s", r, stack)
			m.agentCh <- agentDoneMsg{err: fmt.Errorf("agent panic: %v", r)}
		}
	}()

	// Guard against missing agent config (unit tests)
	if m.cfg.Agent == nil {
		m.agentCh <- agentDoneMsg{err: fmt.Errorf("agent not configured")}
		return
	}

	log := m.cfg.Logger

	for ev, err := range m.cfg.Agent.RunStreaming(m.ctx, m.cfg.SessionID, prompt) {
		if err != nil {
			if log != nil {
				log.Error(err.Error())
			}
			m.agentCh <- agentDoneMsg{err: err}
			return
		}
		if ev == nil || ev.Content == nil {
			continue
		}
		for _, part := range ev.Content.Parts {
			if part.Text != "" && ev.Content.Role == "thinking" {
				m.agentCh <- agentThinkingMsg{text: part.Text}
				continue
			}
			if part.Text != "" {
				if log != nil {
					log.LLMText(ev.Author, part.Text)
				}
				m.agentCh <- agentTextMsg{text: part.Text}
			}
			if part.FunctionCall != nil {
				if log != nil {
					log.ToolCall(ev.Author, part.FunctionCall.Name, part.FunctionCall.Args)
				}
				m.agentCh <- agentToolCallMsg{
					name: part.FunctionCall.Name,
					args: part.FunctionCall.Args,
				}
			}
			if part.FunctionResponse != nil {
				respJSON, _ := json.Marshal(part.FunctionResponse.Response)
				if log != nil {
					log.ToolResult(ev.Author, part.FunctionResponse.Name, string(respJSON))
				}
				m.agentCh <- agentToolResultMsg{
					name:    part.FunctionResponse.Name,
					content: string(respJSON),
				}
			}
		}
	}
}

// handleAgentThinking processes an agentThinkingMsg.
func (m *model) handleAgentThinking(msg agentThinkingMsg) (tea.Model, tea.Cmd) {
	if m.face != nil {
		m.face.SetMood(MoodThinking)
	}
	m.chatModel.Thinking += msg.text
	if len(m.chatModel.Messages) > 0 && m.chatModel.Messages[len(m.chatModel.Messages)-1].role == "thinking" {
		m.chatModel.Messages[len(m.chatModel.Messages)-1].content = m.chatModel.Thinking
	} else {
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role: "thinking", content: m.chatModel.Thinking,
		})
	}
	m.chatModel.Scroll = 0
	return m, waitForAgent(m.agentCh)
}

// handleAgentText processes an agentTextMsg.
func (m *model) handleAgentText(msg agentTextMsg) (tea.Model, tea.Cmd) {
	if m.face != nil {
		m.face.SetMood(MoodSpeaking)
	}
	if m.chatModel.Thinking != "" {
		m.chatModel.Thinking = ""
		if len(m.chatModel.Messages) > 0 && m.chatModel.Messages[len(m.chatModel.Messages)-1].role == "thinking" {
			m.chatModel.Messages[len(m.chatModel.Messages)-1] = message{role: "assistant", content: ""}
		}
	}
	m.chatModel.Streaming += msg.text
	for i := len(m.chatModel.Messages) - 1; i >= 0; i-- {
		if m.chatModel.Messages[i].role == "assistant" {
			m.chatModel.Messages[i].content = m.chatModel.Streaming
			break
		}
	}
	m.chatModel.Scroll = 0
	if len(m.chatModel.TraceLog) > 0 && m.chatModel.TraceLog[len(m.chatModel.TraceLog)-1].kind == "llm" {
		m.chatModel.TraceLog[len(m.chatModel.TraceLog)-1].detail = m.chatModel.Streaming
	} else {
		m.chatModel.TraceLog = append(m.chatModel.TraceLog, traceEntry{
			time: time.Now(), kind: "llm", summary: "LLM response", detail: msg.text,
		})
	}
	return m, waitForAgent(m.agentCh)
}

// handleAgentToolCall processes an agentToolCallMsg.
func (m *model) handleAgentToolCall(msg agentToolCallMsg) (tea.Model, tea.Cmd) {
	if m.face != nil {
		m.face.SetMood(MoodToolCall)
	}
	if m.statusModel.ActiveTools == nil {
		m.statusModel.ActiveTools = make(map[string]time.Time)
	}
	m.statusModel.ActiveTools[msg.name] = time.Now()
	m.statusModel.ActiveTool = msg.name
	m.statusModel.ToolStart = time.Now()
	argsJSON, _ := json.MarshalIndent(msg.args, "", "  ")
	m.chatModel.TraceLog = append(m.chatModel.TraceLog, traceEntry{
		time:    time.Now(),
		kind:    "tool_call",
		summary: fmt.Sprintf(">>> %s", msg.name),
		detail:  string(argsJSON),
	})
	toolIn := toolCallSummary(msg.name, msg.args)
	newMsg := message{
		role: "tool", tool: msg.name, toolIn: toolIn,
	}
	m.chatModel.Messages = append(m.chatModel.Messages, newMsg)
	return m, waitForAgent(m.agentCh)
}

// handleAgentToolResult processes an agentToolResultMsg.
func (m *model) handleAgentToolResult(msg agentToolResultMsg) (tea.Model, tea.Cmd) {
	if m.face != nil {
		m.face.SetMood(MoodProcessing)
	}
	delete(m.statusModel.ActiveTools, msg.name)
	m.statusModel.ActiveTool = ""
	for name := range m.statusModel.ActiveTools {
		m.statusModel.ActiveTool = name
		m.statusModel.ToolStart = m.statusModel.ActiveTools[name]
		break
	}
	m.chatModel.TraceLog = append(m.chatModel.TraceLog, traceEntry{
		time:    time.Now(),
		kind:    "tool_result",
		summary: fmt.Sprintf("<<< %s", msg.name),
		detail:  msg.content,
	})
	for i := len(m.chatModel.Messages) - 1; i >= 0; i-- {
		if m.chatModel.Messages[i].role == "tool" && m.chatModel.Messages[i].tool == msg.name && m.chatModel.Messages[i].content == "" {
			m.chatModel.Messages[i].content = toolResultSummary(msg.content)
			break
		}
	}
	m.refreshDiffStats()
	return m, waitForAgent(m.agentCh)
}


// handleAgentDone processes an agentDoneMsg.
func (m *model) handleAgentDone(msg agentDoneMsg) (tea.Model, tea.Cmd) {
	m.running = false
	m.statusModel.ActiveTool = ""
	m.statusModel.ActiveTools = nil
	if msg.err != nil {
		if m.face != nil {
			m.face.SetMood(MoodSad)
		}
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: fmt.Sprintf("Error: %v", msg.err),
		})
		m.chatModel.TraceLog = append(m.chatModel.TraceLog, traceEntry{
			time: time.Now(), kind: "error", summary: "Error", detail: msg.err.Error(),
		})
	} else {
		if m.face != nil {
			m.face.SetMood(MoodHappy)
		}
	}
	m.chatModel.Streaming = ""
	m.chatModel.Thinking = ""
	m.agentCh = nil
	m.refreshDiffStats()
	return m, nil
}
