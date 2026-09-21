// T403 验收：SiteService 三段式建站/删站——幂等、回收站、坏 vhost 被拦不落库、state:changed 广播
package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/store"
	"phpo/internal/task"
	"phpo/internal/vhost"
	"phpo/internal/vhost/hosts"
)

// fakeSiteStore 实现 SiteStore（内存权威）
type fakeSiteStore struct {
	sites   []model.Site
	trash   []store.TrashItem
	snap    *model.Snapshot
	upserts int
}

func newFakeSiteStore() *fakeSiteStore {
	snap := model.NewSnapshot()
	snap.Installed["php"] = []string{"8.4"}      // vhost 可写的判定依据之一：所选 PHP 已安装
	snap.Installed["nginx"] = []string{"alpine"} // vhost 可写的判定依据之二：nginx 已装且在运行
	snap.Running["nginx"] = []string{"alpine"}
	return &fakeSiteStore{snap: snap}
}
func (f *fakeSiteStore) ListSites() ([]model.Site, error) {
	return append([]model.Site{}, f.sites...), nil
}
func (f *fakeSiteStore) UpsertSite(st model.Site) error {
	f.upserts++
	for i := range f.sites {
		if f.sites[i].Domain == st.Domain {
			f.sites[i] = st
			return nil
		}
	}
	f.sites = append(f.sites, st)
	return nil
}
func (f *fakeSiteStore) DeleteSite(domain string) error {
	out := f.sites[:0]
	for _, st := range f.sites {
		if st.Domain != domain {
			out = append(out, st)
		}
	}
	f.sites = out
	return nil
}
func (f *fakeSiteStore) AddTrashItem(it store.TrashItem) (int64, error) {
	f.trash = append(f.trash, it)
	return int64(len(f.trash)), nil
}
func (f *fakeSiteStore) BuildSnapshot() (*model.Snapshot, error) {
	f.snap.Sites = append([]model.Site{}, f.sites...)
	return f.snap, nil
}

// errValidator 模拟 nginx -t 失败
type errValidator struct{ err error }

func (e errValidator) Validate(context.Context, string, string) error { return e.err }

// newSiteSvc 用临时 home/www 构造真 vhost + 真 hosts(临时文件) + 真回收站 + 假库
func newSiteSvc(t *testing.T, validate vhost.Validator) (*SiteService, *fakeSiteStore, config.Env, *fakeEmitter) {
	t.Helper()
	dir := t.TempDir()
	env := config.DerivePaths(filepath.Join(dir, "phpo"), filepath.Join(dir, "www"))
	os.MkdirAll(env.NginxSitesRoot, 0o755)
	hostsFile := filepath.Join(dir, "hosts")
	os.WriteFile(hostsFile, []byte("127.0.0.1 localhost\n"), 0o644)

	st := newFakeSiteStore()
	vh := vhost.New(env)
	hm := hosts.NewAt(hostsFile)
	tr := engine.NewTrash(filepath.Join(dir, "trash"))
	em := &fakeEmitter{}
	tm := task.NewManager(em)
	return NewSiteService(st, vh, hm, tr, validate, nil, tm, em, env), st, env, em
}

func TestSiteService_Add_CreatesDirVHostAndHosts(t *testing.T) {
	svc, st, env, em := newSiteSvc(t, nil)
	err := svc.Add(context.Background(), AddInput{Domain: "demo.test", Port: 80, PHP: "8.4", Rewrite: "laravel"})
	if err != nil {
		t.Fatal(err)
	}
	// 站点目录
	if _, e := os.Stat(filepath.Join(env.WWWRoot, "demo.test")); e != nil {
		t.Fatalf("应创建站点目录: %v", e)
	}
	// vhost 文件含精确上游
	conf := filepath.Join(env.NginxSitesRoot, "demo.test.conf")
	b, e := os.ReadFile(conf)
	if e != nil {
		t.Fatalf("应写 vhost: %v", e)
	}
	if !strings.Contains(string(b), "set $php_upstream php-8.4-fpm:9000;") {
		t.Fatalf("vhost 上游错误:\n%s", b)
	}
	// hosts 落条目
	hb, _ := os.ReadFile(filepath.Dir(env.PHPOHome) + "/hosts")
	if !strings.Contains(string(hb), "demo.test") {
		t.Fatalf("hosts 未加条目:\n%s", hb)
	}
	// 落库 + 广播
	if len(st.sites) != 1 || st.sites[0].Domain != "demo.test" {
		t.Fatalf("站点未落库: %+v", st.sites)
	}
	if !em.has("task:done") || !em.has("state:changed") {
		t.Fatalf("应发 task:done + state:changed，实得 %v", em.events)
	}
}

