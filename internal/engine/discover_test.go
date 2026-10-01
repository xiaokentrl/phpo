// 发现层的用例分两头：纯函数逐条锁死「认得出什么、认不出为什么」，httptest 假守护进程锁一次完整扫描。
//
// 为什么要拆两头：分类判据（容器名 → 镜像引用 → 端口只作佐证）最容易在改一句话时静默漂走，
// 而漂移的后果是「界面把外部装的 MySQL 说成没有」——纯函数用例最快抓到；
// 完整扫描要锁的是另一件事：同名去重谁赢、Swarm 任务容器落在哪一堆、干净机器上那一串是不是空的。
package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"

	"phpo/internal/model"
)

// --- 容器名这一层 ---

func TestKindVersionFromName(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		version string
		ok      bool
	}{
		{"phpo-mysql-8.0", "mysql", "8.0", true},
		{"phpo-php-8.4", "php", "8.4", true},
		{"phpo-nginx-alpine", "nginx", "alpine", true},
		// 版本自己带连字符：必须按服务种类前缀比，不能按第一个连字符切。
		{"phpo-pgsql-17-alpine3.19", "pgsql", "17-alpine3.19", true},
		// 网络不是服务：IsPhpoResource 为真也不该认成一个 kind。
		{"phpo-network", "", "", false},
		{"phpo-", "", "", false},
		{"phpo-redis-", "", "", false},
		// 非 phpo 命名（外部容器走镜像那一条判据）。
		{"my-mysql", "", "", false},
		// 坏版本直接不认，且不再试别的 kind。
		{"phpo-mysql-8.0/../etc", "", "", false},
	}
	for _, c := range cases {
		kind, version, ok := kindVersionFromName(c.name)
		if ok != c.ok {
			t.Fatalf("kindVersionFromName(%q) ok=%v，期望 %v（got kind=%q version=%q）", c.name, ok, c.ok, kind, version)
		}
		if !ok {
			continue
		}
		if kind != c.kind || version != c.version {
			t.Fatalf("kindVersionFromName(%q) = (%q, %q)，期望 (%q, %q)", c.name, kind, version, c.kind, c.version)
		}
	}
}

// --- 镜像引用这一层 ---

func TestKindVersionFromImage(t *testing.T) {
	type want struct {
		kind    string
		version string
		ok      bool
	}
	cases := []struct {
		image string
		want  want
	}{
		{"mysql:8", want{"mysql", "8", true}},
		// pgsql 的官方仓库名是 postgres，认出来要落到 pgsql 这个 kind。
		{"postgres:17", want{"pgsql", "17", true}},
		{"redis:8", want{"redis", "8", true}},
		{"nginx:1.27", want{"nginx", "1.27", true}},
		{"php:8.4-fpm", want{"php", "8.4", true}},
		// 从镜像源拉下来的名字也要认得出仓库名。
		{"docker.m.daocloud.io/library/mysql:8", want{"mysql", "8", true}},
		{"registry.example.com:5000/library/postgres:17", want{"pgsql", "17", true}},
		// tag 能原样round-trip 的版本即照收。
		{"mysql:latest", want{"mysql", "latest", true}},
		// 以下是必须拒掉的：拒的理由各不相同，测试把它们逐个钉住。
		{"php:latest", want{}},   // ImageTagFor("php","latest") 是 latest-fpm ≠ latest
		{"php:8.4-cli", want{}},  // 没有 -fpm 后缀
		{"phpo/php:8.4", want{}}, // 仓库段 php 认得出种类，但 tag 缺 -fpm——**不是**因为仓库不认识
		{"mysql", want{}},        // 无 tag：不能替用户凭空造一个版本
		{"mysql@sha256:abc123", want{}},
		{"someapp/database:1.0", want{}},
		{"", want{}},
	}
	for _, c := range cases {
		kind, version, ok := kindVersionFromImage(c.image)
		if ok != c.want.ok {
			t.Fatalf("kindVersionFromImage(%q) ok=%v，期望 %v（got kind=%q version=%q）", c.image, ok, c.want.ok, kind, version)
		}
		if !ok {
			continue
		}
		if kind != c.want.kind || version != c.want.version {
			t.Fatalf("kindVersionFromImage(%q) = (%q, %q)，期望 (%q, %q)", c.image, kind, version, c.want.kind, c.want.version)
		}
	}
}

