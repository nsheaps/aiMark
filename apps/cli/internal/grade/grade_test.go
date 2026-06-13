package grade

import (
	"strings"
	"testing"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
)

func spec(kind schema.SuiteManifestV1JsonTasksElemGradingKind) *schema.SuiteManifestV1JsonTasksElemGrading {
	return &schema.SuiteManifestV1JsonTasksElemGrading{Kind: kind}
}

func TestGradeExact(t *testing.T) {
	g := spec(schema.SuiteManifestV1JsonTasksElemGradingKindExact)
	g.Expected = "Positive"

	cases := []struct {
		response string
		want     bool
	}{
		{"positive", true},
		{"  POSITIVE \n", true},
		{"Positive", true},
		{"positive.", false},
		{"the sentiment is positive", false},
	}
	for _, c := range cases {
		if got := Grade(g, c.response).Passed; got != c.want {
			t.Errorf("exact(%q) = %v, want %v", c.response, got, c.want)
		}
	}
}

func TestGradeExactNumericExpected(t *testing.T) {
	g := spec(schema.SuiteManifestV1JsonTasksElemGradingKindExact)
	g.Expected = float64(42)
	if !Grade(g, "42").Passed {
		t.Error("numeric expected should match its string form")
	}
}

func TestGradeNumericTolerance(t *testing.T) {
	tol := 0.001
	g := spec(schema.SuiteManifestV1JsonTasksElemGradingKindNumericTolerance)
	g.Expected = 5.1
	g.Tolerance = &tol

	cases := []struct {
		response string
		want     bool
	}{
		{"5.1", true},
		{"The change is $5.10.", true},
		{"5.1004", true},
		{"5.2", false},
		{"no numbers here", false},
		{"-5.1", false},
	}
	for _, c := range cases {
		res := Grade(g, c.response)
		if res.Passed != c.want {
			t.Errorf("numeric(%q) = %v (%s), want %v", c.response, res.Passed, res.Detail, c.want)
		}
	}
}

func TestGradeNumericToleranceNegativeAndExponent(t *testing.T) {
	tol := 0.01
	g := spec(schema.SuiteManifestV1JsonTasksElemGradingKindNumericTolerance)
	g.Expected = -3.5
	g.Tolerance = &tol
	if !Grade(g, "answer: -3.5 degrees").Passed {
		t.Error("negative number should parse")
	}
	g.Expected = 1500.0
	if !Grade(g, "1.5e3").Passed {
		t.Error("exponent notation should parse")
	}
}

func TestGradeContainsAll(t *testing.T) {
	g := spec(schema.SuiteManifestV1JsonTasksElemGradingKindContainsAll)
	g.RequiredSubstrings = []string{"HydraPeak", "BPA-free"}

	if !Grade(g, "Introducing hydrapeak — our bpa-FREE bottle!").Passed {
		t.Error("case-insensitive contains_all should pass")
	}
	res := Grade(g, "Introducing HydraPeak!")
	if res.Passed {
		t.Error("missing substring should fail")
	}
	if !strings.Contains(res.Detail, "BPA-free") {
		t.Errorf("detail should name the missing substring, got %q", res.Detail)
	}
}

func TestGradeJSONSchema(t *testing.T) {
	g := spec(schema.SuiteManifestV1JsonTasksElemGradingKindJsonSchema)
	g.Schema = schema.SuiteManifestV1JsonTasksElemGradingSchema{
		"type":     "object",
		"required": []any{"name", "age"},
		"properties": map[string]any{
			"name":   map[string]any{"type": "string"},
			"age":    map[string]any{"type": "number"},
			"status": map[string]any{"type": "string", "enum": []any{"active", "retired"}},
		},
		"additionalProperties": false,
	}

	cases := []struct {
		name     string
		response string
		want     bool
	}{
		{"plain", `{"name":"Ada","age":36}`, true},
		{"fenced", "Here you go:\n```json\n{\"name\":\"Ada\",\"age\":36,\"status\":\"active\"}\n```\n", true},
		{"prose prefix", `Sure! {"name":"Ada","age":36}`, true},
		{"missing required", `{"name":"Ada"}`, false},
		{"wrong type", `{"name":"Ada","age":"36"}`, false},
		{"extra key", `{"name":"Ada","age":36,"extra":1}`, false},
		{"bad enum", `{"name":"Ada","age":36,"status":"unknown"}`, false},
		{"no json", `I cannot answer that.`, false},
	}
	for _, c := range cases {
		res := Grade(g, c.response)
		if res.Passed != c.want {
			t.Errorf("%s: passed = %v (%s), want %v", c.name, res.Passed, res.Detail, c.want)
		}
	}
}

func TestGradeJSONSchemaArrayRoot(t *testing.T) {
	g := spec(schema.SuiteManifestV1JsonTasksElemGradingKindJsonSchema)
	g.Schema = schema.SuiteManifestV1JsonTasksElemGradingSchema{"type": "array"}
	if !Grade(g, `[1, 2, 3]`).Passed {
		t.Error("array root should validate")
	}
	if Grade(g, `{"a":1}`).Passed {
		t.Error("object should fail an array-typed schema")
	}
}

func TestGradeJSTests(t *testing.T) {
	tests := `assert(double(4) === 8, "double(4)"); assert(double(-2) === -4, "double(-2)");`
	g := spec(schema.SuiteManifestV1JsonTasksElemGradingKindJsTests)
	g.Tests = &tests

	pass := "Here is the function:\n```js\nfunction double(x) { return x * 2; }\n```\nHope that helps!"
	if res := Grade(g, pass); !res.Passed {
		t.Errorf("fenced solution should pass: %s", res.Detail)
	}

	bare := "function double(x) { return x * 2; }"
	if res := Grade(g, bare); !res.Passed {
		t.Errorf("unfenced solution should pass: %s", res.Detail)
	}

	wrong := "```js\nfunction double(x) { return x + 2; }\n```"
	if Grade(g, wrong).Passed {
		t.Error("wrong solution should fail")
	}
}

func TestExtractCode(t *testing.T) {
	fenced := "intro\n```javascript\nvar a = 1;\n```\noutro\n```\nvar b = 2;\n```"
	if got := ExtractCode(fenced); got != "var a = 1;\n" {
		t.Errorf("ExtractCode = %q, want first fenced block", got)
	}
	if got := ExtractCode("var c = 3;"); got != "var c = 3;" {
		t.Errorf("ExtractCode without fence = %q", got)
	}
}

func TestExtractJSONPrefersFencedBlock(t *testing.T) {
	response := "{broken json\n```json\n{\"ok\": true}\n```"
	v, err := ExtractJSON(response)
	if err != nil {
		t.Fatal(err)
	}
	obj, ok := v.(map[string]any)
	if !ok || obj["ok"] != true {
		t.Errorf("ExtractJSON = %v", v)
	}
}

func TestGradeUnknownAndNilSpecs(t *testing.T) {
	if Grade(nil, "x").Passed {
		t.Error("nil grading spec should fail")
	}
	g := spec(schema.SuiteManifestV1JsonTasksElemGradingKindContainsAll)
	if Grade(g, "x").Passed {
		t.Error("contains_all without substrings should fail")
	}
}
