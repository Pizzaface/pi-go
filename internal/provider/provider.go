package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/adk/model"
)

// BuildTransport creates an http.Transport with optional TLS skip and extra headers.
// Returns nil if no customization is needed.
func BuildTransport(opts *LLMOptions) http.RoundTripper {
	if opts == nil {
		return nil
	}
	hasHeaders := len(opts.ExtraHeaders) > 0
	if !opts.InsecureSkipTLS && !hasHeaders {
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

// LLMOptions holds optional configuration for LLM provider creation.
type LLMOptions struct {
	ExtraHeaders    map[string]string
	InsecureSkipTLS bool
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
	default:
		return nil, fmt.Errorf("unsupported provider family: %s", family)
	}
}