// phpo/php:8.4 被拒的理由必须是「tag 认不出版本」，不是「仓库不认识」。
// 这一条单独立用例：写成「因为仓库不认识」的注释会让下一个改这里的人以为加个仓库名就能收进来。
func TestKindVersionFromImage_PHPCommitImageRejectedByTag(t *testing.T) {
	if _, known := imageKindByRepo["php"]; !known {
		t.Fatalf("仓库名 php 本应映射到 KindPHP，否则下面的断言理由不成立")
	}
	if _, ok := versionFromTag(string(model.KindPHP), "8.4"); ok {
		t.Fatalf("固化镜像的 tag 8.4 不带 -fpm，必须认不出版本")
	}
	if _, ok := versionFromTag(string(model.KindPHP), "8.4-fpm"); !ok {
		t.Fatalf("8.4-fpm 应还原成版本 8.4")
	}
}

func TestVersionFromTag(t *testing.T) {
	type want struct {
		version string
		ok      bool
	}
	cases := []struct {
		kind string
		tag  string
		want want
	}{
		{"mysql", "8.0", want{"8.0", true}},
		{"pgsql", "17-alpine3.19", want{"17-alpine3.19", true}},
		{"php", "8.4-fpm", want{"8.4", true}},
		{"php", "8.4", want{}},     // 正向规则算出来是 8.4-fpm，对不上
		{"php", "8.4-FPM", want{}}, // 大小写不合正向输出
		{"redis", "8", want{"8", true}},
		{"mysql", "8.0/../x", want{}}, // 过不了路径安全
	}
	for _, c := range cases {
		version, ok := versionFromTag(c.kind, c.tag)
		if ok != c.want.ok {
			t.Fatalf("versionFromTag(%q, %q) ok=%v，期望 %v（got %q）", c.kind, c.tag, ok, c.want.ok, version)
		}
		if ok && version != c.want.version {
			t.Fatalf("versionFromTag(%q, %q) = %q，期望 %q", c.kind, c.tag, version, c.want.version)
		}
	}
}

// --- 三层判据的合流 ---

func TestClassifyContainer(t *testing.T) {
	// 名字与镜像都能认时，以名字为准（那才是 phpo 一直在用的容器）。
	cs := container.Summary{
		Image: "mysql:8",
		Names: []string{"/phpo-mysql-8.0"},
		State: container.StateRunning,
	}
	fs, ok := classifyContainer("phpo-mysql-8.0", cs)
	if !ok {
		t.Fatalf("按 phpo 命名应认得出")
	}
	if fs.MatchedBy != MatchedName {
		t.Fatalf("MatchedBy = %q，期望 %q", fs.MatchedBy, MatchedName)
	}
	if !fs.PhpoNamed {
		t.Fatalf("真名正好是 phpo-{kind}-{version}，PhpoNamed 应为 true")
	}
	if !fs.Running {
		t.Fatalf("State=running，Running 应为 true")
	}

	// 外部容器：名字不规则，靠镜像认；版本 8 与镜像 tag 8 原样一致。
	fs, ok = classifyContainer("customer-mysql", container.Summary{Image: "docker.m.daocloud.io/library/mysql:8", State: "exited"})
	if !ok {
		t.Fatalf("按镜像引用应认得出")
	}
	if fs.MatchedBy != MatchedImage || fs.Kind != "mysql" || fs.Version != "8" {
		t.Fatalf("外部容器认成 kind=%q version=%q matchedBy=%q", fs.Kind, fs.Version, fs.MatchedBy)
	}
	if fs.PhpoNamed {
		t.Fatalf("名字不是 phpo 的命名规矩，PhpoNamed 应为 false")
	}
	if fs.Running {
		t.Fatalf("State=exited，Running 应为 false")
	}

	// 名字与镜像都对不上：不隐身，交给「认不出的那一堆」。
	if _, ok := classifyContainer("nginx-proxy", container.Summary{Image: "nginxproxy/proxy:latest"}); ok {
		t.Fatalf("既不是 phpo 命名也不是五种服务的官方镜像，应认不出")
	}
}

// --- 端口只作佐证：说清「看着像谁」，但不据此纳管 ---

