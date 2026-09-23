// T603 · doctor 环境诊断：§5.7 十五项检查逐条落地，每项给出通过/警告/失败三态与人话建议。
// 纯读诊断走「读接口」分区（对齐 AppService.GetState / BackupService.List）；仅「一键修复」（状态校准 / 清临时目录残留）产生写，且均幂等可重复。
// 系统探针（磁盘/端口/hosts/网络）以函数字段注入，默认接真实实现，单测替换以逐条制造失败态。
package service

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/util"
	"phpo/internal/vhost/hosts"
	"phpo/pkg/disk"
	"phpo/pkg/port"
)

// DoctorDocker 枚举托管容器以判定漂移/孤儿（*engine.Client 满足）
type DoctorDocker interface {
	ManagedContainers(ctx context.Context) ([]engine.ActualState, error)
}

// DoctorStore 读权威快照（*store.Store 满足）
type DoctorStore interface {
	BuildSnapshot() (*model.Snapshot, error)
}

// DoctorCache 缓存统计/损坏/临时目录残留（*cache.Manager 满足）
type DoctorCache interface {
	Stats() (model.CacheStats, error)
	ScanResidue() ([]string, error)
	ScanAndClearResidue(ctx context.Context) error
}

// DoctorCalibrator 状态校准一键修复（*LifecycleService 满足；内部写回 + 发 state:changed）
type DoctorCalibrator interface {
	Calibrate(ctx context.Context) (*engine.CalibrateResult, error)
}

// 外部端点与阈值（§5.7）
const (
	dockerHubPingURL = "https://registry-1.docker.io/v2/"
	githubPingURL    = "https://api.github.com/"
	diskWarnBytes    = 10 << 30 // 10 GiB：低于此仅警告
	siteDefaultPort  = 80
)

// DoctorService 组合注入依赖；系统探针字段可在单测替换
type DoctorService struct {
	probe     engine.Probe
	docker    DoctorDocker
	store     DoctorStore
	cache     DoctorCache
	lc        DoctorCalibrator
	env       config.Env
	hostsPath func() string

	freeDisk      func(path string) (uint64, error)
	portFree      func(port int) bool
	hostsWritable func(path string) bool
	dirWritable   func(path string) bool
	reachable     func(ctx context.Context, url string) (bool, error)
}

func NewDoctor(probe engine.Probe, docker DoctorDocker, store DoctorStore, cache DoctorCache, lc DoctorCalibrator, env config.Env) *DoctorService {
	d := &DoctorService{probe: probe, docker: docker, store: store, cache: cache, lc: lc, env: env}
	d.hostsPath = defaultHostsPath
	d.freeDisk = disk.Free
	d.portFree = port.Available
	d.hostsWritable = fileWritable
	d.dirWritable = dirProbeWritable
	d.reachable = httpReachable
	return d
}

// Run 依 §5.7 顺序产出 15 项检查结果与汇总计数
func (s *DoctorService) Run(ctx context.Context) model.DoctorReport {
	h := engine.Check(ctx, s.probe)
	var checks []model.DoctorCheck

	checks = append(checks, s.checkDockerInstalled(h))
	checks = append(checks, s.checkDockerRunning(h))
	checks = append(checks, s.checkDockerVersion(h))
	checks = append(checks, s.checkSitePort())
	checks = append(checks, s.checkDisk())
	checks = append(checks, s.checkDirWritable("home_writable", "PHPO_HOME 可写", s.env.PHPOHome))
	checks = append(checks, s.checkDirWritable("www_writable", "WWW_ROOT 可写", s.env.WWWRoot))
	checks = append(checks, s.checkHostsWritable())
	checks = append(checks, s.checkReachable(ctx, "dockerhub", "网络可访问 Docker Hub", dockerHubPingURL,
		"无法访问 Docker Hub。请检查网络或配置镜像加速。已缓存的版本仍可安装。"))
	checks = append(checks, s.checkReachable(ctx, "github", "GitHub Releases 可访问", githubPingURL,
		"无法检查更新。不影响使用。"))

	// 漂移/孤儿需 Docker 可拨号；否则降级为跳过（warn）
	var actual []engine.ActualState
	dockerOK := h.CanStart
	if dockerOK {
		a, err := s.docker.ManagedContainers(ctx)
		if err != nil {
			dockerOK = false
		} else {
			actual = a
		}
	}
	checks = append(checks, s.checkOrphans(dockerOK, actual))
	checks = append(checks, s.checkDrift(ctx, dockerOK))
	checks = append(checks, s.checkCacheIntegrity())
	checks = append(checks, s.checkCacheUsage())
	checks = append(checks, s.checkTempResidue())

	rep := model.DoctorReport{Checks: checks}
	for _, c := range checks {
		switch c.Status {
		case model.DoctorOK:
			rep.OK++
		case model.DoctorWarn:
			rep.Warnings++
		case model.DoctorErr:
			rep.Errors++
		}
	}
	return rep
}

