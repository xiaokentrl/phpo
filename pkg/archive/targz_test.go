package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestArchive_RoundTrip(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, "php", "8.3", "conf", "php.ini"), "memory=128M")
	writeFile(t, filepath.Join(src, "mysql", "8.4", "data", "ibdata"), "bin")
	archivePath := filepath.Join(dst, "backup.tar.gz")

	tops, err := Create(archivePath, []Source{
		{ArcPrefix: "home", HostPath: src},
		{ArcPrefix: "missing", HostPath: filepath.Join(src, "does-not-exist")},
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if len(tops) != 1 || tops[0] != "home" {
		t.Fatalf("tops 应为 [home]（缺失源跳过），实得 %v", tops)
	}

	out := t.TempDir()
	n, err := Extract(archivePath, out)
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("应解出 2 个文件，实得 %d", n)
	}
	got, err := os.ReadFile(filepath.Join(out, "home", "php", "8.3", "conf", "php.ini"))
	if err != nil || string(got) != "memory=128M" {
		t.Fatalf("内容不符: %q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(out, "home", "mysql", "8.4", "data", "ibdata")); err != nil {
		t.Fatalf("data 文件应解出: %v", err)
	}
}

func TestArchive_TopLevel(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "A")
	www := t.TempDir()
	writeFile(t, filepath.Join(www, "index.php"), "B")
	archivePath := filepath.Join(t.TempDir(), "b.tar.gz")
	if _, err := Create(archivePath, []Source{
		{ArcPrefix: "db", HostPath: filepath.Join(src, "a.txt")},
		{ArcPrefix: "www", HostPath: www},
	}); err != nil {
		t.Fatal(err)
	}
	tops, err := TopLevel(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tops, ",") != "db,www" {
		t.Fatalf("TopLevel 应为 db,www，实得 %v", tops)
	}
}

// 硬红线 3：归档内出现越界条目名时，Extract 必须拒绝而非写到 dstDir 外
func TestArchive_RejectsTraversal(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "evil.tar.gz")
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	body := []byte("pwned")
	_ = tw.WriteHeader(&tar.Header{Name: "../../escape.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gw.Close()
	if err := os.WriteFile(archivePath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	if _, err := Extract(archivePath, out); err == nil {
		t.Fatal("穿越条目应被拒绝")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(out), "escape.txt")); !os.IsNotExist(err) {
		t.Fatal("不应在 dstDir 之外落地文件")
	}
}
