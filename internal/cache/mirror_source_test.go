// 镜像源参与拉取链路的编排单测：最快源优先、逐个回落、全都不行才直连官方，
// 以及「拉取用的是哪一个源」必须随 cache:miss 的 source 字段回流界面。
//
// 这一层要锁死的是「源只是加速手段、不得变成新的失败面」：配了源不等于必须用源，
// 源半路限流或没同步这个镜像时，仍要能像没配源那样把镜像装上来。
package cache

import (
	"context"
	"errors"
	"strings"
	"testing"

	"phpo/internal/model"
)

// withSources 把「设置页配的镜像源」接到 Manager 上（生产由装配层注入同一份 config.yaml 读数）
func withSources(m *Manager, hosts ...string) {
	m.SetSourcesProvider(func() []string { return hosts })
}

// missEvent 取首个 cache:miss 事件（action 与 source 一起看：源只在 action=pull 时有意义）
func missEvent(cap *capEmitter) (model.CacheMissEvent, bool) {
	for _, e := range cap.Capture() {
		if e.Name == "cache:miss" {
			if ev, ok := e.Payload.(model.CacheMissEvent); ok {
				return ev, true
			}
		}
	}
	return model.CacheMissEvent{}, false
}

// 未命中且配了源：按延迟从小到大试，第一个试成功的源要进 cache:miss 的 source。
// 测速返回值故意是「配置顺序」而不是延迟顺序，排序发生在缓存层内部（fastestFirst）。
func TestEnsureImageUsesFastestSource(t *testing.T) {
	m, _, cap, fb := newMgr(t)
	withSources(m, "slow.example.com", "fast.example.com", "broken.example.com")
	fb.probes = []model.MirrorSource{
		{Host: "slow.example.com", OK: true, LatencyMs: 300},
		{Host: "fast.example.com", OK: true, LatencyMs: 40},
		{Host: "broken.example.com", Error: "connection refused"}, // 握手不通，不进尝试序列
	}
	if err := m.EnsureImage(context.Background(), "pgsql", "17", "postgres:17"); err != nil {
		t.Fatal(err)
	}
	if len(fb.pulledFrom) != 1 || fb.pulledFrom[0] != "fast.example.com/postgres:17" {
		t.Fatalf("应只向最快的源拉一次，得 %v", fb.pulledFrom)
	}
	if len(fb.pulled) != 0 {
		t.Fatalf("源拉成功就不该再直连官方，得 %v", fb.pulled)
	}
	ev, ok := missEvent(cap)
	if !ok {
		t.Fatal("应发 cache:miss")
	}
	if ev.Action != "pull" || ev.Source != "fast.example.com" {
		t.Errorf("cache:miss 应带 action=pull 与实际用上的源，得 %+v", ev)
	}
}

// 最快的源也可能拉不动（限流、没同步这个镜像）：必须顺位回落到次快的源，而不是整单失败
func TestEnsureImageFallsBackToNextSource(t *testing.T) {
	m, _, cap, fb := newMgr(t)
	withSources(m, "fast.example.com", "slow.example.com")
	fb.probes = []model.MirrorSource{
		{Host: "fast.example.com", OK: true, LatencyMs: 10},
		{Host: "slow.example.com", OK: true, LatencyMs: 900},
	}
	// 最快源限流（拉不动），次快源正常：注入按源名给，才证得清「换的是哪一台」
	fb.pullFromErrs = map[string]error{"fast.example.com": errors.New("限流")}
	if err := m.EnsureImage(context.Background(), "redis", "8", "redis:8"); err != nil {
		t.Fatal(err)
	}
	if len(fb.pulledFrom) != 2 {
		t.Fatalf("应逐个源顺位回落，得 %v", fb.pulledFrom)
	}
	if fb.pulledFrom[0] != "fast.example.com/redis:8" {
		t.Errorf("第一跳应是最快的源，得 %v", fb.pulledFrom)
	}
	if fb.pulledFrom[1] != "slow.example.com/redis:8" {
		t.Errorf("第二跳应换到次快源，得 %v", fb.pulledFrom)
	}
	ev, _ := missEvent(cap)
	if ev.Source != "slow.example.com" {
		t.Errorf("界面上要说清最终用了谁，source = %q", ev.Source)
	}
}

