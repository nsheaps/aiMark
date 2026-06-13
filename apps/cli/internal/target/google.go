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

// defaultGoogleBaseURL is the hosted Gemini API endpoint.
const defaultGoogleBaseURL = "https://generativelanguage.googleapis.com"

// Google is the native adapter for the Gemini API
// (:streamGenerateContent?alt=sse). Authentication uses the x-goog-api-key
// header so the key never appears in URLs or results.
type Google struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

// NewGoogle builds a Gemini adapter. An empty baseURL uses the hosted
// endpoint.
func NewGoogle(baseURL, model, apiKey string) *Google {
	if baseURL == "" {
		baseURL = defaultGoogleBaseURL
	}
	return &Google{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  apiKey,
		client:  &http.Client{},
	}
}

// Kind reports "hosted", or "local" when pointed at a loopback URL (tests,
// proxies).
func (g *Google) Kind() string {
	if isLoopbackURL(g.baseURL) {
		return "local"
	}
	return "hosted"
}

// Describe reports static metadata; Gemini exposes no weight metadata.
func (g *Google) Describe(_ context.Context) (TargetInfo, error) {
	info := TargetInfo{
		Kind:  g.Kind(),
		Model: g.model,
	}
	if info.Kind == "hosted" {
		info.Provider = "google"
	} else {
		info.Runtime = "google-compat"
	}
	return info, nil
}

// googleChunk is one streamed generateContent response fragment.
type googleChunk struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete streams one generateContent call over SSE. Each chunk's candidate
// text parts are the token events; usageMetadata carries token counts.
func (g *Google) Complete(ctx context.Context, req CompletionRequest, onToken func(t TokenEvent)) (CompletionResult, error) {
	var result CompletionResult

	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	body := map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]string{{"text": req.Prompt}}},
		},
		"generationConfig": map[string]any{
			"temperature":     req.Temperature,
			"maxOutputTokens": req.MaxTokens,
		},
	}
	if req.System != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []map[string]string{{"text": req.System}},
		}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return result, fmt.Errorf("google: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", g.baseURL, g.model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return result, fmt.Errorf("google: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if g.apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", g.apiKey)
	}

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return result, fmt.Errorf("google: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close on read-only body

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return result, fmt.Errorf("google: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	return parseGoogleStream(resp.Body, onToken)
}

// parseGoogleStream consumes Gemini SSE "data:" lines and invokes onToken
// for every text part. Split out for fixture-level tests.
func parseGoogleStream(r io.Reader, onToken func(t TokenEvent)) (CompletionResult, error) {
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
		var chunk googleChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return result, fmt.Errorf("google: parse stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return result, fmt.Errorf("google: stream error: %s", chunk.Error.Message)
		}
		if chunk.UsageMetadata != nil {
			result.InputTokens = chunk.UsageMetadata.PromptTokenCount
			if chunk.UsageMetadata.CandidatesTokenCount > 0 {
				result.OutputTokens = chunk.UsageMetadata.CandidatesTokenCount
			}
		}
		for _, candidate := range chunk.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.Text == "" {
					continue
				}
				streamed++
				text.WriteString(part.Text)
				if onToken != nil {
					onToken(TokenEvent{Content: part.Text, At: time.Now()})
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("google: read stream: %w", err)
	}

	result.Text = text.String()
	if result.OutputTokens == 0 {
		result.OutputTokens = streamed
	}
	if result.Text == "" {
		return result, fmt.Errorf("google: stream produced no content")
	}
	return result, nil
}
