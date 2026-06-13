// Package assets downloads, verifies, caches, and extracts the pinned
// benchmark assets (runtime archives and model GGUFs).
//
// Cache layout under the assets root (default $AIMARK_DATA_DIR/assets or
// ~/.local/share/aimark/assets):
//
//	<file>            — verified download (final name = URL basename)
//	<file>.part       — in-flight download (renamed on success)
//	<file>.ok         — verification marker: the hash the file verified against
//	<file>.sha256     — observed hash recorded for TOFU (null-sha) assets
//	runtime/<key>/    — extracted runtime archive
package assets

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Asset is one downloadable benchmark asset.
type Asset struct {
	// URL is the download location; the cached file name is its basename.
	URL string
	// Sha256 is the pinned hex digest; empty = unverified (trust-on-first-use).
	Sha256 string
	// SizeBytes is the expected size (0 = unknown; used for progress only).
	SizeBytes int64
}

// Name returns the asset's cache file name (the URL path basename).
func (a Asset) Name() (string, error) {
	u, err := url.Parse(a.URL)
	if err != nil {
		return "", fmt.Errorf("assets: parse url %q: %w", a.URL, err)
	}
	name := filepath.Base(u.Path)
	if name == "" || name == "." || name == "/" {
		return "", fmt.Errorf("assets: cannot derive file name from %q", a.URL)
	}
	return name, nil
}

// Manager caches assets under one root directory.
type Manager struct {
	// Dir is the assets root.
	Dir string
	// Log receives human-readable progress lines (nil = silent).
	Log func(format string, args ...any)
	// Client is the HTTP client (nil = http.DefaultClient).
	Client *http.Client
}

// DefaultDir resolves the assets root: $AIMARK_DATA_DIR/assets when set,
// otherwise ~/.local/share/aimark/assets.
func DefaultDir() (string, error) {
	if dataDir := os.Getenv("AIMARK_DATA_DIR"); dataDir != "" {
		return filepath.Join(dataDir, "assets"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("assets: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "aimark", "assets"), nil
}

// Open returns a Manager rooted at the default assets dir, creating it.
func Open(log func(format string, args ...any)) (*Manager, error) {
	dir, err := DefaultDir()
	if err != nil {
		return nil, err
	}
	return OpenAt(dir, log)
}

// OpenAt returns a Manager rooted at dir, creating it if needed.
func OpenAt(dir string, log func(format string, args ...any)) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("assets: create dir %s: %w", dir, err)
	}
	return &Manager{Dir: dir, Log: log}, nil
}

func (m *Manager) logf(format string, args ...any) {
	if m.Log != nil {
		m.Log(format, args...)
	}
}

func (m *Manager) client() *http.Client {
	if m.Client != nil {
		return m.Client
	}
	return http.DefaultClient
}

// Ensure makes the asset available locally and returns its path. Cached
// files that already carry a matching .ok verification marker are reused
// without rehashing; everything else is (re)downloaded and sha256-verified.
func (m *Manager) Ensure(asset Asset) (string, error) {
	name, err := asset.Name()
	if err != nil {
		return "", err
	}
	final := filepath.Join(m.Dir, name)

	if ok, _ := m.verifiedMarker(final, asset.Sha256); ok {
		m.logf("cached: %s", name)
		return final, nil
	}
	// File exists but no (matching) marker: rehash once before redownloading.
	if info, err := os.Stat(final); err == nil && info.Mode().IsRegular() {
		observed, err := fileSha256(final)
		if err == nil && asset.Sha256 != "" && observed == asset.Sha256 {
			if err := m.writeMarker(final, asset.Sha256); err != nil {
				return "", err
			}
			m.logf("cached (verified): %s", name)
			return final, nil
		}
		// Stale or mismatched cache entry — replace it.
		_ = os.Remove(final)
		_ = os.Remove(final + ".ok")
	}

	observed, err := m.download(asset, final)
	if err != nil {
		return "", err
	}

	if asset.Sha256 == "" {
		m.logf("WARNING: %s has no pinned sha256 — trusting first download (observed %s)", name, observed)
		if err := os.WriteFile(final+".sha256", []byte(observed+"\n"), 0o644); err != nil {
			return "", fmt.Errorf("assets: record observed hash: %w", err)
		}
		if err := m.writeMarker(final, observed); err != nil {
			return "", err
		}
		return final, nil
	}

	if observed != asset.Sha256 {
		_ = os.Remove(final)
		return "", fmt.Errorf("assets: sha256 mismatch for %s: expected %s, got %s (deleted — re-run to retry)",
			name, asset.Sha256, observed)
	}
	if err := m.writeMarker(final, asset.Sha256); err != nil {
		return "", err
	}
	return final, nil
}

// verifiedMarker reports whether path exists and its .ok marker records the
// expected hash (any recorded hash matches when expected is "").
func (m *Manager) verifiedMarker(path, expected string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		return false, nil //nolint:nilerr // missing file = not verified
	}
	raw, err := os.ReadFile(path + ".ok")
	if err != nil {
		return false, nil //nolint:nilerr // missing marker = not verified
	}
	recorded := strings.TrimSpace(string(raw))
	if expected == "" {
		return recorded != "", nil
	}
	return recorded == expected, nil
}

