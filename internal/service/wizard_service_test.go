// T607 装机向导服务单测：验证（只读预检：穿越/空/同路径拒绝 + 判存在判可写，绝不落盘）与
// 确认（HomeEnsure 才建目录子树 + 两根落 config.yaml + state:changed）。
// dirReady 已改由快照派生（config.yaml 两根 + 目录存在性），故此处只验两根落库与广播；就绪判定见 config/store 单测。
package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"phpo/internal/config"
	"phpo/internal/model"
	"phpo/internal/store"
	"phpo/internal/task"
	"phpo/pkg/errs"
)

// fakeWizard 同满足 WizardConfig（SetRoots/RootsReady）与 WizardStore（BuildSnapshot）：内存两根 + 落库次数
type fakeWizard struct {
	home, www  string
	rootsCalls int // SetRoots 调用次数：验证「已设置」路径不重复落库/建树
}

func newFakeWizard() *fakeWizard { return &fakeWizard{} }
func (f *fakeWizard) SetRoots(home, www string) error {
	f.home, f.www = home, www
	f.rootsCalls++
	return nil
}
func (f *fakeWizard) RootsReady() (home, www bool) {
	return f.home != "" && pathIsDir(config.ExpandHome(f.home)), f.www != "" && pathIsDir(config.ExpandHome(f.www))
}
func (f *fakeWizard) BuildSnapshot() (*model.Snapshot, error) { return model.NewSnapshot(), nil }

// pathIsDir 跟随符号链接判路径为已存在目录（镜像 ConfigStore.isDir）
func pathIsDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func newWizardSvc(t *testing.T) (*WizardService, *fakeWizard, *fakeEmitter) {
	t.Helper()
	st := newFakeWizard()
	em := &fakeEmitter{}
	return NewWizardService(st, st, em, task.NewManager(em)), st, em
}

// TestHomeVerify_CreatesNothing 「验证」必须是纯只读预检：不建任何目录、不写任何探测文件（创建只允许发生在「确认并创建」）
func TestHomeVerify_CreatesNothing(t *testing.T) {
	svc, _, _ := newWizardSvc(t)
	base := t.TempDir()
	home := filepath.Join(base, "phpo")
	www := filepath.Join(base, "www", "sites") // 连中间层都不存在，考验「祖先可写即视为可创建」

	res, err := svc.HomeVerify(context.Background(), home, www)
	if err != nil {
		t.Fatalf("HomeVerify err: %v", err)
	}
	if !res.OK || len(res.Errors) > 0 {
		t.Fatalf("期望预检通过，得 OK=%v errors=%v", res.OK, res.Errors)
	}
	if _, e := os.Stat(home); !os.IsNotExist(e) {
		t.Fatalf("验证阶段创建了 PHPO_HOME：%s（stat=%v）", home, e)
	}
	if _, e := os.Stat(www); !os.IsNotExist(e) {
		t.Fatalf("验证阶段创建了 WWW_ROOT：%s（stat=%v）", www, e)
	}
	entries, e := os.ReadDir(base)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatalf("验证阶段落了盘，base 下出现 %d 个条目", len(entries))
	}
	if len(res.Lines) != len(config.HomeSubdirs)+2 {
		t.Fatalf("预检行数=%d，期望 %d", len(res.Lines), len(config.HomeSubdirs)+2)
	}
	if !strings.Contains(res.Lines[0], "确认后将创建") {
		t.Fatalf("不存在的目录应标「确认后将创建」，得 %q", res.Lines[0])
	}
}