// Fix 执行一项一键修复：calibrate 走 lifecycle（写回 + 广播），clear_temp 走缓存残留清空；均幂等可重复
func (s *DoctorService) Fix(ctx context.Context, id string) error {
	switch id {
	case "calibrate":
		_, err := s.lc.Calibrate(ctx)
		return err
	case "clear_temp":
		return s.cache.ScanAndClearResidue(ctx)
	default:
		return fmt.Errorf("未知修复动作: %s", id)
	}
}

// ---- 逐项检查 ----

func (s *DoctorService) checkDockerInstalled(h engine.Health) model.DoctorCheck {
	c := mk("docker_installed", "Docker 已安装")
	if h.Status == engine.StatusNotInstalled {
		c.Status, c.Detail, c.Hint = model.DoctorErr, h.Message, h.Hint
	} else {
		c.Status, c.Detail = model.DoctorOK, "已检测到 Docker 引擎"
	}
	return c
}

func (s *DoctorService) checkDockerRunning(h engine.Health) model.DoctorCheck {
	c := mk("docker_running", "Docker 正在运行")
	switch h.Status {
	case engine.StatusNotInstalled:
		c.Status, c.Detail, c.Hint = model.DoctorErr, "Docker 未安装，无法运行", h.Hint
	case engine.StatusNotRunning:
		c.Status, c.Detail, c.Hint = model.DoctorErr, h.Message, h.Hint
	case engine.StatusNoPermission:
		// socket 在盘上、daemon 也可能在跑，只是当前用户拨不动——这不是「未运行」，
		// 报「已安装」+「跑不动」才是可自查的口径。
		c.Status, c.Detail, c.Hint = model.DoctorErr, h.Message, h.Hint
	default:
		c.Status, c.Detail = model.DoctorOK, "Docker 引擎在线"
	}
	return c
}

func (s *DoctorService) checkDockerVersion(h engine.Health) model.DoctorCheck {
	c := mk("docker_version", "Docker 版本")
	if !h.CanStart {
		c.Status, c.Detail = model.DoctorWarn, "Docker 不可用，跳过版本检查"
		return c
	}
	if h.Status == engine.StatusOldVersion {
		c.Status, c.Detail, c.Hint = model.DoctorWarn, h.Message, h.Hint
	} else {
		c.Status, c.Detail = model.DoctorOK, "版本 "+h.Version
	}
	return c
}

func (s *DoctorService) checkSitePort() model.DoctorCheck {
	c := mk("site_port", "80 端口可用")
	if s.portFree(siteDefaultPort) {
		c.Status, c.Detail = model.DoctorOK, "80 端口空闲"
	} else {
		c.Status, c.Detail = model.DoctorWarn, "80 端口已被占用。新建站点会以降级态创建（暂不发布端口、暂不写 vhost），或改用其他端口。"
	}
	return c
}

func (s *DoctorService) checkDisk() model.DoctorCheck {
	c := mk("disk_space", "磁盘空间")
	free, err := s.freeDisk(s.env.PHPOHome)
	if err != nil {
		c.Status, c.Detail = model.DoctorWarn, "无法读取磁盘可用空间"
		return c
	}
	if free < diskWarnBytes {
		c.Status, c.Detail = model.DoctorWarn, fmt.Sprintf("磁盘剩余 %s。容器可能无法启动。", humanSize(int64(free)))
	} else {
		c.Status, c.Detail = model.DoctorOK, fmt.Sprintf("剩余 %s 可用", humanSize(int64(free)))
	}
	return c
}

func (s *DoctorService) checkDirWritable(id, title, path string) model.DoctorCheck {
	c := mk(id, title)
	if s.dirWritable(path) {
		c.Status, c.Detail = model.DoctorOK, path+" 可写"
	} else {
		c.Status, c.Detail, c.Hint = model.DoctorErr, path+" 不存在或无写权限。", "请先在装机向导完成工作目录设置；已存在则检查目录权限。"
	}
	return c
}

func (s *DoctorService) checkHostsWritable() model.DoctorCheck {
	c := mk("hosts_writable", "hosts 可写")
	p := s.hostsPath()
	if s.hostsWritable(p) {
		c.Status, c.Detail = model.DoctorOK, "可直接写入 hosts"
	} else {
		c.Status, c.Detail, c.Hint = model.DoctorWarn, "无法直接修改 hosts。", "请以管理员身份运行，或手动添加站点条目。"
	}
	return c
}

func (s *DoctorService) checkReachable(ctx context.Context, id, title, url, failHint string) model.DoctorCheck {
	c := mk(id, title)
	ok, err := s.reachable(ctx, url)
	if err != nil || !ok {
		c.Status, c.Detail, c.Hint = model.DoctorWarn, failHint, ""
	} else {
		c.Status, c.Detail = model.DoctorOK, "可达"
	}
	return c
}

