package target

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Ollama is the native Ollama adapter: /api/chat streaming NDJSON plus
// /api/show and /api/version for model and runtime metadata.
type Ollama struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllama builds an Ollama adapter against the given base URL
// (default http://localhost:11434).
func NewOllama(baseURL, model string) *Ollama {
	return &Ollama{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		client:  &http.Client{},
	}
}

// Kind is always "local" — Ollama is a local runtime.
func (o *Ollama) Kind() string { return "local" }

// Describe gathers runtime version (/api/version), digest (/api/tags) and
// quantization / parameter count (/api/show). Every probe is best effort:
// metadata failures never fail a run.
func (o *Ollama) Describe(ctx context.Context) (TargetInfo, error) {
	info := TargetInfo{
		Kind:    "local",
		Runtime: "ollama",
		Model:   o.model,
	}

	// Runtime version.
	var version struct {
		Version string `json:"version"`
	}
	if err := o.getJSON(ctx, "/api/version", &version); err == nil {
		info.RuntimeVersion = version.Version
	}

	// Model digest from the local model list.
	var tags struct {
		Models []struct {
			Name   string `json:"name"`
			Model  string `json:"model"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	if err := o.getJSON(ctx, "/api/tags", &tags); err == nil {
		for _, m := range tags.Models {
			if m.Name == o.model || m.Model == o.model ||
				strings.TrimSuffix(m.Name, ":latest") == o.model {
				info.ModelDigest = m.Digest
				break
			}
		}
	}

	// Quantization and parameter count.
	var show struct {
		Details struct {
			QuantizationLevel string `json:"quantization_level"`
			ParameterSize     string `json:"parameter_size"`
		} `json:"details"`
	}
	if err := o.postJSON(ctx, "/api/show", map[string]string{"model": o.model}, &show); err == nil {
		info.Quantization = show.Details.QuantizationLevel
		info.DeclaredParamsB = parseParamsB(show.Details.ParameterSize)
	}

	return info, nil
}

// parseParamsB converts Ollama parameter sizes like "8.0B" or "70B" or
// "3.2M" into billions. Returns 0 when unknown.
func parseParamsB(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	case strings.HasSuffix(s, "M"):
		s = strings.TrimSuffix(s, "M")
		mult = 1e-3
	default:
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v * mult
}

func (o *Ollama) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+path, nil)
	if err != nil {
		return err
	}
	return o.doJSON(req, out)
}

func (o *Ollama) postJSON(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return o.doJSON(req, out)
}

func (o *Ollama) doJSON(req *http.Request, out any) error {
	resp, err := o.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close on read-only body
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama: HTTP %d from %s", resp.StatusCode, req.URL.Path)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

type ollamaChatChunk struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done            bool   `json:"done"`
	Error           string `json:"error"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
}

// Complete streams a chat completion from /api/chat (NDJSON, one JSON object
// per line).
func (o *Ollama) Complete(ctx context.Context, req CompletionRequest, onToken func(t TokenEvent)) (CompletionResult, error) {
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
		"model":    o.model,
		"messages": messages,
		"stream":   true,
		"options": map[string]any{
			"temperature": req.Temperature,
			"num_predict": req.MaxTokens,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return result, fmt.Errorf("ollama: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return result, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return result, fmt.Errorf("ollama: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close on read-only body

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return result, fmt.Errorf("ollama: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	return parseOllamaStream(resp.Body, onToken)
}

// parseOllamaStream consumes NDJSON chat chunks and invokes onToken per
// content token. Split out for fixture-level tests.
func parseOllamaStream(r io.Reader, onToken func(t TokenEvent)) (CompletionResult, error) {
	var result CompletionResult
	var text strings.Builder
	streamed := 0

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var chunk ollamaChatChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			return result, fmt.Errorf("ollama: parse stream chunk: %w", err)
		}
		if chunk.Error != "" {
			return result, fmt.Errorf("ollama: stream error: %s", chunk.Error)
		}
		if chunk.Message.Content != "" {
			streamed++
			text.WriteString(chunk.Message.Content)
			if onToken != nil {
				onToken(TokenEvent{Content: chunk.Message.Content, At: time.Now()})
			}
		}
		if chunk.Done {
			result.InputTokens = chunk.PromptEvalCount
			result.OutputTokens = chunk.EvalCount
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("ollama: read stream: %w", err)
	}

	result.Text = text.String()
	if result.OutputTokens == 0 {
		result.OutputTokens = streamed
	}
	if result.Text == "" {
		return result, fmt.Errorf("ollama: stream produced no content")
	}
	return result, nil
}
