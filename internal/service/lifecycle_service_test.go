// T306 验收：生命周期服务在注入假 Docker/Store/Emitter 下的校准与装卸启停——含漂移事件、幂等、卸载保留数据
package service

import (
	"context"
	"errors"
	"testing"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/pkg/dockerutil"
)

// ---- 假件 ----

// fakeDocker 内存容器世界（name -> running）+ 数据卷（卸载不得触碰）
type fakeDocker struct {
	containers map[string]bool
	volumes    map[string]bool                 // 模拟绑定/命名卷；RemoveContainer 不应删除
	lastSpec   map[string]engine.ContainerSpec // 记录每容器最近一次创建 spec（端口发布断言用）
}

func newFakeDocker() *fakeDocker {
	return &fakeDocker{containers: map[string]bool{}, volumes: map[string]bool{"phpo-mysql-8.4-data": true}, lastSpec: map[string]engine.ContainerSpec{}}
}

func (f *fakeDocker) ManagedContainers(context.Context) ([]engine.ActualState, error) {
	var out []engine.ActualState
	for name, running := range f.containers {
		kind, ver, ok := parseName(name)
		if !ok {
			continue
		}
		out = append(out, engine.ActualState{Ref: engine.ContainerRef{Kind: kind, Version: ver}, Running: running})
	}
	return out, nil
}

func (f *fakeDocker) CreateServiceContainer(_ context.Context, _ config.Env, spec engine.ContainerSpec) (string, error) {
	name := dockerutil.ContainerName(spec.Kind, spec.Version)
	f.containers[name] = false // 新建即停止
	if f.lastSpec == nil {
		f.lastSpec = map[string]engine.ContainerSpec{}
	}
	f.lastSpec[name] = spec
	return "fake-id", nil
}
func (f *fakeDocker) StartContainer(_ context.Context, name string) error {
	if _, ok := f.containers[name]; !ok {
		return errors.New("no such container: " + name)
	}
	f.containers[name] = true
	return nil
}
func (f *fakeDocker) StopContainer(_ context.Context, name string) error {
	if _, ok := f.containers[name]; !ok {
		return errors.New("no such container: " + name)
	}
	f.containers[name] = false
	return nil
}
func (f *fakeDocker) RemoveContainer(_ context.Context, name string) error {
	delete(f.containers, name) // 只删容器；卷保留
	return nil
}
func (f *fakeDocker) PreCleanContainer(_ context.Context, name string) error {
	delete(f.containers, name)
	return nil
}

func parseName(name string) (kind, ver string, ok bool) {
	if len(name) < len(dockerutil.NamespacePrefix) || name[:len(dockerutil.NamespacePrefix)] != dockerutil.NamespacePrefix {
		return "", "", false
	}
	rest := name[len(dockerutil.NamespacePrefix):]
	for i := 0; i < len(rest); i++ {
		if rest[i] == '-' {
			// 版本可含点但 kind 无 '-'：取首个 '-' 切分（kinds 均为单词）
			return rest[:i], rest[i+1:], true
		}
	}
	return "", "", false
}

// fakeStore 实现 StateStore
type fakeStore struct {
	snap *model.Snapshot
}

func newFakeStore() *fakeStore {
	return &fakeStore{snap: model.NewSnapshot()}
}
func (s *fakeStore) BuildSnapshot() (*model.Snapshot, error) { return s.snap, nil }
func (s *fakeStore) SetInstalled(kind, version string, installed bool) error {
	list := s.snap.Installed[kind]
	if installed {
		if !contains(list, version) {
			s.snap.Installed[kind] = append(list, version)
		}
	} else {
		s.snap.Installed[kind] = remove(list, version)
		s.snap.Running[kind] = remove(s.snap.Running[kind], version)
	}
	return nil
}
func (s *fakeStore) SetRunning(kind, version string, running bool) error {
	if running {
		if !contains(s.snap.Running[kind], version) {
			s.snap.Running[kind] = append(s.snap.Running[kind], version)
		}
	} else {
		s.snap.Running[kind] = remove(s.snap.Running[kind], version)
	}
	return nil
}

// EnvReader 子集：假件恒返回未设置，使 DBService/RedisService 回落默认密码与端口
func (s *fakeStore) GetPassword(_, _ string) (string, bool, error) { return "", false, nil }
func (s *fakeStore) GetServicePort(_, _ string) (int, bool, error) { return 0, false, nil }

// fakeEmitter 捕获事件名序列
type fakeEmitter struct {
	events []string
	logs   []string // 捕获 task:log 文本，用于断言步骤日志真的可见
}

func (e *fakeEmitter) Emit(event string, payload any) {
	e.events = append(e.events, event)
	if l, ok := payload.(model.TaskLogEvent); ok {
		e.logs = append(e.logs, l.Text)
	}
}
func (e *fakeEmitter) has(name string) bool { return contains(e.events, name) }

