package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"google.golang.org/adk/model"

	"google.golang.org/genai"
)

func TestOaiFunctionResponseContent(t *testing.T) {
	tests := []struct {
		name string
		resp any
		want string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"map with result", map[string]any{"result": "ok"}, "ok"},
		{"map with content", map[string]any{"content": []any{map[string]any{"text": "hello"}}}, "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := oaiFunctionResponseContent(tt.resp)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOaiFunctionResponseContentEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		resp any
		want string
	}{
		{"nil input", nil, ""},
		{"string input", "hello world", "hello world"},
		{"empty string input", "", ""},
		{"map with result key", map[string]any{"result": "ok"}, "ok"},
		{"map with content array", map[string]any{"content": []any{map[string]any{"text": "extracted"}}}, "extracted"},
		{"map with content array missing text", map[string]any{"content": []any{map[string]any{"type": "image"}}}, `{"content":[{"type":"image"}]}`},
		{"map with content array non-map item", map[string]any{"content": []any{"plain string"}}, `{"content":["plain string"]}`},
		{"map with empty content array", map[string]any{"content": []any{}}, `{"content":[]}`},
		{"map with content not array", map[string]any{"content": "not-array"}, `{"content":"not-array"}`},
		{"map with neither result nor content", map[string]any{"status": "done"}, `{"status":"done"}`},
		{"map with both content and result prefers content", map[string]any{
			"content": []any{map[string]any{"text": "from-content"}},
			"result":  "from-result",
		}, "from-content"},
		{"number input", 42, "42"},
		{"bool input", true, "true"},
		{"slice input", []string{"a", "b"}, `["a","b"]`},
		{"map with result non-string", map[string]any{"result": 123}, `{"result":123}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := oaiFunctionResponseContent(tt.resp)
			if got != tt.want {
				t.Errorf("oaiFunctionResponseContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeOpenAIBaseURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://api.openai.com", "https://api.openai.com/v1"},
		{"https://api.openai.com/", "https://api.openai.com/v1"},
		{"https://custom.example.com/openai", "https://custom.example.com/openai/v1"},
		{"https://custom.example.com/v1", "https://custom.example.com/v1"},
	}
	for _, tt := range tests {
		if got := normalizeOpenAIBaseURL(tt.in); got != tt.want {
			t.Fatalf("normalizeOpenAIBaseURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNewOpenAIWithBaseURL(t *testing.T) {
	llm, err := NewOpenAI(context.Background(), "gpt-4o", "sk-test", "https://custom-api.example.com/v1", EffortMedium, nil)
	if err != nil {
		t.Fatalf("NewOpenAI() with baseURL error: %v", err)
	}
	if llm == nil {
		t.Fatal("NewOpenAI() returned nil")
	}
	if llm.Name() != "gpt-4o" {
		t.Errorf("Name() = %q, want %q", llm.Name(), "gpt-4o")
	}
}

func TestNewOpenAIWithExtraHeaders(t *testing.T) {
	llm, err := NewOpenAI(context.Background(), "gpt-4o", "sk-test", "", EffortMedium, &LLMOptions{
		ExtraHeaders: map[string]string{
			"X-Custom-Header": "custom-value",
			"X-Org-ID":        "org-123",
		},
	})
	if err != nil {
		t.Fatalf("NewOpenAI() with headers error: %v", err)
	}
	if llm == nil {
		t.Fatal("NewOpenAI() returned nil")
	}
	if llm.Name() != "gpt-4o" {
		t.Errorf("Name() = %q, want %q", llm.Name(), "gpt-4o")
	}
}

func TestNewOpenAIWithBaseURLAndHeaders(t *testing.T) {
	llm, err := NewOpenAI(context.Background(), "gpt-4o", "sk-test", "https://custom.example.com", EffortMedium, &LLMOptions{
		ExtraHeaders: map[string]string{"X-Custom": "value"},
	})
	if err != nil {
		t.Fatalf("NewOpenAI() with baseURL+headers error: %v", err)
	}
	if llm == nil {
		t.Fatal("NewOpenAI() returned nil")
	}
}

func TestNewOpenAIEmptyAPIKey(t *testing.T) {
	_, err := NewOpenAI(context.Background(), "gpt-4o", "", "", EffortMedium, nil)
	if err == nil {
		t.Fatal("expected error for empty API key")
	}
}

func TestOpenAIModelName(t *testing.T) {
	// Create a mock OpenAI model to test Name() method
	llm := &openaiModel{modelName: "gpt-4o"}
	if got := llm.Name(); got != "gpt-4o" {
		t.Errorf("Name() = %q, want %q", got, "gpt-4o")
	}
}

func TestOpenAIGenerateContentErrors(t *testing.T) {
	// Test with invalid API key to trigger error path
	llm, err := NewOpenAI(context.Background(), "gpt-4o", "test-key-invalid", "", EffortMedium, nil)
	if err != nil {
		t.Fatalf("failed to create model: %v", err)
	}

	t.Run("empty contents", func(t *testing.T) {
		req := &model.LLMRequest{
			Contents: []*genai.Content{},
		}
		seq := llm.GenerateContent(context.Background(), req, false)
		for resp, err := range seq {
			if err != nil {
				// Expected - no valid content
				return
			}
			_ = resp
		}
	})

	t.Run("with system prompt", func(t *testing.T) {
		req := &model.LLMRequest{
			Contents: []*genai.Content{
				{Role: "user", Parts: []*genai.Part{{Text: "Hi"}}},
			},
			Config: &genai.GenerateContentConfig{
				SystemInstruction: &genai.Content{
					Parts: []*genai.Part{{Text: "You are helpful."}},
				},
			},
		}
		seq := llm.GenerateContent(context.Background(), req, false)
		for resp, err := range seq {
			if err != nil {
				// Expected - API will fail with invalid key
				return
			}
			_ = resp
		}
	})
}

func TestOpenAIGenerateContentStreaming(t *testing.T) {
	// Test streaming mode
	llm, err := NewOpenAI(context.Background(), "gpt-4o", "test-key-invalid", "", EffortMedium, nil)
	if err != nil {
		t.Fatalf("failed to create model: %v", err)
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hi"}}},
		},
	}

	seq := llm.GenerateContent(context.Background(), req, true)
	for resp, err := range seq {
		if err != nil {
			return
		}
		_ = resp
	}
}

func TestOpenAIGenerateContentWithTools(t *testing.T) {
	// Test with tools configured
	llm, err := NewOpenAI(context.Background(), "gpt-4o", "test-key-invalid", "", EffortMedium, nil)
	if err != nil {
		t.Fatalf("failed to create model: %v", err)
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Use the tool"}}},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{
							Name:        "test_tool",
							Description: "A test tool",
							ParametersJsonSchema: map[string]any{
								"type": "object",
								"properties": map[string]any{
									"arg": map[string]any{"type": "string"},
								},
							},
						},
					},
				},
			},
		},
	}

	seq := llm.GenerateContent(context.Background(), req, false)
	for resp, err := range seq {
		if err != nil {
			return
		}
		_ = resp
	}
}

func TestOpenAIGenerateContentWithModelOverride(t *testing.T) {
	// Test with model override in request
	llm, err := NewOpenAI(context.Background(), "gpt-4o", "test-key-invalid", "", EffortMedium, nil)
	if err != nil {
		t.Fatalf("failed to create model: %v", err)
	}

	req := &model.LLMRequest{
		Model: "gpt-4-turbo",
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hi"}}},
		},
	}

	seq := llm.GenerateContent(context.Background(), req, false)
	for resp, err := range seq {
		if err != nil {
			return
		}
		_ = resp
	}
}

func TestOpenAINonStreamingTextResponse(t *testing.T) {
	// Mock server that returns a successful Responses API response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "expected POST", http.StatusMethodNotAllowed)
			return
		}
		body := map[string]any{
			"id":     "resp-test",
			"object": "response",
			"model":  "gpt-4o",
			"status": "completed",
			"output": []map[string]any{
				{
					"type": "message",
					"id":   "msg-001",
					"role": "assistant",
					"content": []map[string]any{
						{
							"type": "output_text",
							"text": "Hello world",
						},
					},
				},
			},
			"usage": map[string]any{
				"input_tokens":  10,
				"output_tokens": 5,
				"total_tokens":  15,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	ctx := context.Background()
	llm, err := NewOpenAI(ctx, "gpt-4o", "sk-test", srv.URL, EffortMedium, nil)
	if err != nil {
		t.Fatalf("NewOpenAI() error: %v", err)
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Say hello"}}},
		},
	}

	var llmResponses []*model.LLMResponse
	for resp, err := range llm.GenerateContent(ctx, req, false) {
		if err != nil {
			t.Fatalf("GenerateContent() error: %v", err)
		}
		llmResponses = append(llmResponses, resp)
	}

	if len(llmResponses) == 0 {
		t.Fatal("expected at least one response")
	}
	final := llmResponses[len(llmResponses)-1]
	if final.Content == nil {
		t.Fatal("expected non-nil Content")
	}
	if len(final.Content.Parts) == 0 {
		t.Fatal("expected at least one part in response")
	}
	if final.Content.Parts[0].Text != "Hello world" {
		t.Errorf("text = %q, want %q", final.Content.Parts[0].Text, "Hello world")
	}
	if !final.TurnComplete {
		t.Error("expected TurnComplete = true")
	}
	if final.FinishReason != genai.FinishReasonStop {
		t.Errorf("finish reason = %v, want Stop", final.FinishReason)
	}
	if final.UsageMetadata == nil {
		t.Fatal("expected non-nil UsageMetadata")
	}
	if final.UsageMetadata.PromptTokenCount != 10 {
		t.Errorf("prompt tokens = %d, want 10", final.UsageMetadata.PromptTokenCount)
	}
	if final.UsageMetadata.CandidatesTokenCount != 5 {
		t.Errorf("completion tokens = %d, want 5", final.UsageMetadata.CandidatesTokenCount)
	}
}

func TestOpenAINonStreamingToolCallResponse(t *testing.T) {
	// Mock server that returns a tool call in the Responses API format.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{
			"id":     "resp-tool-test",
			"object": "response",
			"model":  "gpt-4o",
			"status": "completed",
			"output": []map[string]any{
				{
					"type":      "function_call",
					"id":        "fc-001",
					"call_id":   "call_abc123",
					"name":      "get_weather",
					"arguments": `{"location":"San Francisco"}`,
				},
			},
			"usage": map[string]any{
				"input_tokens":  15,
				"output_tokens": 20,
				"total_tokens":  35,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	ctx := context.Background()
	llm, err := NewOpenAI(ctx, "gpt-4o", "sk-test", srv.URL, EffortMedium, nil)
	if err != nil {
		t.Fatalf("NewOpenAI() error: %v", err)
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "What's the weather in SF?"}}},
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{
							Name:        "get_weather",
							Description: "Get current weather",
							ParametersJsonSchema: map[string]any{
								"type": "object",
								"properties": map[string]any{
									"location": map[string]any{"type": "string"},
								},
								"required": []any{"location"},
							},
						},
					},
				},
			},
		},
	}

	var llmResponses []*model.LLMResponse
	for resp, err := range llm.GenerateContent(ctx, req, false) {
		if err != nil {
			t.Fatalf("GenerateContent() error: %v", err)
		}
		llmResponses = append(llmResponses, resp)
	}

	if len(llmResponses) == 0 {
		t.Fatal("expected at least one response")
	}
	final := llmResponses[len(llmResponses)-1]
	if final.Content == nil {
		t.Fatal("expected non-nil Content")
	}

	// Find the function call part.
	var fcPart *genai.Part
	for _, p := range final.Content.Parts {
		if p.FunctionCall != nil {
			fcPart = p
			break
		}
	}
	if fcPart == nil {
		t.Fatal("expected a FunctionCall part in response")
	}
	if fcPart.FunctionCall.Name != "get_weather" {
		t.Errorf("function name = %q, want get_weather", fcPart.FunctionCall.Name)
	}
	if fcPart.FunctionCall.ID != "call_abc123" {
		t.Errorf("function call ID = %q, want call_abc123", fcPart.FunctionCall.ID)
	}
	loc, _ := fcPart.FunctionCall.Args["location"].(string)
	if loc != "San Francisco" {
		t.Errorf("location arg = %q, want San Francisco", loc)
	}
}

func TestOpenAINonStreamingErrorResponse(t *testing.T) {
	// Mock server that returns a 500 error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"internal server error","type":"server_error"}}`))
	}))
	defer srv.Close()

	ctx := context.Background()
	llm, err := NewOpenAI(ctx, "gpt-4o", "sk-test", srv.URL, EffortMedium, nil)
	if err != nil {
		t.Fatalf("NewOpenAI() error: %v", err)
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		},
	}

	gotError := false
	for resp, err := range llm.GenerateContent(ctx, req, false) {
		if err != nil {
			gotError = true
			break
		}
		if resp != nil && resp.ErrorCode != "" {
			gotError = true
			break
		}
	}
	if !gotError {
		t.Error("expected an error or ErrorCode for 500 response")
	}
}