// TestSiteService_Add_Idempotent 重复建站：站点数仍为 1，不产生脏状态
func TestSiteService_Add_Idempotent(t *testing.T) {
	svc, st, env, _ := newSiteSvc(t, nil)
	in := AddInput{Domain: "demo.test", Port: 80, PHP: "8.4", Rewrite: "laravel"}
	for i := 0; i < 3; i++ {
		if err := svc.Add(context.Background(), in); err != nil {
			t.Fatalf("第 %d 次建站失败: %v", i+1, err)
		}
	}
	if len(st.sites) != 1 {
		t.Fatalf("重复建站应幂等，实得 %d 个", len(st.sites))
	}
	if st.upserts != 3 {
		t.Fatalf("每次应 upsert 一次（覆盖），实得 %d", st.upserts)
	}
	// 目录与 vhost 仍各一份
	entries, _ := os.ReadDir(env.NginxSitesRoot)
	if len(entries) != 1 {
		t.Fatalf("sites 目录应只有 1 个 conf，实得 %d", len(entries))
	}
}

// TestSiteService_Add_BlockedByNginxT 硬红线 2：坏 vhost 被 nginx -t 拦下，不落库、不留文件
func TestSiteService_Add_BlockedByNginxT(t *testing.T) {
	svc, st, env, _ := newSiteSvc(t, errValidator{err: errors.New("nginx: configuration file test failed")})
	err := svc.Add(context.Background(), AddInput{Domain: "bad.test", Port: 80, PHP: "8.4"})
	if err == nil {
		t.Fatal("nginx -t 失败应使建站失败")
	}
	if len(st.sites) != 0 {
		t.Fatalf("校验失败不得落库，实得 %+v", st.sites)
	}
	if _, e := os.Stat(filepath.Join(env.NginxSitesRoot, "bad.test.conf")); !os.IsNotExist(e) {
		t.Fatal("校验失败不得留下 vhost 文件")
	}
}

// TestSiteService_Remove_ToTrash 删站：根目录入回收站、vhost 删除、库删除、回收站条目登记
func TestSiteService_Remove_ToTrash(t *testing.T) {
	svc, st, env, _ := newSiteSvc(t, nil)
	if err := svc.Add(context.Background(), AddInput{Domain: "demo.test", Port: 80, PHP: "8.4", Rewrite: "laravel"}); err != nil {
		t.Fatal(err)
	}
	// 放个文件进站点目录，确保整目录入回收站
	os.WriteFile(filepath.Join(env.WWWRoot, "demo.test", "index.php"), []byte("<?php"), 0o644)

	if err := svc.Remove(context.Background(), "demo.test"); err != nil {
		t.Fatal(err)
	}
	// 原目录消失
	if _, e := os.Stat(filepath.Join(env.WWWRoot, "demo.test")); !os.IsNotExist(e) {
		t.Fatal("站点目录应移走")
	}
	// 回收站里能看到原文件（数据不丢，可恢复）
	trashDir := filepath.Join(filepath.Dir(env.PHPOHome), "trash", "demo.test")
	if _, e := os.Stat(filepath.Join(trashDir, "index.php")); e != nil {
		t.Fatalf("回收站应保留站点源码: %v", e)
	}
	// vhost 文件删除
	if _, e := os.Stat(filepath.Join(env.NginxSitesRoot, "demo.test.conf")); !os.IsNotExist(e) {
		t.Fatal("vhost 文件应删除")
	}
	// 库删除 + 回收站条目登记
	if len(st.sites) != 0 {
		t.Fatalf("站点应注销，实得 %+v", st.sites)
	}
	if len(st.trash) != 1 || st.trash[0].Kind != "site" {
		t.Fatalf("应登记 1 条 site 回收站条目，实得 %+v", st.trash)
	}
}

