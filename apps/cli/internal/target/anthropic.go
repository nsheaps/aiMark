package target

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultAnthropicBaseURL is the hosted Anthropic API endpoint.
const defaultAnthropicBaseURL = "https://api.anthropic.com"

// anthropicVersion is the pinned Messages API version header.
const anthropicVersion = "2023-06-01"

// Anthropic is the native adapter for the Anthropic Messages API
// (SSE streaming). Authentication uses the x-api-key header.
type Anthropic struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

// NewAnthropic builds an Anthropic Messages adapter. An empty baseURL uses
// the hosted endpoint.
func NewAnthropic(baseURL, model, apiKey string) *Anthropic {
	if baseURL == "" {
		baseURL = defaultAnthropicBaseURL
	}
	return &Anthropic{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  apiKey,
		client:  &http.Client{},
	}
}

// Kind reports "hosted", or "local" when pointed at a loopback URL (tests,
// proxies).
func (a *Anthropic) Kind() string {
	if isLoopbackURL(a.baseURL) {
		return "local"
	}
	return "hosted"
}

// Describe reports static metadata; the Messages API exposes no weight
// metadata to probe.
func (a *Anthropic) Describe(_ context.Context) (TargetInfo, error) {
	info := TargetInfo{
		Kind:  a.Kind(),
		Model: a.model,
	}
	if info.Kind == "hosted" {
		info.Provider = "anthropic"
	} else {
		info.Runtime = "anthropic-compat"
	}
	return info, nil
}

// anthropicEvent is the union of the SSE data payloads we consume.
type anthropicEvent struct {
	Type    string `json:"type"`
	Message *struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
	Delta *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Complete streams one message. content_block_delta text deltas are the
// token events; usage arrives in message_start (input) and message_delta
// (output).
func (a *Anthropic) Complete(ctx context.Context, req CompletionRequest, onToken func(t TokenEvent)) (CompletionResult, error) {
	var result CompletionResult

	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	body := map[string]any{
		"model":       a.model,
		"max_tokens":  req.MaxTokens,
		"temperature": req.Temperature,
		"stream":      true,
		"messages": []map[string]string{
			{"role": "user", "content": req.Prompt},
		},
	}
	if req.System != "" {
		body["system"] = req.System
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return result, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return result, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	if a.apiKey != "" {
		httpReq.Header.Set("x-api-key", a.apiKey)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return result, fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close on read-only body

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return result, fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	return parseAnthropicStream(resp.Body, onToken)
}

// parseAnthropicStream consumes Messages SSE "data:" lines and invokes
// onToken for every content_block_delta text delta. Split out for
// fixture-level tests.
func parseAnthropicStream(r io.Reader, onToken func(t TokenEvent)) (CompletionResult, error) {
	var result CompletionResult
	var text strings.Builder
	streamed := 0

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		var event anthropicEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return result, fmt.Errorf("anthropic: parse stream event: %w", err)
		}
		switch event.Type {
		case "message_start":
			if event.Message != nil {
				result.InputTokens = event.Message.Usage.InputTokens
				if event.Message.Usage.OutputTokens > 0 {
					result.OutputTokens = event.Message.Usage.OutputTokens
				}
			}
		case "content_block_delta":
			if event.Delta != nil && event.Delta.Type == "text_delta" && event.Delta.Text != "" {
				streamed++
				text.WriteString(event.Delta.Text)
				if onToken != nil {
					onToken(TokenEvent{Content: event.Delta.Text, At: time.Now()})
				}
			}
		case "message_delta":
			if event.Usage != nil && event.Usage.OutputTokens > 0 {
				result.OutputTokens = event.Usage.OutputTokens
			}
		case "error":
			if event.Error != nil {
				return result, fmt.Errorf("anthropic: stream error: %s: %s", event.Error.Type, event.Error.Message)
			}
			return result, fmt.Errorf("anthropic: stream error")
		case "message_stop":
			// end of message; the loop drains any trailing lines
		}
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("anthropic: read stream: %w", err)
	}

	result.Text = text.String()
	if result.OutputTokens == 0 {
		result.OutputTokens = streamed
	}
	if result.Text == "" {
		return result, fmt.Errorf("anthropic: stream produced no content")
	}
	return result, nil
}
