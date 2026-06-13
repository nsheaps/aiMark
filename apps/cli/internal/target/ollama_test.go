package target

import (
	"strings"
	"testing"
)

const ollamaNDJSONFixture = `{"model":"llama3.1:8b","created_at":"2026-06-12T00:00:00Z","message":{"role":"assistant","content":"Hi"},"done":false}
{"model":"llama3.1:8b","created_at":"2026-06-12T00:00:00Z","message":{"role":"assistant","content":" friend"},"done":false}
{"model":"llama3.1:8b","created_at":"2026-06-12T00:00:01Z","message":{"role":"assistant","content":""},"done":true,"prompt_eval_count":9,"eval_count":2,"eval_duration":123456789}
`

func TestParseOllamaStream(t *testing.T) {
	var tokens []string
	result, err := parseOllamaStream(strings.NewReader(ollamaNDJSONFixture), func(ev TokenEvent) {
		if ev.At.IsZero() {
			t.Error("token event timestamp is zero")
		}
		tokens = append(tokens, ev.Content)
	})
	if err != nil {
		t.Fatalf("parseOllamaStream: %v", err)
	}
	if result.Text != "Hi friend" {
		t.Errorf("Text = %q", result.Text)
	}
	if len(tokens) != 2 {
		t.Errorf("token events = %d, want 2", len(tokens))
	}
	if result.InputTokens != 9 || result.OutputTokens != 2 {
		t.Errorf("usage = in:%d out:%d, want in:9 out:2", result.InputTokens, result.OutputTokens)
	}
}

func TestParseOllamaStreamError(t *testing.T) {
	fixture := `{"error":"model 'nope' not found"}
`
	if _, err := parseOllamaStream(strings.NewReader(fixture), nil); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected stream error, got %v", err)
	}
}

func TestParseOllamaStreamEmpty(t *testing.T) {
	fixture := `{"message":{"content":""},"done":true}
`
	if _, err := parseOllamaStream(strings.NewReader(fixture), nil); err == nil {
		t.Fatal("expected error for stream with no content")
	}
}
