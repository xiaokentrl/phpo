package hosts

import (
	"fmt"
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
	if err := os.WriteFile(path, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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

// TestManager_RemoveWarningSaysDelete 删除路径的警告必须让用户「手动删除」，不能沿用添加措辞
func TestManager_RemoveWarningSaysDelete(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 下写只读文件不会失败，降级分支不触发")
	}
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte("127.0.0.1 localhost demo.test\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	res, err := NewAt(path).Remove("demo.test")
	if err != nil || res.Changed {
		t.Fatalf("只读文件应降级为警告: %+v %v", res, err)
	}
	if !strings.Contains(res.Warning, "手动删除") || strings.Contains(res.Warning, "手动添加") {
		t.Fatalf("删除的警告应指向手动删除该条目: %q", res.Warning)
	}
	if !strings.Contains(res.Warning, "127.0.0.1 demo.test") {
		t.Fatalf("警告要给出待删的具体条目: %q", res.Warning)
	}
}

// TestManager_Elevate 直写被拒时的两条降级路径：提权成功即写入；提权失败回人话警告而不是 error（§5.7）
func TestManager_Elevate(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 下写只读文件不会失败，提权分支不触发")
	}
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte("127.0.0.1 localhost\n"), 0o444); err != nil {
		t.Fatal(err)
	}

	res, err := (&Manager{path: path, ip: "127.0.0.1", elevate: func(string) error { return nil }}).Add("demo.test")
	if err != nil || res.Warning != "" || !res.Changed {
		t.Fatalf("提权成功应视为已写入: %+v %v", res, err)
	}

	res, err = (&Manager{path: path, ip: "127.0.0.1", elevate: func(string) error {
		return fmt.Errorf("polkit 提权写入失败: exit status 127")
	}}).Add("demo.test")
	if err != nil {
		t.Fatalf("提权被拒是警告不是错误: %v", err)
	}
	if res.Changed || !strings.Contains(res.Warning, "提权写入 hosts 失败") || !strings.Contains(res.Warning, "127.0.0.1 demo.test") {
		t.Fatalf("应回可执行的提权失败警告: %+v", res)
	}

	// 无提权器（NewAt 自定义 hosts）：保持原「无法修改 hosts」警告口径
	res, err = NewAt(path).Add("demo.test")
	if err != nil || res.Changed || !strings.Contains(res.Warning, "无法修改 hosts") {
		t.Fatalf("无提权器应降级为警告: %+v %v", res, err)
	}
	b, _ := os.ReadFile(path)
	if Has(string(b), "127.0.0.1", "demo.test") {
		t.Fatalf("三次写入均未真正落盘（只读文件）:\n%s", b)
	}
}
