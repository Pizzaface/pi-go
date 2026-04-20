package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/pizzaface/go-pi/internal/llmutil"
)

// openaiModel implements model.LLM for OpenAI-compatible APIs.
type openaiModel struct {
	modelName string
	client    openai.Client
	effort    EffortLevel
}

// NewOpenAI creates an OpenAI model.LLM.
// If baseURL is non-empty, it overrides the default API endpoint.
// effort controls reasoning_effort for o-series and compatible models.
func NewOpenAI(_ context.Context, modelName, apiKey, baseURL string, effort EffortLevel, llmOpts *LLMOptions) (model.LLM, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key is required")
	}
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		baseURL = normalizeOpenAIBaseURL(baseURL)
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if llmOpts != nil {
		for k, v := range llmOpts.ExtraHeaders {
			opts = append(opts, option.WithHeader(k, v))
		}
		if transport := BuildTransport(llmOpts); transport != nil {
			opts = append(opts, option.WithHTTPClient(&http.Client{Transport: transport}))
		}
	}
	client := openai.NewClient(opts...)
	return &openaiModel{modelName: modelName, client: client, effort: effort}, nil
}

func (m *openaiModel) Name() string { return m.modelName }

func normalizeOpenAIBaseURL(baseURL string) string {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	if strings.HasSuffix(u.Path, "/v1") || strings.Contains(u.Path, "/v1/") {
		return trimmed
	}
	path := strings.TrimRight(u.Path, "/")
	if path == "" {
		u.Path = "/v1"
		return u.String()
	}
	u.Path = path + "/v1"
	return u.String()
}

func (m *openaiModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		items, systemInstruction := oaiContentsToInputItems(req.Contents, req.Config)

		modelName := req.Model
		if modelName == "" {
			modelName = m.modelName
		}

		params := responses.ResponseNewParams{
			Model: shared.ResponsesModel(modelName),
			Input: responses.ResponseNewParamsInputUnion{
				OfInputItemList: items,
			},
			Store: param.NewOpt(false),
		}
		if systemInstruction != "" {
			params.Instructions = param.NewOpt(systemInstruction)
		}

		if re := m.effort.OpenAIReasoningEffort(); re != "" {
			params.Reasoning = shared.ReasoningParam{
				Effort: re,
			}
		}

		if req.Config != nil && len(req.Config.Tools) > 0 {
			params.Tools = oaiGenaiToolsToResponses(req.Config.Tools)
			params.ToolChoice = responses.ResponseNewParamsToolChoiceUnion{
				OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsAuto),
			}
		}

		if stream {
			oaiRunResponsesStreaming(ctx, &m.client, params, yield)
		} else {
			oaiRunResponsesNonStreaming(ctx, &m.client, params, yield)
		}
	}
}

// oaiContentsToInputItems converts genai.Content to Responses API input items.
func oaiContentsToInputItems(contents []*genai.Content, config *genai.GenerateContentConfig) (responses.ResponseInputParam, string) {
	var systemBuilder strings.Builder
	if config != nil && config.SystemInstruction != nil {
		for _, p := range config.SystemInstruction.Parts {
			if p != nil && p.Text != "" {
				systemBuilder.WriteString(p.Text)
				systemBuilder.WriteByte('\n')
			}
		}
	}
	systemInstruction := strings.TrimSpace(systemBuilder.String())

	functionResponses := make(map[string]*genai.FunctionResponse)
	for _, c := range contents {
		if c == nil || c.Parts == nil {
			continue
		}
		for _, p := range c.Parts {
			if p != nil && p.FunctionResponse != nil {
				functionResponses[p.FunctionResponse.ID] = p.FunctionResponse
			}
		}
	}

	var items responses.ResponseInputParam
	for _, content := range contents {
		if content == nil || strings.TrimSpace(content.Role) == "system" {
			continue
		}
		role := strings.TrimSpace(content.Role)
		var textParts []string
		var functionCalls []*genai.FunctionCall

		for _, part := range content.Parts {
			if part == nil {
				continue
			}
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			} else if part.FunctionCall != nil {
				functionCalls = append(functionCalls, part.FunctionCall)
			}
		}

		if len(functionCalls) > 0 && (role == "model" || role == "assistant") {
			if len(textParts) > 0 {
				items = append(items, responses.ResponseInputItemParamOfMessage(
					strings.Join(textParts, "\n"),
					responses.EasyInputMessageRoleAssistant,
				))
			}
			for _, fc := range functionCalls {
				argsJSON, _ := json.Marshal(fc.Args)
				items = append(items, responses.ResponseInputItemParamOfFunctionCall(
					string(argsJSON), fc.ID, fc.Name,
				))
				contentStr := "No response available for this function call."
				if fr := functionResponses[fc.ID]; fr != nil {
					contentStr = oaiFunctionResponseContent(fr.Response)
				}
				items = append(items, responses.ResponseInputItemParamOfFunctionCallOutput(fc.ID, contentStr))
			}
		} else if len(textParts) > 0 {
			text := strings.Join(textParts, "\n")
			msgRole := responses.EasyInputMessageRoleUser
			if role == "model" || role == "assistant" {
				msgRole = responses.EasyInputMessageRoleAssistant
			}
			items = append(items, responses.ResponseInputItemParamOfMessage(text, msgRole))
		}
	}
	return items, systemInstruction
}

