package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// maxPromptOutput is the maximum size of tool output included in the compression prompt.
const maxPromptOutput = 4096

// NoopCompressor is a pass-through compressor that extracts basic metadata
// It serves as the default when no external compression service is configured.
type NoopCompressor struct{}

// NewNoopCompressor creates a compressor that does basic metadata extraction
// without requiring an LLM.
func NewNoopCompressor() *NoopCompressor {
	return &NoopCompressor{}
}

// CompressObservation extracts a structured Observation from raw tool data
// using simple heuristics (no LLM call).
func (c *NoopCompressor) CompressObservation(_ context.Context, raw RawObservation) (*Observation, error) {
	title := fmt.Sprintf("%s observation", raw.ToolName)
	text := summarizeToolIO(raw)

	// Extract source files from input if present.
	var sourceFiles []string
	if raw.ToolInput != nil {
		if path, ok := raw.ToolInput["path"].(string); ok && path != "" {
			sourceFiles = append(sourceFiles, path)
		}
		if path, ok := raw.ToolInput["file_path"].(string); ok && path != "" {
			sourceFiles = append(sourceFiles, path)
		}
	}
	if sourceFiles == nil {
		sourceFiles = []string{}
	}

	return &Observation{
		SessionID:   raw.SessionID,
		Project:     raw.Project,
		Title:       title,
		Type:        TypeChange,
		Text:        text,
		SourceFiles: sourceFiles,
		ToolName:    raw.ToolName,
		CreatedAt:   raw.Timestamp,
	}, nil
}

// SummarizeSession creates a basic session summary without an LLM.
func (c *NoopCompressor) SummarizeSession(_ context.Context, sessionID, project string, observations []*Observation) (*SessionSummary, error) {
	var titles []string
	for _, obs := range observations {
		titles = append(titles, obs.Title)
	}
	completed := "No observations recorded."
	if len(titles) > 0 {
		completed = strings.Join(titles, "; ")
	}
	return &SessionSummary{
		SessionID: sessionID,
		Project:   project,
		Completed: completed,
		CreatedAt: time.Now(),
	}, nil
}

// buildCompressionPrompt creates a JSON prompt suitable for a compression service.
func buildCompressionPrompt(raw RawObservation) string {
	inputJSON, _ := json.Marshal(raw.ToolInput)
	outputJSON, _ := json.Marshal(raw.ToolOutput)

	outputStr := string(outputJSON)
	if len(outputStr) > maxPromptOutput {
		outputStr = outputStr[:maxPromptOutput] + "...(truncated)"
	}

	data := map[string]string{
		"tool_name":   raw.ToolName,
		"tool_input":  string(inputJSON),
		"tool_output": outputStr,
	}
	b, _ := json.Marshal(data)
	return string(b)
}

// summarizeToolIO builds a short text summary from tool input/output.
func summarizeToolIO(raw RawObservation) string {
	inputJSON, _ := json.Marshal(raw.ToolInput)
	text := string(inputJSON)
	if len(text) > maxPromptOutput {
		text = text[:maxPromptOutput] + "...(truncated)"
	}
	return text
}

// compressedResponse is the JSON structure expected from an external compression service.
type compressedResponse struct {
	Title       string   `json:"title"`
	Type        string   `json:"type"`
	Text        string   `json:"text"`
	SourceFiles []string `json:"source_files"`
}

// parseCompressedResponse extracts a structured Observation from JSON output.
func parseCompressedResponse(text string, raw RawObservation) (*Observation, error) {
	text = stripCodeFences(text)
	text = strings.TrimSpace(text)

	if text == "" {
		return nil, fmt.Errorf("empty response from compressor")
	}

	var resp compressedResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w (response: %s)", err, truncateForError(text))
	}

	if resp.Title == "" {
		return nil, fmt.Errorf("compressor returned empty title")
	}

	obsType := ObservationType(resp.Type)
	if !ValidObservationTypes[obsType] {
		obsType = TypeChange
	}

	if resp.SourceFiles == nil {
		resp.SourceFiles = []string{}
	}

	return &Observation{
		SessionID:   raw.SessionID,
		Project:     raw.Project,
		Title:       resp.Title,
		Type:        obsType,
		Text:        resp.Text,
		SourceFiles: resp.SourceFiles,
		ToolName:    raw.ToolName,
		CreatedAt:   raw.Timestamp,
	}, nil
}

// stripCodeFences removes ```json ... ``` wrapping from text.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}

// truncateForError truncates a string for inclusion in error messages.
func truncateForError(s string) string {
	const maxLen = 200
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// buildSummaryPrompt creates a prompt for session summarization.
func buildSummaryPrompt(observations []*Observation) string {
	var b strings.Builder
	b.WriteString("Summarize this coding session. The following observations were recorded:\n\n")
	for _, obs := range observations {
		fmt.Fprintf(&b, "- [%s] %s: %s\n", obs.Type, obs.Title, obs.Text)
		if len(obs.SourceFiles) > 0 {
			fmt.Fprintf(&b, "  Files: %s\n", strings.Join(obs.SourceFiles, ", "))
		}
	}
	b.WriteString("\nRespond with ONLY a JSON object:\n")
	b.WriteString(`{"request": "what was the user trying to do", "investigated": "what was explored", "learned": "key discoveries", "completed": "what was accomplished", "next_steps": "suggested follow-ups"}`)
	return b.String()
}

// summaryResponse is the JSON structure expected for session summaries.
type summaryResponse struct {
	Request      string `json:"request"`
	Investigated string `json:"investigated"`
	Learned      string `json:"learned"`
	Completed    string `json:"completed"`
	NextSteps    string `json:"next_steps"`
}

// parseSummaryResponse parses JSON summary output.
func parseSummaryResponse(text, sessionID, project string) (*SessionSummary, error) {
	text = stripCodeFences(text)
	text = strings.TrimSpace(text)

	if text == "" {
		return nil, fmt.Errorf("empty summary response")
	}

	var resp summaryResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return nil, fmt.Errorf("invalid summary JSON: %w", err)
	}

	return &SessionSummary{
		SessionID:    sessionID,
		Project:      project,
		Request:      resp.Request,
		Investigated: resp.Investigated,
		Learned:      resp.Learned,
		Completed:    resp.Completed,
		NextSteps:    resp.NextSteps,
		CreatedAt:    time.Now(),
	}, nil
}
