// 手工导入缓存单测（需求 1/2）：导入只复制不改走源文件 · 登记 manifest · 落点永远跟随当前生效缓存根
package cache

import (
	"os"
	"testing"

	"phpo/internal/config"
)

// newCustomRootMgr 缓存根自定义到 PHPO_HOME 之外的目录：验证导入/提升都只认那一个根
func newCustomRootMgr(t *testing.T, offline string) (*Manager, config.Env, *capEmitter) {
	t.Helper()
	env := config.DerivePaths(t.TempDir(), t.TempDir()).ApplyRootOverrides(offline, "")
	em := &capEmitter{}
	return NewManager(env, em, nil), env, em
}

func TestImportImageCopiesSourceAndRegistersManifest(t *testing.T) {
	root := t.TempDir()
	m, env, cap := newCustomRootMgr(t, root+"/cache")
	src := root + "/somewhere/mysql-8.4.tar"
	if err := os.MkdirAll(dirOf(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("USER-TAR"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.ImportImage("mysql", "8.4", "mysql:8.4", src); err != nil {
		t.Fatal(err)
	}
	// 源文件是用户资产：导入只复制，绝不搬走（提升路径才搬，见 TestPromoteImageMovesSource）
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("导入后源文件应保留，得 %v", err)
	}
	dst := env.OfflineImageTar("mysql", "8.4")
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("导入应落到自定义缓存根 %s，得 %v", dst, err)
	}
	if string(got) != "USER-TAR" {
		t.Fatalf("内容应原样复制，得 %q", got)
	}
	mf, err := m.LoadManifest("mysql", "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if mf.Image == nil || mf.Image.Name != "mysql:8.4" || mf.Image.Sha256 == "" {
		t.Fatalf("manifest.image 应登记名称与 SHA256，得 %+v", mf.Image)
	}
	if !contains(eventNames(cap), "cache:promote") {
		t.Fatalf("导入应发 cache:promote，得 %v", eventNames(cap))
	}
	// 导入即缓存条目：命中查找必须走同一条根（否则「导入成功但装不上」）
	lookup, err := m.LookupImage("mysql", "8.4")
	if err != nil || !lookup.Hit {
		t.Fatalf("导入后应立即命中缓存，得 %+v / %v", lookup, err)
	}
}

func TestImportExtensionLandsInCustomRoot(t *testing.T) {
	root := t.TempDir()
	m, env, _ := newCustomRootMgr(t, root+"/cache")
	src := root + "/redis-6.0.2.tgz"
	if err := os.WriteFile(src, []byte("EXT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.ImportExtension("8.4", "pecl", src); err != nil {
		t.Fatal(err)
	}
	dst := env.OfflineExtDir("php", "8.4", "pecl") + "/redis-6.0.2.tgz"
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("pecl 包应落到自定义根的 php/8.4/pecl/，得 %v", err)
	}
	mf, err := m.LoadManifest("php", "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(mf.Pecl) != 1 || mf.Pecl[0].Name != "redis-6.0.2.tgz" {
		t.Fatalf("manifest.pecl 应登记该包，得 %+v", mf.Pecl)
	}
}

// 提升（安装管道）与导入（手工）的唯一差别就是源文件处置：临时目录必清，铁律 3
func TestPromoteImageMovesSource(t *testing.T) {
	m, env, _ := newCustomRootMgr(t, t.TempDir()+"/cache")
	tmp := env.PHPOHome + "/php/8.4/ext/image.tar"
	if err := os.MkdirAll(dirOf(tmp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("IMG"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.PromoteImage("php", "8.4", "php:8.4-fpm", tmp); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("提升必须搬走临时文件（临时目录必清），得 %v", err)
	}
}
