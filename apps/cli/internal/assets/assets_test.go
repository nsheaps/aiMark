package assets

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func newServer(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newManager(t *testing.T) (*Manager, *[]string) {
	t.Helper()
	var logs []string
	m, err := OpenAt(t.TempDir(), func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	})
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	return m, &logs
}

func TestEnsureHappyPath(t *testing.T) {
	payload := bytes.Repeat([]byte("aimark!"), 4096)
	srv := newServer(t, map[string][]byte{"model.gguf": payload})
	m, logs := newManager(t)

	asset := Asset{URL: srv.URL + "/model.gguf", Sha256: sha(payload), SizeBytes: int64(len(payload))}
	path, err := m.Ensure(asset)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("downloaded content mismatch (err=%v)", err)
	}
	// Verification marker recorded the expected hash.
	marker, err := os.ReadFile(path + ".ok")
	if err != nil || strings.TrimSpace(string(marker)) != asset.Sha256 {
		t.Fatalf("marker = %q, err=%v", marker, err)
	}
	// No leftover .part file.
	if _, err := os.Stat(path + ".part"); !os.IsNotExist(err) {
		t.Fatalf(".part file left behind: %v", err)
	}
	// Progress lines were emitted.
	joined := strings.Join(*logs, "\n")
	if !strings.Contains(joined, "downloading model.gguf") || !strings.Contains(joined, "%") {
		t.Fatalf("expected progress logs, got: %s", joined)
	}
}

func TestEnsureShaMismatchDeletes(t *testing.T) {
	payload := []byte("not what you expected")
	srv := newServer(t, map[string][]byte{"bad.bin": payload})
	m, _ := newManager(t)

	asset := Asset{URL: srv.URL + "/bad.bin", Sha256: strings.Repeat("0", 64)}
	_, err := m.Ensure(asset)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected sha256 mismatch error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.Dir, "bad.bin")); !os.IsNotExist(statErr) {
		t.Fatalf("mismatched file should be deleted: %v", statErr)
	}
}

func TestEnsureCacheHitSkipsDownload(t *testing.T) {
	payload := []byte("cache me")
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	m, logs := newManager(t)

	asset := Asset{URL: srv.URL + "/cached.bin", Sha256: sha(payload)}
	if _, err := m.Ensure(asset); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if hits != 1 {
		t.Fatalf("expected 1 download, got %d", hits)
	}

	*logs = nil
	path, err := m.Ensure(asset)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if hits != 1 {
		t.Fatalf("cache hit should not redownload (hits=%d)", hits)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, payload) {
		t.Fatal("cached content mismatch")
	}
	if !strings.Contains(strings.Join(*logs, "\n"), "cached") {
		t.Fatalf("expected cached log, got %v", *logs)
	}
}

func TestEnsureMarkerWithWrongHashRedownloads(t *testing.T) {
	payload := []byte("fresh bytes")
	srv := newServer(t, map[string][]byte{"f.bin": payload})
	m, _ := newManager(t)

	// Seed a stale file + marker recording a different hash.
	final := filepath.Join(m.Dir, "f.bin")
	if err := os.WriteFile(final, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(final+".ok", []byte(strings.Repeat("1", 64)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	asset := Asset{URL: srv.URL + "/f.bin", Sha256: sha(payload)}
	path, err := m.Ensure(asset)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, payload) {
		t.Fatal("stale file was not replaced")
	}
}

func TestEnsureTOFURecordsObservedHash(t *testing.T) {
	payload := []byte("unpinned asset")
	srv := newServer(t, map[string][]byte{"tofu.bin": payload})
	m, logs := newManager(t)

	path, err := m.Ensure(Asset{URL: srv.URL + "/tofu.bin"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	recorded, err := os.ReadFile(path + ".sha256")
	if err != nil || strings.TrimSpace(string(recorded)) != sha(payload) {
		t.Fatalf("observed hash not recorded: %q err=%v", recorded, err)
	}
	if !strings.Contains(strings.Join(*logs, "\n"), "WARNING") {
		t.Fatalf("expected TOFU warning, got %v", *logs)
	}
}

func makeTarGz(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeZip(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestEnsureExtractedTarGz(t *testing.T) {
	archive := makeTarGz(t, map[string][]byte{
		"llama-b9616/llama-server": []byte("#!/bin/sh\necho server\n"),
		"llama-b9616/LICENSE":      []byte("MIT"),
	})
	srv := newServer(t, map[string][]byte{"runtime.tar.gz": archive})
	m, _ := newManager(t)

	dir, err := m.EnsureExtracted(Asset{URL: srv.URL + "/runtime.tar.gz", Sha256: sha(archive)}, "b9616-linux-amd64-cpu")
	if err != nil {
		t.Fatalf("EnsureExtracted: %v", err)
	}
	server := filepath.Join(dir, "llama-b9616", "llama-server")
	info, err := os.Stat(server)
	if err != nil {
		t.Fatalf("server binary missing: %v", err)
	}
	if info.Mode()&0o100 == 0 {
		t.Fatalf("server binary not executable: %v", info.Mode())
	}

	// Second call is a no-op cache hit (extraction marker present).
	dir2, err := m.EnsureExtracted(Asset{URL: srv.URL + "/runtime.tar.gz", Sha256: sha(archive)}, "b9616-linux-amd64-cpu")
	if err != nil || dir2 != dir {
		t.Fatalf("cached extraction: dir=%s err=%v", dir2, err)
	}
}

func TestEnsureExtractedZip(t *testing.T) {
	archive := makeZip(t, map[string][]byte{
		"llama-server.exe": []byte("MZ fake exe"),
		"ggml.dll":         []byte("dll"),
	})
	srv := newServer(t, map[string][]byte{"runtime.zip": archive})
	m, _ := newManager(t)

	dir, err := m.EnsureExtracted(Asset{URL: srv.URL + "/runtime.zip", Sha256: sha(archive)}, "b9616-windows-amd64-cpu")
	if err != nil {
		t.Fatalf("EnsureExtracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "llama-server.exe")); err != nil {
		t.Fatalf("exe missing after zip extraction: %v", err)
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	archive := makeTarGz(t, map[string][]byte{"../evil": []byte("nope")})
	tmp := t.TempDir()
	path := filepath.Join(tmp, "evil.tar.gz")
	if err := os.WriteFile(path, archive, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Extract(path, filepath.Join(tmp, "out")); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
}

func TestAssetName(t *testing.T) {
	name, err := Asset{URL: "https://example.com/a/b/model.gguf?x=1"}.Name()
	if err != nil || name != "model.gguf" {
		t.Fatalf("Name = %q, err=%v", name, err)
	}
	if _, err := (Asset{URL: "https://example.com/"}).Name(); err == nil {
		t.Fatal("expected error for empty basename")
	}
}
