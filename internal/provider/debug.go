package provider

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"net/http"
	"sort"
	"strings"
	"time"
)

// DebugEvent describes a low-level HTTP event emitted by provider transports.
type DebugEvent struct {
	Time    time.Time
	Kind    string // http_request, http_response, http_error
	Method  string
	URL     string
	Status  string
	Headers map[string]string
	Body    string
	Note    string
}

// DebugTracer receives provider HTTP debug events.
type DebugTracer struct {
	ch chan DebugEvent
}

// NewDebugTracer creates a buffered tracer suitable for interactive UIs.
func NewDebugTracer() *DebugTracer {
	return &DebugTracer{ch: make(chan DebugEvent, 256)}
}

// Emit sends an event non-blockingly.
func (t *DebugTracer) Emit(ev DebugEvent) {
	if t == nil {
		return
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	select {
	case t.ch <- ev:
	default:
	}
}

// Channel returns the event stream.
func (t *DebugTracer) Channel() <-chan DebugEvent {
	if t == nil {
		return nil
	}
	return t.ch
}

// debugTransport logs request/response summaries without consuming streaming bodies.
type debugTransport struct {
	base   http.RoundTripper
	tracer *DebugTracer
}

func (t *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.tracer != nil && req != nil {
		headers := sanitizeHeaders(req.Header)
		body, _ := snapshotRequestBody(req, 2000)
		t.tracer.Emit(DebugEvent{
			Kind:    "http_request",
			Method:  req.Method,
			URL:     req.URL.String(),
			Headers: headers,
			Body:    body,
			Note:    requestNote(req),
		})
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		if t.tracer != nil {
			t.tracer.Emit(DebugEvent{
				Kind:   "http_error",
				Method: req.Method,
				URL:    req.URL.String(),
				Note:   err.Error(),
			})
		}
		return nil, err
	}

	if t.tracer != nil && resp != nil {
		t.tracer.Emit(DebugEvent{
			Kind:    "http_response",
			Method:  req.Method,
			URL:     req.URL.String(),
			Status:  resp.Status,
			Headers: sanitizeHeaders(resp.Header),
			Note:    responseNote(resp),
		})
	}
	return resp, nil
}

func sanitizeHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, vals := range h {
		key := http.CanonicalHeaderKey(k)
		switch strings.ToLower(key) {
		case "authorization", "x-api-key", "api-key", "proxy-authorization":
			out[key] = "<redacted>"
		default:
			out[key] = strings.Join(vals, ", ")
		}
	}
	return out
}

func snapshotRequestBody(req *http.Request, limit int) (string, error) {
	if req == nil || req.Body == nil {
		return "", nil
	}
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return "", err
	}
	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	if len(bodyBytes) == 0 {
		return "", nil
	}
	if len(bodyBytes) > limit {
		return string(bodyBytes[:limit]) + "…", nil
	}
	return string(bodyBytes), nil
}

func requestNote(req *http.Request) string {
	if req == nil {
		return ""
	}
	accept := req.Header.Get("Accept")
	ct := req.Header.Get("Content-Type")
	parts := make([]string, 0, 2)
	if ct != "" {
		parts = append(parts, "content-type="+ct)
	}
	if accept != "" {
		parts = append(parts, "accept="+accept)
	}
	return strings.Join(parts, " ")
}

func responseNote(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	ct := resp.Header.Get("Content-Type")
	parts := []string{}
	if ct != "" {
		parts = append(parts, "content-type="+ct)
	}
	if strings.Contains(strings.ToLower(ct), "text/event-stream") {
		parts = append(parts, "streaming")
	}
	return strings.Join(parts, " ")
}

// FormatDebugEvent formats a debug event into a short summary + detail body.
func FormatDebugEvent(ev DebugEvent) (summary, detail string) {
	switch ev.Kind {
	case "http_request":
		summary = fmt.Sprintf("%s %s", ev.Method, ev.URL)
		detail = joinDebugDetail(ev.Headers, ev.Body, ev.Note)
	case "http_response":
		summary = fmt.Sprintf("%s %s → %s", ev.Method, ev.URL, ev.Status)
		detail = joinDebugDetail(ev.Headers, ev.Body, ev.Note)
	case "http_error":
		summary = fmt.Sprintf("%s %s", ev.Method, ev.URL)
		detail = ev.Note
	default:
		summary = ev.URL
		detail = joinDebugDetail(ev.Headers, ev.Body, ev.Note)
	}
	return summary, detail
}

func joinDebugDetail(headers map[string]string, body, note string) string {
	parts := make([]string, 0, 3)
	if len(headers) > 0 {
		parts = append(parts, formatHeaderMap(headers))
	}
	if note != "" {
		parts = append(parts, note)
	}
	if body != "" {
		parts = append(parts, body)
	}
	return strings.Join(parts, "\n")
}

func formatHeaderMap(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	copyMap := maps.Clone(headers)
	keys := make([]string, 0, len(copyMap))
	for k := range copyMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("headers:")
	for _, k := range keys {
		b.WriteString("\n")
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(copyMap[k])
	}
	return b.String()
}
