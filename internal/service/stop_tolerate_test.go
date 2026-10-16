// 停止/卸载这一对判据的用例：引擎报错不等于没做成——现读实测已停/已没了就按目标达成收尾，
// 引擎原文作为一行提醒进抽屉；实测仍在跑/仍在库里才判失败。
// 起因是真机取证：Podman 5.7 rootless 停容器后清网络时报 `rootless netns: kill network process: permission denied`，
// 容器其实已经停下，phpo 却把这一单判成失败、连运行态都没落库。
package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"phpo/internal/engine"
	"phpo/internal/model"
)

// netnsRaw 真机那句引擎原文（用例要验它就原样出现在提醒行的参数里）
const netnsRaw = "Error response from daemon: removing container 5264afb582e51d7ecd7cd6874d4b5d3a " +
	"network: 1 error occurred:\n\t* rootless netns: kill network process: permission denied"

func netnsTolerated(name, op string) error {
	return &engine.ToleratedError{Name: name, Op: op, Msg: netnsRaw}
}

// engineErr 普通引擎错误：不是「实测已达成」那一类，照样判死
func engineErr(msg string) error { return errors.New(msg) }

// lineOf 取本次捕获里那条码对应的日志行
func lineOf(em *recEmitter, code string) (model.TaskLogEvent, bool) {
	for _, l := range em.lines {
		if l.Code == code {
			return l, true
		}
	}
	return model.TaskLogEvent{}, false
}

// runPHP 摆一台「库里装着、此刻在跑」的 php/8.0（lifecycle 层用例的前置）
func runPHP(t *testing.T, l *LifecycleService, d *fakeDocker, s *fakeStore) {
	t.Helper()
	ctx := context.Background()
	if err := l.Install(ctx, model.KindPHP, "8.0"); err != nil {
		t.Fatal(err)
	}
	if err := l.Start(ctx, model.KindPHP, "8.0"); err != nil {
		t.Fatal(err)
	}
	if !d.containers["phpo-php-8.0"] || !contains(s.snap.Running["php"], "8.0") {
		t.Fatal("前置：php/8.0 应在运行")
	}
}

// 判据 1A（lifecycle 层）：引擎报错但实测已停——照常 commit(running=false)，提醒原样上抛给步骤去落
func TestLifecycleStop_ToleratedStillCommits(t *testing.T) {
	l, d, s, _ := newSvc()
	runPHP(t, l, d, s)
	d.stopErr = netnsTolerated("phpo-php-8.0", "stop")

	err := l.Stop(context.Background(), model.KindPHP, "8.0")
	if engine.AsTolerated(err) == nil {
		t.Fatalf("应把引擎原文作为提醒上抛，好让步骤落一行日志，实得 %v", err)
	}
	if contains(s.snap.Running["php"], "8.0") {
		t.Error("实测已停：库里不得仍记着 php/8.0 在运行")
	}
	if !contains(s.snap.Installed["php"], "8.0") {
		t.Error("停止不改安装态")
	}
}

// 判据 1A 的反面：报错后现读仍在跑＝真没停下，照常判失败且不改运行态
func TestLifecycleStop_RealFailureStillAborts(t *testing.T) {
	l, d, s, _ := newSvc()
	runPHP(t, l, d, s)
	d.stopErr = engineErr("permission denied")

	err := l.Stop(context.Background(), model.KindPHP, "8.0")
	if err == nil || engine.AsTolerated(err) != nil {
		t.Fatalf("实测仍在跑要照旧判失败，实得 %v", err)
	}
	if !contains(s.snap.Running["php"], "8.0") {
		t.Error("判失败的这一单不得改写运行态")
	}
}

// 判据 2A（lifecycle 层）：删除报错但容器实测已经没了——照常置未安装并回流快照，提醒上抛
func TestLifecycleRemove_ToleratedStillMarksUninstalled(t *testing.T) {
	l, d, s, em := newSvc()
	ctx := context.Background()
	if err := l.Install(ctx, model.KindMySQL, "8.4"); err != nil {
		t.Fatal(err)
	}
	d.removeErr = netnsTolerated("phpo-mysql-8.4", "remove")

	err := l.Remove(ctx, model.KindMySQL, "8.4")
	if engine.AsTolerated(err) == nil {
		t.Fatalf("容器真没了应判达成 + 提醒，实得 %v", err)
	}
	if contains(s.snap.Installed["mysql"], "8.4") {
		t.Error("实测已删除：库里不得仍记为已安装")
	}
	if !em.has("state:changed") {
		t.Error("卸载收口要照常回流快照")
	}
}

// 判据 2A 的反面：删除报错且容器还在库里＝真失败，未安装态不得被改写
func TestLifecycleRemove_RealFailureKeepsInstalled(t *testing.T) {
	l, d, s, _ := newSvc()
	ctx := context.Background()
	if err := l.Install(ctx, model.KindMySQL, "8.4"); err != nil {
		t.Fatal(err)
	}
	d.removeErr = engineErr("device or resource busy")

	err := l.Remove(ctx, model.KindMySQL, "8.4")
	if err == nil || engine.AsTolerated(err) != nil {
		t.Fatalf("容器还在时必须判失败，实得 %v", err)
	}
	if !contains(s.snap.Installed["mysql"], "8.4") {
		t.Error("真失败不得把已安装改成未安装")
	}
}

