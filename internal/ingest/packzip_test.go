package ingest_test

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sanjeev0120test/opsgraph/fixtures"
	"github.com/sanjeev0120test/opsgraph/internal/ingest"
	"github.com/sanjeev0120test/opsgraph/internal/store"
)

func TestZipPackRoundTrip(t *testing.T) {
	fsys, err := fixtures.CheckoutFS()
	if err != nil {
		t.Fatal(err)
	}
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	now, err := ingest.IngestFixtureFS(s, fsys)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := ingest.WritePack(s, now, dir); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "incident.opsgraph")
	if err := ingest.ZipPack(dir, zipPath); err != nil {
		t.Fatal(err)
	}
	got, extra, err := ingest.MaterializeFixture(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(extra)
	s2, cleanup2, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup2)
	now2, err := ingest.IngestFixtureDir(s2, got)
	if err != nil {
		t.Fatal(err)
	}
	if !now2.Equal(now) {
		t.Fatalf("now = %v want %v", now2, now)
	}
	co, err := s2.GetService("checkout")
	if err != nil {
		t.Fatal(err)
	}
	if co.Health != "degraded" {
		t.Fatalf("health=%q", co.Health)
	}
	sum1, err := ingest.FileSHA256(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zip2 := filepath.Join(t.TempDir(), "incident2.opsgraph")
	if err := ingest.ZipPack(dir, zip2); err != nil {
		t.Fatal(err)
	}
	sum2, err := ingest.FileSHA256(zip2)
	if err != nil {
		t.Fatal(err)
	}
	if sum1 != sum2 {
		t.Fatalf("zip hash not reproducible:\n%s\n%s", sum1, sum2)
	}
	for i := 0; i < 25; i++ {
		p := filepath.Join(t.TempDir(), "n.opsgraph")
		if err := ingest.ZipPack(dir, p); err != nil {
			t.Fatal(err)
		}
		sum, err := ingest.FileSHA256(p)
		if err != nil {
			t.Fatal(err)
		}
		if sum != sum1 {
			t.Fatalf("zip hash drifted on from-scratch run %d:\n%s\n%s", i+1, sum1, sum)
		}
	}
}

func TestUnzipPackRejectsDotDot(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "bad.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ingest.UnzipPack(zipPath, dest); err == nil {
		t.Fatal("expected unsafe path error")
	}
}

func TestZipPackHeadersArePortable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "t.opsgraph")
	if err := ingest.ZipPack(dir, zipPath); err != nil {
		t.Fatal(err)
	}
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if len(r.File) != 1 {
		t.Fatalf("files=%d", len(r.File))
	}
	h := r.File[0].FileHeader
	if h.Name != "a.txt" {
		t.Fatalf("name=%q", h.Name)
	}
	if h.CreatorVersion&0xff00 != 0 {
		t.Fatalf("CreatorVersion OS byte = %d want 0 (must not encode GOOS)", h.CreatorVersion>>8)
	}
	want := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	if h.Modified.UTC().Unix() != want {
		t.Fatalf("Modified unix = %d want %d", h.Modified.UTC().Unix(), want)
	}
}