func (s *DoctorService) checkOrphans(dockerOK bool, actual []engine.ActualState) model.DoctorCheck {
	c := mk("docker_clean", "Docker 资源清洁")
	if !dockerOK {
		c.Status, c.Detail = model.DoctorWarn, "Docker 不可用，跳过孤儿扫描"
		return c
	}
	// 卷/网络/镜像级完整扫描属 T605；此处以漂移 Extra 口径报告孤儿容器数
	snap, err := s.store.BuildSnapshot()
	if err != nil {
		c.Status, c.Detail = model.DoctorWarn, "无法读取快照"
		return c
	}
	res := engine.Calibrate(refsOf(snap.Installed), refsOf(snap.Running), actual)
	if n := len(res.Drift.Extra); n > 0 {
		c.Status, c.Detail = model.DoctorWarn, fmt.Sprintf("发现 %d 个孤儿容器，点击清理", n)
	} else {
		c.Status, c.Detail = model.DoctorOK, "未发现孤儿资源"
	}
	return c
}

func (s *DoctorService) checkDrift(ctx context.Context, dockerOK bool) model.DoctorCheck {
	c := mk("docker_consistent", "Docker 状态一致")
	if !dockerOK {
		c.Status, c.Detail = model.DoctorWarn, "Docker 不可用，跳过一致性检查"
		return c
	}
	snap, err := s.store.BuildSnapshot()
	if err != nil {
		c.Status, c.Detail = model.DoctorWarn, "无法读取快照"
		return c
	}
	actual, err := s.docker.ManagedContainers(ctx)
	if err != nil {
		c.Status, c.Detail = model.DoctorWarn, "无法读取容器状态"
		return c
	}
	res := engine.Calibrate(refsOf(snap.Installed), refsOf(snap.Running), actual)
	if res.Changed() {
		c.Status, c.Detail, c.Fix = model.DoctorWarn, "检测到状态漂移，点击校准", "calibrate"
	} else {
		c.Status, c.Detail = model.DoctorOK, "Docker 状态与期望一致"
	}
	return c
}

func (s *DoctorService) checkCacheIntegrity() model.DoctorCheck {
	c := mk("cache_integrity", "离线缓存完整性")
	st, err := s.cache.Stats()
	if err != nil {
		c.Status, c.Detail = model.DoctorWarn, "无法读取缓存统计"
		return c
	}
	if st.EntryCount == 0 {
		c.Status, c.Detail = model.DoctorOK, "暂无缓存条目"
	} else if st.Corrupted > 0 {
		c.Status, c.Detail = model.DoctorWarn, fmt.Sprintf("%d 个缓存条目，%d 个校验失败", st.EntryCount, st.Corrupted)
	} else {
		c.Status, c.Detail = model.DoctorOK, fmt.Sprintf("%d 个缓存条目均校验通过", st.EntryCount)
	}
	return c
}

func (s *DoctorService) checkCacheUsage() model.DoctorCheck {
	c := mk("cache_usage", "离线缓存占用")
	st, err := s.cache.Stats()
	if err != nil {
		c.Status, c.Detail = model.DoctorWarn, "无法读取缓存占用"
		return c
	}
	c.Status, c.Detail = model.DoctorOK, fmt.Sprintf("缓存占用 %s（%d 条目）", humanSize(st.TotalBytes), st.EntryCount)
	return c
}

func (s *DoctorService) checkTempResidue() model.DoctorCheck {
	c := mk("temp_residue", "临时目录残留")
	paths, err := s.cache.ScanResidue()
	if err != nil {
		c.Status, c.Detail = model.DoctorWarn, "无法扫描临时目录"
		return c
	}
	if n := len(paths); n > 0 {
		c.Status, c.Detail, c.Fix = model.DoctorWarn, fmt.Sprintf("检测到 %d 个临时目录残留，点击清空", n), "clear_temp"
	} else {
		c.Status, c.Detail = model.DoctorOK, "无临时目录残留"
	}
	return c
}

// ---- 系统探针默认实现 ----

func defaultHostsPath() string { return hosts.DefaultPath() }

func mk(id, title string) model.DoctorCheck {
	return model.DoctorCheck{ID: id, Title: title, Status: model.DoctorOK}
}

// httpReachable 任一 HTTP 响应即视为可达（401/403 亦说明网络通），仅传输层错误判不可达
func httpReachable(ctx context.Context, url string) (bool, error) {
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	return true, nil
}

// dirProbeWritable 只读判定目录存在且属主可写：诊断绝不建目录（创建只发生在装机向导「确认并创建」）
func dirProbeWritable(path string) bool { return writableByPerm(path) }

// fileWritable 以追加方式试开，不写内容；权限不足返回 false
func fileWritable(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// dirWritable 目录不存在则逐级创建后试写临时文件；任一步失败判不可写。
// **写侧探测：只允许装机向导 HomeEnsure 的 ensureTree 调用**——诊断等只读场景一律用 dirProbeWritable。
func dirWritable(path string) bool {
	if err := util.MkdirAll(path); err != nil {
		return false
	}
	probe := filepath.Join(path, ".phpo-writetest")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, util.FilePerm)
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return true
}
