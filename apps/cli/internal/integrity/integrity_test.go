package integrity

import (
	"regexp"
	"testing"
	"time"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
)

func sampleEnvelope() schema.RunV1Json {
	return schema.RunV1Json{
		SchemaVersion: "aimark.run.v1",
		RunId:         "01HZZZZZZZZZZZZZZZZZZZZZZZ",
		CreatedAt:     time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
		Source:        schema.RunV1JsonSourceUser,
		Cli: schema.RunV1JsonCli{
			Version: "0.1.0",
			Os:      schema.RunV1JsonCliOsLinux,
			Arch:    schema.RunV1JsonCliArchAmd64,
		},
		Suite:  schema.RunV1JsonSuite{Id: "sprint", Version: 1},
		Target: schema.RunV1JsonTarget{Kind: schema.RunV1JsonTargetKindLocal, Model: "llama3.1:8b"},
		Metrics: schema.RunV1JsonMetrics{
			"decode_tps_mean": 123.456,
			"ttft_ms_p50":     42,
		},
	}
}

func TestCanonicalJSONStable(t *testing.T) {
	env := sampleEnvelope()
	a, err := CanonicalJSON(&env)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	b, err := CanonicalJSON(&env)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	if string(a) != string(b) {
		t.Fatal("CanonicalJSON not deterministic")
	}

	// Maps with different insertion orders must canonicalize identically.
	m1 := map[string]any{"b": 2, "a": 1, "c": map[string]any{"y": 2, "x": 1}}
	m2 := map[string]any{"c": map[string]any{"x": 1, "y": 2}, "a": 1, "b": 2}
	c1, err := CanonicalJSON(m1)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := CanonicalJSON(m2)
	if err != nil {
		t.Fatal(err)
	}
	if string(c1) != string(c2) {
		t.Fatalf("canonical forms differ: %s vs %s", c1, c2)
	}
	if string(c1) != `{"a":1,"b":2,"c":{"x":1,"y":2}}` {
		t.Fatalf("unexpected canonical form: %s", c1)
	}
}

func TestSignAndVerify(t *testing.T) {
	env := sampleEnvelope()
	if err := Sign(&env); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if env.Integrity == nil {
		t.Fatal("integrity block missing after Sign")
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(env.Integrity.PayloadSha256) {
		t.Errorf("payload_sha256 = %q", env.Integrity.PayloadSha256)
	}
	if env.Integrity.Hmac == nil || len(*env.Integrity.Hmac) != 64 {
		t.Error("hmac missing or wrong length")
	}
	if env.Integrity.KeyGen == nil || *env.Integrity.KeyGen != "dev" {
		t.Error("key_gen should default to dev")
	}
	if !regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`).MatchString(env.Integrity.Nonce) {
		t.Errorf("nonce = %q, want ULID", env.Integrity.Nonce)
	}

	ok, err := Verify(env)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("Verify = false for untampered envelope")
	}

	// Signing must be deterministic for identical payloads (nonce excluded
	// from the signed bytes).
	env2 := sampleEnvelope()
	if err := Sign(&env2); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if env.Integrity.PayloadSha256 != env2.Integrity.PayloadSha256 {
		t.Error("payload_sha256 differs for identical envelopes")
	}
	if *env.Integrity.Hmac != *env2.Integrity.Hmac {
		t.Error("hmac differs for identical envelopes")
	}

	// Tampering must be detected.
	env.Metrics["decode_tps_mean"] = 999999
	ok, err = Verify(env)
	if err != nil {
		t.Fatalf("Verify tampered: %v", err)
	}
	if ok {
		t.Fatal("Verify = true for tampered envelope")
	}
}
