// T504 验收：EnvService 密码/端口 env 读写——明文零校验（空/含空格合法）、未设回落默认、写入后广播 state:changed（硬红线 4）
package service

import (
	"testing"

	"phpo/internal/config"
	"phpo/internal/model"
)

// fakeEnvStore 实现 EnvStore：内存密码/端口表 + 快照
type fakeEnvStore struct {
	pw     map[string]string
	ports  map[string]int
	snap   *model.Snapshot
	emitter *fakeEmitter
}

func newFakeEnvStore() *fakeEnvStore {
	return &fakeEnvStore{pw: map[string]string{}, ports: map[string]int{}, snap: model.NewSnapshot()}
}
func key(kind, version string) string { return kind + "/" + version }

func (f *fakeEnvStore) GetPassword(kind, version string) (string, bool, error) {
	v, ok := f.pw[key(kind, version)]
	return v, ok, nil
}
func (f *fakeEnvStore) SetPassword(kind, version, password string) error {
	f.pw[key(kind, version)] = password
	return nil
}
func (f *fakeEnvStore) GetServicePort(kind, version string) (int, bool, error) {
	v, ok := f.ports[key(kind, version)]
	return v, ok, nil
}
func (f *fakeEnvStore) SetServicePort(kind, version string, port int) error {
	f.ports[key(kind, version)] = port
	return nil
}
func (f *fakeEnvStore) BuildSnapshot() (*model.Snapshot, error) { return f.snap, nil }

func newEnvSvc() (*EnvService, *fakeEnvStore, *fakeEmitter) {
	st := newFakeEnvStore()
	em := &fakeEmitter{}
	return NewEnvService(st, em), st, em
}

func TestEnvService_GetPasswordFallsBackToDefault(t *testing.T) {
	e, _, _ := newEnvSvc()
	got, err := e.GetPassword(model.KindMySQL, "8.4")
	if err != nil || got != config.DefaultPassword {
		t.Fatalf("未设应回落默认 %s，实得 %q err=%v", config.DefaultPassword, got, err)
	}
}

func TestEnvService_SetPasswordEmptyAndSpecial(t *testing.T) {
	e, st, em := newEnvSvc()
	// 空密码合法（§1.5）
	if err := e.SetPassword(model.KindRedis, "7", ""); err != nil {
		t.Fatal(err)
	}
	if st.pw["redis/7"] != "" {
		t.Fatalf("空密码应存空串，实得 %q", st.pw["redis/7"])
	}
	// 含空格/特殊字符原样
	if err := e.SetPassword(model.KindMySQL, "8.4", "p@ ss$1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.GetPassword(model.KindMySQL, "8.4"); got != "p@ ss$1" {
		t.Fatalf("含空格密码应原样存取，实得 %q", got)
	}
	if !em.has("state:changed") {
		t.Fatalf("写密码应发 state:changed，实得 %v", em.events)
	}
}

func TestEnvService_SetPort(t *testing.T) {
	e, st, em := newEnvSvc()
	if err := e.SetPort(model.KindPgsql, "17", 5433); err != nil {
		t.Fatal(err)
	}
	if st.ports["pgsql/17"] != 5433 {
		t.Fatalf("端口应落库 5433，实得 %d", st.ports["pgsql/17"])
	}
	if !em.has("state:changed") {
		t.Fatal("写端口应发 state:changed")
	}
}
