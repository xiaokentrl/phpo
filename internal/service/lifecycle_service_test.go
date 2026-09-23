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
	localImages map[string]bool                 // 本机 Docker 镜像库（固化扩展镜像的判据）
	lastSpec    map[string]engine.ContainerSpec // 记录每容器最近一次创建 spec（端口发布断言用）
	published   map[string][]int                // 容器当前已发布到宿主的端口（重建前实探的剔除依据）
	createCalls int                             // 建容器次数（「未变即不重建」的判据）
	imageChecks int                             // ImageExists 次数（轻量校准不得拨镜像探针的判据）
	createErr   error                           // 非空则 CreateServiceContainer 失败（模拟端口绑不上等建容器错误）
	imageErr    error                           // 非空则 ImageExists 失败（模拟镜像库探针本身不可用）
}

func newFakeDocker() *fakeDocker {
	return &fakeDocker{containers: map[string]bool{}, volumes: map[string]bool{"phpo-mysql-8.4-data": true}, localImages: map[string]bool{}, lastSpec: map[string]engine.ContainerSpec{}, published: map[string][]int{}}
}

// ImageExists 探本机镜像库：固化扩展镜像 phpo/php:{version} 是否就绪
func (f *fakeDocker) ImageExists(_ context.Context, ref string) (bool, error) {
	f.imageChecks++
	if f.imageErr != nil {
		return false, f.imageErr
	}
	return f.localImages[ref], nil
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
	if f.createErr != nil {
		return "", f.createErr
	}
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

// ContainerExists 假世界以键是否存在记容器存在
func (f *fakeDocker) ContainerExists(_ context.Context, name string) bool {
	_, ok := f.containers[name]
	return ok
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

// SetGaps 缺失态是内存派生态：写回假快照，与 store.Store 同构
func (s *fakeStore) SetGaps(gaps []model.ServiceGap) { s.snap.Gaps = gaps }

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
	levels []string // 与 logs 同序的日志级别，用于断言失败行是 err 级
	drifts []model.StateDrift
}

func (e *fakeEmitter) Emit(event string, payload any) {
	e.events = append(e.events, event)
	if l, ok := payload.(model.TaskLogEvent); ok {
		e.logs = append(e.logs, l.Text)
		e.levels = append(e.levels, string(l.Level))
	}
	if d, ok := payload.(model.StateDrift); ok {
		e.drifts = append(e.drifts, d)
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

// 容器被外部删除（docker rm / Docker Desktop 重置）：库里仍记「运行中」就是虚报，
// 校准必须将运行态拉齐到停止并回流快照，同时保留已安装记录（数据与重建入口不动）
func TestCalibrate_ExternallyRemoved_DropsRunningButKeepsInstalled(t *testing.T) {
	l, _, s, em := newSvc()
	_ = s.SetInstalled("mysql", "8.4", true)
	_ = s.SetRunning("mysql", "8.4", true)
	// Docker 世界里容器不存在（未被 ManagedContainers 列出）

	res, err := l.Calibrate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Corrections) != 1 || res.Corrections[0].Running {
		t.Fatalf("容器已消失应产出 running=false 修正，实得 %+v", res.Corrections)
	}
	if contains(s.snap.Running["mysql"], "8.4") {
		t.Fatal("校准后不应再把已消失的容器记为运行")
	}
	if !contains(s.snap.Installed["mysql"], "8.4") {
		t.Fatal("已安装记录不得被校准删除（卸载需用户显式操作）")
	}
	if !em.has("state:changed") {
		t.Fatalf("运行态修正应回流 state:changed，实得 %v", em.events)
	}
}

// 重建失败（Pre-Clean 已删旧容器、新容器建不起来）：本体错误已上抛，
// 「每任务后校准」必须把库里残留的「运行中」抹掉，否则 UI 永远虚报绿点且启动永久失败
func TestReinstall_FailedCreate_CalibrateClearsGhostRunning(t *testing.T) {
	l, d, s, _ := newSvc()
	ctx := context.Background()
	if err := l.Install(ctx, model.KindPHP, "8.4"); err != nil {
		t.Fatal(err)
	}
	d.createErr = errors.New("Bind for 0.0.0.0:9000 failed: port is already allocated")

	if err := l.Reinstall(ctx, model.KindPHP, "8.4"); err == nil {
		t.Fatal("建容器失败应上抛错误")
	}
	if _, ok := d.containers["phpo-php-8.4"]; ok {
		t.Fatal("回滚后不应残留半相容器")
	}
	if !contains(s.snap.Running["php"], "8.4") {
		t.Fatal("本用例前提：失败路径未写库，库里仍虚报运行中")
	}
	if _, err := l.Calibrate(ctx); err != nil {
		t.Fatal(err)
	}
	if contains(s.snap.Running["php"], "8.4") {
		t.Fatal("任务后校准应抹掉虚报的运行态")
	}
	if !contains(s.snap.Installed["php"], "8.4") {
		t.Fatal("仍视为已安装，用户可从卡片重建自救")
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

// ---- 需求①④：手动「同步状态」的全量口径（§5.19） ----

// 容器被第三方工具删掉：全量同步点名缺失态并随快照回流，但 installed 一字不动（卸载必须是用户显式操作）
func TestSyncAll_ExternallyRemoved_ReportsGapAndKeepsInstalled(t *testing.T) {
	l, d, s, em := newSvc()
	_ = s.SetInstalled("php", "8.4", true)
	_ = s.SetRunning("php", "8.4", true)
	d.localImages["php:8.4-fpm"] = true // 镜像还在，只剩容器被删

	if _, err := l.SyncAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.snap.Gaps) != 1 || s.snap.Gaps[0].Reason != model.GapContainer || s.snap.Gaps[0].Ref != "phpo-php-8.4" {
		t.Fatalf("应点名一条容器缺失，实得 %+v", s.snap.Gaps)
	}
	if !contains(s.snap.Installed["php"], "8.4") {
		t.Fatal("外部删容器不得被当成卸载（数据卷与重建入口要留着）")
	}
	if contains(s.snap.Running["php"], "8.4") {
		t.Fatal("运行态仍要拉齐到停止")
	}
	if len(em.drifts) != 1 || len(em.drifts[0].Gaps) != 1 {
		t.Fatalf("漂移事件应带缺失项供日志逐行点名，实得 %+v", em.drifts)
	}
	if !em.has("state:changed") {
		t.Fatalf("缺失态须随快照回流界面，实得 %v", em.events)
	}
}

// 连镜像也被删（docker rmi / Docker Desktop 重置）：容器在、库里也没记运行，运行态无修正，
// 但全量同步仍要说得出「重建会失败」——这一格正是轻量校准覆盖不到的
func TestSyncAll_MissingImage_ReportsImageGap(t *testing.T) {
	l, d, s, em := newSvc()
	_ = s.SetInstalled("mysql", "8.4", true)
	d.containers["phpo-mysql-8.4"] = false // 在、已停止；localImages 为空即镜像不在本机

	if _, err := l.SyncAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.snap.Gaps) != 1 || s.snap.Gaps[0].Reason != model.GapImage || s.snap.Gaps[0].Ref != "mysql:8.4" {
		t.Fatalf("应点名一条镜像缺失，实得 %+v", s.snap.Gaps)
	}
	if len(em.drifts) != 1 {
		t.Fatalf("有缺失即须发漂移事件，实得 %v", em.events)
	}
}

// php 库里记着启用过扩展、固化镜像却不在本机：容器只能退回基座，那几项其实没在跑（§5.16.2）
func TestSyncAll_MissingExtensionImage(t *testing.T) {
	l, d, s, _ := newSvc()
	_ = s.SetInstalled("php", "8.4", true)
	s.snap.PHPExtensions["8.4"] = []string{"redis"}
	d.containers["phpo-php-8.4"] = true
	d.localImages["php:8.4-fpm"] = true // 基座在，固化镜像不在

	if _, err := l.SyncAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.snap.Gaps) != 1 || s.snap.Gaps[0].Reason != model.GapExtImage || s.snap.Gaps[0].Ref != "phpo/php:8.4" {
		t.Fatalf("应点名一条扩展固化镜像缺失，实得 %+v", s.snap.Gaps)
	}
}

// 固化镜像在本机时以它为准，不重复报基座镜像缺失（php 一个版本只有一条「所需镜像」）
func TestSyncAll_CommittedImageCountsAsSatisfied(t *testing.T) {
	l, d, s, _ := newSvc()
	_ = s.SetInstalled("php", "8.4", true)
	s.snap.PHPExtensions["8.4"] = []string{"redis"}
	d.containers["phpo-php-8.4"] = true
	d.localImages["phpo/php:8.4"] = true // 固化镜像在；基座反而不在（正常：容器跑固化镜像）

	if _, err := l.SyncAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.snap.Gaps) != 0 {
		t.Fatalf("运行所需镜像在本机即无缺失，实得 %+v", s.snap.Gaps)
	}
}

