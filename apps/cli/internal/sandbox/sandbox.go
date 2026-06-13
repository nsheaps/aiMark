// Package sandbox executes untrusted model-generated JavaScript for the
// Forge suite's js_tests grading kind.
//
// Each call runs in a fresh goja VM with no host access: the only bindings
// are a tiny assert(cond, msg) helper and a console.log that captures output
// for the grade detail. Execution is bounded by a wall-clock interrupt
// (vm.Interrupt fired from a time.AfterFunc).
//
// Operation budget: goja exposes no per-instruction hook, so opLimit is not
// enforced as an exact instruction count — the wall-clock time cap bounds
// runaway loops instead. The parameter is kept in the signature so a future
// goja step-limit (or a ticker-based approximation) can slot in without an
// API change.
package sandbox

import (
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// maxLogLines caps captured console.log output per run.
const maxLogLines = 100

// prelude defines assert() in pure JS and routes console.log through the
// capture hook. It is evaluated before the model code.
const prelude = `
function assert(cond, msg) {
	if (!cond) {
		throw new Error("assert failed" + (msg === undefined ? "" : ": " + msg));
	}
}
var console = {
	log: function () {
		var parts = [];
		for (var i = 0; i < arguments.length; i++) {
			parts.push(String(arguments[i]));
		}
		__aimark_log(parts.join(" "));
	}
};
`

// Result is the outcome of one sandboxed test run.
type Result struct {
	// Passed is true when the code and the tests both evaluated without
	// throwing.
	Passed bool
	// Detail is a short human-readable explanation (error message on failure).
	Detail string
	// Logs is the captured console.log output, capped at maxLogLines lines.
	Logs []string
}

// RunTests evaluates code, then tests, in one fresh VM. Any thrown error —
// syntax error, runtime error, failed assert — fails the run. timeLimit
// bounds total wall-clock execution; opLimit is currently advisory (see the
// package comment).
func RunTests(code, tests string, timeLimit time.Duration, opLimit int64) (result Result) {
	_ = opLimit // see package comment: time cap bounds execution

	defer func() {
		if r := recover(); r != nil {
			result.Passed = false
			result.Detail = fmt.Sprintf("sandbox panic: %v", r)
		}
	}()

	vm := goja.New()
	vm.SetMaxCallStackSize(2048)

	var logs []string
	if err := vm.Set("__aimark_log", func(line string) {
		if len(logs) < maxLogLines {
			logs = append(logs, line)
		}
	}); err != nil {
		return Result{Passed: false, Detail: fmt.Sprintf("sandbox setup: %v", err)}
	}

	if timeLimit <= 0 {
		timeLimit = 2 * time.Second
	}
	timer := time.AfterFunc(timeLimit, func() {
		vm.Interrupt("time limit exceeded")
	})
	defer timer.Stop()

	run := func(stage, src string) error {
		if _, err := vm.RunString(src); err != nil {
			return fmt.Errorf("%s: %s", stage, describeJSError(err))
		}
		return nil
	}

	if err := run("prelude", prelude); err != nil {
		return Result{Passed: false, Detail: err.Error(), Logs: logs}
	}
	if err := run("code", code); err != nil {
		return Result{Passed: false, Detail: err.Error(), Logs: logs}
	}
	if err := run("tests", tests); err != nil {
		return Result{Passed: false, Detail: err.Error(), Logs: logs}
	}
	return Result{Passed: true, Detail: "all tests passed", Logs: logs}
}

// describeJSError flattens a goja error into a single short line.
func describeJSError(err error) string {
	var msg string
	switch e := err.(type) {
	case *goja.InterruptedError:
		msg = fmt.Sprintf("interrupted: %v", e.Value())
	case *goja.Exception:
		msg = e.Value().String()
	default:
		msg = err.Error()
	}
	msg = strings.ReplaceAll(msg, "\n", " ")
	if len(msg) > 500 {
		msg = msg[:500] + "..."
	}
	return msg
}
