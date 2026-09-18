// T309 验收：AppService 经 task.Manager 编排写操作——事件序列（task:*/cache/state）、
// 工作目录落盘、硬红线 7 门禁、幂等、卸载保留数据。Docker/Store/Emitter/缓存全部假件注入，无需真实引擎。
package service

import (
	"context"
	"os"
	"testing"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/task"
)

// fakeEnsurer 实现 steps.ImageEnsurer：记录被确保的 kind/version
type fakeEnsurer struct {
	ensured []string
	err     error
}

func (f *fakeEnsurer) EnsureImage(_ context.Context, kind, version, _ string) error {
	f.ensured = append(f.ensured, kind+"/"+version)
	return f.err
}

// okProbe 报告 Docker 可用（err 非空则模拟不可用）
type okProbe struct{ err error }

func (p okProbe) Detect(context.Context) (string, error) {
	if p.err != nil {
		return "", p.err
	}
	return "28.3.2", nil
}

// newApp 构造接线完成的 AppService + 各假件；env 指向临时目录以真实演练落盘步骤
func newApp(t *testing.T, probeErr error) (*AppService, *fakeDocker, *fakeStore, *fakeEmitter, *fakeEnsurer, config.Env) {
	t.Helper()
	home := t.TempDir()
	d, s, em := newFakeDocker(), newFakeStore(), &fakeEmitter{}
	env := config.DerivePaths(home, home+"/www")
	lc := NewLifecycle(d, s, em, env)
	ens := &fakeEnsurer{}
	tm := task.NewManager(em)
	return NewAppService(lc, tm, ens, okProbe{err: probeErr}, env), d, s, em, ens, env
}

func TestAppService_Install_OrchestratesTaskAndCache(t *testing.T) {
	a, d, s, em, ens, env := newApp(t, nil)
	if err := a.Install(context.Background(), model.KindPHP, "8.4"); err != nil {
		t.Fatal(err)
	}
	// 缓存步应先确保镜像（离线优先，T303）
	if len(ens.ensured) != 1 || ens.ensured[0] != "php/8.4" {
		t.Fatalf("应先经缓存确保镜像，实得 %v", ens.ensured)
	}
	// 容器应创建并运行
	if !d.containers["phpo-php-8.4"] {
		t.Fatal("安装后应存在且运行")
	}
	if !contains(s.snap.Installed["php"], "8.4") || !contains(s.snap.Running["php"], "8.4") {
		t.Fatalf("SQLite 未记录: inst=%v run=%v", s.snap.Installed, s.snap.Running)
	}
	// 工作目录与配置应已落盘（T307 模板 → PHPO_HOME，供 bind 挂载）
	if _, err := os.Stat(env.PHPOHome + "/php/8.4/conf/php.ini"); err != nil {
		t.Fatalf("安装应渲染 php.ini: %v", err)
	}
	// 事件：task:* + service:changed + state:changed
	for _, want := range []string{"task:log", "task:progress", "task:done", "service:changed", "state:changed"} {
		if !em.has(want) {
			t.Fatalf("应发 %s，实得 %v", want, em.events)
		}
	}
}

func TestAppService_Install_BlockedWhenDockerDown(t *testing.T) {
	a, d, _, em, ens, env := newApp(t, engine.ErrDockerNotRunning)
	err := a.Install(context.Background(), model.KindPHP, "8.4")
	if err == nil {
		t.Fatal("Docker 未运行应拒绝安装（硬红线 7）")
	}
	if len(ens.ensured) != 0 {
		t.Fatalf("门禁失败不得触碰镜像后端，实得 %v", ens.ensured)
	}
	if len(d.containers) != 0 {
		t.Fatalf("门禁失败不得创建容器，实得 %v", d.containers)
	}
	// 门禁在配置落盘前拦住：不应产生任何宿主配置文件
	if _, err := os.Stat(env.PHPOHome + "/php/8.4/conf/php.ini"); !os.IsNotExist(err) {
		t.Fatalf("门禁失败不得落盘配置，stat err=%v", err)
	}
	if !em.has("task:done") {
		t.Fatalf("应发 task:done（失败终态），实得 %v", em.events)
	}
}

func TestAppService_StartStopRemove_ViaTask(t *testing.T) {
	a, d, s, _, _, _ := newApp(t, nil)
	// 先装入运行态
	d.containers["phpo-nginx-alpine"] = true
	_ = s.SetInstalled("nginx", "alpine", true)
	_ = s.SetRunning("nginx", "alpine", true)

	if err := a.Stop(context.Background(), model.KindNginx, "alpine"); err != nil {
		t.Fatal(err)
	}
	if d.containers["phpo-nginx-alpine"] {
		t.Fatal("停止后不应运行")
	}
	if err := a.Start(context.Background(), model.KindNginx, "alpine"); err != nil {
		t.Fatal(err)
	}
	if !d.containers["phpo-nginx-alpine"] {
		t.Fatal("启动后应运行")
	}

	d.volumes["phpo-mysql-8.4-data"] = true
	if err := a.Remove(context.Background(), model.KindMySQL, "8.4"); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.containers["phpo-mysql-8.4"]; ok {
		t.Fatal("卸载后容器应移除")
	}
	if !d.volumes["phpo-mysql-8.4-data"] {
		t.Fatal("卸载须保留数据卷（§5.13.7）")
	}
}

func TestAppService_UnknownKindFailsInsideTask(t *testing.T) {
	a, _, _, _, _, _ := newApp(t, nil)
	// redis 未在 M3 注册装配策略：容器步内 lifecycle.Install 报错 → 任务失败
	if err := a.Install(context.Background(), model.KindRedis, "8"); err == nil {
		t.Fatal("未注册服务种类应报错")
	}
}

func TestAppService_GetStateAndCancel(t *testing.T) {
	a, _, s, _, _, _ := newApp(t, nil)
	_ = s.SetInstalled("php", "8.4", true)
	snap, err := a.GetState()
	if err != nil || !contains(snap.Installed["php"], "8.4") {
		t.Fatalf("GetState 应返回权威快照，实得 %+v err=%v", snap, err)
	}
	if a.Running() {
		t.Fatal("无任务时 Running 应为 false")
	}
	a.Cancel() // 无运行任务时不应 panic
}