// 缺失项与上次一致即整体静默：重复点「同步状态」不得往抽屉刷同样的行
func TestSyncAll_Idempotent_SilentOnSecondRun(t *testing.T) {
	l, _, s, em := newSvc()
	_ = s.SetInstalled("php", "8.4", true)
	_ = s.SetRunning("php", "8.4", true)

	if _, err := l.SyncAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := len(em.events)
	if _, err := l.SyncAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(em.events) != before {
		t.Fatalf("第二次同步应静默，实得新增 %v", em.events[before:])
	}
}

// 用户把容器建回来（或重装）后缺失态必须清零，不是永久烙印
func TestSyncAll_GapClearsAfterRestore(t *testing.T) {
	l, d, s, em := newSvc()
	_ = s.SetInstalled("php", "8.4", true)
	d.localImages["php:8.4-fpm"] = true

	if _, err := l.SyncAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.snap.Gaps) != 1 {
		t.Fatalf("前提：容器缺席应点名一条，实得 %+v", s.snap.Gaps)
	}
	d.containers["phpo-php-8.4"] = false
	em.events, em.drifts = nil, nil

	if _, err := l.SyncAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.snap.Gaps) != 0 {
		t.Fatalf("容器回来后缺失态应清零，实得 %+v", s.snap.Gaps)
	}
	if len(em.drifts) != 1 || len(em.drifts[0].Gaps) != 0 {
		t.Fatalf("清零也要回流一次，且漂移事件不带缺失项，实得 %+v", em.drifts)
	}
}

