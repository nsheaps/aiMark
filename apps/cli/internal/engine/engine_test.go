package engine

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFreePort(t *testing.T) {
	port, err := FreePort()
	if err != nil {
		t.Fatalf("FreePort: %v", err)
	}
	if port < 1 || port > 65535 {
		t.Fatalf("port out of range: %d", port)
	}
	// The port must be bindable right after.
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("picked port not bindable: %v", err)
	}
	_ = l.Close()
}

func TestBuildArgs(t *testing.T) {
	args := BuildArgs(Options{
		ModelPath:  "/data/model.gguf",
		ModelAlias: "mainstream-model",
		CtxSize:    8192,
	}, 8123)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-m /data/model.gguf",
		"--host 127.0.0.1",
		"--port 8123",
		"-ngl 999",
		"--ctx-size 8192",
		"--alias mainstream-model",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %s", want, joined)
		}
	}

	// Zero ctx and empty alias are omitted.
	minimal := strings.Join(BuildArgs(Options{ModelPath: "m.gguf"}, 1), " ")
	if strings.Contains(minimal, "--ctx-size") || strings.Contains(minimal, "--alias") {
		t.Errorf("unexpected optional args: %s", minimal)
	}
}

// TestStartLifecycleWithStubServer exercises spawn → /health wait → BaseURL →
// Stop using a shell stub that serves one HTTP 200 via netcat-free Go-less
// trickery: a bash loop with /dev/tcp is too fragile, so the stub is a tiny
// python http server (python3 is available everywhere CI runs this).
func TestStartLifecycleWithStubServer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub script is unix-only")
	}
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		if _, err := os.Stat("/usr/local/bin/python3"); err != nil {
			t.Skip("python3 not available")
		}
	}

	dir := t.TempDir()
	stub := filepath.Join(dir, "stub-server")
	script := `#!/bin/sh
# Consume llama-server style args; serve /health on --port.
port=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--port" ]; then port="$a"; fi
  prev="$a"
done
exec python3 -c "
import http.server, sys
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200); self.end_headers(); self.wfile.write(b'ok')
    def log_message(self, *a): pass
http.server.HTTPServer(('127.0.0.1', int(sys.argv[1])), H).serve_forever()
" "$port"
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv, err := Start(ctx, Options{
		BinPath:        stub,
		ModelPath:      "fake.gguf",
		ModelAlias:     "stub",
		CtxSize:        4096,
		StartupTimeout: 20 * time.Second,
		LogFile:        filepath.Join(dir, "server.log"),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	if !strings.HasPrefix(srv.BaseURL(), "http://127.0.0.1:") || !strings.HasSuffix(srv.BaseURL(), "/v1") {
		t.Fatalf("unexpected BaseURL: %s", srv.BaseURL())
	}
	srv.Stop() // double-Stop must be safe
}

func TestStartFailsWhenBinaryExitsImmediately(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub script is unix-only")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "crash")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := Start(ctx, Options{BinPath: stub, ModelPath: "x.gguf", StartupTimeout: 5 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "exited during startup") {
		t.Fatalf("expected startup-exit error, got %v", err)
	}
}
