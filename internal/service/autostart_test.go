// 开机自启的用例：清单解析、三种跳过、启动失败点名、引擎迟迟不起、一次进程只跑一遍、对象图换过就不动。
// 全用假件（无真实引擎），等待窗口临时缩短成毫秒级——运行时那两分钟的取值不在这里验。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/task"
)

// recEmitter 连消息码一起捕获（fakeEmitter 只留文本，这里要断言的是「发的是哪条码」）
type recEmitter struct {
	events []string
	lines  []model.TaskLogEvent
}

func (e *recEmitter) Emit(event string, payload any) {
	e.events = append(e.events, event)
	if l, ok := payload.(model.TaskLogEvent); ok {
		e.lines = append(e.lines, l)
	}
}

// codes 本次捕获到的消息码序列（按发出次序）
func (e *recEmitter) codes() []string {
	out := make([]string, 0, len(e.lines))
	for _, l := range e.lines {
		out = append(out, l.Code)
	}
	return out
}

func (e *recEmitter) hasCode(code string) bool { return contains(e.codes(), code) }

func (e *recEmitter) has(name string) bool { return contains(e.events, name) }

// newAutoApp 接线一份可注入假件的 AppService；probeErr 非空即模拟「容器引擎拨不通」
func newAutoApp(t *testing.T, probeErr error) (*AppService, *fakeDocker, *fakeStore, *recEmitter) {
	t.Helper()
	home := t.TempDir()
	d, s := newFakeDocker(), newFakeStore()
	em := &recEmitter{}
	env := config.DerivePaths(home, home+"/www")
	lc := NewLifecycle(d, s, em, env, s)
	return NewAppService(lc, task.NewManager(em), &fakeEnsurer{}, okProbe{err: probeErr}, env), d, s, em
}

// shortenAutoStartWait 把等引擎的两档缩到毫秒级，用例结束复原（包级取值只这一处会动）
func shortenAutoStartWait(t *testing.T) {
	t.Helper()
	maxOld, everyOld := autoStartWaitMax, autoStartProbeEvery
	autoStartWaitMax, autoStartProbeEvery = 40*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { autoStartWaitMax, autoStartProbeEvery = maxOld, everyOld })
}

func TestAutoStartTargets_ParsesRefsAndDropsMalformed(t *testing.T) {
	// 后四项形状/版本不安全：拿它们去拼容器名与服务数据目录等于让一个坏键走进宿主路径
	got := autoStartTargets([]string{"php/8.4", "mysql/8.4", "redis", "pgsql/", "/17", "  ", "php/..", "php/8\x00", `php/8\..`})
	want := []string{"phpo-php-8.4", "phpo-mysql-8.4"}
	var names []string
	for _, tg := range got {
		names = append(names, tg.name)
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("targets = %v，期望 %v", names, want)
	}
	if got[0].kind != model.KindPHP || got[0].version != "8.4" {
		t.Fatalf("第一项应拆成 php/8.4，实得 %s/%s", got[0].kind, got[0].version)
	}
	if len(autoStartTargets(nil)) != 0 {
		t.Fatal("空清单不该产出任何目标")
	}
}

// 装过且停着的补起来；已经在跑的跳过；库里没记着装过的跳过并点名——三行日志各归各的码。
func TestAppService_AutoStart_StartsStoppedSkipsRunningAndAbsent(t *testing.T) {
	a, d, s, em := newAutoApp(t, nil)
	_ = s.SetInstalled("php", "8.4", true)
	d.containers["phpo-php-8.4"] = false // 装过、容器还在、此刻没跑：这一档要补起来
	_ = s.SetInstalled("nginx", "alpine", true)
	d.containers["phpo-nginx-alpine"] = true // 已经在跑：不得重复启动
	if err := a.AutoStart(context.Background(), []string{"php/8.4", "nginx/alpine", "mysql/8.4"}, alwaysTrue); err != nil {
		t.Fatal(err)
	}
	if !d.containers["phpo-php-8.4"] {
		t.Error("停着的 php/8.4 应被补启动")
	}
	if !em.hasCode(logAutoStartDone) || !em.hasCode(logAutoStartSkipRunning) || !em.hasCode(logAutoStartSkipAbsent) {
		t.Errorf("应同时出现补启动/已在跑/未安装三种码，实得 %v", em.codes())
	}
	// mysql/8.4 从没装过：不得为它建任何容器
	if _, ok := d.containers["phpo-mysql-8.4"]; ok {
		t.Error("未安装的 mysql/8.4 不该被凭空建出来")
	}
	if !contains(s.snap.Running["php"], "8.4") {
		t.Errorf("补启动后应记为运行，实得 %v", s.snap.Running["php"])
	}
	// 任务标签的码随 task:log 一起发出（抽屉那行走 task.autoStart）
	if !em.has("task:done") {
		t.Errorf("应发 task:done，实得 %v", em.events)
	}
	for _, l := range em.lines {
		if l.Code == logAutoStartDone && l.Params["name"] != "phpo-php-8.4" {
			t.Errorf("补启动那行的参数应点名容器名，实得 %q", l.Params["name"])
		}
	}
}