func TestSiteService_Remove_Unknown(t *testing.T) {
	svc, _, _, _ := newSiteSvc(t, nil)
	if err := svc.Remove(context.Background(), "nope.test"); err == nil {
		t.Fatal("删除不存在站点应报错")
	}
}

// TestSiteService_WriteOps_PortPhpRewrite 改端口/切 PHP/伪静态各自重写落盘 vhost 并落库
func TestSiteService_WriteOps_PortPhpRewrite(t *testing.T) {
	svc, st, env, _ := newSiteSvc(t, nil)
	if err := svc.Add(context.Background(), AddInput{Domain: "demo.test", Port: 80, PHP: "8.4", Rewrite: "laravel"}); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(env.NginxSitesRoot, "demo.test.conf")
	read := func() string { b, _ := os.ReadFile(conf); return string(b) }

	// 改端口 80→8080
	if err := svc.SetPort(context.Background(), "demo.test", 8080); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(), "listen 8080;") {
		t.Fatalf("端口未落盘:\n%s", read())
	}

	// 切 PHP 8.4→8.3（硬红线 1 精确上游）
	if err := svc.SwitchPHP(context.Background(), "demo.test", "8.3"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(), "set $php_upstream php-8.3-fpm:9000;") {
		t.Fatalf("PHP 上游未落盘:\n%s", read())
	}
	if st.sites[0].PHP != "8.3" || st.sites[0].Port != 8080 {
		t.Fatalf("库未同步: %+v", st.sites[0])
	}

	// 改伪静态 thinkphp
	if err := svc.SetRewrite(context.Background(), "demo.test", "thinkphp", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(), "# ThinkPHP") {
		t.Fatalf("伪静态未落盘:\n%s", read())
	}
	if st.sites[0].Rewrite != "thinkphp" {
		t.Fatalf("库 rewrite 未同步: %s", st.sites[0].Rewrite)
	}
}

