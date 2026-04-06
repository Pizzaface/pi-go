package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/dimetron/pi-go/internal/claudecli"
	"google.golang.org/adk/model"
)

// BuildTransport creates an http.Transport with optional TLS skip and extra headers.
// Returns nil if no customization is needed.
func BuildTransport(opts *LLMOptions) http.RoundTripper {
	if opts == nil {
		return nil
	}
	hasHeaders := len(opts.ExtraHeaders) > 0
	hasDebug := opts.DebugTracer != nil
	if !opts.InsecureSkipTLS && !hasHeaders && !hasDebug {
		return nil
	}

	base := http.DefaultTransport
	if opts.InsecureSkipTLS {
		if cloned, ok := http.DefaultTransport.(*http.Transport); ok {
			transport := cloned.Clone()
			if transport.TLSClientConfig == nil {
				transport.TLSClientConfig = &tls.Config{}
			}
			transport.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec // user-requested
			base = transport
		}
	}
	if hasDebug {
		base = &debugTransport{base: base, tracer: opts.DebugTracer}
	}
	if hasHeaders {
		base = &headerTransport{base: base, headers: opts.ExtraHeaders}
	}
	return base
}

// BuildHTTPClient creates an *http.Client with optional TLS skip, extra headers, and timeout.
// Returns a default client if no customization is needed.
func BuildHTTPClient(opts *LLMOptions, timeout time.Duration) *http.Client {
	transport := BuildTransport(opts)
	if transport == nil {
		return &http.Client{Timeout: timeout}
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

// headerTransport injects extra HTTP headers into every request.
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.base.RoundTrip(req)
}

// Info describes a provider family and resolved model target.
type Info struct {
	Provider string
	Family   string
	Model    string
	Ollama   bool // true when model is served by Ollama
}

// Resolve determines the provider from a model name using the built-in registry.
func Resolve(modelName string) (Info, error) {
	reg := NewRegistry()
	reg.AddBuiltins()
	return reg.Resolve(modelName, "")
}

// CheckOllama verifies that the Ollama server at baseURL is reachable.
// It first checks TCP connectivity on the port, then issues a GET to the root
// endpoint (Ollama returns "Ollama is running").
func CheckOllama(baseURL string) error {
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid Ollama URL %q: %w", baseURL, err)
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	// TCP port check.
	conn, err := net.DialTimeout("tcp", host, 3*time.Second)
	if err != nil {
		return fmt.Errorf("ollama not reachable at %s: %w", host, err)
	}
	conn.Close()

	// HTTP health check.
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseURL)
	if err != nil {
		return fmt.Errorf("ollama HTTP check failed at %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned status %d at %s", resp.StatusCode, baseURL)
	}
	return nil
}

// ModelEntry describes a single model available from a provider.
// It carries enough information for a TUI to render a selectable list.
type ModelEntry struct {
	ID              string    // canonical model identifier (e.g. "claude-sonnet-4-20250514")
	DisplayName     string    // human-friendly name; may equal ID when the API doesn't provide one
	Provider        string    // provider name (e.g. "anthropic", "openai")
	Created         time.Time // creation / release timestamp; zero if unknown
	MaxInputTokens  int64     // max context window size in tokens; 0 = unknown
	MaxOutputTokens int64     // max output tokens the model can generate; 0 = unknown
}

// LLMOptions holds optional configuration for LLM provider creation.
type LLMOptions struct {
	ExtraHeaders    map[string]string
	InsecureSkipTLS bool
	DebugTracer     *DebugTracer
}

