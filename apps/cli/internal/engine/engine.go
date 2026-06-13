// Package engine manages the pinned local inference server (llama-server):
// spawn with the class model, wait for /health, expose the OpenAI-compatible
// base URL, and tear the process group down between models.
package engine

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// Options configures one server instance.
type Options struct {
	// BinPath is the llama-server binary path.
	BinPath string
	// ModelPath is the GGUF file to load.
	ModelPath string
	// ModelAlias is reported by /v1/models and recorded in run envelopes.
	ModelAlias string
	// CtxSize is the context window (--ctx-size), from the class budget.
	CtxSize int
	// Port is the listen port; 0 picks a free one.
	Port int
	// StartupTimeout bounds the wait for /health (default 5 minutes — large
	// models load slowly from disk).
	StartupTimeout time.Duration
	// Log receives server lifecycle lines (nil = silent). Server stdout and
	// stderr go to LogFile when set, otherwise are discarded.
	Log func(format string, args ...any)
	// LogFile, when set, receives the server's combined output.
	LogFile string
}

// Server is a running llama-server process.
type Server struct {
	opts Options
	port int
	cmd  *exec.Cmd
	log  *os.File
}

// FreePort asks the kernel for an unused TCP port on 127.0.0.1.
func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("engine: pick free port: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		return 0, fmt.Errorf("engine: release probe port: %w", err)
	}
	return port, nil
}

// BuildArgs constructs the llama-server argument vector. -ngl 999 offloads
// every layer to the accelerator; it is harmless on CPU-only builds.
func BuildArgs(opts Options, port int) []string {
	args := []string{
		"-m", opts.ModelPath,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"-ngl", "999",
	}
	if opts.CtxSize > 0 {
		args = append(args, "--ctx-size", strconv.Itoa(opts.CtxSize))
	}
	if opts.ModelAlias != "" {
		args = append(args, "--alias", opts.ModelAlias)
	}
	return args
}

// Start spawns the server and waits until GET /health returns 200.
func Start(ctx context.Context, opts Options) (*Server, error) {
	port := opts.Port
	if port == 0 {
		p, err := FreePort()
		if err != nil {
			return nil, err
		}
		port = p
	}

	cmd := exec.CommandContext(ctx, opts.BinPath, BuildArgs(opts, port)...) //nolint:gosec // pinned, sha-verified binary
	setProcAttr(cmd)

	s := &Server{opts: opts, port: port, cmd: cmd}
	if opts.LogFile != "" {
		f, err := os.OpenFile(opts.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, fmt.Errorf("engine: open server log %s: %w", opts.LogFile, err)
		}
		s.log = f
		cmd.Stdout = f
		cmd.Stderr = f
	}

	if opts.Log != nil {
		opts.Log("starting %s (model %s, port %d, ctx %d)", opts.BinPath, opts.ModelAlias, port, opts.CtxSize)
	}
	if err := cmd.Start(); err != nil {
		s.closeLog()
		return nil, fmt.Errorf("engine: start %s: %w", opts.BinPath, err)
	}

	if err := s.waitHealthy(ctx); err != nil {
		s.Stop()
		return nil, err
	}
	if opts.Log != nil {
		opts.Log("server healthy at %s", s.BaseURL())
	}
	return s, nil
}

// BaseURL is the server's OpenAI-compatible API root.
func (s *Server) BaseURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/v1", s.port)
}

// Port returns the listen port.
func (s *Server) Port() int { return s.port }

// waitHealthy polls GET /health until 200, startup timeout, process exit, or
// context cancellation.
func (s *Server) waitHealthy(ctx context.Context) error {
	timeout := s.opts.StartupTimeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://127.0.0.1:%d/health", s.port)
	client := &http.Client{Timeout: 2 * time.Second}

	// Detect early process death without reaping the process (Stop reaps).
	exited := make(chan error, 1)
	go func() { exited <- s.cmd.Wait() }()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-exited:
			return fmt.Errorf("engine: server exited during startup: %v (see %s)", err, s.opts.LogFile)
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			ok := resp.StatusCode == http.StatusOK
			_ = resp.Body.Close()
			if ok {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("engine: server did not become healthy within %s", timeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Stop terminates the server's process group and releases resources.
func (s *Server) Stop() {
	if s.cmd != nil && s.cmd.Process != nil {
		killProcessGroup(s.cmd)
		// Give the process a moment to die; CommandContext kills on ctx end
		// anyway, and Wait was started in waitHealthy.
		time.Sleep(50 * time.Millisecond)
	}
	s.closeLog()
}

func (s *Server) closeLog() {
	if s.log != nil {
		_ = s.log.Close()
		s.log = nil
	}
}
