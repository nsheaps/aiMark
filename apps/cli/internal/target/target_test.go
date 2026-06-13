package target

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in             string
		adapter, model string
		wantErr        bool
	}{
		{in: "ollama:llama3.1:8b", adapter: "ollama", model: "llama3.1:8b"},
		{in: "openai:gpt-4o-mini", adapter: "openai", model: "gpt-4o-mini"},
		{in: "ollama:", wantErr: true},
		{in: ":model", wantErr: true},
		{in: "justamodel", wantErr: true},
	}
	for _, tc := range cases {
		adapter, model, err := Parse(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("Parse(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.in, err)
			continue
		}
		if adapter != tc.adapter || model != tc.model {
			t.Errorf("Parse(%q) = (%q, %q), want (%q, %q)", tc.in, adapter, model, tc.adapter, tc.model)
		}
	}
}

func TestNewFactory(t *testing.T) {
	tgt, err := New("ollama:llama3.1:8b", Options{})
	if err != nil {
		t.Fatalf("New ollama: %v", err)
	}
	if tgt.Kind() != "local" {
		t.Fatalf("ollama kind = %q, want local", tgt.Kind())
	}
	ol, ok := tgt.(*Ollama)
	if !ok {
		t.Fatalf("expected *Ollama, got %T", tgt)
	}
	if ol.baseURL != "http://localhost:11434" {
		t.Fatalf("default ollama base URL = %q", ol.baseURL)
	}

	if _, err := New("openai:gpt-4o-mini", Options{}); err == nil {
		t.Fatal("openai without --target-url should error")
	}

	hosted, err := New("openai:gpt-4o-mini", Options{BaseURL: "https://api.openai.com/v1"})
	if err != nil {
		t.Fatalf("New openai: %v", err)
	}
	if hosted.Kind() != "hosted" {
		t.Fatalf("api.openai.com kind = %q, want hosted", hosted.Kind())
	}

	local, err := New("openai:qwen2.5", Options{BaseURL: "http://localhost:8000"})
	if err != nil {
		t.Fatalf("New openai local: %v", err)
	}
	if local.Kind() != "local" {
		t.Fatalf("localhost kind = %q, want local", local.Kind())
	}

	if _, err := New("magic:model", Options{}); err == nil || !strings.Contains(err.Error(), "unknown adapter") {
		t.Fatalf("expected unknown adapter error, got %v", err)
	}
}

func TestParseParamsB(t *testing.T) {
	cases := map[string]float64{
		"8.0B":  8.0,
		"70B":   70,
		"137M":  0.137,
		"":      0,
		"weird": 0,
	}
	for in, want := range cases {
		if got := parseParamsB(in); got != want {
			t.Errorf("parseParamsB(%q) = %v, want %v", in, got, want)
		}
	}
}