// ListModels fetches available models from the provider described by info.
// It uses the same family-based dispatch as NewLLM. The returned slice is
// sorted by ID so that callers get deterministic output suitable for a
// scrollable TUI list.
func ListModels(ctx context.Context, info Info, apiKey, baseURL string, opts *LLMOptions) ([]ModelEntry, error) {
	if opts == nil {
		opts = &LLMOptions{}
	}
	family := info.Family
	if family == "" {
		family = info.Provider
	}
	switch family {
	case "ollama":
		return listOllamaModels(ctx, baseURL)
	case "gemini":
		return listGeminiModels(ctx, apiKey, baseURL, opts)
	case "openai":
		return listOpenAIModels(ctx, apiKey, baseURL, opts)
	case "anthropic":
		return listAnthropicModels(ctx, apiKey, baseURL, opts)
	case "claudecli":
		return listClaudeCLIModels()
	default:
		return nil, fmt.Errorf("listing models: unsupported provider family %q", family)
	}
}

// KnownContextWindow returns the max input token limit for well-known models.
// Returns 0 if the model is not in the built-in table.
// This avoids an API call at startup; the value is updated with the real limit
// when the user opens the model picker or switches models.
func KnownContextWindow(modelName string) int64 {
	lower := strings.ToLower(modelName)

	// Anthropic models.
	switch {
	case strings.Contains(lower, "claude-opus-4"),
		strings.Contains(lower, "claude-sonnet-4"),
		strings.Contains(lower, "claude-3-7"):
		return 200_000
	case strings.Contains(lower, "claude-3-5"):
		return 200_000
	case strings.Contains(lower, "claude-3"):
		return 200_000

	// OpenAI models.
	case strings.HasPrefix(lower, "gpt-5"):
		return 1_000_000
	case strings.HasPrefix(lower, "gpt-4o"):
		return 128_000
	case strings.HasPrefix(lower, "gpt-4-turbo"), strings.HasPrefix(lower, "gpt-4-1"):
		return 128_000
	case strings.HasPrefix(lower, "o4"), strings.HasPrefix(lower, "o3"), strings.HasPrefix(lower, "o1"):
		return 200_000

	// Google Gemini models.
	case strings.Contains(lower, "gemini-2.5"):
		return 1_048_576
	case strings.Contains(lower, "gemini-2.0"):
		return 1_048_576
	case strings.Contains(lower, "gemini-1.5-pro"):
		return 2_097_152
	case strings.Contains(lower, "gemini-1.5"):
		return 1_048_576
	}

	return 0
}

// NewLLM creates a model.LLM for the given provider info, API key, optional base URL, thinking level, and options.
func NewLLM(ctx context.Context, info Info, apiKey, baseURL, thinkingLevel string, opts *LLMOptions) (model.LLM, error) {
	if opts == nil {
		opts = &LLMOptions{}
	}
	family := info.Family
	if family == "" {
		family = info.Provider
	}
	switch family {
	case "ollama":
		return NewOllama(ctx, info.Model, baseURL, thinkingLevel, opts)
	case "gemini":
		return NewGemini(ctx, info.Provider, info.Model, apiKey, baseURL, opts)
	case "openai":
		return NewOpenAI(ctx, info.Model, apiKey, baseURL, opts)
	case "anthropic":
		return NewAnthropic(ctx, info.Model, apiKey, baseURL, thinkingLevel, opts)
	case "claudecli":
		return newClaudeCLI()
	default:
		return nil, fmt.Errorf("unsupported provider family: %s", family)
	}
}

// newClaudeCLI creates a Claude CLI provider using the SDK.
// The CLI binary is resolved from $CLAUDE_CLI_PATH or exec.LookPath.
func newClaudeCLI() (model.LLM, error) {
	binaryPath, err := claudecli.FindBinary()
	if err != nil {
		return nil, fmt.Errorf("claude CLI not found: %w (set CLAUDE_CLI_PATH or install claude)", err)
	}
	cwd, _ := os.Getwd()
	return claudecli.New(claudecli.Config{
		BinaryPath: binaryPath,
		WorkDir:    cwd,
	}), nil
}

// listClaudeCLIModels returns a static model list for the Claude CLI provider.
// The actual model is determined by the CLI's own configuration.
func listClaudeCLIModels() ([]ModelEntry, error) {
	return []ModelEntry{
		{
			ID:          "claude-cli",
			DisplayName: "Claude Code CLI",
			Provider:    "claudecli",
		},
	}, nil
}