func (m *Manager) writeMarker(path, hash string) error {
	if err := os.WriteFile(path+".ok", []byte(hash+"\n"), 0o644); err != nil {
		return fmt.Errorf("assets: write verification marker: %w", err)
	}
	return nil
}

// download streams the asset to <final>.part with progress, renames it into
// place, and returns the observed sha256.
func (m *Manager) download(asset Asset, final string) (string, error) {
	name := filepath.Base(final)
	m.logf("downloading %s (%s)", name, humanBytes(asset.SizeBytes))

	resp, err := m.client().Get(asset.URL)
	if err != nil {
		return "", fmt.Errorf("assets: download %s: %w", asset.URL, err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close on read-only body
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("assets: download %s: HTTP %d", asset.URL, resp.StatusCode)
	}

	expectedSize := asset.SizeBytes
	if expectedSize == 0 && resp.ContentLength > 0 {
		expectedSize = resp.ContentLength
	}

	part := final + ".part"
	out, err := os.Create(part)
	if err != nil {
		return "", fmt.Errorf("assets: create %s: %w", part, err)
	}

	hasher := sha256.New()
	written := int64(0)
	lastPct := -5 // emit 0% first
	buf := make([]byte, 1<<20)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				_ = out.Close()
				_ = os.Remove(part)
				return "", fmt.Errorf("assets: write %s: %w", part, err)
			}
			hasher.Write(buf[:n])
			written += int64(n)
			if expectedSize > 0 {
				pct := int(written * 100 / expectedSize)
				if pct >= lastPct+5 {
					lastPct = pct - pct%5
					m.logf("  %s: %d%% (%s / %s)", name, pct, humanBytes(written), humanBytes(expectedSize))
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = out.Close()
			_ = os.Remove(part)
			return "", fmt.Errorf("assets: download %s: %w", asset.URL, readErr)
		}
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(part)
		return "", fmt.Errorf("assets: close %s: %w", part, err)
	}
	if err := os.Rename(part, final); err != nil {
		_ = os.Remove(part)
		return "", fmt.Errorf("assets: finalize %s: %w", final, err)
	}
	m.logf("  %s: done (%s)", name, humanBytes(written))
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// EnsureExtracted ensures the archive asset is downloaded, verified, and
// extracted under <root>/runtime/<key>/, returning the extraction dir.
func (m *Manager) EnsureExtracted(asset Asset, key string) (string, error) {
	dest := filepath.Join(m.Dir, "runtime", key)
	marker := dest + ".ok"
	if raw, err := os.ReadFile(marker); err == nil &&
		(asset.Sha256 == "" || strings.TrimSpace(string(raw)) == asset.Sha256) {
		if _, err := os.Stat(dest); err == nil {
			return dest, nil
		}
	}

	archive, err := m.Ensure(asset)
	if err != nil {
		return "", err
	}

	_ = os.RemoveAll(dest)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", fmt.Errorf("assets: create %s: %w", dest, err)
	}
	if err := Extract(archive, dest); err != nil {
		_ = os.RemoveAll(dest)
		return "", err
	}

	recorded := asset.Sha256
	if recorded == "" {
		recorded = "tofu"
	}
	if err := os.WriteFile(marker, []byte(recorded+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("assets: write extraction marker: %w", err)
	}
	return dest, nil
}

// Extract unpacks a .tar.gz/.tgz or .zip archive into dest, refusing paths
// that escape it.
func Extract(archive, dest string) error {
	switch {
	case strings.HasSuffix(archive, ".tar.gz"), strings.HasSuffix(archive, ".tgz"):
		return extractTarGz(archive, dest)
	case strings.HasSuffix(archive, ".zip"):
		return extractZip(archive, dest)
	default:
		return fmt.Errorf("assets: unsupported archive format: %s", filepath.Base(archive))
	}
}

// securePath joins name under dest, rejecting traversal outside dest.
func securePath(dest, name string) (string, error) {
	target := filepath.Join(dest, filepath.FromSlash(name))
	if rel, err := filepath.Rel(dest, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("assets: archive entry %q escapes extraction dir", name)
	}
	return target, nil
}

func extractTarGz(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("assets: open %s: %w", archive, err)
	}
	defer f.Close() //nolint:errcheck // read-only

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("assets: gunzip %s: %w", archive, err)
	}
	defer gz.Close() //nolint:errcheck // read-only

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("assets: read tar %s: %w", archive, err)
		}
		target, err := securePath(dest, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("assets: mkdir %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("assets: mkdir for %s: %w", target, err)
			}
			mode := os.FileMode(hdr.Mode) & 0o777 //nolint:gosec // tar modes fit in 32 bits
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return fmt.Errorf("assets: create %s: %w", target, err)
			}
			if _, err := io.Copy(out, tr); err != nil { //nolint:gosec // pinned, sha-verified archives
				_ = out.Close()
				return fmt.Errorf("assets: extract %s: %w", target, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("assets: close %s: %w", target, err)
			}
		case tar.TypeSymlink, tar.TypeLink:
			// Resolve links within the archive conservatively: skip absolute
			// or escaping targets, materialize the rest as symlinks.
			if filepath.IsAbs(hdr.Linkname) {
				continue
			}
			if _, err := securePath(filepath.Dir(target), hdr.Linkname); err != nil {
				continue
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("assets: symlink %s: %w", target, err)
			}
		default:
			// Skip fifos, devices, etc.
		}
	}
}

