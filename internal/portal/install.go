package portal

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// downloadAndExtract downloads a tar.gz file from the given URL and extracts it to destDir.
func downloadAndExtract(url, destDir string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	gr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		// Security: prevent path traversal by sanitizing the entry name.
		// Reject absolute paths and entries with ".." in any path segment.
		cleanName := filepath.ToSlash(h.Name)
		// Strip leading "/" if present
		cleanName = strings.TrimPrefix(cleanName, "/")
		if cleanName == "" || strings.HasPrefix(cleanName, "/") {
			return fmt.Errorf("tar entry %q is not allowed: must be a safe relative path", h.Name)
		}
		// Check each path segment for ".." - reject if any segment is ".."
		for _, part := range strings.Split(cleanName, "/") {
			if part == ".." {
				return fmt.Errorf("tar entry %q is not allowed: path traversal detected", h.Name)
			}
		}

		target := filepath.Join(destDir, cleanName)

		// Ensure target is within destDir (defensive: resolve and check prefix)
		cleanTarget, err := filepath.Abs(target)
		if err != nil {
			return fmt.Errorf("failed to resolve target path: %w", err)
		}
		cleanDestDir, err := filepath.Abs(destDir)
		if err != nil {
			return fmt.Errorf("failed to resolve destDir: %w", err)
		}
		if !strings.HasPrefix(cleanTarget, cleanDestDir+string(filepath.Separator)) && cleanTarget != cleanDestDir {
			return fmt.Errorf("tar entry %q would escape install directory", h.Name)
		}

		// Security: mask file permissions to safe values.
		// For directories: 0o755 (rwxr-xr-x)
		// For files: 0o644 (rw-r--r--)
		switch h.Typeflag {
		case tar.TypeDir:
			mode := os.FileMode(h.Mode) & 0o755
			if mode == 0 {
				mode = 0o755
			}
			if err := os.MkdirAll(target, mode); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			mode := os.FileMode(h.Mode) & 0o644
			if mode == 0 {
				mode = 0o644
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, mode)
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			f.Close()
		}
	}

	return nil
}