func TestKindHintFromPorts(t *testing.T) {
	// 端口按数字序排（字典序会把 nginx 写成 443/80/8080）；只报 private 端口，
	// 因为用户填的服务端口就是它；PublicPort==0 的（未发布到宿主）不参与。
	hint := kindHintFromPorts(container.Summary{Ports: []container.Port{
		{PrivatePort: 8080, PublicPort: 8080, Type: "tcp"},
		{PrivatePort: 443, PublicPort: 443, Type: "tcp"},
		{PrivatePort: 80, PublicPort: 80, Type: "tcp"},
	}})
	if hint != "nginx（发布了 80/443/8080 的端口）" {
		t.Fatalf("端口佐证 = %q，期望点名到 80/443/8080", hint)
	}

	// UDP 撞上同一号端口不当成同一个服务。
	if got := kindHintFromPorts(container.Summary{Ports: []container.Port{
		{PrivatePort: 3306, PublicPort: 3306, Type: "udp"},
	}}); got != "" {
		t.Fatalf("UDP 的 3306 不该给出 MySQL 的提示，实得 %q", got)
	}

	// 没发布到宿主（PublicPort==0）不佐证。
	if got := kindHintFromPorts(container.Summary{Ports: []container.Port{
		{PrivatePort: 6379, Type: "tcp"},
	}}); got != "" {
		t.Fatalf("未发布的 6379 不该给出 Redis 的提示，实得 %q", got)
	}

	// php-fpm 的 9000 只在容器网络内部用，不在表里。
	if got := kindHintFromPorts(container.Summary{Ports: []container.Port{
		{PrivatePort: 9000, PublicPort: 9000, Type: "tcp"},
	}}); got != "" {
		t.Fatalf("9000 不该被认成某种服务，实得 %q", got)
	}

	// 多种类逐个点名、用「、」连接。
	multi := kindHintFromPorts(container.Summary{Ports: []container.Port{
		{PrivatePort: 6379, PublicPort: 6379, Type: "tcp"},
		{PrivatePort: 3306, PublicPort: 33060, Type: "tcp"},
	}})
	for _, want := range []string{"mysql（发布了 3306 的端口）", "redis（发布了 6379 的端口）"} {
		if !strings.Contains(multi, want) {
			t.Fatalf("多种类佐证缺 %q，实得 %q", want, multi)
		}
	}
}

func TestUnmatchedReason(t *testing.T) {
	// 有端口佐证：说清看着像谁，同时说明缺的是版本那一半。
	r := unmatchedReason("db", container.Summary{
		Image: "percona/percona-server:8.0",
		Ports: []container.Port{{PrivatePort: 3306, PublicPort: 3306, Type: "tcp"}},
	})
	if !strings.Contains(r, "看着像 mysql") || !strings.Contains(r, "认不出版本") {
		t.Fatalf("端口佐证的原因应同时说「看着像谁」与「认不出版本」，实得 %q", r)
	}

	// 带 phpo- 前缀但不属于五种服务（如 phpo-network）。
	r = unmatchedReason("phpo-network", container.Summary{Image: "alpine:3.19"})
	if !strings.Contains(r, "不属于五个服务种类之一") {
		t.Fatalf("phpo- 前缀的非服务容器应点名「不属于五种服务」，实得 %q", r)
	}

	// 全都认不出：不把「不知道」说成「没有」。
	r = unmatchedReason("web", container.Summary{Image: "someapp/database:1.0"})
	if !strings.Contains(r, "someapp/database:1.0") {
		t.Fatalf("认不出时应把镜像引用原样带出，实得 %q", r)
	}
}

func TestVersionSafe(t *testing.T) {
	for _, okCase := range []string{"8.0", "17-alpine3.19", "latest", "8.4.2"} {
		if !versionSafe(okCase) {
			t.Fatalf("版本 %q 本应通过路径安全校验", okCase)
		}
	}
	tooLong := strings.Repeat("8", 129)
	for _, bad := range []string{"", "/etc", "..", "a..b", "8.0 ", " 8.0", tooLong, "8\x00.0"} {
		if versionSafe(bad) {
			t.Fatalf("版本 %q 本应被拒（路径安全 / 长度 / 空字节 / 首尾空白）", bad)
		}
	}
}

// --- Discovery 的索引与原地替换 ---