func oaiFunctionResponseContent(resp any) string {
	if resp == nil {
		return ""
	}
	if s, ok := resp.(string); ok {
		return s
	}
	if m, ok := resp.(map[string]any); ok {
		if c, ok := m["content"].([]any); ok && len(c) > 0 {
			if item, ok := c[0].(map[string]any); ok {
				if t, ok := item["text"].(string); ok {
					return t
				}
			}
		}
		if r, ok := m["result"].(string); ok {
			return r
		}
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

// oaiGenaiToolsToResponses converts genai tools to Responses API tool params.
func oaiGenaiToolsToResponses(tools []*genai.Tool) []responses.ToolUnionParam {
	var out []responses.ToolUnionParam
	for _, t := range tools {
		if t == nil || t.FunctionDeclarations == nil {
			continue
		}
		for _, fd := range t.FunctionDeclarations {
			if fd == nil {
				continue
			}
			paramsMap := make(map[string]any)
			if fd.ParametersJsonSchema != nil {
				if m, ok := fd.ParametersJsonSchema.(map[string]any); ok {
					maps.Copy(paramsMap, m)
				}
			}
			if _, ok := paramsMap["type"]; !ok {
				paramsMap["type"] = "object"
			}
			if paramsMap["type"] == "object" {
				if _, ok := paramsMap["properties"]; !ok {
					paramsMap["properties"] = map[string]any{}
				}
			}
			tool := responses.ToolParamOfFunction(fd.Name, paramsMap, false)
			tool.OfFunction.Description = param.NewOpt(fd.Description)
			out = append(out, tool)
		}
	}
	return out
}

// oaiStatusToFinishReason maps a Responses API status to genai.FinishReason.
func oaiStatusToFinishReason(resp *responses.Response) genai.FinishReason {
	switch resp.Status {
	case responses.ResponseStatusIncomplete:
		switch resp.IncompleteDetails.Reason {
		case "max_output_tokens":
			return genai.FinishReasonMaxTokens
		case "content_filter":
			return genai.FinishReasonSafety
		default:
			return genai.FinishReasonOther
		}
	case responses.ResponseStatusFailed, responses.ResponseStatusCancelled:
		return genai.FinishReasonOther
	default:
		return genai.FinishReasonStop
	}
}

// parseToolIntent attempts to extract a tool invocation from free-form text
// that a model emitted instead of using the native tool_calls channel.
// Recognizes common shapes produced by LiteLLM / gpt-oss / qwen / etc:
//
//	{"function": "name", "arguments": {...}}
//	{"name":     "name", "arguments": {...}}
//	{"tool":     "name", "parameters": {...}}
//	{"function": {"name": "name", "arguments": {...}}}
//
// Arguments values may be a nested object or a stringified JSON object.
func parseToolIntent(text string) (name string, args map[string]any, ok bool) {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return "", nil, false
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return "", nil, false
	}
	if nested, ok := raw["function"].(map[string]any); ok {
		if n, a, found := extractToolFields(nested); found {
			return n, a, true
		}
	}
	return extractToolFields(raw)
}

// extractToolFields looks for a tool name and arguments among common field
// name variations a model might emit.
func extractToolFields(m map[string]any) (name string, args map[string]any, ok bool) {
	for _, k := range []string{"function", "name", "tool"} {
		if v, found := m[k].(string); found && v != "" {
			name = v
			break
		}
	}
	if name == "" {
		return "", nil, false
	}
	for _, k := range []string{"arguments", "parameters", "args", "input"} {
		v, found := m[k]
		if !found {
			continue
		}
		switch t := v.(type) {
		case map[string]any:
			args = t
		case string:
			_ = json.Unmarshal([]byte(t), &args)
		}
		if args != nil {
			break
		}
	}
	return name, args, true
}

// oaiResponseToLLMResponse converts a Responses API Response to model.LLMResponse.
func oaiResponseToLLMResponse(resp *responses.Response) *model.LLMResponse {
	var parts []*genai.Part

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			msg := item.AsMessage()
			for _, c := range msg.Content {
				if c.Type != "output_text" || c.Text == "" {
					continue
				}
				if name, args, ok := parseToolIntent(c.Text); ok {
					p := genai.NewPartFromFunctionCall(name, args)
					p.FunctionCall.ID = "fc_synth_" + uuid.NewString()
					parts = append(parts, p)
					continue
				}
				parts = append(parts, &genai.Part{Text: c.Text})
			}
		case "function_call":
			fc := item.AsFunctionCall()
			var args map[string]any
			if fc.Arguments != "" {
				_ = json.Unmarshal([]byte(fc.Arguments), &args)
			}
			p := genai.NewPartFromFunctionCall(fc.Name, args)
			p.FunctionCall.ID = fc.CallID
			parts = append(parts, p)
		}
	}

	var usage *genai.GenerateContentResponseUsageMetadata
	if resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 {
		usage = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(resp.Usage.InputTokens),
			CandidatesTokenCount: int32(resp.Usage.OutputTokens),
		}
	}

	return &model.LLMResponse{
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  oaiStatusToFinishReason(resp),
		UsageMetadata: usage,
		Content:       &genai.Content{Role: string(genai.RoleModel), Parts: parts},
	}
}