// TestHomeVerify_ExistingDirsReportedReady 已存在且可写的目录报 ✓，不误报待创建
func TestHomeVerify_ExistingDirsReportedReady(t *testing.T) {
	svc, _, _ := newWizardSvc(t)
	base := t.TempDir()
	home, www := filepath.Join(base, "phpo"), filepath.Join(base, "www")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, sd := range config.HomeSubdirs {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(sd.Path)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(www, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := svc.HomeVerify(context.Background(), home, www)
	if err != nil {
		t.Fatalf("HomeVerify err: %v", err)
	}
	if !res.OK {
		t.Fatalf("已就绪目录应全部通过，得 errors=%v", res.Errors)
	}
	for _, ln := range res.Lines {
		if !strings.HasPrefix(ln, "✓ ") {
			t.Fatalf("已存在目录应报 ✓，得 %q", ln)
		}
	}
}

// TestHomeVerify_RejectsUnwritableAncestor 祖先不可写 → 预检报错且不创建任何东西
func TestHomeVerify_RejectsUnwritableAncestor(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 不受权限位约束，跳过只读目录用例")
	}
	base := t.TempDir()
	ro := filepath.Join(base, "readonly")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })

	svc, _, _ := newWizardSvc(t)
	res, err := svc.HomeVerify(context.Background(), filepath.Join(ro, "phpo"), filepath.Join(base, "www"))
	if err != nil {
		t.Fatalf("HomeVerify err: %v", err)
	}
	if res.OK || len(res.Errors) == 0 || !strings.Contains(res.Errors[0], "不可写") {
		t.Fatalf("不可写祖先应报错，得 %+v", res)
	}
	if _, e := os.Stat(filepath.Join(ro, "phpo")); !os.IsNotExist(e) {
		t.Fatalf("预检失败仍创建了目录：%v", e)
	}
}

// TestHomeEnsure_CreatesTreeOnConfirm 只有「确认并创建」→ HomeEnsure 才真正建出子树与 WWW_ROOT
func TestHomeEnsure_CreatesTreeOnConfirm(t *testing.T) {
	svc, st, _ := newWizardSvc(t)
	base := t.TempDir()
	home, www := filepath.Join(base, "phpo"), filepath.Join(base, "www")

	if err := svc.HomeEnsure(context.Background(), home, www); err != nil {
		t.Fatalf("HomeEnsure err: %v", err)
	}
	for _, sd := range config.HomeSubdirs {
		p := filepath.Join(home, filepath.FromSlash(sd.Path))
		if fi, e := os.Stat(p); e != nil || !fi.IsDir() {
			t.Fatalf("确认后子目录仍未创建：%s", p)
		}
	}
	if fi, e := os.Stat(www); e != nil || !fi.IsDir() {
		t.Fatalf("确认后 WWW_ROOT 仍未创建：%s", www)
	}
	if st.rootsCalls != 1 {
		t.Fatalf("确认后应落库一次，SetRoots 调用 %d 次", st.rootsCalls)
	}
}

func TestHomeVerify_RejectsTraversal(t *testing.T) {
	svc, _, _ := newWizardSvc(t)
	res, err := svc.HomeVerify(context.Background(), "~/../etc", "~/www")
	if err != nil {
		t.Fatalf("HomeVerify err: %v", err)
	}
	if res.OK || len(res.Errors) == 0 || res.Errors[0] != errs.PathTraversal {
		t.Fatalf("期望拒绝路径穿越，得 %+v", res)
	}
}

func TestHomeVerify_RejectsEmptyAndSame(t *testing.T) {
	svc, _, _ := newWizardSvc(t)
	ctx := context.Background()
	if r, _ := svc.HomeVerify(ctx, "", "~/www"); r.OK || !strings.Contains(r.Errors[0], "PHPO_HOME") {
		t.Fatalf("空 PHPO_HOME 应被拒：%+v", r)
	}
	d := filepath.Join(t.TempDir(), "x")
	if r, _ := svc.HomeVerify(ctx, d, d); r.OK || !strings.Contains(r.Errors[0], "相同") {
		t.Fatalf("同路径应被拒：%+v", r)
	}
}

func TestHomeEnsure_PersistsAndEmits(t *testing.T) {
	svc, st, em := newWizardSvc(t)
	home := filepath.Join(t.TempDir(), "phpo")
	www := filepath.Join(t.TempDir(), "www")

	if err := svc.HomeEnsure(context.Background(), home, www); err != nil {
		t.Fatalf("HomeEnsure err: %v", err)
	}
	if st.rootsCalls != 1 {
		t.Fatalf("首启应落库一次，SetRoots 调用 %d 次", st.rootsCalls)
	}
	want := config.DerivePaths(home, www)
	if st.home != want.PHPOHome || st.www != want.WWWRoot {
		t.Fatalf("config.yaml 两根落地不符：home=%q www=%q（期望 %q / %q）", st.home, st.www, want.PHPOHome, want.WWWRoot)
	}
	if !em.has("state:changed") {
		t.Fatalf("HomeEnsure 应广播 state:changed，得 %v", em.events)
	}
}