func extractZip(archive, dest string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("assets: open zip %s: %w", archive, err)
	}
	defer zr.Close() //nolint:errcheck // read-only

	for _, entry := range zr.File {
		target, err := securePath(dest, entry.Name)
		if err != nil {
			return err
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("assets: mkdir %s: %w", target, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("assets: mkdir for %s: %w", target, err)
		}
		mode := entry.Mode() & 0o777
		if mode == 0 {
			mode = 0o644
		}
		in, err := entry.Open()
		if err != nil {
			return fmt.Errorf("assets: open zip entry %s: %w", entry.Name, err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			_ = in.Close()
			return fmt.Errorf("assets: create %s: %w", target, err)
		}
		if _, err := io.Copy(out, in); err != nil { //nolint:gosec // pinned, sha-verified archives
			_ = in.Close()
			_ = out.Close()
			return fmt.Errorf("assets: extract %s: %w", target, err)
		}
		_ = in.Close()
		if err := out.Close(); err != nil {
			return fmt.Errorf("assets: close %s: %w", target, err)
		}
	}
	return nil
}

func fileSha256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck // read-only
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// humanBytes renders a byte count for humans.
func humanBytes(n int64) string {
	switch {
	case n <= 0:
		return "size unknown"
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	case n < 1<<30:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	}
}

// HumanBytes is the exported render helper used by the bench download plan.
func HumanBytes(n int64) string { return humanBytes(n) }
