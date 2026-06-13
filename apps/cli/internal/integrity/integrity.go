// Package integrity fills the run envelope's integrity block: a SHA-256 of
// the canonical envelope JSON (integrity block excluded, keys sorted) plus an
// HMAC-SHA256 keyed by the CLI release key.
//
// Key handling: the constant below is the well-known DEV key (key_gen "dev").
// Release builds inject a rotated key and generation via ldflags:
//
//	go build -ldflags "-X .../internal/integrity.signingKey=<key> -X .../internal/integrity.keyGen=<gen>"
//
// The HMAC raises the bar for casual forgery; it is not a hard security
// boundary because the key ships inside the binary.
package integrity

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
	"github.com/oklog/ulid/v2"
)

// signingKey is the embedded dev key; release builds override via ldflags.
// MUST match services/api/src/canonical.ts DEV_KEY for key_gen "dev".
var signingKey = "aimark-dev-integrity-key-v0"

// keyGen identifies the key generation; release builds override via ldflags.
var keyGen = "dev"

// KeyGen returns the active key generation identifier.
func KeyGen() string { return keyGen }

// CanonicalJSON produces a stable encoding of v: the struct is marshaled,
// re-decoded generically with json.Number (so numbers round-trip untouched),
// and re-marshaled — encoding/json emits map keys sorted, giving canonical
// bytes. HTML escaping is disabled because the server's TS canonicalizer
// (JSON.stringify) does not escape <, >, & — both sides must emit identical
// bytes or the HMAC never matches.
func CanonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("integrity: marshal: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, fmt.Errorf("integrity: decode for canonicalization: %w", err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(generic); err != nil {
		return nil, fmt.Errorf("integrity: canonical marshal: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// block is one computed integrity block, shared by every signable envelope.
type block struct {
	PayloadSha256 string
	Hmac          string
	KeyGen        string
	Nonce         string
}

// computeBlock canonicalizes v (which must NOT contain an integrity block)
// and computes the payload hash, HMAC, and a fresh nonce.
func computeBlock(v any) (block, error) {
	payload, err := CanonicalJSON(v)
	if err != nil {
		return block{}, err
	}
	sum := sha256.Sum256(payload)
	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write(payload)
	return block{
		PayloadSha256: hex.EncodeToString(sum[:]),
		Hmac:          hex.EncodeToString(mac.Sum(nil)),
		KeyGen:        keyGen,
		Nonce:         ulid.Make().String(),
	}, nil
}

// Sign computes the integrity block over the envelope (minus any existing
// integrity block) and attaches it.
func Sign(env *schema.RunV1Json) error {
	env.Integrity = nil
	b, err := computeBlock(env)
	if err != nil {
		return err
	}
	env.Integrity = &schema.RunV1JsonIntegrity{
		PayloadSha256: b.PayloadSha256,
		Hmac:          &b.Hmac,
		KeyGen:        &b.KeyGen,
		Nonce:         b.Nonce,
	}
	return nil
}

// SignBenchmark computes the integrity block over the benchmark envelope
// (minus any existing integrity block) and attaches it.
func SignBenchmark(env *schema.BenchmarkV1Json) error {
	env.Integrity = nil
	b, err := computeBlock(env)
	if err != nil {
		return err
	}
	env.Integrity = &schema.BenchmarkV1JsonIntegrity{
		PayloadSha256: b.PayloadSha256,
		Hmac:          &b.Hmac,
		KeyGen:        &b.KeyGen,
		Nonce:         b.Nonce,
	}
	return nil
}

// VerifyBenchmark recomputes the benchmark integrity block and reports
// whether it matches.
func VerifyBenchmark(env schema.BenchmarkV1Json) (bool, error) {
	blk := env.Integrity
	if blk == nil {
		return false, fmt.Errorf("integrity: benchmark envelope has no integrity block")
	}
	env.Integrity = nil
	payload, err := CanonicalJSON(&env)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(payload)
	if hex.EncodeToString(sum[:]) != blk.PayloadSha256 {
		return false, nil
	}
	if blk.Hmac != nil {
		mac := hmac.New(sha256.New, []byte(signingKey))
		mac.Write(payload)
		if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(*blk.Hmac)) {
			return false, nil
		}
	}
	return true, nil
}

// Verify recomputes the integrity block and reports whether it matches.
func Verify(env schema.RunV1Json) (bool, error) {
	block := env.Integrity
	if block == nil {
		return false, fmt.Errorf("integrity: envelope has no integrity block")
	}
	env.Integrity = nil
	payload, err := CanonicalJSON(&env)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(payload)
	if hex.EncodeToString(sum[:]) != block.PayloadSha256 {
		return false, nil
	}
	if block.Hmac != nil {
		mac := hmac.New(sha256.New, []byte(signingKey))
		mac.Write(payload)
		if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(*block.Hmac)) {
			return false, nil
		}
	}
	return true, nil
}
