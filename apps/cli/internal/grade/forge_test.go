package grade

import (
	"testing"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
	"github.com/nsheaps/aimark/apps/cli/internal/suites"
)

// forgeReferenceSolutions are known-good implementations for every forge-1
// task, proving each task's hidden tests are solvable.
var forgeReferenceSolutions = map[string]string{
	"slugify": "```js\n" +
		"function slugify(s) {\n" +
		"  return s.toLowerCase().replace(/[^a-z0-9]+/g, \"-\").replace(/^-+|-+$/g, \"\");\n" +
		"}\n```",
	"fizzbuzz": "```js\n" +
		"function fizzbuzz(n) {\n" +
		"  var r = [];\n" +
		"  for (var i = 1; i <= n; i++) {\n" +
		"    if (i % 15 === 0) r.push(\"FizzBuzz\");\n" +
		"    else if (i % 3 === 0) r.push(\"Fizz\");\n" +
		"    else if (i % 5 === 0) r.push(\"Buzz\");\n" +
		"    else r.push(i);\n" +
		"  }\n" +
		"  return r;\n" +
		"}\n```",
	"is-balanced": "```js\n" +
		"function isBalanced(s) {\n" +
		"  var pairs = { \")\": \"(\", \"]\": \"[\", \"}\": \"{\" };\n" +
		"  var stack = [];\n" +
		"  for (var i = 0; i < s.length; i++) {\n" +
		"    var ch = s[i];\n" +
		"    if (ch === \"(\" || ch === \"[\" || ch === \"{\") stack.push(ch);\n" +
		"    else if (pairs[ch]) {\n" +
		"      if (stack.pop() !== pairs[ch]) return false;\n" +
		"    }\n" +
		"  }\n" +
		"  return stack.length === 0;\n" +
		"}\n```",
	"word-freq": "```js\n" +
		"function wordFreq(s) {\n" +
		"  var words = s.toLowerCase().match(/[a-z]+/g) || [];\n" +
		"  var counts = {};\n" +
		"  for (var i = 0; i < words.length; i++) {\n" +
		"    counts[words[i]] = (counts.hasOwnProperty(words[i]) ? counts[words[i]] : 0) + 1;\n" +
		"  }\n" +
		"  return counts;\n" +
		"}\n```",
	"fib-sequence": "```js\n" +
		"function fib(n) {\n" +
		"  var r = [];\n" +
		"  for (var i = 0; i < n; i++) {\n" +
		"    if (i === 0) r.push(0);\n" +
		"    else if (i === 1) r.push(1);\n" +
		"    else r.push(r[i - 1] + r[i - 2]);\n" +
		"  }\n" +
		"  return r;\n" +
		"}\n```",
	"deep-get": "```js\n" +
		"function deepGet(obj, path, fallback) {\n" +
		"  var parts = path.split(\".\");\n" +
		"  var cur = obj;\n" +
		"  for (var i = 0; i < parts.length; i++) {\n" +
		"    if (cur === null || cur === undefined || !(parts[i] in Object(cur))) return fallback;\n" +
		"    cur = cur[parts[i]];\n" +
		"  }\n" +
		"  return cur;\n" +
		"}\n```",
}

// TestForgeReferenceSolutionsPass proves every embedded forge-1 task is
// solvable: a known-good reference solution must pass the task's hidden
// tests through the real grading path (fence extraction + sandbox).
func TestForgeReferenceSolutionsPass(t *testing.T) {
	suite, err := suites.Get("forge-1")
	if err != nil {
		t.Fatalf("forge-1 not embedded: %v", err)
	}
	if len(suite.Manifest.Tasks) != 6 {
		t.Fatalf("forge-1 has %d tasks, want 6", len(suite.Manifest.Tasks))
	}
	for _, task := range suite.Manifest.Tasks {
		solution, ok := forgeReferenceSolutions[task.Id]
		if !ok {
			t.Errorf("no reference solution for task %s", task.Id)
			continue
		}
		if task.Grading == nil || task.Grading.Kind != schema.SuiteManifestV1JsonTasksElemGradingKindJsTests {
			t.Errorf("task %s should use js_tests grading", task.Id)
			continue
		}
		res := Grade(task.Grading, solution)
		if !res.Passed {
			t.Errorf("task %s: reference solution failed its own tests: %s", task.Id, res.Detail)
		}
	}
}

// TestForgeTasksRejectStubs proves the hidden tests are not vacuous: a stub
// that defines the right name but returns garbage must fail.
func TestForgeTasksRejectStubs(t *testing.T) {
	stubs := map[string]string{
		"slugify":      "function slugify(s) { return s; }",
		"fizzbuzz":     "function fizzbuzz(n) { return []; }",
		"is-balanced":  "function isBalanced(s) { return true; }",
		"word-freq":    "function wordFreq(s) { return {}; }",
		"fib-sequence": "function fib(n) { return [0]; }",
		"deep-get":     "function deepGet(o, p, f) { return f; }",
	}
	suite, err := suites.Get("forge-1")
	if err != nil {
		t.Fatalf("forge-1 not embedded: %v", err)
	}
	for _, task := range suite.Manifest.Tasks {
		stub, ok := stubs[task.Id]
		if !ok {
			continue
		}
		if res := Grade(task.Grading, stub); res.Passed {
			t.Errorf("task %s: stub solution should fail the hidden tests", task.Id)
		}
	}
}
