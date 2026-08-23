package ingest

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// IsPackArchive reports whether path is a shareable opsgraph pack (.zip or .opsgraph).
func IsPackArchive(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".zip" || ext == ".opsgraph"
}

// MaterializeFixture returns a directory containing a fixture pack.
// Directories are returned as-is (cleanup is a no-op). Archives are extracted
// to a temp dir (ZipSlip-safe) that cleanup removes.
func MaterializeFixture(path string) (dir string, cleanup func(), err error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", func() {}, fmt.Errorf("fixture path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", func() {}, fmt.Errorf("fixture %q: %w", path, err)
	}
	if info.IsDir() {
		return path, func() {}, nil
	}
	if !IsPackArchive(path) && !looksLikeZipFile(path) {
		return "", func() {}, fmt.Errorf("fixture path %q is not a directory or pack archive (.zip/.opsgraph)", path)
	}
	tmp, err := os.MkdirTemp("", "opsgraph-pack-*")
	if err != nil {
		return "", func() {}, err
	}
	if err := UnzipPack(path, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", func() {}, err
	}
	return tmp, func() { _ = os.RemoveAll(tmp) }, nil
}

func looksLikeZipFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var hdr [4]byte
	n, _ := io.ReadFull(f, hdr[:])
	return n >= 2 && hdr[0] == 'P' && hdr[1] == 'K'
}

// ZipPack writes srcDir into destZip using sorted forward-slash names and a
// fixed mtime so the archive hashes identically on every OS.
func ZipPack(srcDir, destZip string) error {
	var names []string
	errWalk := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if name == "." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("refusing to zip %q", rel)
		}
		names = append(names, name)
		return nil
	})
	if errWalk != nil {
		return errWalk
	}
	sort.Strings(names)

	out, err := os.Create(destZip)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	fixed := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, name := range names {
		hdr := &zip.FileHeader{
			Name:           name,
			Method:         zip.Deflate,
			Modified:       fixed,
			CreatorVersion: 20, // MS-DOS host; must not be GOOS-dependent
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			_ = zw.Close()
			_ = out.Close()
			return err
		}
		f, err := os.Open(filepath.Join(srcDir, filepath.FromSlash(name)))
		if err != nil {
			_ = zw.Close()
			_ = out.Close()
			return err
		}
		_, copyErr := io.Copy(w, f)
		closeErr := f.Close()
		if copyErr != nil {
			_ = zw.Close()
			_ = out.Close()
			return copyErr
		}
		if closeErr != nil {
			_ = zw.Close()
			_ = out.Close()
			return closeErr
		}
	}
	closeErr := zw.Close()
	outErr := out.Close()
	if closeErr != nil {
		return closeErr
	}
	return outErr
}

// UnzipPack extracts a pack archive into dest (must exist). Paths containing
// ".." or escaping dest are rejected.
func UnzipPack(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("open pack %q: %w", src, err)
	}
	defer r.Close()
	dest = filepath.Clean(dest)
	for _, f := range r.File {
		if err := extractZipFile(f, dest); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, dest string) error {
	name := strings.ReplaceAll(f.Name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return fmt.Errorf("unsafe path in pack: %q", f.Name)
	}
	for _, p := range strings.Split(name, "/") {
		if p == ".." {
			return fmt.Errorf("unsafe path in pack: %q", f.Name)
		}
	}
	target := filepath.Join(dest, filepath.FromSlash(name))
	rel, err := filepath.Rel(dest, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("unsafe path in pack: %q", f.Name)
	}
	if f.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, rc)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// FileSHA256 returns the hex SHA-256 of path (independent check for emailed packs).
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
