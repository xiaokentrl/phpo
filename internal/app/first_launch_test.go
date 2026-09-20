// 首启零落盘回归（方案B）：装机向导把两根写入 config.yaml 前，对象图启动钩子与只读快照都不得在用户数据目录留下文件
package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// runHook 只执行指定名的启动钩子（避免连带跑临时目录清理等触及真实 HOME 的钩子）
func runHook(t *testing.T, l *Lifecycle, name string) error {
	t.Helper()
	for _, h := range l.startupHooks {
		if h.Name == name {
			return h.Fn(context.Background())
		}
	}
	t.Fatalf("启动钩子 %s 未注册", name)
	return nil
}

func TestObjectGraphHook_FirstLaunchWritesNoUserDataFiles(t *testing.T) {
	cfgRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgRoot)
	home := filepath.Join(t.TempDir(), "phpo") // 工作根刻意落在临时目录，绝不触碰真实 ~/phpo
	www := filepath.Join(t.TempDir(), "www")

	c := NewContainer()
	c.Build()
	if err := runHook(t, c.Lifecycle, "object-graph"); err != nil {
		t.Fatalf("首启 object-graph 应成功: %v", err)
	}
	t.Cleanup(func() { c.Lifecycle.OnShutdown(context.Background()) })

	if c.AppService == nil || c.WizardService == nil {
		t.Fatal("对象图应已装配")
	}
	userDataDir := filepath.Join(cfgRoot, "phpo")
	if _, err := os.Stat(userDataDir); !os.IsNotExist(err) {
		t.Fatalf("首启不得创建用户数据目录 %s（err=%v）", userDataDir, err)
	}

	// 只读快照在无库时也必须可用（空态 + dirReady 双 false），且不落任何文件
	snap, err := c.AppService.GetState()
	if err != nil {
		t.Fatalf("首启 GetState 应返回空态而非报错: %v", err)
	}
	if snap.DirReady["PHPO_HOME"] || snap.DirReady["WWW_ROOT"] {
		t.Fatalf("首启 dirReady 应双 false，得 %+v", snap.DirReady)
	}
	if _, err := os.Stat(userDataDir); !os.IsNotExist(err) {
		t.Fatal("首启 GetState 不得建库")
	}

	// 向导完成两根落地 → 同一进程内透明建库并派生双就绪（无需重启）
	if err := c.WizardService.HomeEnsure(context.Background(), home, www); err != nil {
		t.Fatalf("HomeEnsure err: %v", err)
	}
	snap, err = c.AppService.GetState()
	if err != nil {
		t.Fatalf("向导后 GetState 应成功: %v", err)
	}
	if !snap.DirReady["PHPO_HOME"] || !snap.DirReady["WWW_ROOT"] {
		t.Fatalf("向导后 dirReady 应双 true，得 %+v", snap.DirReady)
	}
	for _, f := range []string{"config.yaml", "phpo.db"} {
		if _, err := os.Stat(filepath.Join(userDataDir, f)); err != nil {
			t.Fatalf("向导后应存在 %s: %v", f, err)
		}
	}
}
