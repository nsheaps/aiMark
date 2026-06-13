// Package grade implements the objective grading kinds defined by
// suite-manifest.v1: exact, numeric_tolerance, json_schema, contains_all,
// and js_tests (delegated to the goja sandbox).
package grade

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nsheaps/aimark/apps/cli/internal/sandbox"
	"github.com/nsheaps/aimark/apps/cli/internal/schema"
)

// jsTestTimeLimit bounds one sandboxed js_tests evaluation.
const jsTestTimeLimit = 2 * time.Second

// Result is one grading outcome.
type Result struct {
	Passed bool
	Detail string
}

// numberPattern matches the first signed decimal number in free text.
var numberPattern = regexp.MustCompile(`[-+]?(?:\d+(?:\.\d+)?|\.\d+)(?:[eE][-+]?\d+)?`)

// fencePattern matches a fenced code block and captures its body.
var fencePattern = regexp.MustCompile("(?s)```[a-zA-Z0-9_-]*[ \t]*\r?\n(.*?)```")

// Grade evaluates a model response against one task grading spec.
func Grade(g *schema.SuiteManifestV1JsonTasksElemGrading, response string) Result {
	if g == nil {
		return Result{Passed: false, Detail: "no grading spec"}
	}
	switch g.Kind {
	case schema.SuiteManifestV1JsonTasksElemGradingKindExact:
		return gradeExact(g, response)
	case schema.SuiteManifestV1JsonTasksElemGradingKindNumericTolerance:
		return gradeNumericTolerance(g, response)
	case schema.SuiteManifestV1JsonTasksElemGradingKindJsonSchema:
		return gradeJSONSchema(g, response)
	case schema.SuiteManifestV1JsonTasksElemGradingKindContainsAll:
		return gradeContainsAll(g, response)
	case schema.SuiteManifestV1JsonTasksElemGradingKindJsTests:
		return gradeJSTests(g, response)
	default:
		return Result{Passed: false, Detail: fmt.Sprintf("unknown grading kind %q", g.Kind)}
	}
}

// gradeExact passes when the trimmed response equals the expected string,
// case-insensitively.
func gradeExact(g *schema.SuiteManifestV1JsonTasksElemGrading, response string) Result {
	expected := strings.TrimSpace(expectedString(g.Expected))
	got := strings.TrimSpace(response)
	if strings.EqualFold(got, expected) {
		return Result{Passed: true, Detail: "exact match"}
	}
	return Result{Passed: false, Detail: fmt.Sprintf("expected %q, got %q", expected, truncate(got, 120))}
}

// gradeNumericTolerance parses the first number in the response and passes
// when it is within tolerance of expected.
func gradeNumericTolerance(g *schema.SuiteManifestV1JsonTasksElemGrading, response string) Result {
	expected, ok := expectedNumber(g.Expected)
	if !ok {
		return Result{Passed: false, Detail: "grading spec has no numeric expected value"}
	}
	match := numberPattern.FindString(response)
	if match == "" {
		return Result{Passed: false, Detail: "no number found in response"}
	}
	value, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return Result{Passed: false, Detail: fmt.Sprintf("unparseable number %q", match)}
	}
	tolerance := 0.0
	if g.Tolerance != nil {
		tolerance = *g.Tolerance
	}
	if math.Abs(value-expected) <= tolerance {
		return Result{Passed: true, Detail: fmt.Sprintf("got %v (expected %v ± %v)", value, expected, tolerance)}
	}
	return Result{Passed: false, Detail: fmt.Sprintf("got %v, expected %v ± %v", value, expected, tolerance)}
}

// gradeJSONSchema extracts the first JSON value from the response and
// validates it against the task's minimal JSON-schema subset.
func gradeJSONSchema(g *schema.SuiteManifestV1JsonTasksElemGrading, response string) Result {
	if len(g.Schema) == 0 {
		return Result{Passed: false, Detail: "grading spec has no schema"}
	}
	value, err := ExtractJSON(response)
	if err != nil {
		return Result{Passed: false, Detail: err.Error()}
	}
	if err := ValidateSchema(map[string]any(g.Schema), value, "$"); err != nil {
		return Result{Passed: false, Detail: err.Error()}
	}
	return Result{Passed: true, Detail: "JSON validates against schema"}
}

// gradeContainsAll passes when every required substring appears in the
// response, case-insensitively.
func gradeContainsAll(g *schema.SuiteManifestV1JsonTasksElemGrading, response string) Result {
	if len(g.RequiredSubstrings) == 0 {
		return Result{Passed: false, Detail: "grading spec has no required_substrings"}
	}
	haystack := strings.ToLower(response)
	var missing []string
	for _, sub := range g.RequiredSubstrings {
		if !strings.Contains(haystack, strings.ToLower(sub)) {
			missing = append(missing, sub)
		}
	}
	if len(missing) > 0 {
		return Result{Passed: false, Detail: fmt.Sprintf("missing required substrings: %s", strings.Join(missing, ", "))}
	}
	return Result{Passed: true, Detail: fmt.Sprintf("all %d required substrings present", len(g.RequiredSubstrings))}
}

