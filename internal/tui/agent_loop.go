package tui

import (
	"context"
	"encoding/json"
	"fmt"
	stdlog "log"
	"runtime/debug"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dimetron/pi-go/internal/llmutil"
)

// agentMsg wraps messages coming from the agent goroutine via a channel.
type agentMsg interface{ agentMsg() }

type agentTextMsg struct {
	text    string
	partial bool
}
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
type agentTraceMsg struct{ entry traceEntry }

func (agentTextMsg) agentMsg()       {}
func (agentThinkingMsg) agentMsg()   {}
func (agentToolCallMsg) agentMsg()   {}
func (agentToolResultMsg) agentMsg() {}
func (agentDoneMsg) agentMsg()       {}
func (agentTraceMsg) agentMsg()      {}

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
	if m.runCancel != nil {
		m.runCancel()
		m.runCancel = nil
	}
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
	// Block prompt submission when no model is configured.
	if m.cfg.NoModelConfigured && m.cfg.Agent == nil {
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: "No model configured. Use `/login <provider>` to set up an API key first.\n\nAvailable providers: `anthropic`, `openai`, `gemini`, `groq`, `mistral`, `xai`, `openrouter`",
		})
		return m, nil
	}

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

	// Trace: log the user prompt being submitted.
	truncated := promptText
	if len(truncated) > 200 {
		truncated = truncated[:200] + "…"
	}
	m.chatModel.TraceLog = append(m.chatModel.TraceLog, traceEntry{
		time: time.Now(), kind: "user_prompt", summary: "User prompt submitted", detail: truncated,
	})

	m.chatModel.Messages = append(m.chatModel.Messages, message{role: "user", content: text})
	m.chatModel.Messages = append(m.chatModel.Messages, message{role: "assistant", content: ""})
	m.chatModel.Streaming = ""
	m.chatModel.Thinking = ""
	m.running = true
	m.chatModel.Scroll = 0
	if m.face != nil {
		m.face.SetMood(MoodThinking)
	}

	parentCtx := m.ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	runCtx, runCancel := context.WithCancel(parentCtx)
	m.runCancel = runCancel
	m.agentCh = make(chan agentMsg, 64)
	go m.runAgentLoop(runCtx, promptText)

	return m, waitForAgent(m.agentCh)
}

// runAgentLoop runs the agent and sends events to the channel.
func (m *model) runAgentLoop(runCtx context.Context, prompt string) {
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

	// Trace: mark that we're dispatching to the LLM provider.
	m.agentCh <- agentTraceMsg{entry: traceEntry{
		time: time.Now(), kind: "request_sent",
		summary: fmt.Sprintf("Request dispatched → %s", m.cfg.ModelName),
		detail:  fmt.Sprintf("session=%s prompt_len=%d", m.cfg.SessionID, len(prompt)),
	}}

	for ev, err := range m.cfg.Agent.RunStreaming(runCtx, m.cfg.SessionID, prompt) {
		if err != nil {
			if log != nil {
				log.Error(err.Error())
			}
			m.agentCh <- agentDoneMsg{err: err}
			return
		}
		if ev == nil {
			continue
		}
		if ev.ErrorCode != "" {
			errMsg := fmt.Errorf("%s", llmutil.ResponseErrorText(ev.ErrorCode, ev.ErrorMessage))
			if log != nil {
				log.Error(errMsg.Error())
			}
			m.agentCh <- agentDoneMsg{err: errMsg}
			return
		}
		if ev.Content == nil {
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
				m.agentCh <- agentTextMsg{text: part.Text, partial: ev.Partial}
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

	// Trace: mark request completion.
	m.agentCh <- agentTraceMsg{entry: traceEntry{
		time: time.Now(), kind: "request_done",
		summary: "Agent loop finished",
	}}
}

// handleAgentTrace processes an agentTraceMsg and appends to the trace log.
func (m *model) handleAgentTrace(msg agentTraceMsg) (tea.Model, tea.Cmd) {
	m.chatModel.TraceLog = append(m.chatModel.TraceLog, msg.entry)
	return m, waitForAgent(m.agentCh)
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
		// Clear the streaming accumulator but keep the thinking message in
		// the chat history so it renders persistently above the response.
		m.chatModel.Thinking = ""
	}

	if msg.partial {
		m.chatModel.Streaming += msg.text
	} else {
		m.chatModel.Streaming = msg.text
	}

	// Ensure the assistant message is at the tail of the list so the
	// response text always renders below tools and thinking messages.
	msgs := m.chatModel.Messages
	lastIdx := len(msgs) - 1
	if lastIdx >= 0 && msgs[lastIdx].role == "assistant" {
		// Already at the tail — update in place.
		msgs[lastIdx].content = m.chatModel.Streaming
	} else {
		// Buried behind tool/thinking messages — relocate to the tail.
		for i := lastIdx; i >= 0; i-- {
			if msgs[i].role == "assistant" {
				m.chatModel.Messages = append(msgs[:i:i], msgs[i+1:]...)
				break
			}
		}
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role: "assistant", content: m.chatModel.Streaming,
		})
	}
	m.chatModel.Scroll = 0
	if len(m.chatModel.TraceLog) > 0 && m.chatModel.TraceLog[len(m.chatModel.TraceLog)-1].kind == "llm" {
		m.chatModel.TraceLog[len(m.chatModel.TraceLog)-1].detail = m.chatModel.Streaming
	} else {
		m.chatModel.TraceLog = append(m.chatModel.TraceLog, traceEntry{
			time: time.Now(), kind: "llm", summary: "LLM response", detail: m.chatModel.Streaming,
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

	// Insert the tool message before any trailing assistant placeholder so
	// that tools render above the final response text, not below it.
	msgs := m.chatModel.Messages
	lastIdx := len(msgs) - 1
	if lastIdx >= 0 && msgs[lastIdx].role == "assistant" {
		tail := msgs[lastIdx]
		m.chatModel.Messages = append(msgs[:lastIdx:lastIdx], newMsg, tail)
	} else {
		m.chatModel.Messages = append(m.chatModel.Messages, newMsg)
	}
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
	m.runCancel = nil
	m.agentCh = nil
	m.refreshDiffStats()
	return m, nil
}
