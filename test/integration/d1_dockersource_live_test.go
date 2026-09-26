// Docker 镜像源的真机取证：证明「装某个镜像时用的是哪一个源」这条事实真能在真实 Docker 上说出来。
//
// 运行：PHPO_LIVE=1 go test ./test/integration/ -run TestDockerSource -v
//
// 单测里那台假件只能证明「编排顺序对」，证不了两件事：
//  1. 带源前缀的名字（docker.m.daocloud.io/library/redis:8）拉回来后，本机存的还是原始引用
//     php:8.4-fpm 这一类——不归一等于拉完仍「本机没有」，下次照样联网，且后面建容器直接找不到镜像；
//  2. 那次未命中事件里真的带着源名，界面上才说得清「这次是从哪台源拉的」。
//
// 因此这一条走的是应用自己的生产装配（steps.NewCacheManager → engine.ProbeSources / MirrorRef /
// ImagePull / ImageTag / ImageRemove），不复制任何生产逻辑。
//
// skip 守护：PHPO_LIVE=1 + Docker 可用；再往下有两处「环境不通就明说而不是判红」——
// 镜像源握手不通（无外网/源挂了下限流）、该镜像已在机（会走零网络重建分支，压根不碰源）。
// 收尾把本次拉下来的那份镜像删掉，不给共享 Docker 留脏状态（§5.13.1）。
package integration

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/task/steps"
)

// srcEvent 一次事件（名 + 原样载荷）：要读 cache:miss 的 source 字段，光记事件名不够
type srcEvent struct {
	Name    string
	Payload any
}

// srcRecorder 并发安全的事件记录器（镜像源拉取在 goroutine 里也可能发事件）
type srcRecorder struct {
	mu     sync.Mutex
	events []srcEvent
}

func (r *srcRecorder) Emit(name string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, srcEvent{name, payload})
}

// miss 取首个 cache:miss 事件载荷（本用例全程只应有一次未命中）
func (r *srcRecorder) miss() (model.CacheMissEvent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.Name == "cache:miss" {
			if ev, ok := e.Payload.(model.CacheMissEvent); ok {
				return ev, true
			}
		}
	}
	return model.CacheMissEvent{}, false
}

func (r *srcRecorder) has(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.Name == name {
			return true
		}
	}
	return false
}

// 候选源：默认只试官方 registry 的规范主机名（它 /v2/ 回 401，按判定算「活着」，
// 因此「能不能选中它」等价于「这台机能不能拨 docker.io」——不通即环境原因，明说并跳过）。
// 想换自己内网的源跑一遍：PHPO_LIVE_MIRROR=mirror.intranet:5000
var mirrorCandidates = []string{"registry-1.docker.io", "docker.m.daocloud.io"}

// 取证用的镜像故意选最小的公开镜像：这条用例的判据是「用了哪个源」，不是下载体积
const (
	dockerSourceKind    = "nginx"
	dockerSourceVersion = "alpine"
	dockerSourceRef     = "alpine:3.20"
)

