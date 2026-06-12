// Package results manages the local results directory: run envelopes,
// raw samples, and submission receipts.
//
// Layout (per run, keyed by ULID):
//
//	<ulid>.aimark.json    — run.v1 envelope
//	<ulid>.samples.json   — samples.v1 artifact
//	<ulid>.submitted.json — API response after a successful submit
package results

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
	"github.com/oklog/ulid/v2"
)

// ULIDPattern matches the run.v1 run_id pattern (Crockford base32 ULID).
var ULIDPattern = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

// NewRunID mints a fresh ULID (48-bit ms timestamp + 80 random bits,
// Crockford base32).
func NewRunID() string {
	return ulid.Make().String()
}

// DefaultDir resolves the results directory: $AIMARK_DATA_DIR/results when
// set, otherwise ~/.local/share/aimark/results (same layout on every OS for
// simplicity).
func DefaultDir() (string, error) {
	if dataDir := os.Getenv("AIMARK_DATA_DIR"); dataDir != "" {
		return filepath.Join(dataDir, "results"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("results: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "aimark", "results"), nil
}

// Store reads and writes run artifacts in one results directory.
type Store struct {
	Dir string
}

// Open returns a Store rooted at the default results dir, creating it.
func Open() (*Store, error) {
	dir, err := DefaultDir()
	if err != nil {
		return nil, err
	}
	return OpenAt(dir)
}

// OpenAt returns a Store rooted at dir, creating it if needed.
func OpenAt(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("results: create dir %s: %w", dir, err)
	}
	return &Store{Dir: dir}, nil
}

func (s *Store) envelopePath(id string) string {
	return filepath.Join(s.Dir, id+".aimark.json")
}

func (s *Store) samplesPath(id string) string {
	return filepath.Join(s.Dir, id+".samples.json")
}

func (s *Store) submittedPath(id string) string {
	return filepath.Join(s.Dir, id+".submitted.json")
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("results: marshal %s: %w", filepath.Base(path), err)
	}
	raw = append(raw, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("results: write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("results: finalize %s: %w", filepath.Base(path), err)
	}
	return nil
}

// Save writes the envelope and samples for one run.
func (s *Store) Save(env schema.RunV1Json, samples schema.SamplesV1Json) error {
	if !ULIDPattern.MatchString(env.RunId) {
		return fmt.Errorf("results: invalid run id %q", env.RunId)
	}
	if err := writeJSON(s.envelopePath(env.RunId), env); err != nil {
		return err
	}
	return writeJSON(s.samplesPath(env.RunId), samples)
}

// LoadEnvelope reads one run envelope by id.
func (s *Store) LoadEnvelope(id string) (schema.RunV1Json, error) {
	var env schema.RunV1Json
	raw, err := os.ReadFile(s.envelopePath(id))
	if err != nil {
		return env, fmt.Errorf("results: load run %s: %w", id, err)
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return env, fmt.Errorf("results: parse run %s: %w", id, err)
	}
	return env, nil
}

// LoadSamples reads the raw samples artifact for one run.
func (s *Store) LoadSamples(id string) (schema.SamplesV1Json, error) {
	var samples schema.SamplesV1Json
	raw, err := os.ReadFile(s.samplesPath(id))
	if err != nil {
		return samples, fmt.Errorf("results: load samples %s: %w", id, err)
	}
	if err := json.Unmarshal(raw, &samples); err != nil {
		return samples, fmt.Errorf("results: parse samples %s: %w", id, err)
	}
	return samples, nil
}

// SubmitReceipt is the persisted API response for a submitted run.
type SubmitReceipt struct {
	RunID      string `json:"run_id"`
	ClaimToken string `json:"claim_token,omitempty"`
	PublicURL  string `json:"public_url,omitempty"`
}

// MarkSubmitted persists the submit receipt for a run.
func (s *Store) MarkSubmitted(id string, receipt SubmitReceipt) error {
	return writeJSON(s.submittedPath(id), receipt)
}

// Receipt returns the submit receipt if the run was submitted.
func (s *Store) Receipt(id string) (*SubmitReceipt, error) {
	raw, err := os.ReadFile(s.submittedPath(id))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("results: load receipt %s: %w", id, err)
	}
	var receipt SubmitReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return nil, fmt.Errorf("results: parse receipt %s: %w", id, err)
	}
	return &receipt, nil
}

// IsSubmitted reports whether a run has a submit receipt.
func (s *Store) IsSubmitted(id string) bool {
	_, err := os.Stat(s.submittedPath(id))
	return err == nil
}

// List returns all run ids in the store, oldest first (ULIDs sort by time).
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, fmt.Errorf("results: list %s: %w", s.Dir, err)
	}
	var ids []string
	for _, e := range entries {
		name := e.Name()
		id, ok := strings.CutSuffix(name, ".aimark.json")
		if !ok || !ULIDPattern.MatchString(id) {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// Pending returns ids of runs that have not been submitted yet.
func (s *Store) Pending() ([]string, error) {
	ids, err := s.List()
	if err != nil {
		return nil, err
	}
	var pending []string
	for _, id := range ids {
		if !s.IsSubmitted(id) {
			pending = append(pending, id)
		}
	}
	return pending, nil
}