// 全部源都不行 + 直连官方也不行：整单报错，且错误里逐源点名（含直连官方那一跳的原因）
func TestEnsureImageAllSourcesFailThenDirect(t *testing.T) {
	m, _, cap, fb := newMgr(t)
	withSources(m, "a.example.com", "b.example.com")
	fb.probes = []model.MirrorSource{
		{Host: "a.example.com", OK: true, LatencyMs: 1},
		{Host: "b.example.com", OK: true, LatencyMs: 2},
	}
	fb.pullFromErr = errors.New("镜像不存在")
	fb.pullErr = errors.New("网络不可达")
	err := m.EnsureImage(context.Background(), "mysql", "8.4", "mysql:8.4")
	if err == nil {
		t.Fatal("全都不行应整单报错")
	}
	msg := err.Error()
	for _, want := range []string{"拉取 mysql:8.4 失败", "a.example.com", "b.example.com", "直连官方: 网络不可达"} {
		if !strings.Contains(msg, want) {
			t.Errorf("错误信息应含 %q，得 %q", want, msg)
		}
	}
	// 两个源都试过，最后才直连官方
	if len(fb.pulledFrom) != 2 || len(fb.pulled) != 1 {
		t.Fatalf("尝试序列不对：pulledFrom=%v pulled=%v", fb.pulledFrom, fb.pulled)
	}
	// 报错的半句话不能同时说「未命中」——镜像没到手就不发 cache:miss
	if _, ok := missEvent(cap); ok {
		t.Error("拉取失败时不得发 cache:miss（否则界面只说未命中却没拉到）")
	}
}

// 没配源 = 与改动前完全一致：直连官方，且 cache:miss 的 source 为空（前端沿用原文案）
func TestEnsureImageWithoutSourcesPullsDirect(t *testing.T) {
	m, _, cap, fb := newMgr(t)
	if err := m.EnsureImage(context.Background(), "nginx", "alpine", "nginx:alpine"); err != nil {
		t.Fatal(err)
	}
	if len(fb.pulledFrom) != 0 || len(fb.pulled) != 1 {
		t.Fatalf("未配源不得拨镜像源，得 pulledFrom=%v pulled=%v", fb.pulledFrom, fb.pulled)
	}
	if len(fb.probeCalls) != 0 {
		t.Errorf("未配源就不该测速，probeCalls=%v", fb.probeCalls)
	}
	ev, _ := missEvent(cap)
	if ev.Source != "" {
		t.Errorf("直连官方时 source 应为空，得 %q", ev.Source)
	}
}

// 本机已有镜像 → 零网络重建缓存：这一条压根不碰源，也不该去测速
func TestEnsureImageLocalRebuildIgnoresSources(t *testing.T) {
	m, _, cap, fb := newMgr(t)
	withSources(m, "fast.example.com")
	fb.exists = map[string]bool{"redis:8": true}
	if err := m.EnsureImage(context.Background(), "redis", "8", "redis:8"); err != nil {
		t.Fatal(err)
	}
	if len(fb.pulledFrom) != 0 || len(fb.pulled) != 0 {
		t.Fatalf("零网络重建不得拉取，得 pulledFrom=%v pulled=%v", fb.pulledFrom, fb.pulled)
	}
	if len(fb.probeCalls) != 0 {
		t.Errorf("零网络重建不该测速，probeCalls=%v", fb.probeCalls)
	}
	ev, _ := missEvent(cap)
	if ev.Action != "local" || ev.Source != "" {
		t.Errorf("action=local 时 source 必须为空，得 %+v", ev)
	}
}

// fastestFirst 是纯排序判定：握手不通的源不进尝试序列，延迟相同保持配置顺序
func TestFastestFirstOrdering(t *testing.T) {
	hosts := fastestFirst([]model.MirrorSource{
		{Host: "c", OK: true, LatencyMs: 50},
		{Host: "dead", Error: "refused"},
		{Host: "a", OK: true, LatencyMs: 50},
		{Host: "b", OK: true, LatencyMs: 5},
	})
	got := strings.Join(hosts, ",")
	if got != "b,c,a" {
		t.Fatalf("排序 = %q，期望 b,c,a（延迟升序、相同保持配置顺序、不通的剔除）", got)
	}
	if len(fastestFirst(nil)) != 0 {
		t.Error("无源应得空序列")
	}
}

// 取消优先于失败：用户中断时不得把「取消」说成「所有源都拉不动」
func TestFetchImageCancelOutranksFailure(t *testing.T) {
	m, _, _, fb := newMgr(t)
	withSources(m, "a.example.com")
	fb.probes = []model.MirrorSource{{Host: "a.example.com", OK: true}}
	fb.pullFromErr = errors.New("boom")
	ctx, cancel := context.WithCancel(context.Background())
	fb.onPullFrom = func(string) { cancel() } // 第一跳失败前先取消，模拟「用户点了取消」
	if _, err := m.fetchImage(ctx, "php:8.4-fpm"); !errors.Is(err, context.Canceled) {
		t.Fatalf("应上报取消，得 %v", err)
	}
}
