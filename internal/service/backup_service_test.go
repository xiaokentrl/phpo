// T602 验收：备份/恢复/删除三段式。
//   - Create：暂停运行中的数据服务 → 导出 SQLite 快照 → 打 tar.gz → 自动重启；成功广播 state:changed。
//   - List：按文件名（含时间戳）倒序，新备份在前。
//   - Delete：存在即删并广播；缺失/穿越名报错。
//   - Restore：解包并校验归档自带 SQLite 快照 → 清空命名空间 → 落盘 → 逻辑重放 SQLite（ApplyTaskResult）→ 按快照重建容器（缓存优先镜像 + Install）。
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
	"phpo/internal/task"
	"phpo/pkg/archive"
)

// ---- 假件 ----

// bkStore 实现 BackupStore：BackupTo 写占位库文件，BuildSnapshot 返回受控快照，ApplyTaskResult 记录重放
type bkStore struct {
	snap     *model.Snapshot
	applied  []model.TaskMeta
	toErr    error
	applyErr error
}

func newBkStore() *bkStore { return &bkStore{snap: model.NewSnapshot()} }

func (s *bkStore) BackupTo(dst string) error {
	if s.toErr != nil {
		return s.toErr
	}
	return os.WriteFile(dst, []byte("sqlite-snapshot"), 0o644)
}
func (s *bkStore) BuildSnapshot() (*model.Snapshot, error) { return s.snap, nil }
func (s *bkStore) ApplyTaskResult(meta model.TaskMeta, _ *model.Snapshot) error {
	if s.applyErr != nil {
		return s.applyErr
	}
	s.applied = append(s.applied, meta)
	return nil
}

// bkLC 实现 BackupLifecycle：记录 Install/Start/Stop 序列
type bkLC struct {
	installed []string
	started   []string
	stopped   []string
}

func (l *bkLC) Install(_ context.Context, kind model.ServiceKind, version string) error {
	l.installed = append(l.installed, string(kind)+"/"+version)
	return nil
}
func (l *bkLC) Start(_ context.Context, kind model.ServiceKind, version string) error {
	l.started = append(l.started, string(kind)+"/"+version)
	return nil
}
func (l *bkLC) Stop(_ context.Context, kind model.ServiceKind, version string) error {
	l.stopped = append(l.stopped, string(kind)+"/"+version)
	return nil
}

// bkImages 实现 BackupImages
type bkImages struct{ ensured []string }

func (i *bkImages) EnsureImage(_ context.Context, kind, version, ref string) error {
	i.ensured = append(i.ensured, kind+"/"+version+"="+ref)
	return nil
}

// bkDocker 实现 BackupDocker：受控托管容器列表 + 停删记录
type bkDocker struct {
	actual  []engine.ActualState
	stopped []string
	removed []string
}

func (d *bkDocker) ManagedContainers(context.Context) ([]engine.ActualState, error) {
	return d.actual, nil
}
func (d *bkDocker) StopContainer(_ context.Context, name string) error {
	d.stopped = append(d.stopped, name)
	return nil
}
func (d *bkDocker) RemoveContainer(_ context.Context, name string) error {
	d.removed = append(d.removed, name)
	return nil
}

// bkReader 实现 SnapshotReader（归档内只读快照）
type bkReader struct{ snap *model.Snapshot }

func (r *bkReader) BuildSnapshot() (*model.Snapshot, error) { return r.snap, nil }
func (r *bkReader) Close() error                            { return nil }

// ---- 装配 ----

