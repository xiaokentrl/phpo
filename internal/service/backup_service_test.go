// T602 验收：备份/恢复/删除三段式。
//   - Create：暂停运行中的数据服务 → 导出 SQLite 快照 → 打 tar.gz → 自动重启；成功广播 state:changed。
//   - List：按文件名（含时间戳）倒序，新备份在前。
//   - Delete：存在即删并广播；缺失/穿越名报错。
//   - Restore：解包并校验归档自带 SQLite 快照 → 清空命名空间 → 落盘 → 逻辑重放 SQLite（ApplyTaskResult）→ 按快照重建容器（缓存优先镜像 + Install）。
package service

import (
	"context"
	"errors"
	"io"
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

// bkDocker 实现 BackupDocker：受控托管容器列表 + 停删记录 + 容器内命令（逻辑导出）回放
type bkDocker struct {
	actual    []engine.ActualState
	stopped   []string
	removed   []string
	execs     []string // 每次容器内命令："<容器名> <argv 拼接>"
	execErr   error    // 非空则令转储失败
	execEmpty bool     // true 则写出零字节转储（模拟命令成功但没内容）
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
func (d *bkDocker) ExecStream(_ context.Context, name string, cmd []string, stdout, _ io.Writer) error {
	d.execs = append(d.execs, name+" "+strings.Join(cmd, " "))
	if d.execErr != nil {
		return d.execErr
	}
	if d.execEmpty {
		return nil
	}
	_, err := io.WriteString(stdout, "-- phpo dump of "+name+"\n")
	return err
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

// ---- 逻辑导出（归档内的 dump/）----

// extractArchive 把归档解到临时目录，供断言「本次备份到底带了什么」。
func extractArchive(t *testing.T, env config.Env, file string) string {
	t.Helper()
	dst := t.TempDir()
	if _, err := archive.Extract(filepath.Join(env.BackupRoot, file), dst); err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestBackup_Create_DumpsRunningServices(t *testing.T) {
	svc, st, lc, _, dock, em, env, cfg := newBackupSvc(t)
	if err := cfg.SetPassword("mysql", "8.4", "p@ss w0rd"); err != nil { // 含空格：argv 直传，不经 shell
		t.Fatal(err)
	}
	st.snap.Running["mysql"] = []string{"8.4"}
	st.snap.Running["pgsql"] = []string{"17"}
	st.snap.Installed["mysql"] = []string{"8.4", "8.0"} // 8.0 装了但没跑

	bf, err := svc.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dock.execs) != 2 {
		t.Fatalf("只应在跑的库上各转储一次，实得 %v", dock.execs)
	}
	if !strings.HasPrefix(dock.execs[0], "phpo-mysql-8.4 mysqldump -u root --password=p@ss w0rd") {
		t.Fatalf("mysql 转储命令应带明文密码，实得 %q", dock.execs[0])
	}
	if dock.execs[1] != "phpo-pgsql-17 pg_dumpall -U postgres" {
		t.Fatalf("pgsql 走 local trust，不应带密码，实得 %q", dock.execs[1])
	}
	if !hasLog(em.logs, "mysql 8.0 未运行") {
		t.Fatalf("已装未跑的库应点名跳过，实得 %v", em.logs)
	}
	if strings.Join(lc.stopped, ",") != "mysql/8.4,pgsql/17" {
		t.Fatalf("转储不得影响暂停序列，实得 %v", lc.stopped)
	}
	dumped := extractArchive(t, env, bf.File)
	for _, want := range []string{"dump/mysql-8.4.sql", "dump/pgsql-17.sql"} {
		b, err := os.ReadFile(filepath.Join(dumped, want))
		if err != nil {
			t.Fatalf("%s 应入档: %v", want, err)
		}
		if !strings.Contains(string(b), "phpo dump of") {
			t.Fatalf("%s 内容应为转储字节，实得 %q", want, b)
		}
	}
}

func TestBackup_Create_DefaultsPasswordWhenUnset(t *testing.T) {
	svc, st, _, _, dock, _, _, _ := newBackupSvc(t)
	st.snap.Running["mysql"] = []string{"8.0"} // 未 SetPassword

	if _, err := svc.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dock.execs[0], "--password="+config.DefaultPassword) {
		t.Fatalf("未设密码应回落默认值（与容器生效值同口径），实得 %q", dock.execs[0])
	}
}

func TestBackup_Create_EmptyPasswordOmitsFlag(t *testing.T) {
	svc, st, _, _, dock, _, _, cfg := newBackupSvc(t)
	if err := cfg.SetPassword("mysql", "8.0", ""); err != nil {
		t.Fatal(err)
	}
	st.snap.Running["mysql"] = []string{"8.0"}

	if _, err := svc.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dock.execs[0], "--password") {
		t.Fatalf("空密码必须省略 --password=，否则 mysqldump 会交互式挂起，实得 %q", dock.execs[0])
	}
}

// 单个库转储失败只点名，不判死整包：可读的配置/站点/其余库仍是有效备份。
func TestBackup_Create_DumpFailureNotFatal(t *testing.T) {
	svc, st, _, _, dock, em, env, _ := newBackupSvc(t)
	st.snap.Running["pgsql"] = []string{"17"}
	dock.execErr = errors.New("pg_dumpall: could not connect to server")

	bf, err := svc.Create(context.Background())
	if err != nil {
		t.Fatalf("转储失败不应中断备份: %v", err)
	}
	if !hasLog(em.logs, "pgsql 17 逻辑导出失败") || !hasLog(em.logs, "本次归档不含该库数据") {
		t.Fatalf("失败必须点名并说明缺了什么，实得 %v", em.logs)
	}
	dumped := extractArchive(t, env, bf.File)
	if _, err := os.Stat(filepath.Join(dumped, "dump")); !os.IsNotExist(err) {
		t.Fatalf("不得留下半截转储文件")
	}
}

// 命令退出码 0 但没吐字节：空文件比没有更危险（恢复时会当成「库里本来就没数据」）。
func TestBackup_Create_EmptyDumpDiscarded(t *testing.T) {
	svc, st, _, _, dock, em, env, _ := newBackupSvc(t)
	st.snap.Running["mysql"] = []string{"8.4"}
	dock.execEmpty = true

	bf, err := svc.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasLog(em.logs, "转储结果为空") {
		t.Fatalf("空转储应报错，实得 %v", em.logs)
	}
	if _, err := os.Stat(filepath.Join(extractArchive(t, env, bf.File), "dump", "mysql-8.4.sql")); !os.IsNotExist(err) {
		t.Fatalf("空转储文件不得入档")
	}
}

// redis 的宿主数据目录同样是 root 0600/0700（真机取证：dump.rdb 与 appendonlydir 宿主全读不动），
// 只能从在跑实例经 redis-cli --rdb 取，产物是二进制 .rdb 而非 .sql。
func TestBackup_Create_DumpsRedisAsRdb(t *testing.T) {
	svc, st, _, _, dock, _, env, cfg := newBackupSvc(t)
	if err := cfg.SetPassword("redis", "8", "123456"); err != nil {
		t.Fatal(err)
	}
	st.snap.Running["redis"] = []string{"8"}

	bf, err := svc.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dock.execs) != 1 || !strings.HasPrefix(dock.execs[0], "phpo-redis-8 sh -c ") {
		t.Fatalf("redis 应经容器内脚本转储，实得 %v", dock.execs)
	}
	b, err := os.ReadFile(filepath.Join(extractArchive(t, env, bf.File), "dump", "redis-8.rdb"))
	if err != nil {
		t.Fatalf("redis 转储应以 .rdb 入档: %v", err)
	}
	if !strings.Contains(string(b), "phpo dump of") {
		t.Fatalf("入档内容应为转储字节，实得 %q", b)
	}
}

