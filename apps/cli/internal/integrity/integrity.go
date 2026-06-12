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
var signingKey = "aimark-dev-integrity-key-not-secret"

// keyGen identifies the key generation; release builds override via ldflags.
var keyGen = "dev"

// KeyGen returns the active key generation identifier.
func KeyGen() string { return keyGen }

// CanonicalJSON produces a stable encoding of v: the struct is marshaled,
// re-decoded generically with json.Number (so numbers round-trip untouched),
// and re-marshaled — encoding/json emits map keys sorted, giving canonical
// bytes.
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
	canonical, err := json.Marshal(generic)
	if err != nil {
		return nil, fmt.Errorf("integrity: canonical marshal: %w", err)
	}
	return canonical, nil
}

// Sign computes the integrity block over the envelope (minus any existing
// integrity block) and attaches it.
func Sign(env *schema.RunV1Json) error {
	env.Integrity = nil
	payload, err := CanonicalJSON(env)
	if err != nil {
		return err
	}

	sum := sha256.Sum256(payload)
	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write(payload)

	hmacHex := hex.EncodeToString(mac.Sum(nil))
	gen := keyGen
	env.Integrity = &schema.RunV1JsonIntegrity{
		PayloadSha256: hex.EncodeToString(sum[:]),
		Hmac:          &hmacHex,
		KeyGen:        &gen,
		Nonce:         ulid.Make().String(),
	}
	return nil
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