// redis/8 库里装过、容器不在了：Start 走幂等重建，重建需要镜像——这里只验「它没有被当成未安装跳过」。
func TestAppService_AutoStart_RebuildsMissingContainer(t *testing.T) {
	a, d, s, em := newAutoApp(t, nil)
	_ = s.SetInstalled("redis", "8", true)
	if err := a.AutoStart(context.Background(), []string{"redis/8"}, alwaysTrue); err != nil {
		t.Fatal(err)
	}
	if em.hasCode(logAutoStartSkipAbsent) {
		t.Error("库里有记录就不该走「未安装」那一档")
	}
	if !d.containers["phpo-redis-8"] {
		t.Errorf("重建后应存在且运行，实得 %v", d.containers)
	}
	if !em.hasCode(logAutoStartDone) {
		t.Errorf("补起来一行应点名成功，实得 %v", em.codes())
	}
}

// 单颗失败不判死整单：排在后面的照常补起来，但整单以失败收尾，并把每颗失败点名到那一行。
func TestAppService_AutoStart_OneFailureDoesNotAbortOthers(t *testing.T) {
	a, d, s, em := newAutoApp(t, nil)
	_ = s.SetInstalled("php", "8.4", true)
	d.containers["phpo-php-8.4"] = false // 容器还在：直接起，碰不到 createErr
	_ = s.SetInstalled("nginx", "alpine", true)
	_ = s.SetInstalled("redis", "8", true) // 这两颗容器没了 → 走重建 → 撞 createErr
	d.createErr = errors.New("port 80 is already allocated")
	err := a.AutoStart(context.Background(), []string{"php/8.4", "nginx/alpine", "redis/8"}, alwaysTrue)
	if err == nil {
		t.Fatal("有颗失败，整单应以失败收尾")
	}
	if !strings.Contains(err.Error(), "phpo-nginx-alpine") || !strings.Contains(err.Error(), "already allocated") {
		t.Errorf("聚合错误应点名是哪一颗、为什么，实得 %v", err)
	}
	if !strings.Contains(err.Error(), "; ") {
		t.Errorf("多颗失败应以分号拼接（不用中文分隔符），实得 %v", err)
	}
	if !d.containers["phpo-php-8.4"] {
		t.Error("前面那颗失败不得连累 php 补启动")
	}
	if !d.containers["phpo-php-8.4"] || len(d.containers) != 1 {
		t.Errorf("失败的容器不得留下半拉状态，实得 %v", d.containers)
	}
	if !em.hasCode(logAutoStartFail) {
		t.Errorf("失败那一行应带 log.autoStartFail 码，实得 %v", em.codes())
	}
}

// 引擎迟迟不起：等满窗口后仍建一单，日志点名「等不到所以跳过」，任务以失败终态收尾，容器一个都不碰。
func TestAppService_AutoStart_EngineNeverReady(t *testing.T) {
	shortenAutoStartWait(t)
	a, d, _, em := newAutoApp(t, engine.ErrDockerNotRunning)
	started := len(d.containers) // 这一档一个容器都不该动
	if err := a.AutoStart(context.Background(), []string{"php/8.4"}, alwaysTrue); err == nil {
		t.Fatal("等不到引擎应当以失败收尾，让界面看得见这一单没成")
	}
	if len(d.containers) != started {
		t.Errorf("引擎没就绪不得动任何容器，实得 %v", d.containers)
	}
	if !em.hasCode(logAutoStartTimeout) {
		t.Errorf("应发一行等引擎超时的日志，实得 %v", em.codes())
	}
	for _, l := range em.lines {
		if l.Code == logAutoStartTimeout && l.Params["wait"] == "" {
			t.Error("超时那行必须带上等了多久（秒）")
		}
	}
	if !em.has("task:done") {
		t.Errorf("应发 task:done（失败终态），实得 %v", em.events)
	}
}

