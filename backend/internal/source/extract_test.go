package source

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io/fs"
	"testing"
)

func makeTarGz(t *testing.T, files []struct {
	name string
	mode int64
	body string
}) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		hdr := &tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.body))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// TestExtractBinaryPrefersExec: 跳过 LICENSE/README，优先可执行文件（flare 场景）。
func TestExtractBinaryPrefersExec(t *testing.T) {
	data := makeTarGz(t, []struct {
		name string
		mode int64
		body string
	}{
		{"LICENSE", 0o644, "license text"},
		{"README.md", 0o644, "readme"},
		{"flare", 0o755, "FLARE-BIN"},
	})
	got, err := ExtractBinary(data, "flame") // hint 与 "flare" 不匹配，应回退到可执行文件
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "FLARE-BIN" {
		t.Fatalf("应取可执行文件, got %q", got)
	}
}

// TestExtractBinaryZipPrefersExec: zip 同样优先可执行文件。
func TestExtractBinaryZipPrefersExec(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct {
		name string
		mode fs.FileMode
		body string
	}{
		{"LICENSE", 0o644, "license"},
		{"mosdns", 0o755, "MOSDNS-BIN"},
	} {
		hdr := &zip.FileHeader{Name: f.name}
		hdr.SetMode(f.mode)
		w, _ := zw.CreateHeader(hdr)
		_, _ = w.Write([]byte(f.body))
	}
	zw.Close()

	got, err := ExtractBinary(buf.Bytes(), "mosdns")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "MOSDNS-BIN" {
		t.Fatalf("应取可执行文件, got %q", got)
	}
}
