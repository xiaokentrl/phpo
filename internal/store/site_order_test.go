// 站点展示顺序（拖拽排序）：config.yaml site_order 持久化 + 快照按序重排（缺席域名原序垫底）
package store

import (
	"path/filepath"
	"reflect"
	"testing"

	"phpo/internal/config"
	"phpo/internal/model"
)

func TestSiteOrder_ReorderedInSnapshotAndPersisted(t *testing.T) {
	dir := t.TempDir()
	cs, err := config.LoadFromPath(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// 两根落地（解除延迟建库门禁）+ 用户把 c.test 拖到最前、a.test 第二（b.test 从未拖动过）
	if err := cs.SetRoots(filepath.Join(dir, "home"), filepath.Join(dir, "www")); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetSiteOrder([]string{"c.test", " a.test", "", "c.test"}); err != nil {
		t.Fatalf("SetSiteOrder: %v", err)
	}

	s := openStore(t)
	s.SetEnvProvider(cs)
	for _, d := range []string{"a.test", "b.test", "c.test"} {
		if err := s.UpsertSite(model.Site{Domain: d, Port: 80, PHP: "8.4", Root: "~/www/" + d}); err != nil {
			t.Fatal(err)
		}
	}

	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(snap.Sites))
	for _, st := range snap.Sites {
		got = append(got, st.Domain)
	}
	// 已排序的按拖拽序，未排序的 b.test 按原序垫底
	if want := []string{"c.test", "a.test", "b.test"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("快照顺序 = %v，应 %v", got, want)
	}

	// 持久化回读：规范化（去空白、去重、去空段）后逐字一致
	cs2, err := config.LoadFromPath(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"c.test", "a.test"}; !reflect.DeepEqual(cs2.SiteOrder(), want) {
		t.Fatalf("回读 site_order = %v，应 %v", cs2.SiteOrder(), want)
	}
}

func TestSiteOrder_EmptyKeepsDomainOrder(t *testing.T) {
	s := openStore(t)
	for _, d := range []string{"b.test", "a.test"} {
		if err := s.UpsertSite(model.Site{Domain: d, Port: 80, PHP: "8.4", Root: "~/www/" + d}); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(snap.Sites))
	for _, st := range snap.Sites {
		got = append(got, st.Domain)
	}
	// 未拖拽过：缺省仍是域名序（ListSites 的 ORDER BY domain）
	if want := []string{"a.test", "b.test"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("无排序时快照顺序 = %v，应域名序 %v", got, want)
	}
}