// 引擎晚起但等得到：前几拨拨不通、后面拨通了，就该照常补起来——这是「延迟启动」那一条的正解。
func TestAppService_AutoStart_WaitsForLateEngine(t *testing.T) {
	shortenAutoStartWait(t)
	a, d, s, em := newAutoApp(t, engine.ErrDockerNotRunning)
	_ = s.SetInstalled("php", "8.4", true)
	d.containers["phpo-php-8.4"] = false
	// 第二拨起就当作引擎起来了：探针按调用次数转好（12 秒轮询那条路不在此验）
	calls := 0
	a.probe = flipProbe{every: 2, onCall: &calls, err: engine.ErrDockerNotRunning}
	if err := a.AutoStart(context.Background(), []string{"php/8.4"}, alwaysTrue); err != nil {
		t.Fatalf("等得到就该成，实得 %v", err)
	}
	if !d.containers["phpo-php-8.4"] {
		t.Error("引擎后来就绪，php/8.4 应被补启动")
	}
	if !em.hasCode(logAutoStartDone) {
		t.Errorf("应有一行补启动成功，实得 %v", em.codes())
	}
}

// 一次进程只跑一遍：重复调用不再建单（延迟校准那一步会再走到这里）。
func TestAppService_AutoStart_RunsOncePerProcess(t *testing.T) {
	a, d, s, em := newAutoApp(t, nil)
	_ = s.SetInstalled("php", "8.4", true)
	d.containers["phpo-php-8.4"] = false
	if err := a.AutoStart(context.Background(), []string{"php/8.4"}, alwaysTrue); err != nil {
		t.Fatal(err)
	}
	before := len(em.events)
	if err := a.AutoStart(context.Background(), []string{"php/8.4"}, alwaysTrue); err != nil {
		t.Fatal(err)
	}
	if len(em.events) != before {
		t.Errorf("第二次调用不得再发任何事件，实得多发 %v", em.events[before:])
	}
	// 空清单连「跑过一遍」都不算：不消耗这一次机会
	a2, _, s2, em2 := newAutoApp(t, nil)
	_ = s2.SetInstalled("php", "8.4", true)
	if err := a2.AutoStart(context.Background(), nil, alwaysTrue); err != nil {
		t.Fatal(err)
	}
	if len(em2.events) != 0 {
		t.Errorf("空清单不该建单，实得 %v", em2.events)
	}
	if err := a2.AutoStart(context.Background(), []string{"php/8.4"}, alwaysTrue); err != nil {
		t.Fatal(err)
	}
	if !em2.has("task:done") {
		t.Errorf("空清单之后仍应能跑真正那一遍，实得 %v", em2.events)
	}
}

// 等待期间对象图被换过（用户改了自定义根）：旧图上的自启不补，一个字都不发。
func TestAppService_AutoStart_StaleGraphSkips(t *testing.T) {
	shortenAutoStartWait(t)
	a, d, s, em := newAutoApp(t, engine.ErrDockerNotRunning)
	_ = s.SetInstalled("php", "8.4", true)
	d.containers["phpo-php-8.4"] = false
	if err := a.AutoStart(context.Background(), []string{"php/8.4"}, func() bool { return false }); err != nil {
		t.Fatal(err)
	}
	if len(em.events) != 0 {
		t.Errorf("图已换过就该静默跳过，实得 %v", em.events)
	}
	if d.containers["phpo-php-8.4"] {
		t.Error("图已换过不得动旧图上的容器")
	}
}

// alwaysTrue 对象图还是当下那张（多数用例不关心这件事）
func alwaysTrue() bool { return true }

// flipProbe 前 every-1 次拨不通、第 every 次起就绪：验「引擎晚起也照样自启」
type flipProbe struct {
	every  int
	onCall *int
	err    error
}

func (p flipProbe) Detect(context.Context) (string, error) {
	*p.onCall++
	if *p.onCall >= p.every {
		return "28.3.2", nil
	}
	return "", p.err
}

// 自启用的消息码必须两侧语言包都有（与 task 包那条断言同一口径，这里锁住 service 侧的常量）
func TestAutoStartLogCodesMatchLocales(t *testing.T) {
	for _, c := range []string{logAutoStartSkipAbsent, logAutoStartSkipRunning, logAutoStartDone, logAutoStartFail, logAutoStartTimeout} {
		if !strings.HasPrefix(c, "log.autoStart") {
			t.Errorf("%q 应登记在 log.autoStart* 之下", c)
		}
	}
	if task.MsgTaskAutoStart != "task.autoStart" {
		t.Errorf("任务标签码应为 task.autoStart，实得 %q", task.MsgTaskAutoStart)
	}
	if opAutoStart == "" {
		t.Error("自启任务要有动作名")
	}
	var _ engine.Probe = okProbe{}
}