func TestDiscoveryIndexAndReplace(t *testing.T) {
	d := &Discovery{index: map[string]string{"mysql\x1f8": "customer-mysql"}}
	d.Services = []FoundService{{Kind: "mysql", Version: "8", Name: "customer-mysql"}}

	if name, ok := d.NameFor("mysql", "8"); !ok || name != "customer-mysql" {
		t.Fatalf("NameFor = (%q, %v)，期望 (customer-mysql, true)", name, ok)
	}
	if _, ok := d.NameFor("mysql", "9"); ok {
		t.Fatalf("没有 9 这个版本，NameFor 应返回 false")
	}

	// 空接收者不炸：调用方拿不到 Discovery 时也要能问。
	var nilD *Discovery
	if _, ok := nilD.NameFor("mysql", "8"); ok {
		t.Fatalf("nil Discovery 不该认得出任何容器")
	}

	if _, seen := d.serviceAt("pgsql", "17"); seen {
		t.Fatalf("serviceAt 不该凭空认出版本")
	}
	d.replaceService("mysql", "8", FoundService{Kind: "mysql", Version: "8", Name: "phpo-mysql-8", PhpoNamed: true})
	if name, _ := d.NameFor("mysql", "8"); name != "phpo-mysql-8" {
		t.Fatalf("replaceService 后索引应指向新容器，实得 %q", name)
	}
	if d.Services[0].Name != "phpo-mysql-8" {
		t.Fatalf("replaceService 应原地改 Services，实得 %q", d.Services[0].Name)
	}
	// 换的是不存在的那一版：原地不动，不新增。
	d.replaceService("redis", "8", FoundService{Kind: "redis", Version: "8"})
	if len(d.Services) != 1 {
		t.Fatalf("replaceService 不该新增服务，现得 %d 条", len(d.Services))
	}
}

// --- 一次完整扫描（假守护进程）---

// 这里不能用 fakeDaemon：它把 GET /containers/{id}/json 一律当 inspect 应答，
// 而发现层要的是「数组」形状的 /containers/json?all=1，落进它的 default 分支即 Fatalf。
// 因此本用例自带一个只回答这两件事的 handler（同 TestRemoveContainer_MissingIsNoop 的写法）。
func TestDiscoverServices_Scan(t *testing.T) {
	summaries := []container.Summary{
		// phpo 自己装的：按容器名认出。
		{ID: "1", Names: []string{"/phpo-mysql-8.0"}, Image: "mysql:8.0", State: container.StateRunning},
		// 外部装的：按镜像认出，且用的是容器真名。
		{ID: "2", Names: []string{"/customer-mysql"}, Image: "docker.m.daocloud.io/library/mysql:8", State: "exited"},
		// 同一个 (kind, version) 上挤了两个：phpo 命名的那颗赢，另一颗进差异清单。
		{ID: "3", Names: []string{"/a-redis"}, Image: "redis:8", State: "exited"},
		{ID: "4", Names: []string{"/phpo-redis-8"}, Image: "redis:8", State: container.StateRunning},
		// Swarm 任务容器：不纳管，但要点名。
		{ID: "5", Names: []string{"/stack_web.1.abc"}, Image: "nginx:1.27", State: container.StateRunning,
			Labels: map[string]string{swarmServiceLabel: "stack_web"}},
		// 认不出的容器不隐身。
		{ID: "6", Names: []string{"/random-box"}, Image: "someapp/database:1.0", State: "exited"},
	}

	daemon := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.51")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			if r.URL.Query().Get("all") != "1" {
				t.Errorf("发现层要看到停掉的容器，all 参数应为 1，实得 %q", r.URL.Query().Get("all"))
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(summaries)
		default:
			t.Fatalf("未预期的守护进程请求: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	})

	c := newFakeClient(t, daemon)
	d, err := c.DiscoverServices(context.Background())
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}

	// 认出来的服务按 kind → version → name 排；(mysql,8) 与 (mysql,8.0) 是两个版本，各自一条。
	// redis 的两颗容器同为 (redis,8)，合并成一条，输的那颗进差异清单（见下）。
	if len(d.Services) != 3 {
		t.Fatalf("认出的服务数 = %d，期望 3（%+v）", len(d.Services), d.Services)
	}
	for _, want := range []string{"mysql/8", "mysql/8.0", "redis/8"} {
		found := false
		for _, s := range d.Services {
			if s.Kind+"/"+s.Version == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("识别结果缺 %q，实得 %+v", want, d.Services)
		}
	}

	// 冲突只认一个，且认的是符合命名规矩的那颗。
	name, ok := d.NameFor("redis", "8")
	if !ok || name != "phpo-redis-8" {
		t.Fatalf("redis/8 应认下 phpo 命名的容器，实得 (%q, %v)", name, ok)
	}
	if len(d.Duplicates) != 1 || d.Duplicates[0].Name != "a-redis" {
		t.Fatalf("输的那颗要进差异清单，实得 %+v", d.Duplicates)
	}

	// 外部容器启停要用真名，不能拿 phpo-{kind}-{version} 去猜。
	if name, ok := d.NameFor("mysql", "8"); !ok || name != "customer-mysql" {
		t.Fatalf("mysql/8 的真名应是 customer-mysql，实得 (%q, %v)", name, ok)
	}

	// 认不出的两堆：random-box + Swarm 任务容器，逐个点名，不静默吞掉。
	if len(d.Unrecognized) != 2 {
		t.Fatalf("认不出的容器数 = %d，期望 2（%+v）", len(d.Unrecognized), d.Unrecognized)
	}
	var sawSwarm bool
	for _, u := range d.Unrecognized {
		if u.Name == "stack_web.1.abc" {
			sawSwarm = true
			if !strings.Contains(u.Reason, "Swarm") {
				t.Fatalf("Swarm 任务容器要说清为什么不接管，实得 %q", u.Reason)
			}
		}
	}
	if !sawSwarm {
		t.Fatalf("Swarm 任务容器要点名进认不出的那一堆，实得 %+v", d.Unrecognized)
	}
	// Swarm 那颗绝不能同时出现在差异清单里。
	for _, s := range d.Duplicates {
		if s.Name == "stack_web.1.abc" {
			t.Fatalf("Swarm 任务容器不该被纳管后再进差异清单")
		}
	}
}

