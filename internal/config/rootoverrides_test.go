// 可自定义根的单一真相单测（需求 1/2/7/8）：缓存根 / 备份根 / 每服务版本数据目录
// 冻结口径：路径永远只有一个——自定义即完全取代默认派生值，两者绝不并存、不做二级回退。
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootOverrides_CustomWinsExclusively(t *testing.T) {
	base := DerivePaths("/data/phpo", "/srv/www")
	if base.OfflineRoot != "/data/phpo/offline" || base.BackupRoot != "/data/phpo/backups" {
		t.Fatalf("默认根派生变了: %+v", base)
	}
	got := base.ApplyRootOverrides("/mnt/cache/", "/mnt/bak")
	if got.OfflineRoot != "/mnt/cache" {
		t.Errorf("自定义缓存根应完全取代默认，得 %q", got.OfflineRoot)
	}
	if got.BackupRoot != "/mnt/bak" {
		t.Errorf("自定义备份根应完全取代默认，得 %q", got.BackupRoot)
	}
	// 缓存内所有派生路径随新根走（装前必查的目录即此根）
	if tar := got.OfflineImageTar("php", "8.4"); tar != "/mnt/cache/php/8.4/image.tar" {
		t.Errorf("image.tar 未随选定根: %q", tar)
	}
	// 空覆盖 = 回落默认（互斥的另一半：不存在「既走自定义又走默认」）
	if back := got.ApplyRootOverrides("", ""); back.OfflineRoot != "/data/phpo/offline" || back.BackupRoot != "/data/phpo/backups" {
		t.Errorf("清空自定义后应回落默认根，得 %q / %q", back.OfflineRoot, back.BackupRoot)
	}
}

func TestRootOverrides_SurviveHomeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无可用主目录，跳过 ~ 展开测试")
	}
	e := DerivePaths("~/h", "~/w").ApplyRootOverrides("~/cache", "~/bak").ApplyDataDirs(map[string]string{"mysql/8.4": "~/mysqldata"})
	exp := ExpandEnvHomes(e)
	if exp.OfflineRoot != filepath.Join(home, "cache") {
		t.Errorf("ExpandEnvHomes 丢了自定义缓存根: %q", exp.OfflineRoot)
	}
	if exp.BackupRoot != filepath.Join(home, "bak") {
		t.Errorf("ExpandEnvHomes 丢了自定义备份根: %q", exp.BackupRoot)
	}
	if got := exp.DataDirFor("mysql", "8.4"); got != filepath.Join(home, "mysqldata") {
		t.Errorf("ExpandEnvHomes 丢了数据目录覆盖: %q", got)
	}
}

// 需求 7：数据目录按「服务版本」单独自定义；未自定义的版本仍走默认派生
func TestDataDirOverride_OnlyAffectsDataMount(t *testing.T) {
	e := DerivePaths("/h", "/w").ApplyDataDirs(map[string]string{"mysql/8.4": "/disk2/mysql84"})
	mounts := e.ResolveMounts("mysql", "8.4")
	var data, conf string
	for _, m := range mounts {
		switch m.Sub {
		case "data":
			data = m.Host
		case "conf":
			conf = m.Host
		}
	}
	if data != "/disk2/mysql84" {
		t.Errorf("mysql 8.4 数据挂载应走自定义根，得 %q", data)
	}
	if conf != "/h/mysql/8.4/conf/my.cnf" {
		t.Errorf("conf 挂载不应受影响，得 %q", conf)
	}
	if len(e.ResolveMounts("mysql", "8.0")) != 4 {
		t.Error("挂载条目数不应因覆盖而变")
	}
	if got := e.DataDirFor("mysql", "8.0"); got != "/h/mysql/8.0/data" {
		t.Errorf("未覆盖版本应回落默认数据目录，得 %q", got)
	}
	if got := e.ResolveMounts("mysql", "8.0")[1].Host; got != "/h/mysql/8.0/data" {
		t.Errorf("8.0 的 data 挂载应仍是默认根，得 %q", got)
	}
	if len(e.ResolveMounts("php", "8.4")) != 4 {
		t.Error("php 挂载数不应变")
	}
}

func TestConfigStore_RootAndDataDirPersist(t *testing.T) {
	dir := t.TempDir()
	cs := newStoreAt(filepath.Join(dir, "config.yaml"))
	if err := cs.SetRoots("/h", "/w"); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetOfflineRoot("/mnt/off"); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetBackupRoot("/mnt/bak"); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetDataDir("mysql", "8.4", "/disk2/mysql84"); err != nil {
		t.Fatal(err)
	}
	flat := cs.FlatEnv()
	if flat["OFFLINE_ROOT"] != filepath.Join("/mnt/off") {
		t.Errorf("OFFLINE_ROOT 未反映自定义根: %q", flat["OFFLINE_ROOT"])
	}
	if flat["BACKUP_ROOT"] != "/mnt/bak" {
		t.Errorf("BACKUP_ROOT 未反映自定义根: %q", flat["BACKUP_ROOT"])
	}
	if flat[EnvKeyDataDir("mysql", "8.4")] != "/disk2/mysql84" {
		t.Errorf("数据目录未进快照 env: %q", flat[EnvKeyDataDir("mysql", "8.4")])
	}

	reloaded, err := LoadFromPath(cs.path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.ExpandedEnv().OfflineRoot; got != "/mnt/off" {
		t.Errorf("重载后自定义缓存根丢失: %q", got)
	}
	if got := reloaded.ExpandedEnv().DataDirFor("mysql", "8.4"); got != "/disk2/mysql84" {
		t.Errorf("重载后数据目录丢失: %q", got)
	}

	// 清空自定义 → 回落默认（同一时刻只有一个根）
	if err := reloaded.SetOfflineRoot(""); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Env().OfflineRoot; got != "/h/offline" {
		t.Errorf("清空后应回落默认根，得 %q", got)
	}
}

// 配置写盘只认路径安全（硬红线 3）：拒绝 `..` 穿越，其余一律放行（§1.6 最小限制）
func TestRootOverrides_RejectTraversalOnly(t *testing.T) {
	for _, bad := range []string{"/a/../b", "../b", "/a/..", `D:\phpo\..\windows`} {
		if _, err := checkRootPath(bad); err == nil {
			t.Errorf("%q 应被路径安全校验拒绝", bad)
		}
	}
	for _, ok := range []string{"", "/a/b", "~/x", `D:\phpo\offline`, "/带 空格/中文"} {
		if _, err := checkRootPath(ok); err != nil {
			t.Errorf("%q 应放行，实得 %v", ok, err)
		}
	}
	if got, err := checkRootPath("/mnt/cache///"); err != nil || got != "/mnt/cache" {
		t.Errorf("规范化失败: %q %v", got, err)
	}
}
