package sandbox

import (
	"strings"
	"testing"
	"time"
)

func TestRunTestsPass(t *testing.T) {
	res := RunTests(
		`function add(a, b) { return a + b; }`,
		`assert(add(2, 3) === 5, "add(2,3)"); assert(add(-1, 1) === 0, "add(-1,1)");`,
		2*time.Second, 0,
	)
	if !res.Passed {
		t.Fatalf("expected pass, got %+v", res)
	}
}

func TestRunTestsAssertFailure(t *testing.T) {
	res := RunTests(
		`function add(a, b) { return a - b; }`,
		`assert(add(2, 3) === 5, "add(2,3) should be 5");`,
		2*time.Second, 0,
	)
	if res.Passed {
		t.Fatal("expected failure")
	}
	if !strings.Contains(res.Detail, "add(2,3) should be 5") {
		t.Errorf("detail should carry the assert message, got %q", res.Detail)
	}
}

func TestRunTestsSyntaxError(t *testing.T) {
	res := RunTests(`function broken( {`, `assert(true);`, 2*time.Second, 0)
	if res.Passed {
		t.Fatal("expected syntax error failure")
	}
	if !strings.HasPrefix(res.Detail, "code:") {
		t.Errorf("detail should name the failing stage, got %q", res.Detail)
	}
}

func TestRunTestsRuntimeErrorInTests(t *testing.T) {
	res := RunTests(`var x = 1;`, `nope();`, 2*time.Second, 0)
	if res.Passed {
		t.Fatal("expected runtime error failure")
	}
	if !strings.HasPrefix(res.Detail, "tests:") {
		t.Errorf("detail should name the failing stage, got %q", res.Detail)
	}
}

func TestRunTestsInfiniteLoopInterrupted(t *testing.T) {
	start := time.Now()
	res := RunTests(`while (true) {}`, `assert(true);`, 100*time.Millisecond, 0)
	if res.Passed {
		t.Fatal("expected interrupt failure")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("interrupt took too long: %v", elapsed)
	}
	if !strings.Contains(res.Detail, "interrupted") {
		t.Errorf("detail should mention the interrupt, got %q", res.Detail)
	}
}

func TestRunTestsNoHostAccess(t *testing.T) {
	for _, src := range []string{
		`require("fs");`,
		`process.exit(1);`,
		`fetch("http://example.com");`,
	} {
		res := RunTests(src, `assert(true);`, time.Second, 0)
		if res.Passed {
			t.Errorf("host access %q should fail", src)
		}
	}
}

func TestRunTestsConsoleLogCaptured(t *testing.T) {
	res := RunTests(`console.log("hello", 42);`, `assert(true);`, time.Second, 0)
	if !res.Passed {
		t.Fatalf("expected pass, got %+v", res)
	}
	if len(res.Logs) != 1 || res.Logs[0] != "hello 42" {
		t.Errorf("logs = %v, want [hello 42]", res.Logs)
	}
}

func TestRunTestsLogCap(t *testing.T) {
	res := RunTests(`for (var i = 0; i < 1000; i++) console.log(i);`, `assert(true);`, 2*time.Second, 0)
	if len(res.Logs) > maxLogLines {
		t.Errorf("logs = %d lines, want <= %d", len(res.Logs), maxLogLines)
	}
}