// TestHomeEnsure_SkipsWhenAlreadyReady 两根目录已持久化且实际存在时，HomeEnsure 应跳过建树/落库，
// 仅广播权威快照令前端即时更新就绪态（禁止重复创建工作目录）。
func TestHomeEnsure_SkipsWhenAlreadyReady(t *testing.T) {
	svc, st, em := newWizardSvc(t)
	base := t.TempDir()
	home := filepath.Join(base, "phpo")
	www := filepath.Join(base, "www")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(www, 0o755); err != nil {
		t.Fatal(err)
	}
	st.home, st.www = home, www // 预置 config.yaml 两根 = 目标目录
	st.rootsCalls = 0
	sub := filepath.Join(home, filepath.FromSlash(config.HomeSubdirs[0].Path)) // home/php，跳过路径不应创建

	if err := svc.HomeEnsure(context.Background(), home, www); err != nil {
		t.Fatalf("HomeEnsure err: %v", err)
	}
	if st.rootsCalls != 0 {
		t.Fatalf("已设置时不应重复落库/建树，SetRoots 调用 %d 次", st.rootsCalls)
	}
	if _, e := os.Stat(sub); !os.IsNotExist(e) {
		t.Fatalf("已设置时不应创建工作目录子树：%s", sub)
	}
	if !em.has("state:changed") {
		t.Fatalf("已设置时仍应广播 state:changed 更新 UI，得 %v", em.events)
	}
}

// TestHomeEnsure_RefusesSecondWorkingDir 工作目录一旦「已设置」，任何再次进入向导的设置请求都必须被拒：
// 既不建第二套目录，也不改写 config.yaml 已存两根——只广播权威快照让 UI 回到已设置态（禁止重复创建）。
func TestHomeEnsure_RefusesSecondWorkingDir(t *testing.T) {
	svc, st, em := newWizardSvc(t)
	base := t.TempDir()
	oldHome, oldWww := filepath.Join(base, "old", "phpo"), filepath.Join(base, "old", "www")
	for _, p := range []string{oldHome, oldWww} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	st.home, st.www = oldHome, oldWww // 预置「已设置」：两根已持久化且目录存在
	if !svc.Configured() {
		t.Fatal("前置条件失效：应判定工作目录已设置")
	}

	newHome, newWww := filepath.Join(base, "new", "phpo"), filepath.Join(base, "new", "www")
	if err := svc.HomeEnsure(context.Background(), newHome, newWww); err != nil {
		t.Fatalf("HomeEnsure err: %v", err)
	}
	if _, e := os.Stat(newHome); !os.IsNotExist(e) {
		t.Fatalf("已设置时重复创建了第二套 PHPO_HOME：%s", newHome)
	}
	if _, e := os.Stat(newWww); !os.IsNotExist(e) {
		t.Fatalf("已设置时重复创建了第二套 WWW_ROOT：%s", newWww)
	}
	if st.rootsCalls != 0 || st.home != oldHome || st.www != oldWww {
		t.Fatalf("已设置时不得改写 config.yaml：calls=%d home=%q www=%q", st.rootsCalls, st.home, st.www)
	}
	if !em.has("state:changed") {
		t.Fatalf("已设置时仍应广播 state:changed 让 UI 归位，得 %v", em.events)
	}
}

