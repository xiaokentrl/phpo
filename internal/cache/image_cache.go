// 镜像安装编排（§5.14.3）：查→命中 load 零网络 / 未命中先探本地镜像（已有即免拉取）→save→promote→清临时
package cache

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"phpo/internal/model"
)

// EnsureImage 保证 kind/version 对应镜像已在本地 Docker store，全程遵守三条铁律
func (m *Manager) EnsureImage(ctx context.Context, kind, version, ref string) error {
	if m.db == nil {
		return errNoBackend
	}
	lookup, err := m.LookupImage(kind, version)
	if err != nil {
		return err
	}
	if lookup.Corrupted {
		m.emitCorrupted(kind, version, model.ManifestPackage{Name: filepath.Base(lookup.Path)})
	}
	if lookup.Hit {
		if err := m.db.LoadImage(ctx, lookup.Path); err != nil {
			return err
		}
		m.emitHit(kind, version, "offline", lookup.Size)
		return nil
	}

	// 未命中（含损坏回退）：先探本地 Docker store——已有镜像即就地 save 提升，全程不碰网络
	// （断网/内网机器缓存丢失后重建的唯一路径）；确实没有才拉取。走网络时仍遵守必清临时目录。
	has, err := m.db.ImageExists(ctx, ref)
	if err != nil {
		return err
	}
	action := "local"
	if !has {
		action = "pull"
	}
	reason := ReasonCompileFailed
	// 取消优先于失败：ctx 被取消时按必清时机 3（cancelled）上报
	defer func() {
		r := reason
		if ctx.Err() != nil {
			r = ReasonCancelled
		}
		_ = m.ClearTempDir(ctx, kind, version, r)
	}()

	tmpDir, err := m.EnsureTempDir(kind, version)
	if err != nil {
		return err
	}
	tmpTar := filepath.Join(tmpDir, "image.tar")

	source := ""
	if !has {
		// cache:miss 留到镜像真到手之后再发：action=pull 要同时说清用的哪一个源，
		// 而「最快的源」也可能拉不动（限流、没同步这个镜像），顺位回落之后才知道最终用了谁。
		// 全都拉不动时整单报错（错误里逐源点名），不留「说了未命中却没拉到」的半句话。
		var perr error
		source, perr = m.fetchImage(ctx, ref)
		if perr != nil {
			return perr
		}
	}
	m.emitMiss(kind, version, action, source)
	if err := m.db.SaveImage(ctx, ref, tmpTar); err != nil {
		return err
	}
	if err := m.PromoteImage(kind, version, ref, tmpTar); err != nil {
		return err
	}
	reason = ReasonCompileOK
	m.emitPromote(kind, version, nil)
	return nil
}

// fetchImage 把 ref 拉到本机 Docker store，返回**实际用上**的那个镜像源（空串 = 直连官方）。
//
// 为什么还要逐个试：设置页给出的「最快」只是握手那一刻的结论，源可能半路限流、也可能根本没同步这个镜像。
// 于是按延迟从小到大顺位回落，全都不行才直连官方——镜像源是加速手段，不该变成新的失败面（§0.2 规则 16）。
func (m *Manager) fetchImage(ctx context.Context, ref string) (string, error) {
	hosts := m.sourceHosts()
	if len(hosts) == 0 {
		return "", m.db.PullImage(ctx, ref)
	}
	order := fastestFirst(m.db.ProbeSources(ctx, hosts))
	var fails []string
	for _, host := range order {
		err := m.db.PullFromSource(ctx, host, ref)
		if err == nil {
			return host, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err() // 取消优先于失败：让 defer 按 cancelled 清临时目录
		}
		fails = append(fails, fmt.Sprintf("%s: %v", host, err))
	}
	if err := m.db.PullImage(ctx, ref); err != nil {
		fails = append(fails, fmt.Sprintf("直连官方: %v", err))
		return "", fmt.Errorf("拉取 %s 失败（%s）", ref, strings.Join(fails, "；"))
	}
	return "", nil
}

// fastestFirst 只保留握手通的源，按延迟升序（延迟相同保持配置顺序）。
// 握手不通的不进尝试序列：它连一次握手都没答上，拿它去拉一个镜像只是把失败拖成几十秒。
func fastestFirst(probes []model.MirrorSource) []string {
	ok := make([]model.MirrorSource, 0, len(probes))
	for _, p := range probes {
		if p.OK {
			ok = append(ok, p)
		}
	}
	sort.SliceStable(ok, func(i, j int) bool { return ok[i].LatencyMs < ok[j].LatencyMs })
	hosts := make([]string, 0, len(ok))
	for _, p := range ok {
		hosts = append(hosts, p.Host)
	}
	return hosts
}

// LoadExtImage 把缓存里的扩展固化镜像零网络载入本机 Docker，返回它的 ref（§5.14.3 第一优先级）。
// 缓存缺席返回 ("", false, nil)——由调用方回落基座；损坏先告警再按缺席处理（校验失败不得当作命中）。
func (m *Manager) LoadExtImage(ctx context.Context, version string) (string, bool, error) {
	if m.db == nil {
		return "", false, errNoBackend
	}
	lk, err := m.LookupExtImage(version)
	if err != nil {
		return "", false, err
	}
	if lk.Corrupted {
		m.emitCorrupted("php", version, model.ManifestPackage{Name: filepath.Base(lk.Path)})
	}
	if !lk.Hit {
		return "", false, nil
	}
	mf, err := m.LoadManifest("php", version)
	if err != nil {
		return "", false, err
	}
	if mf == nil || mf.ExtImage == nil || mf.ExtImage.Name == "" {
		return "", false, nil
	}
	if err := m.db.LoadImage(ctx, lk.Path); err != nil {
		return "", false, err
	}
	m.emitHit("php", version, "offline", lk.Size)
	return mf.ExtImage.Name, true, nil
}