func TestOaiContentsToInputItems(t *testing.T) {
	t.Run("extracts system instruction", func(t *testing.T) {
		config := &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: "You are a helpful assistant."}},
			},
		}
		contents := []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		}
		items, sysInstr := oaiContentsToInputItems(contents, config)
		if sysInstr != "You are a helpful assistant." {
			t.Errorf("system instruction = %q, want %q", sysInstr, "You are a helpful assistant.")
		}
		if len(items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(items))
		}
		if items[0].OfMessage == nil {
			t.Fatal("expected message item")
		}
	})

	t.Run("converts user and model messages", func(t *testing.T) {
		contents := []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "What is Go?"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "Go is a programming language."}}},
			{Role: "user", Parts: []*genai.Part{{Text: "Tell me more."}}},
		}
		items, _ := oaiContentsToInputItems(contents, nil)
		if len(items) != 3 {
			t.Fatalf("expected 3 items, got %d", len(items))
		}
	})

	t.Run("handles function calls with responses", func(t *testing.T) {
		fc := genai.NewPartFromFunctionCall("read_file", map[string]any{"path": "/tmp/test.go"})
		fc.FunctionCall.ID = "call_123"
		fr := &genai.Part{
			FunctionResponse: &genai.FunctionResponse{
				ID:       "call_123",
				Name:     "read_file",
				Response: map[string]any{"result": "file contents here"},
			},
		}
		contents := []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Read the file"}}},
			{Role: "model", Parts: []*genai.Part{fc}},
			{Role: "user", Parts: []*genai.Part{fr}},
		}
		items, _ := oaiContentsToInputItems(contents, nil)
		if len(items) != 3 {
			t.Fatalf("expected 3 items, got %d", len(items))
		}
		if items[1].OfFunctionCall == nil {
			t.Error("expected function call item at index 1")
		}
		if items[2].OfFunctionCallOutput == nil {
			t.Error("expected function call output item at index 2")
		}
	})

	t.Run("nil config is handled", func(t *testing.T) {
		contents := []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		}
		items, sysInstr := oaiContentsToInputItems(contents, nil)
		if len(items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(items))
		}
		if sysInstr != "" {
			t.Errorf("expected empty system instruction, got %q", sysInstr)
		}
	})

	t.Run("nil content entries are skipped", func(t *testing.T) {
		contents := []*genai.Content{nil, {Role: "user", Parts: []*genai.Part{{Text: "Hello"}}}, nil}
		items, _ := oaiContentsToInputItems(contents, nil)
		if len(items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(items))
		}
	})

	t.Run("system role content is skipped", func(t *testing.T) {
		contents := []*genai.Content{
			{Role: " system ", Parts: []*genai.Part{{Text: "ignored"}}},
			{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		}
		items, _ := oaiContentsToInputItems(contents, nil)
		if len(items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(items))
		}
	})

	t.Run("assistant message with text and function calls", func(t *testing.T) {
		fc := genai.NewPartFromFunctionCall("my_tool", map[string]any{"arg": "val"})
		fc.FunctionCall.ID = "call_abc"
		fr := &genai.Part{
			FunctionResponse: &genai.FunctionResponse{
				ID: "call_abc", Name: "my_tool",
				Response: map[string]any{"result": "tool output text"},
			},
		}
		contents := []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Do something"}}},
			{Role: "model", Parts: []*genai.Part{{Text: "I will call the tool"}, fc}},
			{Role: "user", Parts: []*genai.Part{fr}},
		}
		items, _ := oaiContentsToInputItems(contents, nil)
		// user_msg + assistant_text_msg + function_call + function_call_output = 4
		if len(items) != 4 {
			t.Fatalf("expected 4 items, got %d", len(items))
		}
	})

	t.Run("function call without matching response", func(t *testing.T) {
		fc := genai.NewPartFromFunctionCall("orphan_tool", map[string]any{})
		fc.FunctionCall.ID = "call_orphan"
		contents := []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "Call it"}}},
			{Role: "model", Parts: []*genai.Part{fc}},
		}
		items, _ := oaiContentsToInputItems(contents, nil)
		// user_msg + function_call + function_call_output(default) = 3
		if len(items) != 3 {
			t.Fatalf("expected 3 items, got %d", len(items))
		}
	})

	t.Run("empty text parts produce no item", func(t *testing.T) {
		contents := []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: ""}}},
		}
		items, _ := oaiContentsToInputItems(contents, nil)
		if len(items) != 0 {
			t.Fatalf("expected 0 items for empty text, got %d", len(items))
		}
	})

	t.Run("system instruction with multiple parts", func(t *testing.T) {
		config := &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: "Part one."}, nil, {Text: "Part two."}},
			},
		}
		contents := []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "Hi"}}}}
		_, sysInstr := oaiContentsToInputItems(contents, config)
		if sysInstr != "Part one.\nPart two." {
			t.Errorf("system instruction = %q, want %q", sysInstr, "Part one.\nPart two.")
		}
	})
}

