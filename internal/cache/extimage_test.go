// 扩展固化镜像（phpo/php:{version}）的缓存槽位测试（§5.14.2 两槽位）：
// 基座 image.tar 与固化镜像各占一份文件、各占一条清单记录，互不覆盖。
package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeTmpTar(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "image.tar")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestPromoteExtImageKeepsBaseSlot 装完扩展后基座缓存必须原样：覆盖它会让下次装 php 基座加载到扩展镜像
func TestPromoteExtImageKeepsBaseSlot(t *testing.T) {
	m, env, cap, _ := newMgr(t)
	seedImage(t, m, env, "php", "8.4", []byte("BASE-TAR"))
	baseTar := env.OfflineImageTar("php", "8.4")

	src := writeTmpTar(t, []byte("EXT-TAR"))
	if err := m.PromoteExtImage("8.4", "phpo/php:8.4", src); err != nil {
		t.Fatal(err)
	}

	if b, err := os.ReadFile(baseTar); err != nil || string(b) != "BASE-TAR" {
		t.Fatalf("基座镜像缓存被覆盖: %q err=%v", b, err)
	}
	extTar := env.OfflineExtImageTar("php", "8.4")
	if b, err := os.ReadFile(extTar); err != nil || string(b) != "EXT-TAR" {
		t.Fatalf("固化镜像未落到独立槽位 %s: %q err=%v", extTar, b, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("提升必须搬走临时文件（§5.14.4）")
	}

	mf, err := m.LoadManifest("php", "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if mf.Image == nil || mf.Image.Name != "php:8.4" {
		t.Fatalf("基座记录被改写: %+v", mf.Image)
	}
	if mf.ExtImage == nil || mf.ExtImage.Name != "phpo/php:8.4" || mf.ExtImage.Sha256 == "" {
		t.Fatalf("manifest.extensions_image 未登记: %+v", mf.ExtImage)
	}
	lk, err := m.LookupExtImage("8.4")
	if err != nil || !lk.Hit {
		t.Fatalf("固化镜像缓存应命中: %+v err=%v", lk, err)
	}
	if !contains(eventNames(cap), "cache:promote") {
		t.Fatalf("提升要发 cache:promote，得 %v", eventNames(cap))
	}
}

// TestLookupExtImageCorrupted 无记录或 SHA 不匹配即报损坏，不得误判命中而走零网络 load
func TestLookupExtImageCorrupted(t *testing.T) {
	m, env, _, _ := newMgr(t)
	src := writeTmpTar(t, []byte("EXT-TAR"))
	if err := m.PromoteExtImage("8.4", "phpo/php:8.4", src); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.OfflineExtImageTar("php", "8.4"), []byte("TAMPERED"), 0o644); err != nil {
		t.Fatal(err)
	}
	lk, err := m.LookupExtImage("8.4")
	if err != nil {
		t.Fatal(err)
	}
	if lk.Hit || !lk.Corrupted {
		t.Fatalf("篡改后应判损坏，得 %+v", lk)
	}
	if failed, err := m.VerifyEntry("php", "8.4"); err != nil || len(failed) != 1 {
		t.Fatalf("校验应报出固化镜像一项，得 %v err=%v", failed, err)
	}
}

// TestLoadExtImageZeroNetwork 本机没有固化镜像时从缓存 load 回来（画像 F：断网重装带扩展环境）
func TestLoadExtImageZeroNetwork(t *testing.T) {
	m, env, cap, fb := newMgr(t)
	src := writeTmpTar(t, []byte("EXT-TAR"))
	if err := m.PromoteExtImage("8.4", "phpo/php:8.4", src); err != nil {
		t.Fatal(err)
	}
	fb.loaded = nil
	ref, ok, err := m.LoadExtImage(context.Background(), "8.4")
	if err != nil || !ok || ref != "phpo/php:8.4" {
		t.Fatalf("应载入并返回 ref，得 ref=%q ok=%v err=%v", ref, ok, err)
	}
	if len(fb.loaded) != 1 || fb.loaded[0] != env.OfflineExtImageTar("php", "8.4") {
		t.Fatalf("应 load 固化镜像 tar，得 %v", fb.loaded)
	}
	if len(fb.pulled) != 0 {
		t.Fatal("命中缓存不得联网拉取")
	}
	if !contains(eventNames(cap), "cache:hit") {
		t.Fatalf("应发 cache:hit，得 %v", eventNames(cap))
	}
}

// TestLoadExtImageAbsent 缓存缺席即返回 false，由调用方回落基座镜像
func TestLoadExtImageAbsent(t *testing.T) {
	m, _, _, fb := newMgr(t)
	ref, ok, err := m.LoadExtImage(context.Background(), "8.4")
	if err != nil || ok || ref != "" {
		t.Fatalf("无缓存应安静回落，得 ref=%q ok=%v err=%v", ref, ok, err)
	}
	if len(fb.loaded) != 0 {
		t.Fatalf("无缓存不应 load，得 %v", fb.loaded)
	}
}

// TestExtImageEntryCountedInStats 只有固化镜像的条目也要计入统计与损坏判定
func TestExtImageEntryCountedInStats(t *testing.T) {
	m, _, _, _ := newMgr(t)
	src := writeTmpTar(t, []byte("EXT-TAR"))
	if err := m.PromoteExtImage("8.4", "phpo/php:8.4", src); err != nil {
		t.Fatal(err)
	}
	st, err := m.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.ImageCount != 1 {
		t.Fatalf("固化镜像应计入 ImageCount，得 %+v", st)
	}
	var hasExt bool
	entries, _ := m.ListEntries()
	for _, e := range entries {
		if e.Kind == "php" && e.Version == "8.4" {
			hasExt = e.Size > 0
		}
	}
	if !hasExt {
		t.Fatal("条目体积应含固化镜像 tar")
	}
}

// TestManifestKeepsBaseAndExtTogether 两条镜像记录可共存（schema 向后兼容：老清单缺该字段即 nil）
func TestManifestKeepsBaseAndExtTogether(t *testing.T) {
	m, env, _, _ := newMgr(t)
	seedImage(t, m, env, "php", "8.4", []byte("BASE-TAR"))
	if err := m.PromoteExtImage("8.4", "phpo/php:8.4", writeTmpTar(t, []byte("EXT-TAR"))); err != nil {
		t.Fatal(err)
	}
	mf, err := m.LoadManifest("php", "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if mf.Image == nil || mf.ExtImage == nil {
		t.Fatalf("两条记录须共存: %+v", mf)
	}
	if mf.Image.Sha256 == mf.ExtImage.Sha256 {
		t.Fatal("两条记录的 sha256 应各自独立")
	}
	// 基座仍可命中
	if lk, _ := m.LookupImage("php", "8.4"); !lk.Hit {
		t.Fatalf("基座缓存应仍命中: %+v", lk)
	}
}
