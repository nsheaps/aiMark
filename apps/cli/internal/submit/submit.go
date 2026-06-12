// Package submit posts run envelopes to the aiMark API.
package submit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
)

// DefaultAPI is the production API base URL.
const DefaultAPI = "https://aimark.dev"

// ResolveAPI picks the API base URL: flag value, then $AIMARK_API, then the
// default.
func ResolveAPI(flag string) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("AIMARK_API"); env != "" {
		return env
	}
	return DefaultAPI
}

// Response is the API's answer to a successful run submission.
type Response struct {
	RunID      string `json:"run_id"`
	ClaimToken string `json:"claim_token"`
	PublicURL  string `json:"public_url"`
}

// ErrDuplicate is returned when the API already has this run (HTTP 409).
var ErrDuplicate = errors.New("run already submitted (HTTP 409)")

// ValidationError is returned when the API rejects the envelope (HTTP 422).
type ValidationError struct {
	Detail string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("the API rejected this run as invalid (HTTP 422): %s", e.Detail)
}

// Client submits runs to one API base URL.
type Client struct {
	API    string
	client *http.Client
}

// New builds a submit client for the given API base URL.
func New(api string) *Client {
	return &Client{API: strings.TrimRight(api, "/"), client: &http.Client{}}
}

// Submit POSTs the envelope to {api}/v1/runs.
func (c *Client) Submit(ctx context.Context, env schema.RunV1Json) (*Response, error) {
	payload, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("submit: marshal envelope: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.API+"/v1/runs", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("submit: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("submit: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close on read-only body

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		var out Response
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, fmt.Errorf("submit: parse API response: %w", err)
		}
		return &out, nil
	case http.StatusConflict:
		return nil, fmt.Errorf("%w: %s", ErrDuplicate, apiDetail(body))
	case http.StatusUnprocessableEntity:
		return nil, &ValidationError{Detail: apiDetail(body)}
	default:
		return nil, fmt.Errorf("submit: HTTP %d from %s/v1/runs: %s", resp.StatusCode, c.API, apiDetail(body))
	}
}

// apiDetail extracts a human-readable message from an API error body.
func apiDetail(body []byte) string {
	var parsed struct {
		Error   string `json:"error"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		for _, s := range []string{parsed.Detail, parsed.Message, parsed.Error} {
			if s != "" {
				return s
			}
		}
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "(no detail)"
	}
	if len(trimmed) > 300 {
		trimmed = trimmed[:300] + "..."
	}
	return trimmed
}
