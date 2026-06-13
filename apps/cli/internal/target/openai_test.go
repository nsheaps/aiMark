package target

import (
	"strings"
	"testing"
)

const openAISSEFixture = `data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"content":" there"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15}}

data: [DONE]
`

func TestParseOpenAIStream(t *testing.T) {
	var tokens []string
	result, err := parseOpenAIStream(strings.NewReader(openAISSEFixture), func(ev TokenEvent) {
		if ev.At.IsZero() {
			t.Error("token event timestamp is zero")
		}
		tokens = append(tokens, ev.Content)
	})
	if err != nil {
		t.Fatalf("parseOpenAIStream: %v", err)
	}
	if result.Text != "Hello there!" {
		t.Errorf("Text = %q", result.Text)
	}
	// Empty leading delta must not count as a content token (TTFT correctness).
	if len(tokens) != 3 {
		t.Errorf("token events = %d, want 3 (%v)", len(tokens), tokens)
	}
	if tokens[0] != "Hello" {
		t.Errorf("first token = %q, want Hello", tokens[0])
	}
	if result.OutputTokens != 3 || result.InputTokens != 12 {
		t.Errorf("usage = in:%d out:%d, want in:12 out:3", result.InputTokens, result.OutputTokens)
	}
}

func TestParseOpenAIStreamNoUsageFallsBackToCount(t *testing.T) {
	fixture := `data: {"choices":[{"delta":{"content":"a"}}]}
data: {"choices":[{"delta":{"content":"b"}}]}
data: [DONE]
`
	result, err := parseOpenAIStream(strings.NewReader(fixture), nil)
	if err != nil {
		t.Fatalf("parseOpenAIStream: %v", err)
	}
	if result.OutputTokens != 2 {
		t.Errorf("OutputTokens = %d, want 2", result.OutputTokens)
	}
}

func TestParseOpenAIStreamError(t *testing.T) {
	fixture := `data: {"error":{"message":"model not found"}}
`
	if _, err := parseOpenAIStream(strings.NewReader(fixture), nil); err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("expected stream error, got %v", err)
	}
}

func TestParseOpenAIStreamEmpty(t *testing.T) {
	if _, err := parseOpenAIStream(strings.NewReader("data: [DONE]\n"), nil); err == nil {
		t.Fatal("expected error for stream with no content")
	}
}

func TestOpenAIEndpointJoining(t *testing.T) {
	withV1 := NewOpenAI("http://localhost:8000/v1", "m", "")
	if got := withV1.endpoint("/chat/completions"); got != "http://localhost:8000/v1/chat/completions" {
		t.Errorf("endpoint with /v1 = %q", got)
	}
	withoutV1 := NewOpenAI("http://localhost:8000/", "m", "")
	if got := withoutV1.endpoint("/chat/completions"); got != "http://localhost:8000/v1/chat/completions" {
		t.Errorf("endpoint without /v1 = %q", got)
	}
}
