// T607 装机向导服务单测：校验（穿越/空/同路径拒绝 + 建树可写）与落地（env 派生 + dirReady + state:changed）。
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

type fakeWizardStore struct {
	env map[string]string
	dir map[string]bool
}

func newFakeWizardStore() *fakeWizardStore {
	return &fakeWizardStore{env: map[string]string{}, dir: map[string]bool{}}
}
func (f *fakeWizardStore) SetEnv(k, v string) error                { f.env[k] = v; return nil }
func (f *fakeWizardStore) SetDirReady(k string, r bool) error      { f.dir[k] = r; return nil }
func (f *fakeWizardStore) BuildSnapshot() (*model.Snapshot, error) { return model.NewSnapshot(), nil }

func newWizardSvc(t *testing.T) (*WizardService, *fakeWizardStore, *fakeEmitter) {
	t.Helper()
	st := newFakeWizardStore()
	em := &fakeEmitter{}
	return NewWizardService(st, em, task.NewManager(em)), st, em
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
	want := config.DerivePaths(home, www)
	if st.env["PHPO_HOME"] != want.PHPOHome || st.env["WWW_ROOT"] != want.WWWRoot {
		t.Fatalf("env PHPO_HOME/WWW_ROOT 落地不符：%v / %v", st.env["PHPO_HOME"], st.env["WWW_ROOT"])
	}
	if st.env["OFFLINE_ROOT"] != want.OfflineRoot {
		t.Fatalf("派生 OFFLINE_ROOT 不符：期望 %s 得 %s", want.OfflineRoot, st.env["OFFLINE_ROOT"])
	}
	if !em.has("state:changed") {
		t.Fatalf("HomeEnsure 应广播 state:changed，得 %v", em.events)
	}
}

func TestHomeEnsure_RejectsInvalid(t *testing.T) {
	svc, _, _ := newWizardSvc(t)
	if err := svc.HomeEnsure(context.Background(), "~/a/../b", "~/www"); err == nil {
		t.Fatal("期望路径穿越被拒并返回错误")
	}
}