// 界面口径：点「停止」这一单以成功收尾，抽屉里多一行 dim 提醒，带容器名与引擎原文
func TestAppService_Stop_ToleratedSucceedsWithWarnLine(t *testing.T) {
	a, d, s, em := newAutoApp(t, nil)
	ctx := context.Background()
	if err := s.SetInstalled("php", "8.0", true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRunning("php", "8.0", true); err != nil {
		t.Fatal(err)
	}
	d.containers["phpo-php-8.0"] = true
	d.stopErr = netnsTolerated("phpo-php-8.0", "stop")

	if err := a.Stop(ctx, model.KindPHP, "8.0"); err != nil {
		t.Fatalf("实测已停这一单要按完成收尾: %v", err)
	}
	line, ok := lineOf(em, logStopNetnsWarn)
	if !ok {
		t.Fatalf("应落一行 %s 提醒，实得码 %v", logStopNetnsWarn, em.codes())
	}
	if line.Level != model.LogDim {
		t.Errorf("提醒是 dim 级（这一单没失败），实得 %s", line.Level)
	}
	if line.Params["name"] != "phpo-php-8.0" || !strings.Contains(line.Params["reason"], "rootless netns") {
		t.Errorf("提醒要点名容器并带上引擎原文，实得 %+v", line.Params)
	}
	if contains(s.snap.Running["php"], "8.0") {
		t.Error("这一单要照常把运行态落回停止")
	}
}

// 真失败那一支不受影响：仍判失败，且不会误落提醒行
func TestAppService_Stop_RealFailureStillFails(t *testing.T) {
	a, d, s, em := newAutoApp(t, nil)
	ctx := context.Background()
	if err := s.SetInstalled("php", "8.0", true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRunning("php", "8.0", true); err != nil {
		t.Fatal(err)
	}
	d.containers["phpo-php-8.0"] = true
	d.stopErr = engineErr("permission denied")

	if err := a.Stop(ctx, model.KindPHP, "8.0"); err == nil {
		t.Fatal("实测仍在跑要照旧判失败")
	}
	if _, ok := lineOf(em, logStopNetnsWarn); ok {
		t.Error("真失败不该落这条提醒行")
	}
	if !contains(s.snap.Running["php"], "8.0") {
		t.Error("判失败的这一单不得改写运行态")
	}
}

// 卸载同一把尺子：容器实测没了 → 这一单成功、置未安装、落一行提醒
func TestAppService_Remove_ToleratedSucceedsWithWarnLine(t *testing.T) {
	a, d, s, em := newAutoApp(t, nil)
	ctx := context.Background()
	if err := s.SetInstalled("redis", "8", true); err != nil {
		t.Fatal(err)
	}
	d.containers["phpo-redis-8"] = true
	d.removeErr = netnsTolerated("phpo-redis-8", "remove")

	if err := a.Remove(ctx, model.KindRedis, "8"); err != nil {
		t.Fatalf("容器真没了这一单要按完成收尾: %v", err)
	}
	if _, ok := lineOf(em, logStopNetnsWarn); !ok {
		t.Fatalf("应落一行 %s 提醒，实得码 %v", logStopNetnsWarn, em.codes())
	}
	if contains(s.snap.Installed["redis"], "8") {
		t.Error("实测已删除：库里不得仍记为已安装")
	}
}

// 后端那句中文 fallback 必须与 zh-CN 语言包那条逐字一致（总纲 §5.15：不得把中文改得与 fallback 不一致）。
// 改了语言包忘了改构造点（或反过来）在这里红——后果是缺码兜底与中文界面各说一套。
func TestStopNetnsWarnFallbackMatchesLocale(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "locales", "zh-CN.ts"))
	if err != nil {
		t.Fatal(err)
	}
	want := ""
	for _, line := range strings.Split(string(b), "\n") {
		if m := regexp.MustCompile(`^\s*"` + regexp.QuoteMeta(logStopNetnsWarn) + `":\s*"(.*)",?\s*$`).FindStringSubmatch(line); m != nil {
			want = m[1]
		}
	}
	if want == "" {
		t.Fatalf("zh-CN.ts 没找到 %s 这一条", logStopNetnsWarn)
	}
	name := "phpo-php-8.0"
	a, d, s, em := newAutoApp(t, nil)
	if err := s.SetInstalled("php", "8.0", true); err != nil {
		t.Fatal(err)
	}
	d.containers["phpo-php-8.0"] = true
	d.stopErr = netnsTolerated(name, "stop")
	if err := a.Stop(context.Background(), model.KindPHP, "8.0"); err != nil {
		t.Fatal(err)
	}
	line, ok := lineOf(em, logStopNetnsWarn)
	if !ok {
		t.Fatalf("没落提醒行，实得码 %v", em.codes())
	}
	got := strings.ReplaceAll(strings.ReplaceAll(want, "{name}", name), "{reason}", netnsRaw)
	if line.Text != got {
		t.Errorf("后端 fallback 与 zh-CN 语言包不一致:\n  fallback: %q\n  语言包填回: %q", line.Text, got)
	}
}
