// nginx 单例容器名解析：必须跟随权威快照，而非装配期写死的版本字面量
package app

import (
	"errors"
	"testing"

	"phpo/internal/model"
)

type fakeNginxStore struct {
	vers []string
	err  error
}

func (f fakeNginxStore) BuildSnapshot() (*model.Snapshot, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &model.Snapshot{Installed: map[string][]string{string(model.KindNginx): f.vers}}, nil
}

func TestNginxContainerOfFollowsSnapshot(t *testing.T) {
	got, err := nginxContainerOf(fakeNginxStore{vers: []string{"1.25"}})()
	if err != nil {
		t.Fatal(err)
	}
	if got != "phpo-nginx-1.25" {
		t.Fatalf("非默认版本 nginx 应解析为 phpo-nginx-1.25，实得 %q", got)
	}
}

func TestNginxContainerOfWithoutNginx(t *testing.T) {
	if _, err := nginxContainerOf(fakeNginxStore{})(); err == nil {
		t.Fatal("nginx 未安装时应报错，不得回落到任何默认容器名")
	}
}

func TestNginxContainerOfPropagatesSnapshotError(t *testing.T) {
	want := errors.New("db 未就绪")
	if _, err := nginxContainerOf(fakeNginxStore{err: want})(); !errors.Is(err, want) {
		t.Fatalf("快照失败应原样上抛，实得 %v", err)
	}
}
