package target

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"net/http/httptest"
)

// jsonDecode is a tiny test helper shared by the hosted adapter tests.
func jsonDecode(r *http.Request, out any) error {
	return json.NewDecoder(r.Body).Decode(out)
}

// googleFixture is a canned Gemini streamGenerateContent?alt=sse stream:
// two text chunks, then a final chunk with usage metadata.
const googleFixture = `data: {"candidates":[{"content":{"parts":[{"text":"Bonjour"}],"role":"model"}}]}

data: {"candidates":[{"content":{"parts":[{"text":" le monde"}],"role":"model"}}]}

data: {"candidates":[{"content":{"parts":[],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":6,"totalTokenCount":17}}
`

func TestParseGoogleStream(t *testing.T) {
	var tokens []string
	result, err := parseGoogleStream(strings.NewReader(googleFixture), func(e TokenEvent) {
		tokens = append(tokens, e.Content)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Bonjour le monde" {
		t.Errorf("text = %q", result.Text)
	}
	if result.InputTokens != 11 {
		t.Errorf("input tokens = %d, want 11", result.InputTokens)
	}
	if result.OutputTokens != 6 {
		t.Errorf("output tokens = %d, want 6", result.OutputTokens)
	}
	if len(tokens) != 2 {
		t.Errorf("token events = %v", tokens)
	}
}

func TestParseGoogleStreamError(t *testing.T) {
	fixture := `data: {"error":{"code":429,"message":"quota exceeded"}}
`
	_, err := parseGoogleStream(strings.NewReader(fixture), nil)
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("expected quota error, got %v", err)
	}
}

func TestParseGoogleStreamEmpty(t *testing.T) {
	if _, err := parseGoogleStream(strings.NewReader(""), nil); err == nil {
		t.Error("empty stream should error")
	}
}

func TestGoogleKindAndDescribe(t *testing.T) {
	hosted := NewGoogle("", "gemini-x", "key")
	if hosted.Kind() != "hosted" {
		t.Errorf("default base Kind = %q, want hosted", hosted.Kind())
	}
	info, err := hosted.Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Provider != "google" || info.Model != "gemini-x" {
		t.Errorf("info = %+v", info)
	}

	local := NewGoogle("http://localhost:9999", "gemini-x", "key")
	if local.Kind() != "local" {
		t.Errorf("loopback Kind = %q, want local", local.Kind())
	}
}

func TestGoogleCompleteRequestShape(t *testing.T) {
	var gotPath, gotQuery, gotKey string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotKey = r.Header.Get("x-goog-api-key")
		_ = jsonDecode(r, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, googleFixture)
	}))
	defer srv.Close()

	g := NewGoogle(srv.URL, "gemini-x", "secret-key")
	result, err := g.Complete(context.Background(), CompletionRequest{
		System:      "be brief",
		Prompt:      "hello",
		Temperature: 0.5,
		MaxTokens:   128,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Bonjour le monde" {
		t.Errorf("text = %q", result.Text)
	}
	if gotPath != "/v1beta/models/gemini-x:streamGenerateContent" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery != "alt=sse" {
		t.Errorf("query = %q, want alt=sse", gotQuery)
	}
	if gotKey != "secret-key" {
		t.Error("x-goog-api-key header missing")
	}
	if gotBody["systemInstruction"] == nil || gotBody["generationConfig"] == nil {
		t.Errorf("body = %v", gotBody)
	}
}

func TestNewBedrockNotSupported(t *testing.T) {
	_, err := New("bedrock:anthropic.claude-3", Options{})
	if err == nil {
		t.Fatal("bedrock should not be supported yet")
	}
	for _, want := range []string{"SigV4", "bedrock-access-gateway", "openai:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("bedrock error should mention %q, got %q", want, err.Error())
		}
	}
}

func TestNewHostedAdapters(t *testing.T) {
	a, err := New("anthropic:claude-x", Options{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind() != "hosted" {
		t.Errorf("anthropic Kind = %q", a.Kind())
	}

	g, err := New("google:gemini-x", Options{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if g.Kind() != "hosted" {
		t.Errorf("google Kind = %q", g.Kind())
	}
}

func TestNewHostedAdaptersEnvKeys(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-anthropic")
	t.Setenv("GOOGLE_API_KEY", "env-google")

	a, err := New("anthropic:claude-x", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if a.(*Anthropic).apiKey != "env-anthropic" {
		t.Error("anthropic adapter should fall back to $ANTHROPIC_API_KEY")
	}

	g, err := New("google:gemini-x", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if g.(*Google).apiKey != "env-google" {
		t.Error("google adapter should fall back to $GOOGLE_API_KEY")
	}
}