// 口令作位置参数传给容器内的 sh，不拼进脚本字符串：含空格/引号/分号的密码无需转义也不会被二次展开。
func TestBackup_DumpCmd_RedisPasswordIsArgvNotScript(t *testing.T) {
	pw := `a "b'; touch /pwned`
	argv := dumpCmd(model.KindRedis, pw)
	if len(argv) != 6 || argv[0] != "sh" || argv[1] != "-c" {
		t.Fatalf("redis 转储命令应为 sh -c <脚本> <占位> <口令> <暂存文件>，实得 %q", argv)
	}
	if argv[4] != pw {
		t.Fatalf("口令应是独立 argv 元素，实得 %q", argv[4])
	}
	if strings.Contains(argv[2], pw) {
		t.Fatalf("口令不得出现在脚本文本里: %q", argv[2])
	}
	if got := dumpCmd(model.KindRedis, ""); !strings.Contains(got[2], `if [ -n "$1" ]`) || got[4] != "" {
		t.Fatalf("空密码分支应由脚本内判断兜住，实得 %q", got)
	}
	if dumpExt(model.KindRedis) != ".rdb" || dumpExt(model.KindMySQL) != ".sql" {
		t.Fatal("转储产物后缀按种类区分")
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
	if _, _, err := archive.Create(noDB, []archive.Source{{ArcPrefix: "php", HostPath: filepath.Join(staging, "php")}}); err != nil {
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
