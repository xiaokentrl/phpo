// T602 · 备份 / 恢复 / 删除备份：把 PHPO_HOME 配置/数据、WWW 站点、离线缓存与 SQLite 快照打成 tar.gz，
// 并支持异机恢复（解包并验货 → 清空 phpo 命名空间 → 落盘 → 应用内逻辑重放 SQLite → 重建容器，§5.13.11）。
// 全程三段式（硬红线 5）+ 后端权威广播（硬红线 4）；不含 Docker 镜像（原型 restore warn4）；config.yaml/密码原样随 SQLite 快照打包（用户裁决）。
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/task"
	"phpo/pkg/archive"
)

// BackupStore 备份所需持久子集（*store.Store 满足）：快照导出 / 读取 / 全量逻辑重放
type BackupStore interface {
	BackupTo(dst string) error
	BuildSnapshot() (*model.Snapshot, error)
	ApplyTaskResult(meta model.TaskMeta, snap *model.Snapshot) error
}

// BackupLifecycle 容器编排子集（*LifecycleService 满足；均 Op 包装，可安全嵌于本服务任务内）
type BackupLifecycle interface {
	Install(ctx context.Context, kind model.ServiceKind, version string) error
	Start(ctx context.Context, kind model.ServiceKind, version string) error
	Stop(ctx context.Context, kind model.ServiceKind, version string) error
}

// BackupImages 缓存优先镜像就绪（*cache.Manager 满足）
type BackupImages interface {
	EnsureImage(ctx context.Context, kind, version, ref string) error
}

// BackupDocker 枚举并停删全部托管容器（*engine.Client 满足）
type BackupDocker interface {
	ManagedContainers(ctx context.Context) ([]engine.ActualState, error)
	StopContainer(ctx context.Context, name string) error
	RemoveContainer(ctx context.Context, name string) error
}

// SnapshotReader 只读打开的归档库快照（*store.Store 满足）
type SnapshotReader interface {
	BuildSnapshot() (*model.Snapshot, error)
	Close() error
}

// SnapshotOpener 打开归档内 db/phpo.db 为只读快照（DI 注入 store.Open；单测注入假件）
type SnapshotOpener func(path string) (SnapshotReader, error)

// BackupConfig 配置权威门面子集（*config.ConfigStore 满足）：取 config.yaml 路径 + 落盘后热重载内存态
type BackupConfig interface {
	Path() string
	Reload() error
}

// BackupService 备份门面；写操作一律经 task.Manager 三段式
type BackupService struct {
	store BackupStore
	lc    BackupLifecycle
	imgs  BackupImages
	dock  BackupDocker
	open  SnapshotOpener
	em    Emitter
	env   config.Env
	cfg   BackupConfig // config.yaml：随包携带、恢复落回并热重载
	tasks *task.Manager
	seq   atomic.Uint64
}

func NewBackupService(store BackupStore, lc BackupLifecycle, imgs BackupImages, dock BackupDocker, open SnapshotOpener, em Emitter, env config.Env, cfg BackupConfig, tm *task.Manager) *BackupService {
	return &BackupService{store: store, lc: lc, imgs: imgs, dock: dock, open: open, em: em, env: env, cfg: cfg, tasks: tm}
}

// dbKinds 备份前需暂停以保证宿主数据目录一致的服务种类
var dbKinds = []model.ServiceKind{model.KindMySQL, model.KindPgsql, model.KindRedis}

// ---- 读接口 ----

// List 扫描 BACKUP_ROOT 下 backup-*.tar.gz，按时间倒序返回展示条目
func (s *BackupService) List() ([]model.BackupFile, error) {
	entries, err := os.ReadDir(s.env.BackupRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []model.BackupFile{}, nil
		}
		return nil, err
	}
	var out []model.BackupFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "backup-") || !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items := 0
		if tops, err := archive.TopLevel(filepath.Join(s.env.BackupRoot, e.Name())); err == nil {
			items = len(tops)
		}
		out = append(out, model.BackupFile{
			File:  e.Name(),
			Size:  humanSize(info.Size()),
			At:    info.ModTime().Format("2006-01-02 15:04"),
			Items: items,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File > out[j].File }) // 文件名含时间戳，倒序即新在前
	return out, nil
}

// Path 返回归档绝对路径（供前端触发本地下载）；仅接受合法文件名，拒绝穿越
func (s *BackupService) Path(file string) (string, error) {
	host, err := s.archivePath(file)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(host); err != nil {
		return "", fmt.Errorf("%s 不存在", file)
	}
	return host, nil
}