func TestOaiStatusToFinishReason(t *testing.T) {
	tests := []struct {
		name   string
		status responses.ResponseStatus
		reason string
		want   genai.FinishReason
	}{
		{"completed", responses.ResponseStatusCompleted, "", genai.FinishReasonStop},
		{"incomplete max tokens", responses.ResponseStatusIncomplete, "max_output_tokens", genai.FinishReasonMaxTokens},
		{"incomplete content filter", responses.ResponseStatusIncomplete, "content_filter", genai.FinishReasonSafety},
		{"incomplete other", responses.ResponseStatusIncomplete, "", genai.FinishReasonOther},
		{"failed", responses.ResponseStatusFailed, "", genai.FinishReasonOther},
		{"canceled", responses.ResponseStatusCancelled, "", genai.FinishReasonOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &responses.Response{
				Status:            tt.status,
				IncompleteDetails: responses.ResponseIncompleteDetails{Reason: tt.reason},
			}
			got := oaiStatusToFinishReason(resp)
			if got != tt.want {
				t.Errorf("oaiStatusToFinishReason() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOaiResponseToLLMResponse_TextOnly(t *testing.T) {
	resp := &responses.Response{
		Status: responses.ResponseStatusCompleted,
		Output: []responses.ResponseOutputItemUnion{
			fakeOutputMessage("Hello world"),
		},
		Usage: responses.ResponseUsage{InputTokens: 10, OutputTokens: 5},
	}
	result := oaiResponseToLLMResponse(resp)
	if result.Partial {
		t.Error("expected Partial = false")
	}
	if !result.TurnComplete {
		t.Error("expected TurnComplete = true")
	}
	if result.FinishReason != genai.FinishReasonStop {
		t.Errorf("FinishReason = %v, want Stop", result.FinishReason)
	}
	if len(result.Content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(result.Content.Parts))
	}
	if result.Content.Parts[0].Text != "Hello world" {
		t.Errorf("text = %q", result.Content.Parts[0].Text)
	}
	if result.UsageMetadata.PromptTokenCount != 10 {
		t.Errorf("PromptTokenCount = %d, want 10", result.UsageMetadata.PromptTokenCount)
	}
	if result.UsageMetadata.CandidatesTokenCount != 5 {
		t.Errorf("CandidatesTokenCount = %d, want 5", result.UsageMetadata.CandidatesTokenCount)
	}
}

func TestOaiResponseToLLMResponse_WithFunctionCall(t *testing.T) {
	resp := &responses.Response{
		Status: responses.ResponseStatusCompleted,
		Output: []responses.ResponseOutputItemUnion{
			fakeOutputFunctionCall("call_abc", "get_weather", `{"location":"San Francisco"}`),
		},
		Usage: responses.ResponseUsage{InputTokens: 15, OutputTokens: 20},
	}
	result := oaiResponseToLLMResponse(resp)
	var fcPart *genai.Part
	for _, p := range result.Content.Parts {
		if p.FunctionCall != nil {
			fcPart = p
			break
		}
	}
	if fcPart == nil {
		t.Fatal("expected a FunctionCall part")
	}
	if fcPart.FunctionCall.Name != "get_weather" {
		t.Errorf("function name = %q, want get_weather", fcPart.FunctionCall.Name)
	}
	if fcPart.FunctionCall.ID != "call_abc" {
		t.Errorf("function call ID = %q, want call_abc", fcPart.FunctionCall.ID)
	}
	loc, _ := fcPart.FunctionCall.Args["location"].(string)
	if loc != "San Francisco" {
		t.Errorf("location arg = %q, want San Francisco", loc)
	}
}

func TestOaiResponseToLLMResponse_Empty(t *testing.T) {
	resp := &responses.Response{
		Status: responses.ResponseStatusCompleted,
		Output: []responses.ResponseOutputItemUnion{},
	}
	result := oaiResponseToLLMResponse(resp)
	if len(result.Content.Parts) != 0 {
		t.Errorf("expected 0 parts, got %d", len(result.Content.Parts))
	}
	if result.UsageMetadata != nil {
		t.Error("expected nil UsageMetadata when tokens are 0")
	}
}

func TestOaiResponseToLLMResponse_MaxTokens(t *testing.T) {
	resp := &responses.Response{
		Status:            responses.ResponseStatusIncomplete,
		IncompleteDetails: responses.ResponseIncompleteDetails{Reason: "max_output_tokens"},
		Output: []responses.ResponseOutputItemUnion{
			fakeOutputMessage("truncated"),
		},
		Usage: responses.ResponseUsage{InputTokens: 100, OutputTokens: 4096},
	}
	result := oaiResponseToLLMResponse(resp)
	if result.FinishReason != genai.FinishReasonMaxTokens {
		t.Errorf("FinishReason = %v, want MaxTokens", result.FinishReason)
	}
}

func TestOaiGenaiToolsToResponses(t *testing.T) {
	t.Run("basic tool", func(t *testing.T) {
		tools := []*genai.Tool{
			{
				FunctionDeclarations: []*genai.FunctionDeclaration{
					{
						Name:        "read_file",
						Description: "Read a file",
						ParametersJsonSchema: map[string]any{
							"type": "object",
							"properties": map[string]any{
								"path": map[string]any{"type": "string"},
							},
							"required": []any{"path"},
						},
					},
				},
			},
		}
		result := oaiGenaiToolsToResponses(tools)
		if len(result) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(result))
		}
		if result[0].OfFunction == nil {
			t.Fatal("expected function tool")
		}
		if result[0].OfFunction.Name != "read_file" {
			t.Errorf("name = %q, want read_file", result[0].OfFunction.Name)
		}
	})

	t.Run("nil tool entries", func(t *testing.T) {
		tools := []*genai.Tool{
			nil,
			{},
			{FunctionDeclarations: nil},
			{FunctionDeclarations: []*genai.FunctionDeclaration{nil}},
			{
				FunctionDeclarations: []*genai.FunctionDeclaration{
					{Name: "test", Description: "test"},
				},
			},
		}
		result := oaiGenaiToolsToResponses(tools)
		if len(result) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(result))
		}
	})

	t.Run("default type is object", func(t *testing.T) {
		tools := []*genai.Tool{
			{
				FunctionDeclarations: []*genai.FunctionDeclaration{
					{
						Name:        "test",
						Description: "Test",
						ParametersJsonSchema: map[string]any{
							"properties": map[string]any{
								"arg": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		}
		result := oaiGenaiToolsToResponses(tools)
		if len(result) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(result))
		}
		if result[0].OfFunction.Parameters["type"] != "object" {
			t.Error("expected type=object default")
		}
	})
}

// --- Test helpers for Responses API output items ---

func TestParseToolIntent(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantName string
		wantArgs map[string]any
		wantOK   bool
	}{
		{
			name:     "litellm function+arguments",
			text:     `{"function":"grep","arguments":{"pattern":"foo"}}`,
			wantName: "grep",
			wantArgs: map[string]any{"pattern": "foo"},
			wantOK:   true,
		},
		{
			name:     "openai-style name+arguments",
			text:     `{"name":"grep","arguments":{"pattern":"foo","path":"."}}`,
			wantName: "grep",
			wantArgs: map[string]any{"pattern": "foo", "path": "."},
			wantOK:   true,
		},
		{
			name:     "tool+parameters variant",
			text:     `{"tool":"read","parameters":{"file_path":"x.go"}}`,
			wantName: "read",
			wantArgs: map[string]any{"file_path": "x.go"},
			wantOK:   true,
		},
		{
			name:     "nested function object",
			text:     `{"function":{"name":"grep","arguments":{"pattern":"foo"}}}`,
			wantName: "grep",
			wantArgs: map[string]any{"pattern": "foo"},
			wantOK:   true,
		},
		{
			name:     "stringified arguments",
			text:     `{"name":"grep","arguments":"{\"pattern\":\"foo\"}"}`,
			wantName: "grep",
			wantArgs: map[string]any{"pattern": "foo"},
			wantOK:   true,
		},
		{
			name:     "whitespace wrapped",
			text:     "  \n{\"function\":\"tree\",\"arguments\":{}}  \n",
			wantName: "tree",
			wantArgs: nil,
			wantOK:   true,
		},
		{
			name:   "plain prose",
			text:   "I'll search for that pattern now.",
			wantOK: false,
		},
		{
			name:   "json but no name field",
			text:   `{"arguments":{"pattern":"foo"}}`,
			wantOK: false,
		},
		{
			name:   "json but not a tool shape",
			text:   `{"result":42}`,
			wantOK: false,
		},
		{
			name:   "truncated json",
			text:   `{"function":"grep","arguments":{"pattern":`,
			wantOK: false,
		},
		{
			name:   "empty string",
			text:   "",
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, args, ok := parseToolIntent(tt.text)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if fmt.Sprintf("%v", args) != fmt.Sprintf("%v", tt.wantArgs) {
				t.Errorf("args = %v, want %v", args, tt.wantArgs)
			}
		})
	}
}

