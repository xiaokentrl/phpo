// 「启动后最小化」的建窗前取值与门面守卫用例：读失败一律显示主窗口，界面只认落盘值
package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"phpo/internal/config"
)

// isolateUserConfig 把用户数据目录指进临时盘：这些用例碰的是 config.yaml，绝不能读真机那份
func isolateUserConfig(t *testing.T, body string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	dir := filepath.Join(root, "phpo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if body == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 没勾（含首启没有 config.yaml）与勾上：建窗前那一次现读必须如实给出，别的配置项不干扰它
func TestStartMinimizedAtBoot_ReadsPersistedSwitch(t *testing.T) {
	isolateUserConfig(t, "")
	if startMinimizedAtBoot() {
		t.Fatal("首启没有 config.yaml 时应按「显示主窗口」建窗")
	}

	isolateUserConfig(t, "phpo_home: /h/boot\nwww_root: /w/boot\n")
	if startMinimizedAtBoot() {
		t.Fatal("未写 start_minimized 时应按「显示主窗口」建窗")
	}

	isolateUserConfig(t, "phpo_home: /h/boot\nwww_root: /w/boot\nstart_minimized: true\n")
	if !startMinimizedAtBoot() {
		t.Fatal("勾选后建窗参数应带 Hidden")
	}
}

// 配置读不出结果（那一路径上根本是个目录）时不得最小化：
// 宁可显示窗口，也不给用户一个「既没窗口也没解释」的开箱
func TestStartMinimizedAtBoot_FallsBackToShowWindow(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	if err := os.MkdirAll(filepath.Join(root, "phpo", "config.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadConfigStore(); err == nil {
		t.Fatal("构造失败：这份环境把目录当配置读成功了")
	}
	if startMinimizedAtBoot() {
		t.Fatal("读配置失败必须回落「显示主窗口」")
	}
}

// 对象图未装配（启动早期 / 装配失败）时门面拒绝：
// 返回一个假的 false 会让界面以为「现读到了未勾选」，把开关画错
func TestStartMinimizedFacade_NotReady(t *testing.T) {
	a := NewApp()
	got, err := a.StartMinimizedGet()
	if !errors.Is(err, errNotReady) || got {
		t.Fatalf("期望 (false, errNotReady)，实得 (%v, %v)", got, err)
	}
	if err := a.StartMinimizedSet(true); !errors.Is(err, errNotReady) {
		t.Fatalf("期望 errNotReady，实得 %v", err)
	}
}