// TestHomeEnsure_RefusesSecondWorkingDirRealStore 真 ConfigStore 下走完一次向导后再请求其它路径：
// 既不建第二套工作目录，也不改写 config.yaml 已存两根；重启进程（另开 ConfigStore）后判据依旧成立。
func TestHomeEnsure_RefusesSecondWorkingDirRealStore(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, "config.yaml")
	dbPath := filepath.Join(base, "phpo.db")
	cfg, err := config.LoadFromPath(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(dbPath)
	st.SetEnvProvider(cfg)
	t.Cleanup(func() { st.Close() })
	em := &fakeEmitter{}
	svc := NewWizardService(cfg, st, em, task.NewManager(em))

	home, www := filepath.Join(base, "phpo"), filepath.Join(base, "www")
	if err := svc.HomeEnsure(context.Background(), home, www); err != nil {
		t.Fatal(err)
	}
	if !svc.Configured() {
		t.Fatal("向导确认后应判定工作目录已设置")
	}

	reopened, err := config.LoadFromPath(cfgPath) // 模拟重启：判据取自落盘值而非内存态
	if err != nil {
		t.Fatal(err)
	}
	svc2 := NewWizardService(reopened, st, em, task.NewManager(em))
	if !svc2.Configured() {
		t.Fatal("重启后仍应判定工作目录已设置")
	}

	altHome, altWww := filepath.Join(base, "alt", "phpo"), filepath.Join(base, "alt", "www")
	if err := svc2.HomeEnsure(context.Background(), altHome, altWww); err != nil {
		t.Fatalf("HomeEnsure err: %v", err)
	}
	if _, e := os.Stat(altHome); !os.IsNotExist(e) {
		t.Fatalf("重复创建了第二套 PHPO_HOME：%s", altHome)
	}
	if _, e := os.Stat(altWww); !os.IsNotExist(e) {
		t.Fatalf("重复创建了第二套 WWW_ROOT：%s", altWww)
	}
	if h, w := reopened.Roots(); config.ExpandHome(h) != home || config.ExpandHome(w) != www {
		t.Fatalf("config.yaml 两根被改写：%q / %q（期望 %q / %q）", h, w, home, www)
	}
}

func TestHomeEnsure_RejectsInvalid(t *testing.T) {
	svc, _, _ := newWizardSvc(t)
	if err := svc.HomeEnsure(context.Background(), "~/a/../b", "~/www"); err == nil {
		t.Fatal("期望路径穿越被拒并返回错误")
	}
}

// 方案B 端到端接线（真 ConfigStore + 真 Store）：首启不建库；向导把两根写入 config.yaml 后，
// 同一实例内透明建库，dirReady 由「两根已持久化 + 目录实际存在」派生，无需重启。
func TestWizardUnlocksDBAndDerivesDirReady(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, "config.yaml")
	dbPath := filepath.Join(base, "phpo.db")
	cfg, err := config.LoadFromPath(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(dbPath)
	st.SetEnvProvider(cfg)
	t.Cleanup(func() { st.Close() })
	em := &fakeEmitter{}
	svc := NewWizardService(cfg, st, em, task.NewManager(em))

	// 首启：快照可读且双 false，运行态库文件不存在
	snap, err := st.BuildSnapshot()
	if err != nil {
		t.Fatalf("首启快照应可读: %v", err)
	}
	if snap.DirReady["PHPO_HOME"] || snap.DirReady["WWW_ROOT"] {
		t.Fatalf("首启 dirReady 应双 false，得 %+v", snap.DirReady)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatal("首启不得建库")
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatal("首启不得写 config.yaml")
	}

	// 向导确认：两根落地 + 建树 → 门禁解除，快照 dirReady 双 true
	home := filepath.Join(base, "phpo")
	www := filepath.Join(base, "www")
	if err := svc.HomeEnsure(context.Background(), home, www); err != nil {
		t.Fatal(err)
	}
	if snap, err = st.BuildSnapshot(); err != nil {
		t.Fatalf("向导后快照应可读: %v", err)
	}
	if !snap.DirReady["PHPO_HOME"] || !snap.DirReady["WWW_ROOT"] {
		t.Fatalf("向导后 dirReady 应双 true，得 %+v", snap.DirReady)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("两根应已写入 config.yaml: %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("向导后应已透明建库: %v", err)
	}

	// 幂等：重复确认不报错、不改派生结果
	if err := svc.HomeEnsure(context.Background(), home, www); err != nil {
		t.Fatalf("重复 HomeEnsure 应幂等: %v", err)
	}

	// 网站目录被删：WWW_ROOT 回落 false 重新拦截，PHPO_HOME 与库内状态不受影响
	if err := os.RemoveAll(www); err != nil {
		t.Fatal(err)
	}
	if snap, err = st.BuildSnapshot(); err != nil {
		t.Fatalf("目录缺失时快照仍应可读: %v", err)
	}
	if !snap.DirReady["PHPO_HOME"] || snap.DirReady["WWW_ROOT"] {
		t.Fatalf("期望 (true,false)，得 %+v", snap.DirReady)
	}
}
