// nginx 单例容器名解析：必须跟随装机表里的实际版本，而非装配期写死的版本字面量
package app

import (
	"errors"
	"testing"

	"phpo/internal/model"
)

// fakeNginxStore 只满足 installedLister：解析容器名若改回「建整份快照」，本假件即编译不过，
// 这正是想要的锁——拿一个版本号不该付 Docker 拨号 + hosts 读盘 + 逐站点 stat 的代价。
type fakeNginxStore struct {
	vers []string
	err  error
}

func (f fakeNginxStore) InstalledVersions(kind string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	if kind != string(model.KindNginx) {
		return nil, nil
	}
	return f.vers, nil
}

func TestNginxContainerOfFollowsInstalledVersions(t *testing.T) {
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

func TestNginxContainerOfPropagatesStoreError(t *testing.T) {
	want := errors.New("db 未就绪")
	if _, err := nginxContainerOf(fakeNginxStore{err: want})(); !errors.Is(err, want) {
		t.Fatalf("装机表读取失败应原样上抛，实得 %v", err)
	}
}
