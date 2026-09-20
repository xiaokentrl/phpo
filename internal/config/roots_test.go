// 工作根状态判定单测：RootsPersisted（建库门禁）与 RootsReady（快照 dirReady 派生）——纯读、零副作用
package config

import (
	"os"
	"path/filepath"
	"testing"
)

// 首启（config.yaml 缺失）：双判据均为假，且不得在用户数据目录留下任何文件
func TestRootsStatus_FirstLaunchHasNoSideEffect(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "phpo") // 尚不存在的用户数据目录
	cs := newStoreAt(filepath.Join(sub, "config.yaml"))

	if cs.RootsPersisted() {
		t.Error("首启 RootsPersisted 应为 false")
	}
	if h, w := cs.RootsReady(); h || w {
		t.Errorf("首启 RootsReady 应为双 false，得 %v %v", h, w)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatalf("纯读判定不得创建用户数据目录，实得 err=%v", err)
	}
}

// 三态：仅落库（未建目录）→ 部分就绪 → 双就绪；重新载入后判定不变（卸载重装靠 config.yaml 记得设置过）
func TestRootsStatus_ThreeStates(t *testing.T) {
	base := t.TempDir()
	home, www := filepath.Join(base, "phpo"), filepath.Join(base, "www")
	cs := newStoreAt(filepath.Join(base, "config.yaml"))

	if err := cs.SetRoots(home, www); err != nil {
		t.Fatal(err)
	}
	if !cs.RootsPersisted() {
		t.Error("SetRoots 后 RootsPersisted 应为 true")
	}
	if h, w := cs.RootsReady(); h || w {
		t.Errorf("目录未建时 RootsReady 应为双 false，得 %v %v", h, w)
	}

	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if h, w := cs.RootsReady(); !h || w {
		t.Errorf("仅主目录存在应为 (true,false)，得 %v %v", h, w)
	}

	if err := os.MkdirAll(www, 0o755); err != nil {
		t.Fatal(err)
	}
	if h, w := cs.RootsReady(); !h || !w {
		t.Errorf("双目录存在应为 (true,true)，得 %v %v", h, w)
	}

	// 磁盘往返：判定只依赖 config.yaml + 目录存在性，与内存态无关
	reloaded, err := LoadFromPath(cs.path)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.RootsPersisted() {
		t.Error("重载后 RootsPersisted 应为 true")
	}
	if h, w := reloaded.RootsReady(); !h || !w {
		t.Errorf("重载后 RootsReady 应为双 true，得 %v %v", h, w)
	}

	// 两根被删除 → 仍算「已持久化」（据信建库），但就绪标记回落（重新拦截写操作）
	if err := os.RemoveAll(www); err != nil {
		t.Fatal(err)
	}
	if !reloaded.RootsPersisted() {
		t.Error("目录被删不应改变「已持久化」判定")
	}
	if h, w := reloaded.RootsReady(); !h || w {
		t.Errorf("网站目录被删应为 (true,false)，得 %v %v", h, w)
	}
}

// 单根缺失（只写过 PHPO_HOME）不构成「已持久化」：首启半成品仍须走完整向导
func TestRootsPersisted_RequiresBothRoots(t *testing.T) {
	cs := newStoreAt(filepath.Join(t.TempDir(), "config.yaml"))
	cs.fc.PHPOHome = "phpo-roots-partial-missing" // 相对 TempDir 之外的不存在路径
	if cs.RootsPersisted() {
		t.Error("WWW_ROOT 为空时 RootsPersisted 应为 false")
	}
	if h, _ := cs.RootsReady(); h {
		t.Error("主目录不存在时不应判就绪")
	}
}

// `~` 前缀的持久值须经 ExpandHome 后再判存在性
func TestRootsStatus_ExpandsHomePrefix(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("无可用主目录，跳过 ~ 展开测试")
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(home, "phpo-roots-test")) })
	if err := os.MkdirAll(filepath.Join(home, "phpo-roots-test"), 0o755); err != nil {
		t.Fatal(err)
	}
	cs := newStoreAt(filepath.Join(t.TempDir(), "config.yaml"))
	if err := cs.SetRoots("~/phpo-roots-test", "~/phpo-roots-test-www"); err != nil {
		t.Fatal(err)
	}
	if h, w := cs.RootsReady(); !h || w {
		t.Errorf("~/ 前缀主目录应判就绪 (true,false)，得 %v %v", h, w)
	}
}
