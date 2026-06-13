// Package target defines the adapter interface aiMark uses to talk to
// inference backends, plus a factory that parses target strings like
// "ollama:llama3.1:8b" and "openai:gpt-4o-mini".
package target

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// TargetInfo describes the backend a run executed against. None of these
// fields ever contain credentials.
type TargetInfo struct {
	// Kind is "local" or "hosted".
	Kind string
	// Runtime is the local runtime id (ollama, openai-compat, ...) — local kind only.
	Runtime string
	// RuntimeVersion is the runtime/server version where discoverable.
	RuntimeVersion string
	// Provider is the hosted provider id — hosted kind only.
	Provider string
	// Model is the model identifier as the target reports it.
	Model string
	// ModelDigest is the content digest of the weights where exposed.
	ModelDigest string
	// Quantization is e.g. "Q4_K_M" where exposed.
	Quantization string
	// DeclaredParamsB is the declared parameter count in billions (0 = unknown).
	DeclaredParamsB float64
}

// CompletionRequest is one benchmark request.
type CompletionRequest struct {
	System      string
	Prompt      string
	Temperature float64
	MaxTokens   int
	Timeout     time.Duration
}

// TokenEvent is one streamed content token. At carries Go's monotonic clock
// reading, so durations derived from it are immune to wall-clock jumps.
type TokenEvent struct {
	Content string
	At      time.Time
}

// CompletionResult summarizes one completed streaming request.
type CompletionResult struct {
	Text string
	// InputTokens is the prompt token count if the backend reports it (0 = unknown).
	InputTokens int
	// OutputTokens is the completion token count: backend-reported when
	// available, otherwise the number of streamed content events.
	OutputTokens int
}

// Target is the adapter interface. Implementations stream completions and
// invoke onToken once per content token with a monotonic timestamp.
type Target interface {
	Kind() string // "local" | "hosted"
	Describe(ctx context.Context) (TargetInfo, error)
	Complete(ctx context.Context, req CompletionRequest, onToken func(t TokenEvent)) (CompletionResult, error)
}

// Options configures the factory.
type Options struct {
	// BaseURL overrides the backend base URL (--target-url).
	BaseURL string
	// APIKey is the bearer token for OpenAI-compatible backends. It is used
	// only for requests and never stored in results.
	APIKey string
}

// Parse splits a target string into adapter and model: "ollama:llama3.1:8b"
// yields ("ollama", "llama3.1:8b").
func Parse(spec string) (adapter, model string, err error) {
	adapter, model, ok := strings.Cut(spec, ":")
	if !ok || adapter == "" || model == "" {
		return "", "", fmt.Errorf("target: invalid target %q (expected <adapter>:<model>, e.g. ollama:llama3.1:8b)", spec)
	}
	return adapter, model, nil
}

// New builds a Target from a target string like "ollama:<model>",
// "openai:<model>", "anthropic:<model>", or "google:<model>".
func New(spec string, opts Options) (Target, error) {
	adapter, model, err := Parse(spec)
	if err != nil {
		return nil, err
	}
	switch adapter {
	case "ollama":
		base := opts.BaseURL
		if base == "" {
			base = "http://localhost:11434"
		}
		return NewOllama(base, model), nil
	case "openai":
		if opts.BaseURL == "" {
			return nil, fmt.Errorf("target: openai:<model> requires --target-url (vLLM, LM Studio, llama.cpp, OpenRouter, OpenAI, ...)")
		}
		return NewOpenAI(opts.BaseURL, model, opts.APIKey), nil
	case "anthropic":
		key := opts.APIKey
		if key == "" {
			key = os.Getenv("ANTHROPIC_API_KEY")
		}
		return NewAnthropic(opts.BaseURL, model, key), nil
	case "google":
		key := opts.APIKey
		if key == "" {
			key = os.Getenv("GOOGLE_API_KEY")
		}
		return NewGoogle(opts.BaseURL, model, key), nil
	case "bedrock":
		return nil, fmt.Errorf("target: bedrock:<model> is not yet supported — Bedrock requires AWS SigV4 request signing. " +
			"Run a bedrock-access-gateway (OpenAI-compatible proxy) and use openai:<model> --target-url against it instead")
	default:
		return nil, fmt.Errorf("target: unknown adapter %q (supported: ollama, openai, anthropic, google)", adapter)
	}
}

// isLoopbackURL reports whether the URL host resolves syntactically to a
// loopback or local address — used to classify openai-compat targets as
// local vs hosted.
func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate()
	}
	return false
}
