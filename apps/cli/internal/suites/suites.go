// Package suites exposes the benchmark suite manifests that are embedded
// into the binary at build time.
//
// The files under embedded/ are byte-for-byte copies of
// packages/suites/<id>-<version>/manifest.json, synced by the
// "codegen:suites" script in apps/cli/package.json. A test
// (TestEmbeddedManifestsMatchSource) fails when the copies drift.
package suites

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
)

//go:embed embedded/*.json
var embeddedFS embed.FS

// Suite is one embedded suite manifest plus its raw frozen bytes.
type Suite struct {
	// Key is the canonical "<id>-<version>" identifier, e.g. "sprint-1".
	Key string
	// Manifest is the parsed manifest.
	Manifest schema.SuiteManifestV1Json
	// Raw is the exact embedded manifest bytes (used for protocol_hash).
	Raw []byte
}

// ProtocolHash returns the SHA-256 hex digest of the frozen manifest bytes.
func (s Suite) ProtocolHash() string {
	sum := sha256.Sum256(s.Raw)
	return hex.EncodeToString(sum[:])
}

var (
	loadOnce sync.Once
	loaded   []Suite
	loadErr  error
)

func load() ([]Suite, error) {
	loadOnce.Do(func() {
		entries, err := embeddedFS.ReadDir("embedded")
		if err != nil {
			loadErr = fmt.Errorf("suites: read embedded dir: %w", err)
			return
		}
		for _, entry := range entries {
			raw, err := embeddedFS.ReadFile("embedded/" + entry.Name())
			if err != nil {
				loadErr = fmt.Errorf("suites: read %s: %w", entry.Name(), err)
				return
			}
			var manifest schema.SuiteManifestV1Json
			if err := json.Unmarshal(raw, &manifest); err != nil {
				loadErr = fmt.Errorf("suites: parse %s: %w", entry.Name(), err)
				return
			}
			key := fmt.Sprintf("%s-%d", manifest.Id, manifest.Version)
			if want := key + ".json"; entry.Name() != want {
				loadErr = fmt.Errorf("suites: embedded file %s should be named %s", entry.Name(), want)
				return
			}
			loaded = append(loaded, Suite{Key: key, Manifest: manifest, Raw: raw})
		}
		sort.Slice(loaded, func(i, j int) bool { return loaded[i].Key < loaded[j].Key })
	})
	return loaded, loadErr
}

// All returns every embedded suite, sorted by key.
func All() ([]Suite, error) {
	return load()
}

// Get resolves a suite by "<id>-<version>" key. A bare "<id>" is accepted
// when exactly one version of that suite is embedded.
func Get(key string) (Suite, error) {
	all, err := load()
	if err != nil {
		return Suite{}, err
	}
	var idMatches []Suite
	for _, s := range all {
		if s.Key == key {
			return s, nil
		}
		if s.Manifest.Id == key {
			idMatches = append(idMatches, s)
		}
	}
	if len(idMatches) == 1 {
		return idMatches[0], nil
	}
	if len(idMatches) > 1 {
		keys := make([]string, 0, len(idMatches))
		for _, s := range idMatches {
			keys = append(keys, s.Key)
		}
		return Suite{}, fmt.Errorf("suites: %q is ambiguous, use one of: %s", key, strings.Join(keys, ", "))
	}
	keys := make([]string, 0, len(all))
	for _, s := range all {
		keys = append(keys, s.Key)
	}
	return Suite{}, fmt.Errorf("suites: unknown suite %q (available: %s)", key, strings.Join(keys, ", "))
}