// TestSiteService_SwitchPHP_BlockedByNginxT nginx -t 失败：vhost 文件与库均不落地（硬红线 2 回滚）
func TestSiteService_SwitchPHP_BlockedByNginxT(t *testing.T) {
	svc, st, env, _ := newSiteSvc(t, nil)
	if err := svc.Add(context.Background(), AddInput{Domain: "demo.test", Port: 80, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	// 注入会失败的校验器
	svc.validate = errValidator{err: errors.New("nginx -t failed")}
	conf := filepath.Join(env.NginxSitesRoot, "demo.test.conf")
	before, _ := os.ReadFile(conf)

	if err := svc.SwitchPHP(context.Background(), "demo.test", "8.2"); err == nil {
		t.Fatal("校验失败应使切换失败")
	}
	// 文件回滚为原内容（8.4），库仍为 8.4
	after, _ := os.ReadFile(conf)
	if string(after) != string(before) {
		t.Fatalf("回滚后正文应还原:\n%s", after)
	}
	if st.sites[0].PHP != "8.4" {
		t.Fatalf("库 PHP 不应被改: %s", st.sites[0].PHP)
	}
}

// TestSiteService_SetVhostContent 手改正文回读端口/root
func TestSiteService_SetVhostContent(t *testing.T) {
	svc, st, env, _ := newSiteSvc(t, nil)
	if err := svc.Add(context.Background(), AddInput{Domain: "demo.test", Port: 80, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	manual := "server {\n    listen 9090;\n    root /var/www/custom;\n    set $php_upstream php-8.4-fpm:9000;\n}"
	if err := svc.SetVhostContent(context.Background(), "demo.test", manual); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(env.NginxSitesRoot, "demo.test.conf"))
	if string(b) != manual {
		t.Fatalf("手改正文应逐字落盘:\n%s", b)
	}
	// 端口/root 从正文回读进库（root 容器路径反映射回宿主）
	if st.sites[0].Port != 9090 {
		t.Fatalf("端口应回读为 9090，实得 %d", st.sites[0].Port)
	}
	if !st.sites[0].VhostCustomized {
		t.Fatal("手改后应标记 customized")
	}
}

// TestSiteService_Add_DegradesWithoutPhp #1：建站只以 nginx 为硬门禁——无可用 PHP 时站点照建
// （目录 + hosts + 落库），仅不写 vhost（否则 nginx -t 因上游不存在必失败，硬红线 2）；
// PHP 就绪后经 SwitchPHP 自动补写 vhost，降级可自愈。
func TestSiteService_Add_DegradesWithoutPhp(t *testing.T) {
	ctx := context.Background()
	svc, st, env, _ := newSiteSvc(t, nil)
	hostsFile := filepath.Join(filepath.Dir(env.PHPOHome), "hosts")

	// 未指定 PHP 与选定未安装版本，两者都应降级
	if err := svc.Add(ctx, AddInput{Domain: "a.test", Port: 8081, PHP: ""}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Add(ctx, AddInput{Domain: "b.test", Port: 8082, PHP: "9.9"}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"a.test", "b.test"} {
		if _, e := os.Stat(filepath.Join(env.NginxSitesRoot, d+".conf")); !os.IsNotExist(e) {
			t.Fatalf("PHP 未就绪时不得写 %s 的 vhost", d)
		}
		if _, e := os.Stat(filepath.Join(env.WWWRoot, d)); e != nil {
			t.Fatalf("站点目录仍应创建: %v", e)
		}
	}
	if len(st.sites) != 2 {
		t.Fatalf("两个站点都应落库，实得 %+v", st.sites)
	}
	hb, _ := os.ReadFile(hostsFile)
	if !strings.Contains(string(hb), "a.test") || !strings.Contains(string(hb), "b.test") {
		t.Fatalf("hosts 仍应写入条目:\n%s", hb)
	}

	// 自愈：PHP 8.4 就绪后切换 → vhost 补写且上游精确
	if err := svc.SwitchPHP(ctx, "a.test", "8.4"); err != nil {
		t.Fatal(err)
	}
	b, e := os.ReadFile(filepath.Join(env.NginxSitesRoot, "a.test.conf"))
	if e != nil {
		t.Fatalf("切换 PHP 后应补写 vhost: %v", e)
	}
	if !strings.Contains(string(b), "set $php_upstream php-8.4-fpm:9000;") {
		t.Fatalf("补写的上游应逐字符精确:\n%s", b)
	}
}

// TestSiteService_Add_DegradesOnPortConflict #2：端口被占用时不擅改用户所填端口、站点照建（目录 + hosts + 落库），
// 仅降级为「vhost 不落盘、端口不发布」；改用空闲端口后经 SetPort 自愈补写（总纲 §5.8 / v2.9.2）。
func TestSiteService_Add_DegradesOnPortConflict(t *testing.T) {
	ctx := context.Background()
	svc, st, env, _ := newSiteSvc(t, nil)
	// 既有站点占用 80（快照占用表由库派生）
	st.sites = []model.Site{{Domain: "old.test", Port: 80, PHP: "8.4", Root: filepath.Join(env.WWWRoot, "old.test")}}

	if err := svc.Add(ctx, AddInput{Domain: "new.test", Port: 80, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	if _, e := os.Stat(filepath.Join(env.NginxSitesRoot, "new.test.conf")); !os.IsNotExist(e) {
		t.Fatal("端口占用时不得写 vhost")
	}
	if _, e := os.Stat(filepath.Join(env.WWWRoot, "new.test")); e != nil {
		t.Fatalf("站点目录仍应创建: %v", e)
	}
	if len(st.sites) != 2 || st.sites[1].Domain != "new.test" || st.sites[1].Port != 80 {
		t.Fatalf("应保留用户所填端口并落库，实得 %+v", st.sites)
	}

	// 自愈：改到空闲端口 → vhost 补写、监听端口为新值
	if err := svc.SetPort(ctx, "new.test", 8080); err != nil {
		t.Fatal(err)
	}
	b, e := os.ReadFile(filepath.Join(env.NginxSitesRoot, "new.test.conf"))
	if e != nil {
		t.Fatalf("改端口后应补写 vhost: %v", e)
	}
	if !strings.Contains(string(b), "listen 8080;") {
		t.Fatalf("补写的 vhost 端口错误:\n%s", b)
	}
}

// TestSiteService_SwitchPHP_RoundTrip T405 验收：8.4↔8.3 往返切换，上游逐字符精确（硬红线 1），库始终同步
func TestSiteService_SwitchPHP_RoundTrip(t *testing.T) {
	svc, st, env, _ := newSiteSvc(t, nil)
	if err := svc.Add(context.Background(), AddInput{Domain: "demo.test", Port: 80, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(env.NginxSitesRoot, "demo.test.conf")
	read := func() string { b, _ := os.ReadFile(conf); return string(b) }

	for _, php := range []string{"8.3", "7.4", "8.1", "8.4"} {
		if err := svc.SwitchPHP(context.Background(), "demo.test", php); err != nil {
			t.Fatalf("切换到 %s 失败: %v", php, err)
		}
		want := "set $php_upstream php-" + php + "-fpm:9000;"
		if !strings.Contains(read(), want) {
			t.Fatalf("切换后上游应逐字符为 %q:\n%s", want, read())
		}
		if st.sites[0].PHP != php {
			t.Fatalf("库 PHP 应为 %s，实得 %s", php, st.sites[0].PHP)
		}
	}
}

// TestSiteService_Add_DegradesWhenNginxNotReady 门禁在 preflight（未装 nginx 直接阻断建站）；服务层是兜底：
// nginx 不就绪（未装 / 未运行）一律降级——照常建目录 + 写 hosts + 落库，仅暂不写 vhost、暂不发布端口
// （否则 docker exec nginx -t 必失败，硬红线 2）；端口保留用户所填值。
func TestSiteService_Add_DegradesWhenNginxNotReady(t *testing.T) {
	ctx := context.Background()
	svc, st, env, _ := newSiteSvc(t, nil)
	st.snap.Installed["nginx"] = nil
	st.snap.Running["nginx"] = nil

	if err := svc.Add(ctx, AddInput{Domain: "a.test", Port: 8081, PHP: "8.4"}); err != nil {
		t.Fatalf("nginx 缺席应放行建站，实得 %v", err)
	}
	if _, e := os.Stat(filepath.Join(env.NginxSitesRoot, "a.test.conf")); !os.IsNotExist(e) {
		t.Fatal("nginx 未就绪时不得写 vhost")
	}
	if len(st.sites) != 1 || st.sites[0].Port != 8081 {
		t.Fatalf("站点应落库并保留端口 8081，实得 %+v", st.sites)
	}
	if _, e := os.Stat(filepath.Join(env.WWWRoot, "a.test")); e != nil {
		t.Fatalf("站点目录仍应创建: %v", e)
	}

	// 已装但未运行同样降级（docker exec 打不通）
	st.snap.Installed["nginx"] = []string{"alpine"}
	st.snap.Running["nginx"] = nil
	if err := svc.Add(ctx, AddInput{Domain: "b.test", Port: 8082, PHP: "8.4"}); err != nil {
		t.Fatalf("nginx 未运行应放行建站，实得 %v", err)
	}
	if _, e := os.Stat(filepath.Join(env.NginxSitesRoot, "b.test.conf")); !os.IsNotExist(e) {
		t.Fatal("nginx 未运行时不得写 vhost")
	}
}

// TestSiteService_ReconcileServe_HealsAfterNginxReady 启动 nginx 后自动补齐降级站点：
// 补写 vhost（上游与监听端口精确）、把端口发布给 nginx；幂等且不动仍降级的站点。
func TestSiteService_ReconcileServe_HealsAfterNginxReady(t *testing.T) {
	ctx := context.Background()
	svc, st, env, _ := newSiteSvc(t, nil)
	// nginx 已装但停着（建站门禁已过，站点落为降级）
	st.snap.Running["nginx"] = nil
	if err := svc.Add(ctx, AddInput{Domain: "a.test", Port: 8081, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	// c.test 的端口被 mysql 占用：nginx 就绪后仍应维持降级，不得抢绑
	st.snap.Installed["mysql"] = []string{"8.4"}
	st.snap.Env[config.EnvKeyPort("mysql", "8.4")] = "3306"
	if err := svc.Add(ctx, AddInput{Domain: "c.test", Port: 3306, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}

	pub := &recordingPublisher{}
	svc.SetNginxPublisher(pub)
	st.snap.Installed["nginx"] = []string{"alpine"}
	st.snap.Running["nginx"] = []string{"alpine"}

	if err := svc.ReconcileServe(ctx); err != nil {
		t.Fatal(err)
	}
	b, e := os.ReadFile(filepath.Join(env.NginxSitesRoot, "a.test.conf"))
	if e != nil {
		t.Fatalf("nginx 就绪后应补写 a.test 的 vhost: %v", e)
	}
	if !strings.Contains(string(b), "listen 8081;") || !strings.Contains(string(b), "set $php_upstream php-8.4-fpm:9000;") {
		t.Fatalf("补写的 vhost 端口/上游应精确:\n%s", b)
	}
	if _, e := os.Stat(filepath.Join(env.NginxSitesRoot, "c.test.conf")); !os.IsNotExist(e) {
		t.Fatal("端口仍被占用的站点应保持降级，不得写 vhost")
	}
	if len(pub.calls) != 1 || !containsInt(pub.calls[0], 8081) || containsInt(pub.calls[0], 3306) {
		t.Fatalf("补齐后应发布 {8081}，实得 %v", pub.calls)
	}

	// 幂等仍需校对端口：站点全都有 conf 时不写盘，但 nginx 容器实际绑的端口集可能已与站点不符
	// （停机期间删过站、或重发布被跳过）。是否真重建由 RepublishNginx 按发布集判等决定。
	pub.calls = nil
	if err := svc.ReconcileServe(ctx); err != nil {
		t.Fatal(err)
	}
	if len(pub.calls) != 1 || !containsInt(pub.calls[0], 8081) || containsInt(pub.calls[0], 3306) {
		t.Fatalf("无待补站点时仍应以权威发布集 {8081} 校对一次，实得 %v", pub.calls)
	}
}

// TestSiteService_Remove_HealsSiteFreedByDeletedPort 删站让出的端口可能正卡着别的降级站点
// （两个站点同填一个端口，后者无 conf）：删除落库后顺手补齐，否则用户删完冲突站点，
// 界面上会留下一个「条件已满足却仍不服务、且健康列已说不出原因」的站点。
func TestSiteService_Remove_HealsSiteFreedByDeletedPort(t *testing.T) {
	ctx := context.Background()
	svc, _, env, _ := newSiteSvc(t, nil)
	if err := svc.Add(ctx, AddInput{Domain: "a.test", Port: 8081, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Add(ctx, AddInput{Domain: "b.test", Port: 8081, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	confB := filepath.Join(env.NginxSitesRoot, "b.test.conf")
	if _, e := os.Stat(confB); !os.IsNotExist(e) {
		t.Fatal("端口被 a.test 占用时 b.test 应降级（不写 vhost）")
	}
	if err := svc.Remove(ctx, "a.test"); err != nil {
		t.Fatal(err)
	}
	b, e := os.ReadFile(confB)
	if e != nil {
		t.Fatalf("删掉冲突站点后应补齐 b.test 的 vhost: %v", e)
	}
	if !strings.Contains(string(b), "listen 8081;") {
		t.Fatalf("补齐的 vhost 端口应精确:\n%s", b)
	}
}

// TestSiteService_SetPort_HealsSiteFreedByNewPort 改端口同样会腾出端口：a.test 从 8081 让开后，
// 卡在 8081 上的 b.test 应在本次写操作末尾补齐（与删站同一判据，落库之后才看得见新占用表）。
func TestSiteService_SetPort_HealsSiteFreedByNewPort(t *testing.T) {
	ctx := context.Background()
	svc, _, env, _ := newSiteSvc(t, nil)
	if err := svc.Add(ctx, AddInput{Domain: "a.test", Port: 8081, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Add(ctx, AddInput{Domain: "b.test", Port: 8081, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPort(ctx, "a.test", 8082); err != nil {
		t.Fatal(err)
	}
	b, e := os.ReadFile(filepath.Join(env.NginxSitesRoot, "b.test.conf"))
	if e != nil {
		t.Fatalf("a.test 让开 8081 后应补写 b.test 的 vhost: %v", e)
	}
	if !strings.Contains(string(b), "listen 8081;") {
		t.Fatalf("补齐的 vhost 端口应精确:\n%s", b)
	}
}

// hostsFake 可编排的 HostsOps：记录调用次数，按预设返回 Result/error
type hostsFake struct {
	res       hosts.Result
	err       error
	calls     int
	removeRes hosts.Result
	removeErr error
	removeGot []string
}

func (f *hostsFake) Add(string) (hosts.Result, error) {
	f.calls++
	return f.res, f.err
}

func (f *hostsFake) Remove(domain string) (hosts.Result, error) {
	f.removeGot = append(f.removeGot, domain)
	return f.removeRes, f.removeErr
}

// TestSiteService_Remove_RecyclesHosts 删站必须连带回收该域名的 hosts 条目（不留孤儿行）
func TestSiteService_Remove_RecyclesHosts(t *testing.T) {
	ctx := context.Background()
	svc, _, env, _ := newSiteSvc(t, nil)
	hostsPath := filepath.Join(filepath.Dir(env.PHPOHome), "hosts")

	if err := svc.Add(ctx, AddInput{Domain: "demo.test", Port: 80, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(hostsPath); !hosts.Has(string(b), "127.0.0.1", "demo.test") {
		t.Fatalf("建站应先写入 hosts 条目:\n%s", b)
	}
	if err := svc.Remove(ctx, "demo.test"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	if hosts.Has(string(b), "127.0.0.1", "demo.test") {
		t.Fatalf("删站后 hosts 仍留着该域名（孤儿条目）:\n%s", b)
	}
	if !hosts.Has(string(b), "127.0.0.1", "localhost") {
		t.Fatalf("不得动无关条目:\n%s", b)
	}
}

// TestSiteService_Remove_HostsWarningKeepsGoing hosts 需提权/写不进时只警告，删站照常完成（§3.2 原则 7）
func TestSiteService_Remove_HostsWarningKeepsGoing(t *testing.T) {
	ctx := context.Background()
	svc, st, _, _ := newSiteSvc(t, nil)
	hf := &hostsFake{removeRes: hosts.Result{Warning: "无法修改 hosts（/etc/hosts）。请以管理员身份运行后手动删除：127.0.0.1 demo.test"}}
	if err := svc.Add(ctx, AddInput{Domain: "demo.test", Port: 80, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	svc.hosts = hf

	if err := svc.Remove(ctx, "demo.test"); err != nil {
		t.Fatalf("hosts 未回收是警告，不该让删站失败: %v", err)
	}
	if len(hf.removeGot) != 1 || hf.removeGot[0] != "demo.test" {
		t.Fatalf("删站应回收本站域名，实得 %v", hf.removeGot)
	}
	if len(st.sites) != 0 {
		t.Fatalf("站点仍应注销: %+v", st.sites)
	}

	// 只删本站域名：多站点共存时不得顺手删掉别人的条目
	svc2, _, _, _ := newSiteSvc(t, nil)
	if err := svc2.Add(ctx, AddInput{Domain: "a.test", Port: 8080, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	if err := svc2.Add(ctx, AddInput{Domain: "b.test", Port: 8081, PHP: "8.4"}); err != nil {
		t.Fatal(err)
	}
	hf2 := &hostsFake{}
	svc2.hosts = hf2
	if err := svc2.Remove(ctx, "a.test"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(hf2.removeGot, ",") != "a.test" {
		t.Fatalf("只应回收 a.test，实得 %v", hf2.removeGot)
	}
}

// failingPublisher 模拟建站最后一步（发布端口到 nginx）失败，用于验证整体回滚
type failingPublisher struct{ err error }

func (f failingPublisher) RepublishNginx(context.Context, []int) error { return f.err }

// TestSiteService_Add_RollsBackHostsWhenLaterStepFails 建站末步失败必须连 hosts 一起回滚：
// 站点没建成却在 /etc/hosts 留下解析行 = 脏状态（§0.2-19 失败必须回滚、§5.13.13 不留无名资源）
func TestSiteService_Add_RollsBackHostsWhenLaterStepFails(t *testing.T) {
	ctx := context.Background()
	svc, st, env, _ := newSiteSvc(t, nil)
	hostsPath := filepath.Join(filepath.Dir(env.PHPOHome), "hosts")
	svc.SetNginxPublisher(failingPublisher{err: errors.New("nginx 容器重建失败")})

	if err := svc.Add(ctx, AddInput{Domain: "demo.test", Port: 8090, PHP: "8.4"}); err == nil {
		t.Fatal("发布端口失败应让建站失败")
	}
	b, e := os.ReadFile(hostsPath)
	if e != nil {
		t.Fatal(e)
	}
	if hosts.Has(string(b), "127.0.0.1", "demo.test") {
		t.Fatalf("建站失败后 hosts 仍留着该域名（孤儿条目）:\n%s", b)
	}
	if !hosts.Has(string(b), "127.0.0.1", "localhost") {
		t.Fatalf("回滚不得动无关条目:\n%s", b)
	}
	if len(st.sites) != 0 {
		t.Fatalf("任务失败不应执行 Apply 段落库: %+v", st.sites)
	}
}

// TestSiteService_AddHosts 手动补写 hosts 走真实 Manager：缺失→补上→再点幂等，且广播快照
func TestSiteService_AddHosts(t *testing.T) {
	ctx := context.Background()
	svc, _, env, em := newSiteSvc(t, nil)
	hostsPath := filepath.Join(filepath.Dir(env.PHPOHome), "hosts")
	if err := svc.Add(ctx, AddInput{Domain: "demo.test", Port: 80, PHP: "8.4", Rewrite: "laravel"}); err != nil {
		t.Fatal(err)
	}

	// 模拟建站时 hosts 未落上（如提权被拒）：文件回到无条目态
	if err := os.WriteFile(hostsPath, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	warn, err := svc.AddHosts(ctx, "demo.test")
	if err != nil || warn != "" {
		t.Fatalf("补写应成功且无警告: warn=%q err=%v", warn, err)
	}
	b, e := os.ReadFile(hostsPath)
	if e != nil || !hosts.Has(string(b), "127.0.0.1", "demo.test") {
		t.Fatalf("hosts 应已写入条目:\n%s %v", b, e)
	}
	if !em.has("state:changed") {
		t.Fatalf("补写后应广播 state:changed，实得 %v", em.events)
	}

	// 幂等：重复点击不再追加、不报错
	if _, err := svc.AddHosts(ctx, "demo.test"); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(hostsPath)
	if strings.Count(string(b2), "demo.test") != 1 {
		t.Fatalf("重复补写应幂等:\n%s", b2)
	}
}

// TestSiteService_AddHosts_Errors 「加 hosts」的三种未生效路径：站点不存在 / 需提权 / 写失败
func TestSiteService_AddHosts_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("站点不存在不得触写", func(t *testing.T) {
		svc, _, _, _ := newSiteSvc(t, nil)
		hf := &hostsFake{}
		svc.hosts = hf
		if _, err := svc.AddHosts(ctx, "ghost.test"); err == nil {
			t.Fatal("站点不存在应报错")
		}
		if hf.calls != 0 {
			t.Fatalf("站点不存在时不得写 hosts，实得 %d 次", hf.calls)
		}
	})

	t.Run("需提权返回警告不当作错误", func(t *testing.T) {
		svc, st, _, _ := newSiteSvc(t, nil)
		if err := svc.Add(ctx, AddInput{Domain: "demo.test", Port: 80, PHP: "8.4"}); err != nil {
			t.Fatal(err)
		}
		want := "无法修改 hosts（/etc/hosts）。请以管理员身份运行后手动添加：127.0.0.1 demo.test"
		svc.hosts = &hostsFake{res: hosts.Result{Warning: want}}
		warn, err := svc.AddHosts(ctx, "demo.test")
		if err != nil {
			t.Fatalf("提权被拒是警告不是错误: %v", err)
		}
		if warn != want {
			t.Fatalf("应原样回传 hosts 的人话警告，实得 %q", warn)
		}
		if len(st.sites) != 1 {
			t.Fatalf("补写 hosts 不得改动站点: %+v", st.sites)
		}
	})

	t.Run("写入失败必须报错不静默", func(t *testing.T) {
		svc, _, _, em := newSiteSvc(t, nil)
		if err := svc.Add(ctx, AddInput{Domain: "demo.test", Port: 80, PHP: "8.4"}); err != nil {
			t.Fatal(err)
		}
		svc.hosts = &hostsFake{err: errors.New("读取 hosts 失败: permission denied")}
		em.events = nil
		if _, err := svc.AddHosts(ctx, "demo.test"); err == nil {
			t.Fatal("hosts 写入失败不得被吞掉")
		}
		if em.has("state:changed") {
			t.Fatalf("失败的任务不得走 Apply 广播，实得 %v", em.events)
		}
	})
}