func TestOaiResponseToLLMResponse_SynthesizedToolIntent(t *testing.T) {
	resp := &responses.Response{
		Status: responses.ResponseStatusCompleted,
		Output: []responses.ResponseOutputItemUnion{
			fakeOutputMessage(`{"function":"grep","arguments":{"pattern":"TODO"}}`),
		},
		Usage: responses.ResponseUsage{InputTokens: 10, OutputTokens: 5},
	}
	result := oaiResponseToLLMResponse(resp)
	if len(result.Content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(result.Content.Parts))
	}
	fc := result.Content.Parts[0].FunctionCall
	if fc == nil {
		t.Fatalf("expected FunctionCall part, got text %q", result.Content.Parts[0].Text)
	}
	if fc.Name != "grep" {
		t.Errorf("name = %q, want grep", fc.Name)
	}
	if fc.Args["pattern"] != "TODO" {
		t.Errorf("args = %v, want pattern=TODO", fc.Args)
	}
	if fc.ID == "" {
		t.Error("expected synthetic ID to be set")
	}
}

func TestOaiResponseToLLMResponse_PlainTextUnchanged(t *testing.T) {
	resp := &responses.Response{
		Status: responses.ResponseStatusCompleted,
		Output: []responses.ResponseOutputItemUnion{
			fakeOutputMessage("I'll look into that."),
		},
	}
	result := oaiResponseToLLMResponse(resp)
	if len(result.Content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(result.Content.Parts))
	}
	if result.Content.Parts[0].Text != "I'll look into that." {
		t.Errorf("text = %q", result.Content.Parts[0].Text)
	}
	if result.Content.Parts[0].FunctionCall != nil {
		t.Error("expected no FunctionCall for plain text")
	}
}

