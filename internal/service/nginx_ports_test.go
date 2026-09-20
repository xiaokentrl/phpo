// 站点端口发布增强验收：nginxSpec 端口并集/去重/回落、RepublishNginx 重建幂等且未装 nginx 时跳过、
// SiteService 在 publisher 注入下建站/改端口会触发重发布（端口并集正确）。
package service

import (
	"context"
	"sort"
	"testing"

	"phpo/internal/config"
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
	spec, err := nginxSpec(config.DerivePaths("~/phpo", "~/www"), "alpine", nil)
	if err != nil {
		t.Fatal(err)
	}
	if spec.PortMap["80/tcp"] != "80" || len(spec.PortMap) != 1 {
		t.Fatalf("无站点应发布默认 80，实得 %v", spec.PortMap)
	}
}

func TestNginxSpec_UnionDedupAndSkipInvalid(t *testing.T) {
	spec, err := nginxSpec(config.DerivePaths("~/phpo", "~/www"), "alpine", []int{8090, 80, 8090, 0, -5})
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
