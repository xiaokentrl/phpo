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

	tops, _, err := Create(archivePath, []Source{
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
	if _, _, err := Create(archivePath, []Source{
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

// 真机装机后 mysql 的 ibdata1、pgsql 的整个 data/、各服务 logs 里的文件由容器内 uid 拥有且 0700/0600，
// 宿主进程读不动。读不动的条目必须「跳过 + 上报」而非判死整包——否则备份功能在真实数据目录上直接不可用。
func TestArchive_SkipsUnreadableAndReports(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 下 chmod 000 不生效")
	}
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "conf", "php.ini"), "[PHP]\n")
	writeFile(t, filepath.Join(src, "logs", "access.log"), "owned-by-root-in-real-life")
	writeFile(t, filepath.Join(src, "data", "PG_VERSION"), "17\n")
	if err := os.Chmod(filepath.Join(src, "logs", "access.log"), 0o000); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(src, "data")
	if err := os.Chmod(dataDir, 0o000); err != nil { // 仿 pgsql/17/data：连 ReadDir 都失败
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dataDir, 0o755) }) // 后注册先跑，赶在 TempDir 清场之前

	dst := filepath.Join(t.TempDir(), "b.tar.gz")
	tops, skipped, err := Create(dst, []Source{{ArcPrefix: "home", HostPath: src}})
	if err != nil {
		t.Fatalf("个别条目读不动不应判死整包: %v", err)
	}
	if len(tops) != 1 || tops[0] != "home" {
		t.Fatalf("可读内容仍应入档，tops 实得 %v", tops)
	}
	if len(skipped) != 2 {
		t.Fatalf("应上报 2 条跳过（不可读文件 + 不可进目录），实得 %+v", skipped)
	}
	for _, s := range skipped {
		if !strings.Contains(s.Reason, "权限不足") {
			t.Fatalf("跳过原因应是人话权限说明，实得 %+v", s)
		}
	}
	if !strings.HasSuffix(skipped[0].Path, "data") || !strings.HasSuffix(skipped[1].Path, "access.log") {
		t.Fatalf("跳过项应按 walk 字典序（data 目录先于 logs/access.log），实得 %v / %v", skipped[0].Path, skipped[1].Path)
	}

	// 归档本身必须完好：解包不得因跳过的条目而中断
	out := t.TempDir()
	n, err := Extract(dst, out)
	if err != nil {
		t.Fatalf("归档应仍可解包: %v", err)
	}
	if n != 1 {
		t.Fatalf("应只解出 php.ini 一个文件，实得 %d", n)
	}
	if _, err := os.Stat(filepath.Join(out, "home", "conf", "php.ini")); err != nil {
		t.Fatalf("php.ini 应解出: %v", err)
	}
}
