// T603 · doctor 诊断单测：注入探针逐条制造 ok/warn/err 三态，覆盖 §5.7 全 15 项 + 两类一键修复。
package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
)

// TestDirProbeWritable_CreatesNothing doctor 的目录可写判定必须只读：目录不存在不得被顺手创建
// （装机向导「确认并创建」是唯一允许创建目录/文件的入口）
func TestDirProbeWritable_CreatesNothing(t *testing.T) {
	base := t.TempDir()
	miss := filepath.Join(base, "phpo")
	if dirProbeWritable(miss) {
		t.Fatal("不存在的目录不应判可写")
	}
	if _, e := os.Stat(miss); !os.IsNotExist(e) {
		t.Fatalf("诊断探测创建了目录：%s", miss)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("诊断探测落了盘，base 下出现 %d 个条目", len(entries))
	}
}

// ---- 依赖替身 ----

type docProbe struct {
	ver string
	err error
}

func (p docProbe) Detect(context.Context) (string, error) { return p.ver, p.err }

type docDocker struct {
	actual []engine.ActualState
	err    error
}

func (d docDocker) ManagedContainers(context.Context) ([]engine.ActualState, error) {
	return d.actual, d.err
}

type docStore struct {
	snap *model.Snapshot
	err  error
}

func (s docStore) BuildSnapshot() (*model.Snapshot, error) { return s.snap, s.err }

type docCache struct {
	stats   model.CacheStats
	statsEr error
	resid   []string
	residEr error
	cleared int
}

func (c *docCache) Stats() (model.CacheStats, error)          { return c.stats, c.statsEr }
func (c *docCache) ScanResidue() ([]string, error)            { return c.resid, c.residEr }
func (c *docCache) ScanAndClearResidue(context.Context) error { c.cleared++; return nil }

type docCalibrator struct {
	called int
	err    error
}

func (l *docCalibrator) Calibrate(context.Context) (*engine.CalibrateResult, error) {
	l.called++
	return &engine.CalibrateResult{}, l.err
}

// newDoctor 构造「全绿」基线：Docker 可用、端口空闲、磁盘充足、目录/hosts 可写、网络可达、无残留无漂移。
// 单个测试按需覆写字段以制造目标状态。
func newDoctor() (*DoctorService, *docCache, *docCalibrator) {
	env := config.DerivePaths(t_HOME, t_WWW)
	cache := &docCache{stats: model.CacheStats{EntryCount: 2}}
	cal := &docCalibrator{}
	d := NewDoctor(
		docProbe{ver: "24.0.0"},
		docDocker{},
		docStore{snap: model.NewSnapshot()},
		cache, cal, env,
	)
	d.hostsPath = func() string { return "/etc/hosts" }
	d.freeDisk = func(string) (uint64, error) { return 500 << 30, nil }
	d.portFree = func(int) bool { return true }
	d.hostsWritable = func(string) bool { return true }
	d.dirWritable = func(string) bool { return true }
	d.reachable = func(context.Context, string) (bool, error) { return true, nil }
	return d, cache, cal
}

const (
	t_HOME = "/tmp/phpo-doctor-home"
	t_WWW  = "/tmp/phpo-doctor-www"
)

// find 从报告中按 ID 取出检查项，缺失即 fatal
func find(t *testing.T, rep model.DoctorReport, id string) model.DoctorCheck {
	t.Helper()
	for _, c := range rep.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("报告缺少检查项 %q", id)
	return model.DoctorCheck{}
}

func TestDoctor_AllGreen(t *testing.T) {
	d, _, _ := newDoctor()
	rep := d.Run(context.Background())
	if len(rep.Checks) != 15 {
		t.Fatalf("应产出 15 项检查，实得 %d", len(rep.Checks))
	}
	if rep.Errors != 0 || rep.Warnings != 0 {
		t.Fatalf("全绿基线不该有 warn/err：warn=%d err=%d", rep.Warnings, rep.Errors)
	}
	if rep.OK != 15 {
		t.Fatalf("OK 计数应为 15，实得 %d", rep.OK)
	}
}

func TestDoctor_DockerNotInstalled(t *testing.T) {
	d, _, _ := newDoctor()
	d.probe = docProbe{err: engine.ErrDockerNotInstalled}
	rep := d.Run(context.Background())
	if find(t, rep, "docker_installed").Status != model.DoctorErr {
		t.Fatal("Docker 未安装应判 err")
	}
	if find(t, rep, "docker_running").Status != model.DoctorErr {
		t.Fatal("未安装时 running 应判 err")
	}
	if find(t, rep, "docker_version").Status != model.DoctorWarn {
		t.Fatal("不可用时版本检查应降级 warn")
	}
	if find(t, rep, "docker_clean").Status != model.DoctorWarn {
		t.Fatal("不可用时孤儿扫描应跳过为 warn")
	}
	if find(t, rep, "docker_consistent").Status != model.DoctorWarn {
		t.Fatal("不可用时一致性检查应跳过为 warn")
	}
}

func TestDoctor_DockerOldVersion(t *testing.T) {
	d, _, _ := newDoctor()
	d.probe = docProbe{ver: "20.1.0"}
	rep := d.Run(context.Background())
	if find(t, rep, "docker_version").Status != model.DoctorWarn {
		t.Fatal("过旧版本应判 warn（§5.7 仅警告）")
	}
	if find(t, rep, "docker_running").Status != model.DoctorOK {
		t.Fatal("过旧但可用，running 仍应 OK")
	}
}