func TestDockerSource_PullUsesFastestAndReportsIt_Live(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")

	cli, err := engine.New()
	if err != nil {
		t.Fatalf("构造 Docker 客户端失败: %v", err)
	}
	defer func() { _ = cli.Close() }()
	skipUnlessLive(t, cli)

	// 该镜像已在机上 → EnsureImage 走「零网络 save 重建缓存」分支，根本不碰源，本用例判据不适用。
	// 注意：这里只探测、不删除用户既有的镜像。
	has, err := cli.ImageExists(ctx, dockerSourceRef)
	if err != nil {
		t.Fatalf("探测本机镜像失败（探针错误不得当作「本机没有」）: %v", err)
	}
	if has {
		t.Skipf("%s 已在本机 Docker 里，安装链路会走零网络重建缓存而不碰镜像源，本用例取证不到源", dockerSourceRef)
	}

	// 先按应用自己的判定选一台真能握手的源；全不通即环境原因（无外网/源不可达）
	hosts := mirrorCandidates
	if v := os.Getenv("PHPO_LIVE_MIRROR"); v != "" {
		hosts = []string{v}
	}
	probes := engine.ProbeSources(ctx, hosts)
	var source string
	var latency int64 = -1
	for _, p := range probes {
		if !p.OK {
			continue
		}
		if source == "" || p.LatencyMs < latency {
			source, latency = p.Host, p.LatencyMs
		}
	}
	if source == "" {
		var why []string
		for _, p := range probes {
			why = append(why, p.Host+": "+p.Error)
		}
		t.Skipf("候选镜像源全部握手不通，无法取证「用了哪个源」：%s", strings.Join(why, "；"))
	}
	t.Logf("选中源 %s（握手延迟 %d ms）", source, latency)

	// 生产装配：真实的 PullFromSource（MirrorRef → 拉带前缀的名字 → ImageTag 归一 → 删前缀那份）
	rec := &srcRecorder{}
	m := steps.NewCacheManager(env, rec, cli)
	m.SetSourcesProvider(func() []string { return []string{source} })

	if err := m.EnsureImage(ctx, dockerSourceKind, dockerSourceVersion, dockerSourceRef); err != nil {
		// 源握手通了却拉不动：限流、没同步这个镜像、网络中途断，都属环境而非编排缺陷
		if msg := err.Error(); looksEnvironmental(msg) {
			t.Skipf("镜像源可达但本次拉取受环境所限（%s），本用例不判红: %v", source, err)
		}
		t.Fatalf("EnsureImage 经镜像源装 %s 失败: %v", dockerSourceRef, err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_ = cli.ImageRemove(bg, dockerSourceRef)
		_ = cli.ImageRemove(bg, engine.MirrorRef(source, dockerSourceRef))
	})

	// ① 界面要说得清这次用的哪一个源
	ev, ok := rec.miss()
	if !ok {
		t.Fatal("未命中应发 cache:miss")
	}
	if ev.Action != "pull" {
		t.Errorf("action 应为 pull（真拨了网络），得 %q", ev.Action)
	}
	if ev.Source != source {
		t.Errorf("cache:miss 的 source 应是实际用上的源 %q，得 %q", source, ev.Source)
	}

	// ② 带源前缀的名字必须归一回原始引用，否则下次照样联网、且建容器找不到镜像
	has, err = cli.ImageExists(ctx, dockerSourceRef)
	if err != nil {
		t.Fatalf("复探本机镜像失败: %v", err)
	}
	if !has {
		t.Fatalf("从 %s 拉回来的镜像应以原始引用 %s 落在本机（tag 归一未生效）", source, dockerSourceRef)
	}
	if prefixed := engine.MirrorRef(source, dockerSourceRef); prefixed != dockerSourceRef {
		if got, err := cli.ImageExists(ctx, prefixed); err == nil && got {
			t.Errorf("带源前缀的别名 %s 用完必须删掉（§5.13 不留脏状态）", prefixed)
		}
	}

	// ③ 拉到手即提升进缓存根，下次装机零网络；临时目录无论成败都要清空
	tar := env.OfflineImageTar(dockerSourceKind, dockerSourceVersion)
	if _, err := os.Stat(tar); err != nil {
		t.Fatalf("未命中拉取后应提升进缓存根 %s: %v", tar, err)
	}
	if !rec.has("cache:promote") {
		t.Error("提升应发 cache:promote")
	}
	if !rec.has("cache:tempdir-cleared") {
		t.Error("临时目录必须清空并发 cache:tempdir-cleared")
	}
	mf, err := m.LoadManifest(dockerSourceKind, dockerSourceVersion)
	if err != nil {
		t.Fatalf("读缓存清单失败: %v", err)
	}
	if mf == nil || mf.Image == nil || mf.Image.Sha256 == "" {
		t.Fatalf("清单应登记镜像条目及其 SHA256，得 %+v", mf)
	}
}

// TestDockerSource_LocalRebuildNeedsNoMirror_Live 证明「配了源也不该白拨网络」：
// 缓存丢了但镜像还在本机时，仍走零网络 save 重建（决策 22 的第二优先级），源完全不参与。
func TestDockerSource_LocalRebuildNeedsNoMirror_Live(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	home := t.TempDir()
	env := config.DerivePaths(home, home+"/www")
	cli, err := engine.New()
	if err != nil {
		t.Fatalf("构造 Docker 客户端失败: %v", err)
	}
	defer func() { _ = cli.Close() }()
	skipUnlessLive(t, cli)

	rec := &srcRecorder{}
	m := steps.NewCacheManager(env, rec, cli)
	m.SetSourcesProvider(func() []string { return []string{"unreachable.example.invalid"} })

	// 挑一个本机已有的公开镜像作证据（不动它，只 save 到临时缓存根）
	ref := pickLocalPublicImage(ctx, cli)
	if ref == "" {
		t.Skip("本机 Docker 里没有可用作证据的公开镜像（alpine/nginx/redis 一个都没有），跳过")
	}
	kind, version := dockerSourceKind, "local-rebuild"
	t.Logf("用本机已有镜像 %s 取证零网络重建", ref)
	if err := m.EnsureImage(ctx, kind, version, ref); err != nil {
		t.Fatalf("零网络重建缓存失败: %v", err)
	}
	ev, ok := rec.miss()
	if !ok {
		t.Fatal("未命中应发 cache:miss")
	}
	if ev.Action != "local" || ev.Source != "" {
		t.Errorf("本机已有镜像时应 action=local 且不带源（源没参与），得 %+v", ev)
	}
	if _, err := os.Stat(env.OfflineImageTar(kind, version)); err != nil {
		t.Fatalf("应零网络提升进缓存根: %v", err)
	}
}

// pickLocalPublicImage 在本机已有的镜像里找一个公开小镜像作证据；找不到返回空串。
// 只读取，绝不删除或改动用户的镜像。
func pickLocalPublicImage(ctx context.Context, cli *engine.Client) string {
	for _, ref := range []string{"alpine:3.20", "alpine:latest", "nginx:alpine", "redis:alpine", "hello-world:latest"} {
		if ok, err := cli.ImageExists(ctx, ref); err == nil && ok {
			return ref
		}
	}
	return ""
}

// looksEnvironmental 把「源通了但这一次拉不动」的环境原因与编排缺陷分开：
// 限流/超时/DNS/TLS/磁盘满属前者（明说并跳过），其余（如内部断言、路径错误）必须判红。
func looksEnvironmental(msg string) bool {
	for _, s := range []string{"429", "rate limit", "too many", "timeout", "deadline", "no such host",
		"tls", "certificate", "connection refused", "i/o timeout", "Temporary failure", "no space left"} {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(s)) {
			return true
		}
	}
	return false
}
