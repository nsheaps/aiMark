package target

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// anthropicFixture is a canned Messages API SSE stream: message_start with
// input usage, two text deltas, message_delta with output usage, stop.
const anthropicFixture = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":17,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9}}

event: message_stop
data: {"type":"message_stop"}
`

func TestParseAnthropicStream(t *testing.T) {
	var tokens []string
	result, err := parseAnthropicStream(strings.NewReader(anthropicFixture), func(e TokenEvent) {
		tokens = append(tokens, e.Content)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Hello world" {
		t.Errorf("text = %q", result.Text)
	}
	if result.InputTokens != 17 {
		t.Errorf("input tokens = %d, want 17 (message_start usage)", result.InputTokens)
	}
	if result.OutputTokens != 9 {
		t.Errorf("output tokens = %d, want 9 (message_delta usage)", result.OutputTokens)
	}
	if len(tokens) != 2 || tokens[0] != "Hello" || tokens[1] != " world" {
		t.Errorf("token events = %v", tokens)
	}
}

func TestParseAnthropicStreamError(t *testing.T) {
	fixture := `event: error
data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}
`
	_, err := parseAnthropicStream(strings.NewReader(fixture), nil)
	if err == nil || !strings.Contains(err.Error(), "Overloaded") {
		t.Errorf("expected overloaded error, got %v", err)
	}
}

func TestParseAnthropicStreamEmpty(t *testing.T) {
	if _, err := parseAnthropicStream(strings.NewReader(""), nil); err == nil {
		t.Error("empty stream should error")
	}
}

func TestAnthropicKindAndDescribe(t *testing.T) {
	hosted := NewAnthropic("", "claude-x", "key")
	if hosted.Kind() != "hosted" {
		t.Errorf("default base Kind = %q, want hosted", hosted.Kind())
	}
	info, err := hosted.Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Provider != "anthropic" || info.Model != "claude-x" {
		t.Errorf("info = %+v", info)
	}

	local := NewAnthropic("http://127.0.0.1:9999", "claude-x", "key")
	if local.Kind() != "local" {
		t.Errorf("loopback Kind = %q, want local", local.Kind())
	}
}

func TestAnthropicCompleteHeadersAndBody(t *testing.T) {
	var gotHeaders http.Header
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		gotHeaders = r.Header.Clone()
		_ = jsonDecode(r, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicFixture)
	}))
	defer srv.Close()

	a := NewAnthropic(srv.URL, "claude-x", "secret-key")
	result, err := a.Complete(context.Background(), CompletionRequest{
		System:      "be brief",
		Prompt:      "hi",
		Temperature: 0,
		MaxTokens:   64,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Hello world" {
		t.Errorf("text = %q", result.Text)
	}
	if gotHeaders.Get("x-api-key") != "secret-key" {
		t.Error("x-api-key header missing")
	}
	if gotHeaders.Get("anthropic-version") == "" {
		t.Error("anthropic-version header missing")
	}
	if gotBody["system"] != "be brief" || gotBody["stream"] != true {
		t.Errorf("body = %v", gotBody)
	}
}

func TestAnthropicCompleteHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	a := NewAnthropic(srv.URL, "claude-x", "")
	_, err := a.Complete(context.Background(), CompletionRequest{Prompt: "hi", MaxTokens: 8}, nil)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected HTTP 401 error, got %v", err)
	}
}
