package target

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OpenAI is an adapter for any OpenAI-compatible /v1/chat/completions
// endpoint (vLLM, LM Studio, llama.cpp server, OpenRouter, OpenAI, ...).
type OpenAI struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

// NewOpenAI builds an OpenAI-compatible adapter. baseURL may or may not
// include the trailing /v1.
func NewOpenAI(baseURL, model, apiKey string) *OpenAI {
	return &OpenAI{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  apiKey,
		client:  &http.Client{},
	}
}

func (o *OpenAI) endpoint(path string) string {
	base := o.baseURL
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base + path
}

// Kind reports "local" for loopback/private hosts, "hosted" otherwise.
func (o *OpenAI) Kind() string {
	if isLoopbackURL(o.baseURL) {
		return "local"
	}
	return "hosted"
}

// providerFromHost maps well-known hosted endpoints to provider ids.
func (o *OpenAI) providerFromHost() string {
	u, err := url.Parse(o.baseURL)
	if err != nil {
		return "openai-compatible"
	}
	host := u.Hostname()
	switch {
	case strings.HasSuffix(host, "openai.com"):
		return "openai"
	case strings.HasSuffix(host, "openrouter.ai"):
		return "openrouter"
	default:
		return host
	}
}

// Describe probes GET /v1/models for the model id; everything else degrades
// gracefully because generic OpenAI-compatible servers expose no weight
// metadata.
func (o *OpenAI) Describe(ctx context.Context) (TargetInfo, error) {
	info := TargetInfo{
		Kind:  o.Kind(),
		Model: o.model,
	}
	if info.Kind == "local" {
		info.Runtime = "openai-compat"
	} else {
		info.Provider = o.providerFromHost()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.endpoint("/models"), nil)
	if err != nil {
		return info, nil
	}
	o.authorize(req)
	resp, err := o.client.Do(req)
	if err != nil {
		return info, nil // best effort
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close on read-only body
	if resp.StatusCode != http.StatusOK {
		return info, nil
	}
	var models struct {
		Data []struct {
			Id string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&models); err != nil {
		return info, nil
	}
	for _, m := range models.Data {
		if m.Id == o.model {
			info.Model = m.Id
			break
		}
	}
	return info, nil
}

func (o *OpenAI) authorize(req *http.Request) {
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete streams a chat completion over SSE. The first delta with content
// is the TTFT event.
func (o *OpenAI) Complete(ctx context.Context, req CompletionRequest, onToken func(t TokenEvent)) (CompletionResult, error) {
	var result CompletionResult

	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	messages := []map[string]string{}
	if req.System != "" {
		messages = append(messages, map[string]string{"role": "system", "content": req.System})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.Prompt})

	body := map[string]any{
		"model":       o.model,
		"messages":    messages,
		"stream":      true,
		"temperature": req.Temperature,
		"max_tokens":  req.MaxTokens,
		"stream_options": map[string]any{
			"include_usage": true,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return result, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint("/chat/completions"), bytes.NewReader(payload))
	if err != nil {
		return result, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	o.authorize(httpReq)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return result, fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close on read-only body

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return result, fmt.Errorf("openai: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	return parseOpenAIStream(resp.Body, onToken)
}

// parseOpenAIStream consumes SSE "data:" lines and invokes onToken for every
// delta that carries content. Split out for fixture-level tests.
func parseOpenAIStream(r io.Reader, onToken func(t TokenEvent)) (CompletionResult, error) {
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
		if data == "[DONE]" {
			break
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return result, fmt.Errorf("openai: parse stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return result, fmt.Errorf("openai: stream error: %s", chunk.Error.Message)
		}
		if chunk.Usage != nil {
			result.InputTokens = chunk.Usage.PromptTokens
			result.OutputTokens = chunk.Usage.CompletionTokens
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content == "" {
				continue
			}
			streamed++
			text.WriteString(choice.Delta.Content)
			if onToken != nil {
				onToken(TokenEvent{Content: choice.Delta.Content, At: time.Now()})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("openai: read stream: %w", err)
	}

	result.Text = text.String()
	if result.OutputTokens == 0 {
		result.OutputTokens = streamed
	}
	if result.Text == "" {
		return result, fmt.Errorf("openai: stream produced no content")
	}
	return result, nil
}