func newBackupSvc(t *testing.T) (*BackupService, *bkStore, *bkLC, *bkImages, *bkDocker, *fakeEmitter, config.Env, *config.ConfigStore) {
	t.Helper()
	home := t.TempDir()
	env := config.DerivePaths(home, filepath.Join(home, "www"))
	for _, d := range []string{env.BackupRoot, env.PHPRoot, env.NginxRoot, env.WWWRoot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// 放一个真实配置文件，令归档至少含 php 顶层
	if err := os.MkdirAll(filepath.Join(env.PHPRoot, "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.PHPRoot, "conf", "php.ini"), []byte("[PHP]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 真实 ConfigStore：config.yaml 落在临时 home，Path/Reload 走真实现
	cfg, err := config.LoadFromPath(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	st := newBkStore()
	lc := &bkLC{}
	imgs := &bkImages{}
	dock := &bkDocker{}
	em := &fakeEmitter{}
	open := func(string) (SnapshotReader, error) { return &bkReader{snap: st.snap}, nil }
	svc := NewBackupService(st, lc, imgs, dock, open, em, env, cfg, task.NewManager(em))
	return svc, st, lc, imgs, dock, em, env, cfg
}

// ---- Create ----

func TestBackup_Create_Happy(t *testing.T) {
	svc, st, lc, _, _, em, env, _ := newBackupSvc(t)
	st.snap.Running["mysql"] = []string{"8.4"} // 运行中的数据服务应被暂停并重启

	bf, err := svc.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(bf.File, "backup-") || !strings.HasSuffix(bf.File, ".tar.gz") {
		t.Fatalf("归档命名不符: %s", bf.File)
	}
	if bf.Items == 0 {
		t.Fatalf("items 应 > 0")
	}
	if _, err := os.Stat(filepath.Join(env.BackupRoot, bf.File)); err != nil {
		t.Fatalf("归档应落盘: %v", err)
	}
	// 暂停 → 重启 mysql 8.4
	if strings.Join(lc.stopped, ",") != "mysql/8.4" {
		t.Fatalf("应暂停 mysql/8.4，实得 %v", lc.stopped)
	}
	if strings.Join(lc.started, ",") != "mysql/8.4" {
		t.Fatalf("备份后应自动重启 mysql/8.4，实得 %v", lc.started)
	}
	if !em.has("state:changed") {
		t.Fatalf("应广播 state:changed，实得 %v", em.events)
	}
	// List 能见到该备份
	list, err := svc.List()
	if err != nil || len(list) != 1 || list[0].File != bf.File {
		t.Fatalf("List 应含新备份，实得 %+v err=%v", list, err)
	}
}

func TestBackup_Create_ExportFailure_NoRestart(t *testing.T) {
	svc, st, lc, _, _, em, env, _ := newBackupSvc(t)
	st.snap.Running["mysql"] = []string{"8.4"}
	st.toErr = errors.New("db busy") // 导出失败 → 回滚：暂停步 RB 重启

	if _, err := svc.Create(context.Background()); err == nil {
		t.Fatal("导出失败应报错")
	}
	if strings.Join(lc.started, ",") != "mysql/8.4" {
		t.Fatalf("失败回滚应重启被暂停服务，实得 %v", lc.started)
	}
	// 归档不应残留
	if entries, _ := os.ReadDir(env.BackupRoot); len(entries) != 0 {
		t.Fatalf("失败不应留下归档，实得 %v", entries)
	}
	if em.has("state:changed") {
		t.Fatalf("失败不应广播 state:changed")
	}
}

// ---- List 排序 ----

func TestBackup_List_Order(t *testing.T) {
	svc, _, _, _, _, _, env, _ := newBackupSvc(t)
	for _, n := range []string{"backup-20260101-000000.tar.gz", "backup-20260901-000000.tar.gz", "backup-20260301-000000.tar.gz", "stray.txt"} {
		if err := os.WriteFile(filepath.Join(env.BackupRoot, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("应忽略非 backup-*.tar.gz，实得 %+v", list)
	}
	if list[0].File != "backup-20260901-000000.tar.gz" || list[2].File != "backup-20260101-000000.tar.gz" {
		t.Fatalf("应按时间倒序，实得 %s ... %s", list[0].File, list[2].File)
	}
}

// ---- Delete ----

func TestBackup_Delete(t *testing.T) {
	svc, _, _, _, _, _, env, _ := newBackupSvc(t)
	file := "backup-20260101-000000.tar.gz"
	host := filepath.Join(env.BackupRoot, file)
	if err := os.WriteFile(host, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(host); !os.IsNotExist(err) {
		t.Fatalf("删除后归档不应存在")
	}
	// 缺失 → 任务执行期报错
	if err := svc.Delete(context.Background(), file); err == nil {
		t.Fatal("删除不存在的归档应报错")
	}
	// 穿越 → 直接拒绝，不落任务
	if err := svc.Delete(context.Background(), "../evil.tar.gz"); err == nil {
		t.Fatal("穿越文件名应被拒绝")
	}
}

func TestBackup_Path(t *testing.T) {
	svc, _, _, _, _, _, env, _ := newBackupSvc(t)
	file := "backup-20260101-000000.tar.gz"
	if err := os.WriteFile(filepath.Join(env.BackupRoot, file), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := svc.Path(file)
	if err != nil || p != filepath.Join(env.BackupRoot, file) {
		t.Fatalf("Path 应返回绝对路径，实得 %s err=%v", p, err)
	}
	if _, err := svc.Path("missing.tar.gz"); err == nil {
		t.Fatal("不存在的归档应报错")
	}
	if _, err := svc.Path("sub/dir.tar.gz"); err == nil {
		t.Fatal("含目录分隔的文件名应被拒绝")
	}
}

// ---- Restore ----

func TestBackup_Restore(t *testing.T) {
	svc, st, lc, imgs, dock, em, _, cfg := newBackupSvc(t)
	// 写入明文密码到 config.yaml（YAML 权威），Create 应原样打包
	if err := cfg.SetPassword("mysql", "8.4", "123456"); err != nil {
		t.Fatal(err)
	}
	// 先制造一个真实归档
	bf, err := svc.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// 抹去宿主 config.yaml 并清内存态，模拟异机/丢失后恢复
	if err := os.Remove(cfg.Path()); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Reload(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cfg.GetPassword("mysql", "8.4"); ok {
		t.Fatal("清空后应无 mysql 密码")
	}
	// 现状：已安装 php/mysql；已有两个托管容器待清空
	st.snap.Installed["php"] = []string{"8.4"}
	st.snap.Installed["mysql"] = []string{"8.4"}
	dock.actual = []engine.ActualState{
		{Ref: engine.ContainerRef{Kind: "nginx", Version: "alpine"}, Running: true},
		{Ref: engine.ContainerRef{Kind: "redis", Version: "8"}, Running: false},
	}

	if err := svc.Restore(context.Background(), bf.File); err != nil {
		t.Fatal(err)
	}
	// 清空命名空间：两个都删
	if strings.Join(dock.removed, ",") != "phpo-nginx-alpine,phpo-redis-8" {
		t.Fatalf("应删除全部托管容器，实得 %v", dock.removed)
	}
	// 逻辑重放：ApplyTaskResult 被调用且带 restore 语义
	if len(st.applied) != 1 || st.applied[0].Type != "restore" {
		t.Fatalf("应逻辑重放 SQLite，实得 %+v", st.applied)
	}
	// 重建：按快照对每个已安装版本缓存优先镜像 + Install
	if len(imgs.ensured) != 2 || len(lc.installed) != 2 {
		t.Fatalf("应重建 php+mysql，实得 imgs=%v install=%v", imgs.ensured, lc.installed)
	}
	// 明文 config.yaml 应随包恢复落回，且热重载反映到内存态
	if pw, ok, _ := cfg.GetPassword("mysql", "8.4"); !ok || pw != "123456" {
		t.Fatalf("应恢复明文密码并热重载，实得 pw=%q ok=%v", pw, ok)
	}
	if !em.has("state:changed") {
		t.Fatalf("恢复后应广播 state:changed")
	}
}

func TestBackup_Restore_RejectsTraversal(t *testing.T) {
	svc, _, _, _, _, _, _, _ := newBackupSvc(t)
	if err := svc.Restore(context.Background(), "../outside.tar.gz"); err == nil {
		t.Fatal("穿越文件名应被拒绝且不落任务")
	}
	if err := svc.Restore(context.Background(), "backup-20260101-000000.tar.gz"); err == nil {
		t.Fatal("不存在的归档应报错")
	}
}

// 归档缺 db/phpo.db：必须在破坏性步骤之前中止。store.Open 会顺手建库，若让它凭空造一个空快照，
// 后续「空重放」会抹掉 installed/sites/ext，而容器命名空间已被第一步清空——恢复失败就此升级为数据全丢。
func TestBackup_Restore_ArchiveWithoutSQLiteSnapshot(t *testing.T) {
	svc, st, _, _, dock, _, env, _ := newBackupSvc(t)
	st.snap.Installed["php"] = []string{"8.4"}
	bf, err := svc.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// 重打包：只留 php 顶层，去掉归档内的 SQLite 快照
	staging := t.TempDir()
	if _, err := archive.Extract(filepath.Join(env.BackupRoot, bf.File), staging); err != nil {
		t.Fatal(err)
	}
	noDB := filepath.Join(env.BackupRoot, "backup-no-db.tar.gz")
	if _, err := archive.Create(noDB, []archive.Source{{ArcPrefix: "php", HostPath: filepath.Join(staging, "php")}}); err != nil {
		t.Fatal(err)
	}

	if err := svc.Restore(context.Background(), "backup-no-db.tar.gz"); err == nil {
		t.Fatal("缺 SQLite 快照的归档必须中止恢复")
	}
	if len(dock.removed) != 0 {
		t.Fatalf("中止前不得清空容器命名空间，实得 %v", dock.removed)
	}
	if len(st.applied) != 0 {
		t.Fatalf("不得将空快照重放进运行态库，实得 %+v", st.applied)
	}
}
