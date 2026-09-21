// T306 验收：生命周期服务在注入假 Docker/Store/Emitter 下的校准与装卸启停——含漂移事件、幂等、卸载保留数据
package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/pkg/dockerutil"
)

// ---- 假件 ----

// fakeDocker 内存容器世界（name -> running）+ 数据卷（卸载不得触碰）
type fakeDocker struct {
	containers  map[string]bool
	volumes     map[string]bool                 // 模拟绑定/命名卷；RemoveContainer 不应删除
	lastSpec    map[string]engine.ContainerSpec // 记录每容器最近一次创建 spec（端口发布断言用）
	published   map[string][]int                // 容器当前已发布到宿主的端口（重建前实探的剔除依据）
	createCalls int                             // 建容器次数（「未变即不重建」的判据）
}

func newFakeDocker() *fakeDocker {
	return &fakeDocker{containers: map[string]bool{}, volumes: map[string]bool{"phpo-mysql-8.4-data": true}, lastSpec: map[string]engine.ContainerSpec{}, published: map[string][]int{}}
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
	f.createCalls++
	if f.lastSpec == nil {
		f.lastSpec = map[string]engine.ContainerSpec{}
	}
	f.lastSpec[name] = spec
	var held []int
	for _, hostPort := range spec.PortMap {
		if p, err := strconv.Atoi(hostPort); err == nil {
			held = append(held, p)
		}
	}
	f.published[name] = held
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
	delete(f.published, name)
	return nil
}
func (f *fakeDocker) PreCleanContainer(_ context.Context, name string) error {
	delete(f.containers, name)
	delete(f.published, name)
	return nil
}

// PublishedPorts 返回假世界里该容器当前占住的宿主端口（副本，调用方不得改）
func (f *fakeDocker) PublishedPorts(_ context.Context, name string) ([]int, error) {
	if len(f.published[name]) == 0 {
		return nil, nil
	}
	out := make([]int, len(f.published[name]))
	copy(out, f.published[name])
	return out, nil
}

// ContainerRunning 假世界以 containers[name] 记运行态；无容器即 false（不报错）
func (f *fakeDocker) ContainerRunning(_ context.Context, name string) (bool, error) {
	return f.containers[name], nil
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
	snap  *model.Snapshot
	ports map[string]int // "kind version" → 已落库宿主端口（与 snap.Env 同源，模拟 config.yaml）
}

func newFakeStore() *fakeStore {
	return &fakeStore{snap: model.NewSnapshot(), ports: map[string]int{}}
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

// EnvReader 子集：密码恒回落默认；端口读 setPort 预置值（未设时回落注册表默认）
func (s *fakeStore) GetPassword(_, _ string) (string, bool, error) { return "", false, nil }
func (s *fakeStore) GetServicePort(kind, version string) (int, bool, error) {
	p, ok := s.ports[kind+"/"+version]
	return p, ok, nil
}

// setPort 模拟「改服务端口并落库 config.yaml」：reader 与快照 env 同源，故两处一起写
func (s *fakeStore) setPort(kind, version string, port int) {
	s.ports[kind+"/"+version] = port
	s.snap.Env[config.EnvKeyPort(kind, version)] = strconv.Itoa(port)
}

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

// ---- 重建生效（§5.13.4 幂等「重装」：端口与密码 env 只能在建容器时落定）----

// publishedPort 读 fake 记录的最近一次创建 spec 里某容器端口发布到的宿主端口
func publishedPort(d *fakeDocker, name, containerPort string) string {
	return d.lastSpec[name].PortMap[containerPort]
}

func TestReinstall_AppliesNewHostPortAndPreservesVolumes(t *testing.T) {
	l, d, s, em := newSvc()
	ctx := context.Background()
	if err := l.Install(ctx, model.KindMySQL, "8.4"); err != nil {
		t.Fatal(err)
	}
	if got := publishedPort(d, "phpo-mysql-8.4", "3306/tcp"); got != "3306" {
		t.Fatalf("安装应发布默认端口 3306，实得 %q", got)
	}
	s.setPort("mysql", "8.4", 3307)

	// 停/起只是启停同名容器，不重建 → 端口不变（这正是「改了端口不生效」的根因）
	if err := l.Stop(ctx, model.KindMySQL, "8.4"); err != nil {
		t.Fatal(err)
	}
	if err := l.Start(ctx, model.KindMySQL, "8.4"); err != nil {
		t.Fatal(err)
	}
	if got := publishedPort(d, "phpo-mysql-8.4", "3306/tcp"); got != "3306" {
		t.Fatalf("Start 不得改端口，实得 %q", got)
	}

	before := len(d.volumes)
	if err := l.Reinstall(ctx, model.KindMySQL, "8.4"); err != nil {
		t.Fatal(err)
	}
	if got := publishedPort(d, "phpo-mysql-8.4", "3306/tcp"); got != "3307" {
		t.Fatalf("重建后应发布新端口 3307，实得 %q", got)
	}
	if !d.containers["phpo-mysql-8.4"] {
		t.Fatal("重建后应在运行")
	}
	if len(d.volumes) != before {
		t.Fatalf("重建只换容器，不得动数据卷，卷数 %d→%d", before, len(d.volumes))
	}
	if !em.has("state:changed") {
		t.Fatalf("重建应发 state:changed，实得 %v", em.events)
	}
}

// TestReinstall_BusyPortFailsBeforeDestroyingRunningContainer 端口被别的服务占了就拒绝重建：
// 否则 Pre-Clean 已删掉在跑的容器，新容器却绑不上端口——服务白丢（§5.13.13 不留脏状态）
func TestReinstall_BusyPortFailsBeforeDestroyingRunningContainer(t *testing.T) {
	l, d, s, _ := newSvc()
	ctx := context.Background()
	if err := l.Install(ctx, model.KindRedis, "8"); err != nil {
		t.Fatal(err)
	}
	if err := l.Install(ctx, model.KindMySQL, "8.4"); err != nil {
		t.Fatal(err)
	}
	s.setPort("mysql", "8.4", 6379) // 改到 redis 已在用的端口

	err := l.Reinstall(ctx, model.KindMySQL, "8.4")
	if err == nil || !strings.Contains(err.Error(), "6379") {
		t.Fatalf("端口冲突应报错并点明端口号，实得 %v", err)
	}
	if !d.containers["phpo-mysql-8.4"] {
		t.Fatal("冲突时不得已删/停正在运行的容器")
	}
	if got := publishedPort(d, "phpo-mysql-8.4", "3306/tcp"); got != "3306" {
		t.Fatalf("冲突时旧容器应原样保留，端口实得 %q", got)
	}
	if got := publishedPort(d, "phpo-redis-8", "6379/tcp"); got != "6379" {
		t.Fatalf("redis 不应被牵连，端口实得 %q", got)
	}
}

func TestReinstall_RequiresInstalled(t *testing.T) {
	l, d, _, _ := newSvc()
	// Docker 里有同名容器，但库里未记为已安装 → 重建不是卸载/安装的替代入口，应拒绝且不碰容器
	d.containers["phpo-mysql-8.4"] = true
	if err := l.Reinstall(context.Background(), model.KindMySQL, "8.4"); err == nil ||
		!strings.Contains(err.Error(), "未安装") {
		t.Fatalf("未安装的服务重建应报未安装，实得 %v", err)
	}
	if !d.containers["phpo-mysql-8.4"] {
		t.Fatal("拒绝重建时不得动容器")
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