func fakeOutputMessage(text string) responses.ResponseOutputItemUnion {
	raw := fmt.Sprintf(`{"type":"message","id":"msg_test","role":"assistant","status":"completed","content":[{"type":"output_text","text":%s}]}`, mustJSON(text))
	var item responses.ResponseOutputItemUnion
	_ = json.Unmarshal([]byte(raw), &item)
	return item
}

func fakeOutputFunctionCall(callID, name, arguments string) responses.ResponseOutputItemUnion {
	raw := fmt.Sprintf(`{"type":"function_call","id":"fc_test","call_id":%s,"name":%s,"arguments":%s,"status":"completed"}`,
		mustJSON(callID), mustJSON(name), mustJSON(arguments))
	var item responses.ResponseOutputItemUnion
	_ = json.Unmarshal([]byte(raw), &item)
	return item
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestAsRateLimitError(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		if _, ok := asRateLimitError(nil); ok {
			t.Error("expected false for nil")
		}
	})

	t.Run("non-openai error", func(t *testing.T) {
		if _, ok := asRateLimitError(errors.New("boom")); ok {
			t.Error("expected false for plain error")
		}
	})

	t.Run("non-429 openai error", func(t *testing.T) {
		err := &openai.Error{
			StatusCode: http.StatusInternalServerError,
			Message:    "internal",
			Response:   &http.Response{Header: make(http.Header)},
		}
		if _, ok := asRateLimitError(err); ok {
			t.Error("expected false for 500")
		}
	})

	t.Run("429 with Retry-After seconds", func(t *testing.T) {
		hdr := make(http.Header)
		hdr.Set("Retry-After", "42")
		err := &openai.Error{
			StatusCode: http.StatusTooManyRequests,
			Message:    "rate limit exceeded",
			Response:   &http.Response{Header: hdr},
		}
		rl, ok := asRateLimitError(err)
		if !ok {
			t.Fatal("expected true for 429")
		}
		if rl.RetryAfter() != 42*time.Second {
			t.Errorf("RetryAfter = %v, want 42s", rl.RetryAfter())
		}
		if !strings.Contains(rl.Error(), "42s") || !strings.Contains(rl.Error(), "rate limit exceeded") {
			t.Errorf("Error() = %q, want 42s and underlying message", rl.Error())
		}
	})

	t.Run("429 with Retry-After-Ms takes precedence", func(t *testing.T) {
		hdr := make(http.Header)
		hdr.Set("Retry-After", "60")
		hdr.Set("Retry-After-Ms", "250")
		err := &openai.Error{
			StatusCode: http.StatusTooManyRequests,
			Message:    "slow down",
			Response:   &http.Response{Header: hdr},
		}
		rl, ok := asRateLimitError(err)
		if !ok {
			t.Fatal("expected true")
		}
		if rl.RetryAfter() != 250*time.Millisecond {
			t.Errorf("RetryAfter = %v, want 250ms", rl.RetryAfter())
		}
	})

	t.Run("429 with HTTP-date Retry-After", func(t *testing.T) {
		hdr := make(http.Header)
		hdr.Set("Retry-After", time.Now().Add(3*time.Second).UTC().Format(http.TimeFormat))
		err := &openai.Error{
			StatusCode: http.StatusTooManyRequests,
			Message:    "slow down",
			Response:   &http.Response{Header: hdr},
		}
		rl, ok := asRateLimitError(err)
		if !ok {
			t.Fatal("expected true")
		}
		if d := rl.RetryAfter(); d <= 0 || d > 5*time.Second {
			t.Errorf("RetryAfter = %v, want ~3s", d)
		}
	})

	t.Run("429 without Retry-After", func(t *testing.T) {
		err := &openai.Error{
			StatusCode: http.StatusTooManyRequests,
			Message:    "rate limit",
			Response:   &http.Response{Header: make(http.Header)},
		}
		rl, ok := asRateLimitError(err)
		if !ok {
			t.Fatal("expected true")
		}
		if rl.RetryAfter() != 0 {
			t.Errorf("RetryAfter = %v, want 0", rl.RetryAfter())
		}
		if !strings.Contains(rl.Error(), "rate limit") {
			t.Errorf("Error() = %q, want mention rate limit", rl.Error())
		}
	})

	t.Run("wrapped 429 is detected via errors.As", func(t *testing.T) {
		hdr := make(http.Header)
		hdr.Set("Retry-After", "1")
		err := fmt.Errorf("wrapped: %w", &openai.Error{
			StatusCode: http.StatusTooManyRequests,
			Message:    "rate limit",
			Response:   &http.Response{Header: hdr},
		})
		if _, ok := asRateLimitError(err); !ok {
			t.Error("expected true for wrapped 429")
		}
	})
}

func TestOpenAINonStreamingRateLimit(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After-Ms", "1")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit exceeded","type":"rate_limit_error"}}`))
	}))
	defer srv.Close()

	ctx := context.Background()
	llm, err := NewOpenAI(ctx, "gpt-4o", "sk-test", srv.URL, EffortMedium, nil)
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	req := &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}},
	}

	var finalErr error
	for _, e := range llm.GenerateContent(ctx, req, false) {
		if e != nil {
			finalErr = e
			break
		}
	}
	if finalErr == nil {
		t.Fatal("expected error on 429")
	}
	var rl interface{ RetryAfter() time.Duration }
	if !errors.As(finalErr, &rl) {
		t.Fatalf("error does not carry RetryAfter hint: %T: %v", finalErr, finalErr)
	}
	if got := rl.RetryAfter(); got != time.Millisecond {
		t.Errorf("RetryAfter = %v, want 1ms", got)
	}
	if !strings.Contains(strings.ToLower(finalErr.Error()), "rate limit") {
		t.Errorf("error message %q should mention rate limit", finalErr.Error())
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Errorf("expected SDK to retry 429 at least once, got %d calls", calls)
	}
}