func newSvc() (*LifecycleService, *fakeDocker, *fakeStore, *fakeEmitter) {
	d, s, em := newFakeDocker(), newFakeStore(), &fakeEmitter{}
	return NewLifecycle(d, s, em, config.DerivePaths("~/phpo", "~/www"), s), d, s, em
}

// ---- 测试 ----

func TestCalibrate_KillDetected_PersistsAndEmits(t *testing.T) {
	l, d, s, em := newSvc()
	// SQLite 认为 php 8.4 已安装且在运行；Docker 实际存在但已停止（被 kill）
	_ = s.SetInstalled("php", "8.4", true)
	_ = s.SetRunning("php", "8.4", true)
	d.containers["phpo-php-8.4"] = false

	res, err := l.Calibrate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Corrections) != 1 || res.Corrections[0].Running {
		t.Fatalf("应产出一条 running=false 修正，实得 %+v", res.Corrections)
	}
	if contains(s.snap.Running["php"], "8.4") {
		t.Fatal("校准后 SQLite 不应再把 php/8.4 记为运行")
	}
	// 漂移事件 + 状态快照事件按序发出
	if !em.has("docker:state-drift") || !em.has("state:changed") {
		t.Fatalf("应发 docker:state-drift + state:changed，实得 %v", em.events)
	}
}

func TestCalibrate_Consistent_Silent(t *testing.T) {
	l, d, s, em := newSvc()
	_ = s.SetInstalled("php", "8.4", true)
	_ = s.SetRunning("php", "8.4", true)
	d.containers["phpo-php-8.4"] = true

	res, err := l.Calibrate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed() {
		t.Fatalf("一致时不应判变化: %+v", res)
	}
	if len(em.events) != 0 {
		t.Fatalf("一致时不应发事件，实得 %v", em.events)
	}
}

func TestInstall_CreatesStartsAndIsIdempotent(t *testing.T) {
	l, d, s, em := newSvc()
	if err := l.Install(context.Background(), model.KindPHP, "8.4"); err != nil {
		t.Fatal(err)
	}
	if !d.containers["phpo-php-8.4"] {
		t.Fatal("安装后应存在且运行")
	}
	if !contains(s.snap.Installed["php"], "8.4") || !contains(s.snap.Running["php"], "8.4") {
		t.Fatalf("SQLite 未记录安装/运行: %+v", s.snap.Installed)
	}
	if !em.has("service:changed") || !em.has("state:changed") {
		t.Fatalf("安装应发 service:changed + state:changed，实得 %v", em.events)
	}
	// 再装一次结果一致（Pre-Clean 抹平同名）
	if err := l.Install(context.Background(), model.KindPHP, "8.4"); err != nil {
		t.Fatalf("重复安装应幂等成功: %v", err)
	}
	if len(d.containers) != 1 {
		t.Fatalf("重复安装不应残留额外容器，实得 %v", d.containers)
	}
}

func TestStopStart_Idempotent(t *testing.T) {
	l, d, _, _ := newSvc()
	d.containers["phpo-nginx-alpine"] = true
	for i := 0; i < 5; i++ {
		if err := l.Stop(context.Background(), model.KindNginx, "alpine"); err != nil {
			t.Fatalf("第 %d 次 Stop 失败: %v", i, err)
		}
	}
	if d.containers["phpo-nginx-alpine"] {
		t.Fatal("Stop 后应停止")
	}
	for i := 0; i < 5; i++ {
		if err := l.Start(context.Background(), model.KindNginx, "alpine"); err != nil {
			t.Fatalf("第 %d 次 Start 失败: %v", i, err)
		}
	}
	if !d.containers["phpo-nginx-alpine"] {
		t.Fatal("Start 后应运行")
	}
}

func TestRemove_PreservesVolumes(t *testing.T) {
	l, d, s, _ := newSvc()
	_ = s.SetInstalled("mysql", "8.4", true)
	_ = s.SetRunning("mysql", "8.4", true)
	d.containers["phpo-mysql-8.4"] = true
	before := len(d.volumes)

	if err := l.Remove(context.Background(), model.KindMySQL, "8.4"); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.containers["phpo-mysql-8.4"]; ok {
		t.Fatal("卸载后容器应移除")
	}
	if len(d.volumes) != before {
		t.Fatalf("卸载默认保留数据卷（§5.13.7），卷数 %d→%d", before, len(d.volumes))
	}
	if contains(s.snap.Installed["mysql"], "8.4") {
		t.Fatal("卸载后 SQLite 不应记为已安装")
	}
}

func TestInstall_UnknownKindErrors(t *testing.T) {
	l, _, _, _ := newSvc()
	// 五类服务（php/nginx/mysql/pgsql/redis）均已注册；未注册种类应报错
	if err := l.Install(context.Background(), model.ServiceKind("mongodb"), "1"); err == nil {
		t.Fatal("未注册的服务种类应报错")
	}
}

// 断言小工具
func contains(list []string, x string) bool {
	for _, v := range list {
		if v == x {
			return true
		}
	}
	return false
}

func remove(list []string, x string) []string {
	out := list[:0]
	for _, v := range list {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}