// SaveAs 把归档原样导出到用户经原生保存框选定的目标路径（下载用，不落任务）。
// 源经 archivePath 白名单约束（硬红线 3）；目标由系统对话框保证可信。
func (s *BackupService) SaveAs(file, dst string) error {
	host, err := s.archivePath(file)
	if err != nil {
		return err
	}
	src, err := os.Open(host)
	if err != nil {
		return fmt.Errorf("%s 不存在", file)
	}
	defer src.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, src)
	return err
}

// ---- 写接口（三段式）----

// Create 备份：暂停数据服务 → 导出 SQLite 快照 → 打 tar.gz → 重启数据服务
func (s *BackupService) Create(ctx context.Context) (model.BackupFile, error) {
	ts := time.Now()
	name := fmt.Sprintf("backup-%s.tar.gz", ts.Format("20060102-150405"))
	archivePath := filepath.Join(s.env.BackupRoot, name)
	tmpDir, err := os.MkdirTemp(s.env.BackupRoot, ".bak-")
	if err != nil {
		return model.BackupFile{}, err
	}
	dbSnap := filepath.Join(tmpDir, "phpo.db")

	var items int
	var paused []engine.ContainerRef
	t := &task.Task{
		ID:    s.newID("backup"),
		Label: "创建备份 " + name,
		Meta:  model.TaskMeta{Type: "backup"},
		Steps: []task.Step{
			s.pauseStep(&paused),
			&task.FuncStep{StepName: "导出 SQLite 快照", Exec: func(context.Context, task.StepLog) error {
				return s.store.BackupTo(dbSnap)
			}, Clean: func() { _ = os.RemoveAll(tmpDir) }},
			&task.FuncStep{StepName: "打包归档", Exec: func(_ context.Context, log task.StepLog) error {
				tops, err := archive.Create(archivePath, backupSources(s.env, s.cfg.Path(), dbSnap))
				if err != nil {
					return err
				}
				items = len(tops)
				log.Log(string(model.LogOk), "归档顶层: "+strings.Join(tops, ", "))
				return nil
			}, RB: func(context.Context) error {
				if e := os.Remove(archivePath); e != nil && !os.IsNotExist(e) {
					return e
				}
				return nil
			}},
			restartStep(s.lc, &paused), // 备份完成后自动重启（原型 backup.warning）
		},
	}
	if _, err := s.tasks.Run(ctx, t); err != nil {
		return model.BackupFile{}, err
	}
	size := int64(0)
	if info, e := os.Stat(archivePath); e == nil {
		size = info.Size()
	}
	s.em.Emit("state:changed", snapshotPayload(s.store))
	return model.BackupFile{File: name, Size: humanSize(size), At: ts.Format("2006-01-02 15:04"), Items: items}, nil
}

// Restore 恢复（异机口径，§5.13.11）：解包并校验归档自带 SQLite 快照 → 清空 phpo 命名空间 → 落盘 → 逻辑重放 SQLite → 重建容器
func (s *BackupService) Restore(ctx context.Context, file string) error {
	host, err := s.archivePath(file)
	if err != nil {
		return err
	}
	if _, err := os.Stat(host); err != nil {
		return fmt.Errorf("备份归档不存在: %s", file)
	}
	staging, err := os.MkdirTemp("", "phpo-restore-")
	if err != nil {
		return err
	}
	meta := model.TaskMeta{Type: "restore"}
	t := &task.Task{
		ID:    s.newID("restore"),
		Label: "恢复备份 " + file,
		Meta:  meta,
		Steps: []task.Step{
			&task.FuncStep{StepName: "解包归档", Exec: func(_ context.Context, log task.StepLog) error {
				n, err := archive.Extract(host, staging)
				if err != nil {
					return err
				}
				// 解包后立即验货：归档必须自带 SQLite 快照，缺快照即在此中止。
				// 这一步特意排在清命名空间之前——store.Open 会顺手建库，凭空造出的空快照
				// 一旦被重放，就会连容器带运行态一起抹掉，把「恢复失败」升级成「数据全丢」。
				if info, e := os.Stat(filepath.Join(staging, "db", "phpo.db")); e != nil || info.Size() == 0 {
					return errors.New("归档缺少数据库快照 db/phpo.db，已中止恢复（未清空容器、未落盘）")
				}
				log.Log(string(model.LogOk), fmt.Sprintf("解出 %d 个文件", n))
				return nil
			}, Clean: func() { _ = os.RemoveAll(staging) }},
			&task.FuncStep{StepName: "清空 phpo 容器命名空间", Exec: func(ctx context.Context, _ task.StepLog) error {
				return s.clearNamespace(ctx)
			}},
			&task.FuncStep{StepName: "落盘配置/数据/站点/缓存", Exec: func(_ context.Context, _ task.StepLog) error {
				return s.materialize(staging)
			}},
			&task.FuncStep{StepName: "逻辑重放 SQLite 快照", Exec: func(context.Context, task.StepLog) error {
				r, err := s.open(filepath.Join(staging, "db", "phpo.db"))
				if err != nil {
					return fmt.Errorf("打开归档快照失败: %w", err)
				}
				defer r.Close()
				snap, err := r.BuildSnapshot()
				if err != nil {
					return err
				}
				return s.store.ApplyTaskResult(meta, snap)
			}},
			&task.FuncStep{StepName: "重建已安装容器", Exec: func(ctx context.Context, log task.StepLog) error {
				return s.recreateAll(ctx, log)
			}},
		},
	}
	if _, err := s.tasks.Run(ctx, t); err != nil {
		return err
	}
	s.em.Emit("state:changed", snapshotPayload(s.store))
	return nil
}