// gradeJSTests extracts the model's code (first fenced block, else the whole
// response) and runs the task's hidden tests against it in the sandbox.
func gradeJSTests(g *schema.SuiteManifestV1JsonTasksElemGrading, response string) Result {
	if g.Tests == nil || strings.TrimSpace(*g.Tests) == "" {
		return Result{Passed: false, Detail: "grading spec has no tests"}
	}
	code := ExtractCode(response)
	res := sandbox.RunTests(code, *g.Tests, jsTestTimeLimit, 0)
	return Result{Passed: res.Passed, Detail: res.Detail}
}

// ExtractCode returns the body of the first fenced code block, or the whole
// response when no fence is present.
func ExtractCode(response string) string {
	if m := fencePattern.FindStringSubmatch(response); m != nil {
		return m[1]
	}
	return response
}

// ExtractJSON finds the first JSON object or array in free text — fenced
// code blocks are searched first — and decodes it.
func ExtractJSON(response string) (any, error) {
	candidates := []string{}
	for _, m := range fencePattern.FindAllStringSubmatch(response, -1) {
		candidates = append(candidates, m[1])
	}
	candidates = append(candidates, response)

	for _, text := range candidates {
		for i := 0; i < len(text); i++ {
			if text[i] != '{' && text[i] != '[' {
				continue
			}
			dec := json.NewDecoder(strings.NewReader(text[i:]))
			var value any
			if err := dec.Decode(&value); err == nil {
				return value, nil
			}
		}
	}
	return nil, fmt.Errorf("no JSON object or array found in response")
}

// ValidateSchema validates value against a minimal JSON-schema subset:
// type, required, properties, additionalProperties (boolean), and enum.
// Nested schemas under properties are validated recursively. path names the
// location for error messages ("$" at the root).
func ValidateSchema(s map[string]any, value any, path string) error {
	if enum, ok := s["enum"].([]any); ok {
		matched := false
		for _, allowed := range enum {
			if reflect.DeepEqual(normalizeNumber(allowed), normalizeNumber(value)) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s: value %v not in enum", path, value)
		}
	}

	if typ, ok := s["type"].(string); ok {
		if err := checkType(typ, value, path); err != nil {
			return err
		}
	}

	obj, isObj := value.(map[string]any)
	if !isObj {
		return nil
	}

	if required, ok := s["required"].([]any); ok {
		for _, r := range required {
			key, _ := r.(string)
			if _, present := obj[key]; !present {
				return fmt.Errorf("%s: missing required property %q", path, key)
			}
		}
	}

	properties, _ := s["properties"].(map[string]any)
	for key, sub := range properties {
		subSchema, ok := sub.(map[string]any)
		if !ok {
			continue
		}
		if v, present := obj[key]; present {
			if err := ValidateSchema(subSchema, v, path+"."+key); err != nil {
				return err
			}
		}
	}

	if ap, ok := s["additionalProperties"].(bool); ok && !ap {
		for key := range obj {
			if _, declared := properties[key]; !declared {
				return fmt.Errorf("%s: unexpected additional property %q", path, key)
			}
		}
	}

	return nil
}

// checkType validates a JSON-schema primitive type name against a decoded
// JSON value (encoding/json's any representation).
func checkType(typ string, value any, path string) error {
	ok := false
	switch typ {
	case "object":
		_, ok = value.(map[string]any)
	case "array":
		_, ok = value.([]any)
	case "string":
		_, ok = value.(string)
	case "number":
		_, ok = value.(float64)
	case "integer":
		if f, isNum := value.(float64); isNum {
			ok = f == math.Trunc(f)
		}
	case "boolean":
		_, ok = value.(bool)
	case "null":
		ok = value == nil
	default:
		return fmt.Errorf("%s: unsupported schema type %q", path, typ)
	}
	if !ok {
		return fmt.Errorf("%s: expected type %s, got %T", path, typ, value)
	}
	return nil
}

// normalizeNumber converts integer-typed values to float64 so enum
// comparisons match encoding/json's number representation.
func normalizeNumber(v any) any {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float32:
		return float64(n)
	}
	return v
}

// expectedString renders the grading "expected" field as a string.
func expectedString(expected any) string {
	switch v := expected.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// expectedNumber coerces the grading "expected" field to a float64.
func expectedNumber(expected any) (float64, bool) {
	switch v := expected.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	}
	return 0, false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
