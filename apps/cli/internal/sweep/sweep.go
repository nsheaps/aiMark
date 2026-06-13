// Package sweep implements parameter sweeps: a YAML config declares target
// parameter defaults and a matrix of values; the cartesian product of the
// matrix yields cells, each of which is one full benchmark run sharing a
// sweep_id ULID.
package sweep

import (
	"fmt"
	"math"
	"os"
	"sort"

	"github.com/oklog/ulid/v2"
	"gopkg.in/yaml.v3"
)

// Config is the parsed sweep YAML:
//
//	target:
//	  num_ctx: 4096        # defaults applied to every cell
//	matrix:
//	  model: [llama3.1:8b, qwen2.5:7b]
//	  temperature: [0, 0.7]
type Config struct {
	// Target holds default target params applied to every cell.
	Target map[string]any `yaml:"target"`
	// Matrix maps param name to the list of values to sweep.
	Matrix map[string][]any `yaml:"matrix"`
}

// Load reads and validates a sweep YAML file.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("sweep: read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("sweep: parse %s: %w", path, err)
	}
	if len(cfg.Matrix) == 0 {
		return Config{}, fmt.Errorf("sweep: %s has no matrix — nothing to sweep", path)
	}
	for key, values := range cfg.Matrix {
		if len(values) == 0 {
			return Config{}, fmt.Errorf("sweep: matrix key %q has no values", key)
		}
	}
	return cfg, nil
}

// NewSweepID mints the shared sweep ULID.
func NewSweepID() string {
	return ulid.Make().String()
}

// Cells expands the matrix into its cartesian product. Matrix keys are
// iterated in sorted order (last key varies fastest) so cell order is
// deterministic. Target defaults are merged in first; matrix values win.
func (c Config) Cells() []map[string]any {
	keys := make([]string, 0, len(c.Matrix))
	for k := range c.Matrix {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	total := 1
	for _, k := range keys {
		total *= len(c.Matrix[k])
	}

	cells := make([]map[string]any, 0, total)
	indices := make([]int, len(keys))
	for i := 0; i < total; i++ {
		cell := map[string]any{}
		for k, v := range c.Target {
			cell[k] = v
		}
		for j, k := range keys {
			cell[k] = c.Matrix[k][indices[j]]
		}
		cells = append(cells, cell)

		// Increment the mixed-radix counter, last key fastest.
		for j := len(keys) - 1; j >= 0; j-- {
			indices[j]++
			if indices[j] < len(c.Matrix[keys[j]]) {
				break
			}
			indices[j] = 0
		}
	}
	return cells
}

// String coerces a cell value to its string form, for the model param.
func String(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// Float coerces a cell value to float64 (YAML decodes whole numbers as int).
func Float(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}

// Int coerces a cell value to int, rejecting fractional floats.
func Int(v any) (int, bool) {
	f, ok := Float(v)
	if !ok || f != math.Trunc(f) {
		return 0, false
	}
	return int(f), true
}