// Delete 删除归档（二次确认在前端；此处仅执行，路径经白名单约束）
func (s *BackupService) Delete(ctx context.Context, file string) error {
	host, err := s.archivePath(file)
	if err != nil {
		return err
	}
	t := &task.Task{
		ID:    s.newID("backup-delete"),
		Label: "删除备份 " + file,
		Meta:  model.TaskMeta{Type: "backup-delete"},
		Steps: []task.Step{
			&task.FuncStep{StepName: "删除归档文件", Exec: func(context.Context, task.StepLog) error {
				return os.Remove(host)
			}},
		},
	}
	if _, err := s.tasks.Run(ctx, t); err != nil {
		return err
	}
	s.em.Emit("state:changed", snapshotPayload(s.store))
	return nil
}

// ---- 内部编排助手 ----

// pauseStep 暂停运行中的数据服务以保证宿主数据一致；被暂停的容器记入 paused，供后续重启与 RB 复用
func (s *BackupService) pauseStep(paused *[]engine.ContainerRef) *task.FuncStep {
	return &task.FuncStep{StepName: "暂停数据服务", Exec: func(ctx context.Context, log task.StepLog) error {
		snap, err := s.store.BuildSnapshot()
		if err != nil {
			return err
		}
		for _, k := range dbKinds {
			for _, v := range snap.Running[string(k)] {
				log.Log(string(model.LogDim), "暂停 "+string(k)+" "+v)
				if err := s.lc.Stop(ctx, k, v); err != nil {
					return err
				}
				*paused = append(*paused, engine.ContainerRef{Kind: string(k), Version: v})
			}
		}
		return nil
	}, RB: func(ctx context.Context) error {
		for _, r := range *paused {
			if e := s.lc.Start(ctx, model.ServiceKind(r.Kind), r.Version); e != nil {
				return e
			}
		}
		return nil
	}}
}

// restartStep 备份成功后自动重启暂停过的数据服务（原型 backup.warning：完成后自动重启）
func restartStep(lc BackupLifecycle, paused *[]engine.ContainerRef) *task.FuncStep {
	return &task.FuncStep{StepName: "重启数据服务", Exec: func(ctx context.Context, log task.StepLog) error {
		for _, r := range *paused {
			log.Log(string(model.LogDim), "重启 "+r.Kind+" "+r.Version)
			if err := lc.Start(ctx, model.ServiceKind(r.Kind), r.Version); err != nil {
				return err
			}
		}
		return nil
	}}
}

