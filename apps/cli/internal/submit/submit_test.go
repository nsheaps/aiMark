package submit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
)

func TestResolveAPI(t *testing.T) {
	t.Setenv("AIMARK_API", "")
	if got := ResolveAPI(""); got != DefaultAPI {
		t.Errorf("ResolveAPI default = %q", got)
	}
	t.Setenv("AIMARK_API", "https://staging.aimark.dev")
	if got := ResolveAPI(""); got != "https://staging.aimark.dev" {
		t.Errorf("ResolveAPI env = %q", got)
	}
	if got := ResolveAPI("https://flag.example"); got != "https://flag.example" {
		t.Errorf("ResolveAPI flag = %q", got)
	}
}

func TestSubmitStatuses(t *testing.T) {
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runs" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var env schema.RunV1Json
		if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
			t.Errorf("decode envelope: %v", err)
		}
		w.WriteHeader(status)
		switch status {
		case http.StatusCreated:
			_ = json.NewEncoder(w).Encode(Response{RunID: env.RunId, ClaimToken: "claim-123", PublicURL: "https://aimark.dev/r/abc"})
		case http.StatusConflict:
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "duplicate run"})
		case http.StatusUnprocessableEntity:
			_ = json.NewEncoder(w).Encode(map[string]string{"detail": "metrics.ttft_ms_p50 out of plausible range"})
		}
	}))
	defer srv.Close()

	client := New(srv.URL)
	env := schema.RunV1Json{RunId: "01HZZZZZZZZZZZZZZZZZZZZZZZ"}

	status = http.StatusCreated
	resp, err := client.Submit(context.Background(), env)
	if err != nil {
		t.Fatalf("Submit 201: %v", err)
	}
	if resp.ClaimToken != "claim-123" || resp.PublicURL == "" {
		t.Fatalf("Submit 201 response = %+v", resp)
	}

	status = http.StatusConflict
	_, err = client.Submit(context.Background(), env)
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("Submit 409 err = %v, want ErrDuplicate", err)
	}

	status = http.StatusUnprocessableEntity
	_, err = client.Submit(context.Background(), env)
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Submit 422 err = %v, want ValidationError", err)
	}
	if verr.Detail != "metrics.ttft_ms_p50 out of plausible range" {
		t.Fatalf("422 detail = %q", verr.Detail)
	}

	status = http.StatusInternalServerError
	if _, err = client.Submit(context.Background(), env); err == nil {
		t.Fatal("Submit 500 should error")
	}
}
