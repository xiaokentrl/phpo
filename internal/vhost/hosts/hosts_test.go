package hosts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddAndHas(t *testing.T) {
	content := "# managed by OS\n127.0.0.1 localhost\n"
	next, changed := Add(content, "127.0.0.1", "demo.test")
	if !changed {
		t.Fatal("首次 Add 应 changed")
	}
	if !Has(next, "127.0.0.1", "demo.test") {
		t.Fatalf("Add 后应命中:\n%s", next)
	}
	// 幂等：重复 Add 不改
	again, changed2 := Add(next, "127.0.0.1", "demo.test")
	if changed2 || again != next {
		t.Fatal("重复 Add 应幂等")
	}
}

func TestRemove_MultiHostLine(t *testing.T) {
	content := "127.0.0.1\tdemo.test blog.test\n::1\tlocalhost\n"
	next, changed := Remove(content, "127.0.0.1", "demo.test")
	if !changed {
		t.Fatal("应删除")
	}
	// demo.test 移除，blog.test 保留；::1 行不动
	if Has(next, "127.0.0.1", "demo.test") {
		t.Fatalf("demo.test 应移除:\n%s", next)
	}
	if !Has(next, "127.0.0.1", "blog.test") {
		t.Fatalf("blog.test 应保留:\n%s", next)
	}
	if !strings.Contains(next, "::1\tlocalhost") {
		t.Fatalf("IPv6 行不应被动:\n%s", next)
	}
}

func TestRemove_WholeLineDropped(t *testing.T) {
	next, changed := Remove("127.0.0.1\tdemo.test\n", "127.0.0.1", "demo.test")
	if !changed {
		t.Fatal("应删除")
	}
	if Has(next, "127.0.0.1", "demo.test") {
		t.Fatalf("条目应消失:\n%q", next)
	}
}

func TestManager_AddRemovePersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	os.WriteFile(path, []byte("127.0.0.1 localhost\n"), 0o644)
	m := NewAt(path)

	res, err := m.Add("demo.test")
	if err != nil || !res.Changed || res.Warning != "" {
		t.Fatalf("Add 失败: %+v err=%v", res, err)
	}
	if ok, _ := m.Has("demo.test"); !ok {
		t.Fatal("落盘后应命中")
	}
	// 幂等
	res2, _ := m.Add("demo.test")
	if res2.Changed {
		t.Fatal("重复 Add 不应再改")
	}
	// 删除
	if _, err := m.Remove("demo.test"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := m.Has("demo.test"); ok {
		t.Fatal("删除后不应命中")
	}
	// localhost 仍在
	if ok, _ := m.Has("localhost"); !ok {
		t.Fatal("不应误删 localhost")
	}
}

// TestManager_MissingFileTreatedEmpty 文件不存在时视为空并成功创建
func TestManager_MissingFileTreatedEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	m := NewAt(path)
	res, err := m.Add("demo.test")
	if err != nil || !res.Changed {
		t.Fatalf("应创建文件并写入: %+v err=%v", res, err)
	}
}