// clearNamespace 停 + 删全部 phpo 托管容器（保留宿主数据，卸载语义 §5.13.7）
func (s *BackupService) clearNamespace(ctx context.Context) error {
	actual, err := s.dock.ManagedContainers(ctx)
	if err != nil {
		return err
	}
	for _, a := range actual {
		name := a.Ref.Name()
		_ = s.dock.StopContainer(ctx, name) // 未运行时停失败可忽略
		if err := s.dock.RemoveContainer(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

// materialize 把暂存解包内容落回宿主：五类服务目录 + offline → PHPO_HOME，www → WWW_ROOT
func (s *BackupService) materialize(staging string) error {
	resolver := map[string]string{
		"php": s.env.PHPRoot, "nginx": s.env.NginxRoot, "mysql": s.env.MysqlRoot,
		"pgsql": s.env.PgsqlRoot, "redis": s.env.RedisRoot, "offline": s.env.OfflineRoot,
	}
	for _, d := range []string{"php", "nginx", "mysql", "pgsql", "redis", "offline"} {
		src := filepath.Join(staging, d)
		if _, err := os.Stat(src); err != nil {
			continue // 归档未含该组（异机可缺）：跳过
		}
		if err := copyTree(resolver[d], src); err != nil {
			return err
		}
	}
	www := filepath.Join(staging, "www")
	if _, err := os.Stat(www); err == nil {
		if err := copyTree(s.env.WWWRoot, www); err != nil {
			return err
		}
	}
	// 明文 config.yaml 原样落回 XDG 用户配置目录（用户裁决：随包携带、恢复覆盖，§1.5），并热重载内存态供后续重建读取
	cfgSrc := filepath.Join(staging, "config", "config.yaml")
	if b, err := os.ReadFile(cfgSrc); err == nil {
		if err := os.MkdirAll(filepath.Dir(s.cfg.Path()), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(s.cfg.Path(), b, 0o600); err != nil {
			return err
		}
		if err := s.cfg.Reload(); err != nil {
			return err
		}
	}
	return nil
}

// recreateAll 按活库快照重建已安装容器：缓存优先镜像 → lifecycle.Install（复用已落盘配置）
func (s *BackupService) recreateAll(ctx context.Context, log task.StepLog) error {
	snap, err := s.store.BuildSnapshot()
	if err != nil {
		return err
	}
	kinds := make([]string, 0, len(snap.Installed))
	for k := range snap.Installed {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds) // php→mysql 等固定顺序，减少启动竞态
	for _, k := range kinds {
		vers := append([]string(nil), snap.Installed[k]...)
		sort.Strings(vers)
		for _, v := range vers {
			ref, err := engine.ImageRefFor(k, v)
			if err != nil {
				return err
			}
			log.Log(string(model.LogDim), "重建 "+k+" "+v)
			if err := s.imgs.EnsureImage(ctx, k, v, ref); err != nil {
				return err
			}
			if err := s.lc.Install(ctx, model.ServiceKind(k), v); err != nil {
				return err
			}
		}
	}
	return nil
}

// archivePath 约束归档文件名：必须是纯 basename 且位于 BACKUP_ROOT 下（硬红线 3）
func (s *BackupService) archivePath(file string) (string, error) {
	if file == "" || file != filepath.Base(file) || strings.Contains(file, "..") {
		return "", fmt.Errorf("非法备份文件名: %s", file)
	}
	return filepath.Join(s.env.BackupRoot, file), nil
}

func (s *BackupService) newID(op string) string {
	return fmt.Sprintf("%s-%d", op, s.seq.Add(1))
}

// snapshotPayload 拉取权威快照并组成 state:changed 载荷；读失败退化为空快照事件（不阻断收尾）
func snapshotPayload(st BackupStore) map[string]any {
	snap, err := st.BuildSnapshot()
	if err != nil {
		snap = model.NewSnapshot()
	}
	return map[string]any{"snapshot": snap}
}

// backupSources 归档内容清单：PHPO_HOME 五服务目录 + 离线缓存 + WWW 站点 + SQLite 快照 + 明文 config.yaml；不含 Docker 镜像。
// 用户裁决：config.yaml 与密码原样打包（明文策略，§1.5），恢复时一并落回。
func backupSources(env config.Env, cfgPath, dbSnap string) []archive.Source {
	return []archive.Source{
		{ArcPrefix: "php", HostPath: env.PHPRoot},
		{ArcPrefix: "nginx", HostPath: env.NginxRoot},
		{ArcPrefix: "mysql", HostPath: env.MysqlRoot},
		{ArcPrefix: "pgsql", HostPath: env.PgsqlRoot},
		{ArcPrefix: "redis", HostPath: env.RedisRoot},
		{ArcPrefix: "offline", HostPath: env.OfflineRoot},
		{ArcPrefix: "www", HostPath: env.WWWRoot},
		{ArcPrefix: "db/phpo.db", HostPath: dbSnap},
		{ArcPrefix: "config/config.yaml", HostPath: cfgPath},
	}
}

// copyTree 把 srcRoot 目录内容合并写入 dstRoot（覆盖同名，保留 dst 其余文件）
func copyTree(dstRoot, srcRoot string) error {
	return filepath.Walk(srcRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dstRoot, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	f := float64(n)
	idx := -1
	for f >= unit && idx < len(units)-1 {
		f /= unit
		idx++
	}
	return fmt.Sprintf("%.1f %s", f, units[idx])
}