// 镜像探针本身报错不得当作「镜像不在本机」：一次 Docker 抖动不能说成缺失（§5.14.12 禁止静默）
func TestSyncAll_ImageProbeError_Propagates(t *testing.T) {
	l, d, s, em := newSvc()
	_ = s.SetInstalled("php", "8.4", true)
	d.containers["phpo-php-8.4"] = true
	d.imageErr = errors.New("docker daemon unavailable")

	if _, err := l.SyncAll(context.Background()); err == nil {
		t.Fatal("探针报错应上抛")
	}
	if len(em.events) != 0 {
		t.Fatalf("报错时不得发任何事件，实得 %v", em.events)
	}
	if len(s.snap.Gaps) != 0 {
		t.Fatalf("报错时不得落缺失态，实得 %+v", s.snap.Gaps)
	}
}

// 分档：轻量校准（启动 / 每任务后）只判容器存在性，不拨镜像探针
func TestCalibrate_LightweightSkipsImageAudit(t *testing.T) {
	l, d, s, _ := newSvc()
	_ = s.SetInstalled("php", "8.4", true)
	_ = s.SetRunning("php", "8.4", true)
	d.containers["phpo-php-8.4"] = false // 容器在、已停止；镜像不在本机

	if _, err := l.Calibrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d.imageChecks != 0 {
		t.Fatalf("轻量校准不得逐个探镜像，实得 %d 次", d.imageChecks)
	}
	if len(s.snap.Gaps) != 0 {
		t.Fatalf("容器在即无缺失，实得 %+v", s.snap.Gaps)
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

// 容器被外部删掉（docker rm / Desktop 重置）后点「启动」：已安装即按当前配置重建并拉起，
// 而不是抛一句 no such container 把用户逼进死路（校准已把运行态拉回停止，此处负责把服务真起来）
func TestStart_RecreatesMissingContainer(t *testing.T) {
	l, d, s, em := newSvc()
	ctx := context.Background()
	if err := l.Install(ctx, model.KindMySQL, "8.4"); err != nil {
		t.Fatal(err)
	}
	delete(d.containers, "phpo-mysql-8.4") // Docker 侧容器消失
	_ = s.SetRunning("mysql", "8.4", false)
	s.setPort("mysql", "8.4", 3307) // 重建要按当前落库端口发布，不是注册表默认

	if err := l.Start(ctx, model.KindMySQL, "8.4"); err != nil {
		t.Fatalf("容器缺失时启动应自愈重建: %v", err)
	}
	if !d.containers["phpo-mysql-8.4"] {
		t.Fatal("启动后容器应在运行")
	}
	if got := publishedPort(d, "phpo-mysql-8.4", "3306/tcp"); got != "3307" {
		t.Fatalf("重建应按落库端口 3307 发布，实得 %q", got)
	}
	if !contains(s.snap.Running["mysql"], "8.4") {
		t.Fatal("启动后应回流 running=true")
	}
	if !em.has("state:changed") {
		t.Fatalf("应发 state:changed 供前端同步，实得 %v", em.events)
	}
}

// 未安装的服务没有可重建的容器：启动仍应拒绝且不建容器（门禁在 preflight，此处是后门防线）
func TestStart_WithoutInstalledDoesNotCreate(t *testing.T) {
	l, d, _, _ := newSvc()
	if err := l.Start(context.Background(), model.KindMySQL, "8.4"); err == nil {
		t.Fatal("未安装的服务启动应报错")
	}
	if len(d.containers) != 0 {
		t.Fatalf("报错后不应残留容器，实得 %v", d.containers)
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

// TestReinstall_PhpUsesCommittedExtImage 启用过扩展的 php 版本，重建必须用固化镜像 phpo/php:{v}。
// 从基座 php:{v}-fpm 建容器等于把用户编译进镜像的扩展悄悄抹掉，而 php_extensions 表还记着它们
// （§5.13.1 一致性 / 硬红线 4：展示的权威态与真实跑的东西必须是一回事）。
func TestReinstall_PhpUsesCommittedExtImage(t *testing.T) {
	l, d, _, _ := newSvc()
	ctx := context.Background()
	if err := l.Install(ctx, model.KindPHP, "8.4"); err != nil {
		t.Fatal(err)
	}
	if got := d.lastSpec["phpo-php-8.4"].Image; got != "php:8.4-fpm" {
		t.Fatalf("本机无固化镜像时应装基座，实得 %q", got)
	}
	d.localImages["phpo/php:8.4"] = true // 扩展链路 docker commit 的产物

	if err := l.Reinstall(ctx, model.KindPHP, "8.4"); err != nil {
		t.Fatal(err)
	}
	if got := d.lastSpec["phpo-php-8.4"].Image; got != "phpo/php:8.4" {
		t.Fatalf("已固化扩展的 php 重建应用 phpo/php:8.4，实得 %q", got)
	}
}

// fakeExtImageLoader 假离线缓存：本机没有固化镜像时能否从缓存 tar 零网络载入
type fakeExtImageLoader struct {
	ref     string
	ok      bool
	err     error
	calls   int
	version string
}

func (c *fakeExtImageLoader) LoadExtImage(_ context.Context, version string) (string, bool, error) {
	c.calls++
	c.version = version
	return c.ref, c.ok, c.err
}

// TestReinstall_PhpLoadsExtImageFromCache 固化镜像被外部删掉（docker rmi / 换机）时，
// 重建必须先试离线缓存的 image-extensions.tar 零网络载入——退回基座即抹掉已启用扩展（§5.14.3 第一优先级）。
func TestReinstall_PhpLoadsExtImageFromCache(t *testing.T) {
	l, d, _, _ := newSvc()
	ld := &fakeExtImageLoader{ref: "phpo/php:8.4", ok: true}
	l.SetExtImageLoader(ld)
	ctx := context.Background()
	if err := l.Install(ctx, model.KindPHP, "8.4"); err != nil {
		t.Fatal(err)
	}
	if got := d.lastSpec["phpo-php-8.4"].Image; got != "phpo/php:8.4" {
		t.Fatalf("缓存有固化镜像时应用它建容器，实得 %q", got)
	}
	if ld.calls == 0 || ld.version != "8.4" {
		t.Fatalf("本机无固化镜像必须查离线缓存，实得 calls=%d version=%q", ld.calls, ld.version)
	}
}

// TestReinstall_ExtImageCacheMissFallsBackToBase 缓存也没有固化镜像才退回基座（首装无扩展是正常路径）
func TestReinstall_ExtImageCacheMissFallsBackToBase(t *testing.T) {
	l, d, _, _ := newSvc()
	l.SetExtImageLoader(&fakeExtImageLoader{})
	ctx := context.Background()
	if err := l.Install(ctx, model.KindPHP, "8.4"); err != nil {
		t.Fatal(err)
	}
	if got := d.lastSpec["phpo-php-8.4"].Image; got != "php:8.4-fpm" {
		t.Fatalf("缓存缺席时应装基座，实得 %q", got)
	}
}

// TestReinstall_ExtImageLoadFailureIsNotSilent 缓存载入报错不得静默退回基座：
// 那会把「扩展没了」伪装成一次正常重建（§5.14.12 禁止静默）
func TestReinstall_ExtImageLoadFailureIsNotSilent(t *testing.T) {
	l, _, _, _ := newSvc()
	l.SetExtImageLoader(&fakeExtImageLoader{err: errors.New("缓存 tar 校验失败")})
	if err := l.Install(context.Background(), model.KindPHP, "8.4"); err == nil {
		t.Fatal("载入固化镜像失败应上抛，不得退回基座")
	}
}

// TestReinstall_ImageProbeFailureIsNotTreatedAsMissing 镜像库探针本身报错不能静默当作「本机没有」：
// 退回基座即抹掉扩展（§5.14.12 禁止静默）。宁可重建失败，也不装一个配置对不上的容器。
func TestReinstall_ImageProbeFailureIsNotTreatedAsMissing(t *testing.T) {
	l, d, _, _ := newSvc()
	ctx := context.Background()
	if err := l.Install(ctx, model.KindPHP, "8.4"); err != nil {
		t.Fatal(err)
	}
	d.imageErr = errors.New("docker daemon 不可用")
	if err := l.Reinstall(ctx, model.KindPHP, "8.4"); err == nil {
		t.Fatal("php 重建时镜像探针报错应上抛，不得退回基座镜像")
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
