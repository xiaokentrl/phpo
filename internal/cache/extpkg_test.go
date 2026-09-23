// 扩展包（apk / pecl）缓存查找与提升：前缀命中、版本序取最大、SHA256 不符判损坏、提升发 cache:promote。
package cache

import (
	"os"
	"path/filepath"
	"testing"

	"phpo/internal/config"
	"phpo/internal/model"
)

// seedPkg 往缓存根写一份扩展包并登记清单；sha 传 "" 即故意留坏（清单与文件不符）
func seedPkg(t *testing.T, m *Manager, env config.Env, version, extType, name, body, sha string) string {
	t.Helper()
	p := filepath.Join(env.OfflineExtDir("php", version, extType), name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mf := newManifest("php", version)
	pkg := model.ManifestPackage{Name: name, Size: int64(len(body))}
	pkg.Sha256 = sha
	if extType == "pecl" {
		mf.Pecl = []model.ManifestPackage{pkg}
	} else {
		mf.Apk = []model.ManifestPackage{pkg}
	}
	if err := m.SaveManifest(mf); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLookupExtPackage_PrefixHitAndMaxVersion(t *testing.T) {
	m, env, _, _ := newMgr(t)
	peclDir := env.OfflineExtDir("php", "8.4", "pecl")
	for _, n := range []string{"redis-6.0.2.tgz", "redis-5.3.7.tgz", "igbinary-3.2.14.tgz"} {
		p := filepath.Join(peclDir, n)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("pkg-"+n), 0o644); err != nil {
			t.Fatal(err)
		}
		sha, err := FileSHA256(p)
		if err != nil {
			t.Fatal(err)
		}
		mf, err := m.LoadManifest("php", "8.4")
		if err != nil {
			t.Fatal(err)
		}
		mf.Pecl = append(mf.Pecl, model.ManifestPackage{Name: n, Sha256: sha, Size: int64(len("pkg-" + n))})
		if err := m.SaveManifest(mf); err != nil {
			t.Fatal(err)
		}
	}
	// 前缀命中：查询名是不带版本的 "redis"，精确名永远查不到
	lk, err := m.LookupExtPackage("8.4", "pecl", "redis")
	if err != nil {
		t.Fatal(err)
	}
	if !lk.Hit || filepath.Base(lk.Path) != "redis-6.0.2.tgz" {
		t.Fatalf("应命中版本序最大的一份，实得 %+v", lk)
	}
	if lk.Size != int64(len("pkg-redis-6.0.2.tgz")) {
		t.Fatalf("命中应带回体积，实得 %d", lk.Size)
	}
	// 同前缀不误伤：igbinary 不能被 redis 的查询命中
	if lk2, _ := m.LookupExtPackage("8.4", "pecl", "redis2"); lk2.Hit || lk2.Corrupted {
		t.Fatalf("无匹配条目应报未命中，实得 %+v", lk2)
	}
	// ListExtPackages 供 apk 回填整目录用：全量、稳定序
	list, err := m.ListExtPackages("8.4", "pecl")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || filepath.Base(list[0]) != "igbinary-3.2.14.tgz" {
		t.Fatalf("应返回 3 份且按名升序，实得 %v", list)
	}
}

func TestLookupExtPackage_CorruptedAndMissingDir(t *testing.T) {
	m, env, _, _ := newMgr(t)
	p := seedPkg(t, m, env, "8.4", "pecl", "redis-6.0.2.tgz", "REAL", "deadbeef")
	lk, err := m.LookupExtPackage("8.4", "pecl", "redis")
	if err != nil {
		t.Fatal(err)
	}
	if lk.Hit || !lk.Corrupted || lk.Path != p {
		t.Fatalf("SHA256 不符应判损坏并带回路径，实得 %+v", lk)
	}
	// 目录不存在即未命中（不是错误）：全新装机第一次装扩展要能走网络
	if lk2, err := m.LookupExtPackage("7.4", "pecl", "redis"); err != nil || lk2.Hit || lk2.Corrupted {
		t.Fatalf("缓存目录缺席应报未命中，实得 %+v %v", lk2, err)
	}
	if list, err := m.ListExtPackages("7.4", "apk"); err != nil || list != nil {
		t.Fatalf("apk 目录缺席应返回空，实得 %v %v", list, err)
	}
}

func TestPromoteExtension_RegistersAndEmits(t *testing.T) {
	m, env, cap, _ := newMgr(t)
	// 提升来自临时目录
	tmp := filepath.Join(env.RootFor("php", "8.4"), "ext", "pecl")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(tmp, "swoole-5.1.1.tgz")
	if err := os.WriteFile(src, []byte("TGZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.PromoteExtension("8.4", "pecl", src); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(env.OfflineExtDir("php", "8.4", "pecl"), "swoole-5.1.1.tgz")
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("包文件应落到缓存根: %v", err)
	}
	// 提升是搬走：临时目录不留原件（§5.14.4）
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("提升后临时目录内不应仍有该文件")
	}
	mf, err := m.LoadManifest("php", "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(mf.Pecl) != 1 || mf.Pecl[0].Name != "swoole-5.1.1.tgz" || mf.Pecl[0].Sha256 == "" {
		t.Fatalf("manifest.pecl 未登记: %+v", mf.Pecl)
	}
	if !contains(eventNames(cap), "cache:promote") {
		t.Fatalf("提升应发 cache:promote，实得 %v", eventNames(cap))
	}
	// 登记后即可前缀命中（下一次应用扩展零网络）
	lk, err := m.LookupExtPackage("8.4", "pecl", "swoole")
	if err != nil || !lk.Hit {
		t.Fatalf("提升后应命中缓存，实得 %+v %v", lk, err)
	}
}