type oaiFunctionCallAcc struct {
	id   string
	name string
	args string
}

func oaiRunResponsesStreaming(ctx context.Context, client *openai.Client, params responses.ResponseNewParams, yield func(*model.LLMResponse, error) bool) {
	stream := client.Responses.NewStreaming(ctx, params)
	defer func() {
		_ = stream.Close()
	}()

	var finalResp *responses.Response
	var accumulatedText string
	var functionCalls []oaiFunctionCallAcc

	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.output_text.delta":
			if event.Delta != "" {
				accumulatedText += event.Delta
				if !yield(&model.LLMResponse{
					Partial:      true,
					TurnComplete: false,
					Content:      &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{{Text: event.Delta}}},
				}, nil) {
					return
				}
			}
		case "response.reasoning_text.delta":
			if event.Delta != "" {
				if !yield(&model.LLMResponse{
					Partial:      true,
					TurnComplete: false,
					Content:      &genai.Content{Role: "thinking", Parts: []*genai.Part{{Text: event.Delta}}},
				}, nil) {
					return
				}
			}
		case "response.function_call_arguments.done":
			functionCalls = append(functionCalls, oaiFunctionCallAcc{
				id:   event.ItemID,
				name: event.Name,
				args: event.Arguments,
			})
		case "response.completed":
			finalResp = &event.Response
		}
	}

	if err := stream.Err(); err != nil {
		if ctx.Err() == context.Canceled {
			return
		}
		if rl, ok := asRateLimitError(err); ok {
			yield(nil, rl)
			return
		}
		_ = yield(&model.LLMResponse{
			ErrorCode:    "STREAM_ERROR",
			ErrorMessage: llmutil.ResponseErrorText("STREAM_ERROR", err.Error()),
			TurnComplete: true,
			FinishReason: genai.FinishReasonOther,
			Content:      genai.NewContentFromText(llmutil.ResponseErrorDisplayText("STREAM_ERROR", err.Error()), genai.RoleModel),
		}, nil)
		return
	}

	if finalResp != nil {
		_ = yield(oaiResponseToLLMResponse(finalResp), nil)
		return
	}

	// Fallback for OpenAI-compatible providers that don't send response.completed.
	_ = yield(oaiBuildFallbackResponse(accumulatedText, functionCalls), nil)
}

// oaiBuildFallbackResponse constructs a final LLMResponse from state accumulated
// during streaming. Used when an OpenAI-compatible provider omits the
// response.completed event.
func oaiBuildFallbackResponse(text string, fcs []oaiFunctionCallAcc) *model.LLMResponse {
	parts := make([]*genai.Part, 0, 1+len(fcs))
	if text != "" {
		if name, args, ok := parseToolIntent(text); ok {
			p := genai.NewPartFromFunctionCall(name, args)
			p.FunctionCall.ID = "fc_synth_" + uuid.NewString()
			parts = append(parts, p)
		} else {
			parts = append(parts, &genai.Part{Text: text})
		}
	}
	for _, fc := range fcs {
		var args map[string]any
		if fc.args != "" {
			_ = json.Unmarshal([]byte(fc.args), &args)
		}
		p := genai.NewPartFromFunctionCall(fc.name, args)
		p.FunctionCall.ID = fc.id
		parts = append(parts, p)
	}
	return &model.LLMResponse{
		Partial:      false,
		TurnComplete: true,
		FinishReason: genai.FinishReasonStop,
		Content:      &genai.Content{Role: string(genai.RoleModel), Parts: parts},
	}
}

