// T607 装机向导服务单测：校验（穿越/空/同路径拒绝 + 建树可写）与落地（config.yaml 两根 + dirReady + state:changed）。
package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"phpo/internal/config"
	"phpo/internal/model"
	"phpo/internal/task"
	"phpo/pkg/errs"
)

// fakeWizard 同满足 WizardConfig（SetRoots/Roots）与 WizardStore（SetDirReady/BuildSnapshot）：内存两根 + 就绪标记
type fakeWizard struct {
	home, www  string
	dir        map[string]bool
	rootsCalls int // SetRoots 调用次数：验证「已设置」路径不重复落库/建树
}

func newFakeWizard() *fakeWizard { return &fakeWizard{dir: map[string]bool{}} }
func (f *fakeWizard) SetRoots(home, www string) error {
	f.home, f.www = home, www
	f.rootsCalls++
	return nil
}
func (f *fakeWizard) Roots() (string, string)                 { return f.home, f.www }
func (f *fakeWizard) SetDirReady(k string, r bool) error      { f.dir[k] = r; return nil }
func (f *fakeWizard) BuildSnapshot() (*model.Snapshot, error) { return model.NewSnapshot(), nil }

func newWizardSvc(t *testing.T) (*WizardService, *fakeWizard, *fakeEmitter) {
	t.Helper()
	st := newFakeWizard()
	em := &fakeEmitter{}
	return NewWizardService(st, st, em, task.NewManager(em)), st, em
}

func TestHomeVerify_CreatesTree(t *testing.T) {
	svc, _, _ := newWizardSvc(t)
	home := filepath.Join(t.TempDir(), "phpo")
	www := filepath.Join(t.TempDir(), "www")

	res, err := svc.HomeVerify(context.Background(), home, www)
	if err != nil {
		t.Fatalf("HomeVerify err: %v", err)
	}
	if !res.OK || len(res.Errors) > 0 {
		t.Fatalf("期望校验通过，得 OK=%v errors=%v", res.OK, res.Errors)
	}
	for _, sd := range config.HomeSubdirs {
		p := filepath.Join(home, filepath.FromSlash(sd.Path))
		if fi, e := os.Stat(p); e != nil || !fi.IsDir() {
			t.Fatalf("子目录未创建：%s", p)
		}
	}
	if fi, e := os.Stat(www); e != nil || !fi.IsDir() {
		t.Fatalf("WWW_ROOT 未创建：%s", www)
	}
	if len(res.Lines) != len(config.HomeSubdirs)+2 {
		t.Fatalf("进度行数=%d，期望 %d", len(res.Lines), len(config.HomeSubdirs)+2)
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
	if st.dir["PHPO_HOME"] != true {
		t.Fatalf("dirReady[PHPO_HOME] 期望 true，得 %v", st.dir)
	}
	if st.dir["WWW_ROOT"] != true {
		t.Fatalf("dirReady[WWW_ROOT] 期望 true，得 %v", st.dir)
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
// 仅校正 dirReady 并广播以即时更新 UI（禁止重复创建工作目录）。
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
	if st.dir["PHPO_HOME"] != true || st.dir["WWW_ROOT"] != true {
		t.Fatalf("应校正 dirReady=true 以更新 UI，得 %v", st.dir)
	}
	if !em.has("state:changed") {
		t.Fatalf("已设置时仍应广播 state:changed 更新 UI，得 %v", em.events)
	}
}

func TestHomeEnsure_RejectsInvalid(t *testing.T) {
	svc, _, _ := newWizardSvc(t)
	if err := svc.HomeEnsure(context.Background(), "~/a/../b", "~/www"); err == nil {
		t.Fatal("期望路径穿越被拒并返回错误")
	}
}

// RefreshDirReady 依据「env 已持久化 + 目录实际存在」重算双就绪标记：首启/缺失/被删均回落 false（永久阻断写操作）。
func TestRefreshDirReady(t *testing.T) {
	t.Run("env为空双false", func(t *testing.T) {
		svc, st, _ := newWizardSvc(t)
		changed, err := svc.RefreshDirReady()
		if err != nil {
			t.Fatalf("RefreshDirReady err: %v", err)
		}
		if !changed {
			t.Fatal("两根全空应报告降级变化")
		}
		if st.dir["PHPO_HOME"] || st.dir["WWW_ROOT"] {
			t.Fatalf("期望双 false，得 %v", st.dir)
		}
	})

	t.Run("两根齐且目录存在双true", func(t *testing.T) {
		svc, st, _ := newWizardSvc(t)
		home := filepath.Join(t.TempDir(), "phpo")
		www := filepath.Join(t.TempDir(), "www")
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(www, 0o755); err != nil {
			t.Fatal(err)
		}
		st.home, st.www = home, www
		changed, err := svc.RefreshDirReady()
		if err != nil {
			t.Fatalf("RefreshDirReady err: %v", err)
		}
		if changed {
			t.Fatal("双就绪不应报告降级")
		}
		if !st.dir["PHPO_HOME"] || !st.dir["WWW_ROOT"] {
			t.Fatalf("期望双 true，得 %v", st.dir)
		}
	})

	t.Run("目录被删回落false", func(t *testing.T) {
		svc, st, _ := newWizardSvc(t)
		base := t.TempDir()
		home := filepath.Join(base, "phpo")
		www := filepath.Join(base, "www")
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatal(err)
		}
		// www 目录刻意不创建 → 两根已存但目录不存在 → WWW_ROOT 回落 false
		st.home, st.www = home, www
		changed, err := svc.RefreshDirReady()
		if err != nil {
			t.Fatalf("RefreshDirReady err: %v", err)
		}
		if !changed {
			t.Fatal("WWW_ROOT 缺失应报告降级")
		}
		if !st.dir["PHPO_HOME"] {
			t.Fatal("PHPO_HOME 目录存在应保持 true")
		}
		if st.dir["WWW_ROOT"] {
			t.Fatalf("WWW_ROOT 目录不存在应回落 false，得 %v", st.dir)
		}
	})
}
