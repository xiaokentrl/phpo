// 站点端口发布增强验收：nginxSpec 端口并集/去重/回落、用户配置的 nginx 服务端口真正进容器 spec、
// RepublishNginx 重建幂等且未装 nginx 时跳过、端口被外部进程占用时在 Pre-Clean 之前拒绝、
// 站点/安装链路走同一份权威发布集（SiteService.PublishPorts）。
package service

import (
	"context"
	"net"
	"sort"
	"strconv"
	"strings"
	"testing"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/pkg/dockerutil"
)

func portKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestNginxSpec_FallsBackToRegistryPort(t *testing.T) {
	spec, err := nginxSpec(config.DerivePaths("~/phpo", "~/www"), "alpine", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if spec.PortMap["80/tcp"] != "80" || len(spec.PortMap) != 1 {
		t.Fatalf("无站点应发布默认 80，实得 %v", spec.PortMap)
	}
}

func TestNginxSpec_UnionDedupAndSkipInvalid(t *testing.T) {
	spec, err := nginxSpec(config.DerivePaths("~/phpo", "~/www"), "alpine", []int{8090, 80, 8090, 0, -5}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := portKeys(spec.PortMap); len(got) != 2 || got[0] != "80/tcp" || got[1] != "8090/tcp" {
		t.Fatalf("端口并集应去重且跳过非法值，实得 %v", got)
	}
	if spec.PortMap["8090/tcp"] != "8090" {
		t.Fatalf("应 1:1 发布 host==container，实得 %v", spec.PortMap)
	}
}

// TestNginxSpec_ConfiguredPortIsPublished 用户在装机弹窗改过的 nginx 服务端口必须进容器 spec：
// 界面（服务卡片 / 站点页汇总）显示的正是这个值，落不了地就是虚报（硬红线 4）。
func TestNginxSpec_ConfiguredPortIsPublished(t *testing.T) {
	st := newFakeStore()
	st.setPort(string(model.KindNginx), "alpine", 8080)
	spec, err := nginxSpec(config.DerivePaths("~/phpo", "~/www"), "alpine", []int{8090}, st)
	if err != nil {
		t.Fatal(err)
	}
	if got := portKeys(spec.PortMap); len(got) != 2 || got[0] != "8080/tcp" || got[1] != "8090/tcp" {
		t.Fatalf("应发布 {配置的 8080} ∪ {站点 8090}，实得 %v", got)
	}
}

// TestNginxSpec_DefaultPortNotForcedAlongsideSites 未配置端口时不把注册表默认 80 硬塞进发布集：
// 没有站点监听 80 却绑 80，只会让本机 Apache/IIS 占着 80 的机器整站起不来（能警告的不要阻止）。
func TestNginxSpec_DefaultPortNotForcedAlongsideSites(t *testing.T) {
	spec, err := nginxSpec(config.DerivePaths("~/phpo", "~/www"), "alpine", []int{8090}, newFakeStore())
	if err != nil {
		t.Fatal(err)
	}
	if got := portKeys(spec.PortMap); len(got) != 1 || got[0] != "8090/tcp" {
		t.Fatalf("无配置端口 + 有站点时应只发布站点端口，实得 %v", got)
	}
}

// fixedPorts 权威站点发布集的假件（真实实现：*SiteService.PublishPorts）
type fixedPorts []int

func (f fixedPorts) PublishPorts() []int { return []int(f) }

// TestInstallNginx_PublishesSitePorts 装 nginx 时按当时站点并集发布端口：
// 只发布默认 80 会让 8090 上的站点整片 404，而界面照常显示「运行中」。
func TestInstallNginx_PublishesSitePorts(t *testing.T) {
	l, d, _, _ := newSvc()
	l.SetNginxPortSource(fixedPorts{8090, 8081})
	if err := l.Install(context.Background(), model.KindNginx, "alpine"); err != nil {
		t.Fatal(err)
	}
	got := portKeys(d.lastSpec[dockerutil.ContainerName("nginx", "alpine")].PortMap)
	if !contains(got, "8090/tcp") || !contains(got, "8081/tcp") {
		t.Fatalf("安装 nginx 应发布站点端口并集，实得 %v", got)
	}
}

// TestReinstallNginx_PublishesSitePorts 重建走同一入口（配置只有建容器时才落定）
func TestReinstallNginx_PublishesSitePorts(t *testing.T) {
	l, d, s, _ := newSvc()
	_ = s.SetInstalled("nginx", "alpine", true)
	l.SetNginxPortSource(fixedPorts{8090})
	if err := l.Reinstall(context.Background(), model.KindNginx, "alpine"); err != nil {
		t.Fatal(err)
	}
	if got := d.lastSpec[dockerutil.ContainerName("nginx", "alpine")].PortMap; got["8090/tcp"] != "8090" {
		t.Fatalf("重建 nginx 应发布站点端口 8090，实得 %v", got)
	}
}

// TestRepublishNginx_RefusesOccupiedPortBeforeDestroyingRunning 新站点端口被本机外部进程占了，
// 就不得先把在跑的 nginx 删掉：Pre-Clean 之后建容器必然绑不上端口，回滚只能删壳——全站瘫痪。
// 反向也要成立：容器自己已发布的端口在宿主上确实「被占」（docker-proxy 持着），探针不得把它当成外部占用，
// 否则任何一次重发布都会被自己的端口拦死。
func TestRepublishNginx_RefusesOccupiedPortBeforeDestroyingRunning(t *testing.T) {
	own, blocked := listenPort(t), listenPort(t)

	l, d, s, _ := newSvc()
	_ = s.SetInstalled("nginx", "alpine", true)
	name := dockerutil.ContainerName("nginx", "alpine")
	d.containers[name] = true
	d.published[name] = []int{own}
	d.lastSpec[name] = engine.ContainerSpec{Kind: "nginx", Version: "alpine", PortMap: map[string]string{portProto(own): strconv.Itoa(own)}}

	if err := l.RepublishNginx(context.Background(), []int{own}); err != nil {
		t.Fatalf("端口 %d 是本容器自己发布的，探针不得判成外部占用，实得 %v", own, err)
	}
	before := d.lastSpec[name]
	if err := l.RepublishNginx(context.Background(), []int{own, blocked}); err == nil ||
		!strings.Contains(err.Error(), strconv.Itoa(blocked)) {
		t.Fatalf("外部占用的新端口应拒绝重建并点明端口号，实得 %v", err)
	}
	if !d.containers[name] {
		t.Fatal("拒绝重建时不得删掉正在运行的 nginx")
	}
	if got := d.lastSpec[name]; got.PortMap[portProto(own)] != before.PortMap[portProto(own)] || len(got.PortMap) != 1 {
		t.Fatalf("拒绝重建时旧容器应原样保留，实得 %v", got.PortMap)
	}
}

// listenPort 占住一个随机端口直到用例结束，模拟本机上的其它监听者
func listenPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

// TestSiteService_PublishPortsIsSharedAuthority PublishPorts 是 nginx 建容器的唯一发布集判据：
// 与站点写链路同源（只收 vhost 已落盘、未被数据服务占用的端口），不另立标准。
func TestSiteService_PublishPortsIsSharedAuthority(t *testing.T) {
	ctx := context.Background()
	s, st, _, _ := newSiteSvc(t, nil)
	if err := s.Add(ctx, AddInput{Domain: "a.test", Port: 8090, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	st.snap.Installed["mysql"] = []string{"3306"}
	st.snap.Env[config.EnvKeyPort("mysql", "3306")] = "3306"
	// b.test 选 mysql 已占的 3306 → 降级（vhost 不落盘），其端口不得进发布集
	if err := s.Add(ctx, AddInput{Domain: "b.test", Port: 3306, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	if got := s.PublishPorts(); len(got) != 1 || got[0] != 8090 {
		t.Fatalf("权威发布集应只含已落盘的 8090，实得 %v", got)
	}
}

func TestRepublishNginx_RecreatesRunningWithPorts(t *testing.T) {
	l, d, s, _ := newSvc()
	_ = s.SetInstalled("nginx", "alpine", true)
	d.containers["phpo-nginx-alpine"] = true // 旧容器已运行

	if err := l.RepublishNginx(context.Background(), []int{8090, 80}); err != nil {
		t.Fatal(err)
	}
	name := dockerutil.ContainerName("nginx", "alpine")
	if !d.containers[name] {
		t.Fatal("重发布后 nginx 应处于运行")
	}
	got := d.lastSpec[name].PortMap
	if got["8090/tcp"] != "8090" || got["80/tcp"] != "80" {
		t.Fatalf("重发布未按并集绑定端口，实得 %v", got)
	}
}

// TestRepublishNginx_SkipsWhenStopped 用户停掉（或手工删掉）nginx 后，站点写链路不得把它悄悄拉起：
// 停机期间站点维持降级（§5.8），端口等 nginx 下次启动的就绪补齐再绑——
// 「我关掉了 nginx，它却因为我在改站点又活了并占回 80」是不可接受的越权。
func TestRepublishNginx_SkipsWhenStopped(t *testing.T) {
	l, d, s, _ := newSvc()
	_ = s.SetInstalled("nginx", "alpine", true)
	name := dockerutil.ContainerName("nginx", "alpine")
	d.containers[name] = false // 已装、已停

	if err := l.RepublishNginx(context.Background(), []int{8090}); err != nil {
		t.Fatal(err)
	}
	if d.containers[name] {
		t.Fatal("站点写链路不得启动用户已停止的 nginx")
	}
	if d.createCalls != 0 {
		t.Fatalf("停止的 nginx 不得被重建，实得建容器 %d 次", d.createCalls)
	}
}

// TestRepublishNginx_SkipsRebuildWhenPortsUnchanged 在跑容器的发布集与目标一致时不重建：
// 端口校对会被补齐链路反复触发，无差异也重建等于每次白闪断一次整站。
func TestRepublishNginx_SkipsRebuildWhenPortsUnchanged(t *testing.T) {
	l, d, s, _ := newSvc()
	_ = s.SetInstalled("nginx", "alpine", true)
	name := dockerutil.ContainerName("nginx", "alpine")
	d.containers[name] = true
	d.published[name] = []int{80, 8090}

	// 目标并集与容器当前发布集相同（顺序无关）：不重建、不重启
	if err := l.RepublishNginx(context.Background(), []int{8090, 80}); err != nil {
		t.Fatal(err)
	}
	if d.createCalls != 0 {
		t.Fatalf("端口集未变不应重建 nginx，实得 %d 次", d.createCalls)
	}

	// 删站让开 8090 后校对：发布集变小即必须重建，否则 nginx 白占着已无人使用的宿主端口
	if err := l.RepublishNginx(context.Background(), []int{80}); err != nil {
		t.Fatal(err)
	}
	if d.createCalls != 1 {
		t.Fatalf("端口集变了应重建一次，实得 %d 次", d.createCalls)
	}
	if got := d.lastSpec[name].PortMap; len(got) != 1 || got["80/tcp"] != "80" {
		t.Fatalf("重建应只绑 {80}，实得 %v", got)
	}
}

func TestRepublishNginx_SkipsWhenNotInstalled(t *testing.T) {
	l, d, _, _ := newSvc() // nginx 未安装
	if err := l.RepublishNginx(context.Background(), []int{8090}); err != nil {
		t.Fatal(err)
	}
	if len(d.containers) != 0 {
		t.Fatalf("未装 nginx 不应创建任何容器，实得 %v", d.containers)
	}
}

// recordingPublisher 记录每次重发布收到的端口，恒成功
type recordingPublisher struct{ calls [][]int }

func (p *recordingPublisher) RepublishNginx(_ context.Context, ports []int) error {
	p.calls = append(p.calls, ports)
	return nil
}

func TestSiteService_PublishesSitePortOnAdd(t *testing.T) {
	s, _, _, _ := newSiteSvc(t, nil)
	pub := &recordingPublisher{}
	s.SetNginxPublisher(pub)

	if err := s.Add(context.Background(), AddInput{Domain: "a.test", Port: 8090, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	if len(pub.calls) != 1 {
		t.Fatalf("建站应触发一次端口重发布，实得 %d 次", len(pub.calls))
	}
	if len(pub.calls[0]) != 1 || pub.calls[0][0] != 8090 {
		t.Fatalf("重发布端口应含 8090，实得 %v", pub.calls[0])
	}
}

func TestSiteService_PublishesUnionOnPortChange(t *testing.T) {
	s, _, _, _ := newSiteSvc(t, nil)
	pub := &recordingPublisher{}
	s.SetNginxPublisher(pub)
	ctx := context.Background()

	if err := s.Add(ctx, AddInput{Domain: "a.test", Port: 80, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, AddInput{Domain: "b.test", Port: 8090, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	pub.calls = nil // 清历史，只看改端口这一次

	if err := s.SetPort(ctx, "a.test", 8091); err != nil {
		t.Fatal(err)
	}
	if len(pub.calls) != 1 {
		t.Fatalf("改端口应触发一次重发布，实得 %v", pub.calls)
	}
	// 应含新端口 8091 与既有 b.test 的 8090，不含旧的 80
	if len(pub.calls[0]) != 2 || !containsInt(pub.calls[0], 8091) || !containsInt(pub.calls[0], 8090) || containsInt(pub.calls[0], 80) {
		t.Fatalf("重发布并集应为 {8091,8090}，实得 %v", pub.calls[0])
	}
}

// TestSiteService_AddPortConflictSkipsPublish 端口冲突降级：既不写 vhost 也不把该端口发布给 nginx
// （发布会让容器重建去绑一个已被占用的端口）；改用空闲端口后经 SetPort 一次性补发。
func TestSiteService_AddPortConflictSkipsPublish(t *testing.T) {
	ctx := context.Background()
	s, _, _, _ := newSiteSvc(t, nil)
	pub := &recordingPublisher{}
	s.SetNginxPublisher(pub)
	if err := s.Add(ctx, AddInput{Domain: "old.test", Port: 80, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	pub.calls = nil // 只看降级建站这一次

	if err := s.Add(ctx, AddInput{Domain: "new.test", Port: 80, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	if len(pub.calls) != 0 {
		t.Fatalf("降级站点不得发布端口，实得 %v", pub.calls)
	}

	if err := s.SetPort(ctx, "new.test", 8090); err != nil {
		t.Fatal(err)
	}
	if len(pub.calls) != 1 || !containsInt(pub.calls[0], 8090) {
		t.Fatalf("改到空闲端口后应发布 8090，实得 %v", pub.calls)
	}
}

// TestSiteService_DegradedSitePortNeverPublished 降级站点的端口不得混进后续发布集：
// 站点因端口被服务占用而降级后，库里仍记着该端口；若另一站点触发重发布时把它带上，
// nginx 容器会去绑一个已被占用的宿主端口而起不来。
func TestSiteService_DegradedSitePortNeverPublished(t *testing.T) {
	ctx := context.Background()
	s, st, _, _ := newSiteSvc(t, nil)
	pub := &recordingPublisher{}
	s.SetNginxPublisher(pub)
	st.snap.Installed["mysql"] = []string{"3306"}
	st.snap.Env[config.EnvKeyPort("mysql", "3306")] = "3306"

	if err := s.Add(ctx, AddInput{Domain: "a.test", Port: 80, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	// b.test 选 3306：被 mysql 占用 → 降级（不写 vhost、不发布端口）
	if err := s.Add(ctx, AddInput{Domain: "b.test", Port: 3306, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	pub.calls = nil // 只看此后 a.test 改端口这一次

	if err := s.SetPort(ctx, "a.test", 8080); err != nil {
		t.Fatal(err)
	}
	if len(pub.calls) != 1 || !containsInt(pub.calls[0], 8080) || containsInt(pub.calls[0], 3306) {
		t.Fatalf("发布集应只含 {8080}，降级站点的 3306 不得出现，实得 %v", pub.calls)
	}
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