func oaiRunResponsesNonStreaming(ctx context.Context, client *openai.Client, params responses.ResponseNewParams, yield func(*model.LLMResponse, error) bool) {
	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		if rl, ok := asRateLimitError(err); ok {
			yield(nil, rl)
			return
		}
		yield(nil, fmt.Errorf("OpenAI response failed: %w", err))
		return
	}

	if resp.Status == responses.ResponseStatusFailed {
		errMsg := resp.Error.Message
		yield(&model.LLMResponse{
			ErrorCode:    "API_ERROR",
			ErrorMessage: llmutil.ResponseErrorText("API_ERROR", errMsg),
			TurnComplete: true,
			FinishReason: genai.FinishReasonOther,
			Content:      genai.NewContentFromText(llmutil.ResponseErrorDisplayText("API_ERROR", errMsg), genai.RoleModel),
		}, nil)
		return
	}

	yield(oaiResponseToLLMResponse(resp), nil)
}

// listOpenAIModels fetches available models from the OpenAI-compatible API
// and returns them as []ModelEntry.
func listOpenAIModels(ctx context.Context, apiKey, baseURL string, llmOpts *LLMOptions) ([]ModelEntry, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key is required to list models")
	}
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		baseURL = normalizeOpenAIBaseURL(baseURL)
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if llmOpts != nil {
		for k, v := range llmOpts.ExtraHeaders {
			opts = append(opts, option.WithHeader(k, v))
		}
		if transport := BuildTransport(llmOpts); transport != nil {
			opts = append(opts, option.WithHTTPClient(&http.Client{Transport: transport}))
		}
	}
	client := openai.NewClient(opts...)

	pager := client.Models.ListAutoPaging(ctx)
	var entries []ModelEntry
	for pager.Next() {
		m := pager.Current()
		created := time.Time{}
		if m.Created > 0 {
			created = time.Unix(m.Created, 0)
		}
		entries = append(entries, ModelEntry{
			ID:          m.ID,
			DisplayName: m.ID, // OpenAI API does not provide a separate display name
			Provider:    "openai",
			Created:     created,
		})
	}
	if err := pager.Err(); err != nil {
		return nil, fmt.Errorf("listing openai models: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}

// rateLimitError wraps a 429 Too Many Requests response and exposes the
// server-suggested delay from the Retry-After / Retry-After-Ms headers.
// It satisfies the retryAfterHint interface consumed by agent.WithRetry.
type rateLimitError struct {
	err        error
	status     int
	retryAfter time.Duration
	message    string
}

func (e *rateLimitError) Error() string {
	if e.retryAfter > 0 {
		return fmt.Sprintf("rate limited by OpenAI (HTTP %d): retry after %s: %s",
			e.status, e.retryAfter.Round(time.Millisecond), e.message)
	}
	return fmt.Sprintf("rate limited by OpenAI (HTTP %d): %s", e.status, e.message)
}

func (e *rateLimitError) Unwrap() error { return e.err }

// RetryAfter returns the server-suggested delay before retrying. Zero when the
// server did not send a Retry-After / Retry-After-Ms header.
func (e *rateLimitError) RetryAfter() time.Duration { return e.retryAfter }

// asRateLimitError inspects err for a 429 Too Many Requests response from the
// OpenAI SDK and, if found, returns a wrapped error that exposes Retry-After
// via RetryAfter().
func asRateLimitError(err error) (*rateLimitError, bool) {
	if err == nil {
		return nil, false
	}
	var oaiErr *openai.Error
	if !errors.As(err, &oaiErr) || oaiErr == nil || oaiErr.StatusCode != http.StatusTooManyRequests {
		return nil, false
	}
	message := strings.TrimSpace(oaiErr.Message)
	if message == "" {
		message = http.StatusText(oaiErr.StatusCode)
	}
	return &rateLimitError{
		err:        err,
		status:     oaiErr.StatusCode,
		retryAfter: parseOpenAIRetryAfter(oaiErr.Response),
		message:    message,
	}, true
}

// parseOpenAIRetryAfter extracts the server-suggested retry delay from the
// Retry-After-Ms (preferred) or Retry-After headers. Supports numeric seconds,
// numeric milliseconds, and HTTP-date values per RFC 7231.
func parseOpenAIRetryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	if v := strings.TrimSpace(resp.Header.Get("Retry-After-Ms")); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n > 0 {
			return time.Duration(n * float64(time.Millisecond))
		}
	}
	if v := strings.TrimSpace(resp.Header.Get("Retry-After")); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n > 0 {
			return time.Duration(n * float64(time.Second))
		}
		if t, err := http.ParseTime(v); err == nil {
			if d := time.Until(t); d > 0 {
				return d
			}
		}
	}
	return 0
}