func TestDiscoverServices_CleanScanHasNoUnrecognized(t *testing.T) {
	daemon := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.51")
			_, _ = w.Write([]byte("OK"))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]container.Summary{
				{ID: "1", Names: []string{"/phpo-php-8.4"}, Image: "php:8.4-fpm", State: container.StateRunning},
			})
		default:
			t.Fatalf("未预期的守护进程请求: %s %s", r.Method, r.URL.Path)
		}
	})

	d, err := newFakeClient(t, daemon).DiscoverServices(context.Background())
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	// 干净机器上必须「什么都不说」：这里靠 len 判空而不是 != nil——无名字时那一片本就是 nil 切片。
	if len(d.Unrecognized) != 0 || len(d.Duplicates) != 0 {
		t.Fatalf("全认得出的机器不该有认不出的项，实得 unrecognized=%+v duplicates=%+v", d.Unrecognized, d.Duplicates)
	}
	if len(d.Services) != 1 || d.Services[0].Kind != "php" || d.Services[0].Version != "8.4" {
		t.Fatalf("服务识别 = %+v，期望只有一条 php/8.4", d.Services)
	}
}

// 没有名字的容器（极少见）跳过，而不是纳成一个无名服务。
func TestDiscoverServices_SkipsUnnamed(t *testing.T) {
	daemon := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.51")
			_, _ = w.Write([]byte("OK"))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]container.Summary{
				{ID: "1", Names: []string{}, Image: "mysql:8", State: container.StateRunning},
			})
		default:
			t.Fatalf("未预期的守护进程请求: %s %s", r.Method, r.URL.Path)
		}
	})

	d, err := newFakeClient(t, daemon).DiscoverServices(context.Background())
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if len(d.Services) != 0 || len(d.Unrecognized) != 0 {
		t.Fatalf("无名容器应跳过，实得 services=%+v unrecognized=%+v", d.Services, d.Unrecognized)
	}
}

// 一次 Docker 调用挂了不能当成「这台机器上没有服务」——那会让服务层把库存清空。
func TestDiscoverServices_ErrorNotSwallowed(t *testing.T) {
	c := newFakeClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("API-Version", "1.51")
			_, _ = w.Write([]byte("OK"))
			return
		}
		http.Error(w, "daemon is gone", http.StatusInternalServerError)
	}))
	if _, err := c.DiscoverServices(context.Background()); err == nil {
		t.Fatalf("列出容器失败必须上抛，不得当成「没有服务」")
	} else if !strings.Contains(err.Error(), "列出 Docker 容器失败") {
		t.Fatalf("错误应说清是列容器挂了，实得 %q", err.Error())
	}
}

// 客户端还没准备好：直接拒绝，不去碰一个 nil 的 SDK。
func TestDiscoverServices_NilClient(t *testing.T) {
	var c *Client
	if _, err := c.DiscoverServices(context.Background()); err == nil {
		t.Fatalf("无客户端时应报错")
	}
}