func TestDoctor_PortOccupied(t *testing.T) {
	d, _, _ := newDoctor()
	d.portFree = func(int) bool { return false }
	rep := d.Run(context.Background())
	c := find(t, rep, "site_port")
	if c.Status != model.DoctorWarn {
		t.Fatalf("80 占用应 warn（自动顺延），实得 %s", c.Status)
	}
}

func TestDoctor_LowDisk(t *testing.T) {
	d, _, _ := newDoctor()
	d.freeDisk = func(string) (uint64, error) { return 1 << 30, nil } // 1 GiB < 10 GiB
	rep := d.Run(context.Background())
	if find(t, rep, "disk_space").Status != model.DoctorWarn {
		t.Fatal("磁盘不足应 warn")
	}
}

func TestDoctor_HomeNotWritable(t *testing.T) {
	d, _, _ := newDoctor()
	d.dirWritable = func(string) bool { return false }
	rep := d.Run(context.Background())
	if find(t, rep, "home_writable").Status != model.DoctorErr {
		t.Fatal("PHPO_HOME 不可写应 err")
	}
	if find(t, rep, "www_writable").Status != model.DoctorErr {
		t.Fatal("WWW_ROOT 不可写应 err")
	}
}

func TestDoctor_HostsNotWritable(t *testing.T) {
	d, _, _ := newDoctor()
	d.hostsWritable = func(string) bool { return false }
	rep := d.Run(context.Background())
	if find(t, rep, "hosts_writable").Status != model.DoctorWarn {
		t.Fatal("hosts 不可写应 warn（可手动添加）")
	}
}

func TestDoctor_NetUnreachable(t *testing.T) {
	d, _, _ := newDoctor()
	d.reachable = func(context.Context, string) (bool, error) { return false, errors.New("net down") }
	rep := d.Run(context.Background())
	if find(t, rep, "dockerhub").Status != model.DoctorWarn {
		t.Fatal("Docker Hub 不可达应 warn（已缓存版本仍可装）")
	}
	if find(t, rep, "github").Status != model.DoctorWarn {
		t.Fatal("GitHub 不可达应 warn（不影响使用）")
	}
}

func TestDoctor_OrphanAndDrift(t *testing.T) {
	d, _, _ := newDoctor()
	// 期望为空、实际有一个 phpo-php-9.9 → Extra 非空（孤儿）+ Changed（漂移）
	d.docker = docDocker{actual: []engine.ActualState{
		{Ref: engine.ContainerRef{Kind: "php", Version: "9.9"}, Running: true},
	}}
	rep := d.Run(context.Background())
	o := find(t, rep, "docker_clean")
	if o.Status != model.DoctorWarn {
		t.Fatalf("发现孤儿应 warn，实得 %s", o.Status)
	}
	c := find(t, rep, "docker_consistent")
	if c.Status != model.DoctorWarn || c.Fix != "calibrate" {
		t.Fatalf("漂移应 warn 且带 calibrate 修复，实得 %s fix=%q", c.Status, c.Fix)
	}
}

func TestDoctor_CacheCorrupted(t *testing.T) {
	d, _, _ := newDoctor()
	d.cache = &docCache{stats: model.CacheStats{EntryCount: 3, Corrupted: 1}}
	rep := d.Run(context.Background())
	if find(t, rep, "cache_integrity").Status != model.DoctorWarn {
		t.Fatal("有损坏条目应 warn")
	}
}

func TestDoctor_CacheStatsError(t *testing.T) {
	d, _, _ := newDoctor()
	d.cache = &docCache{statsEr: errors.New("io")}
	rep := d.Run(context.Background())
	if find(t, rep, "cache_integrity").Status != model.DoctorWarn {
		t.Fatal("统计读取失败应降级 warn，不阻断")
	}
	if find(t, rep, "cache_usage").Status != model.DoctorWarn {
		t.Fatal("占用读取失败应降级 warn")
	}
}

func TestDoctor_TempResidue(t *testing.T) {
	d, cache, _ := newDoctor()
	cache.resid = []string{"/tmp/a/ext", "/tmp/b/ext"}
	rep := d.Run(context.Background())
	c := find(t, rep, "temp_residue")
	if c.Status != model.DoctorWarn || c.Fix != "clear_temp" {
		t.Fatalf("残留应 warn 且带 clear_temp 修复，实得 %s fix=%q", c.Status, c.Fix)
	}
	// 一键修复：清空残留，幂等可重复
	for i := 0; i < 2; i++ {
		if err := d.Fix(context.Background(), "clear_temp"); err != nil {
			t.Fatalf("Fix clear_temp 失败: %v", err)
		}
	}
	if cache.cleared != 2 {
		t.Fatalf("ScanAndClearResidue 应被调用 2 次，实得 %d", cache.cleared)
	}
}

func TestDoctor_FixCalibrate(t *testing.T) {
	d, _, cal := newDoctor()
	if err := d.Fix(context.Background(), "calibrate"); err != nil {
		t.Fatalf("Fix calibrate 失败: %v", err)
	}
	if cal.called != 1 {
		t.Fatalf("Calibrate 应被调用 1 次，实得 %d", cal.called)
	}
}

func TestDoctor_FixUnknown(t *testing.T) {
	d, _, _ := newDoctor()
	if err := d.Fix(context.Background(), "nope"); err == nil {
		t.Fatal("未知修复动作应报错")
	}
}
