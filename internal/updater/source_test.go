// 多发布源探测用例（§5.9）：跨源取版本最新、版本并列取配置靠前、单源超时不叠加、全败点名每个源、按本机平台选包
package updater

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"phpo/internal/model"
)

func manifestServer(t *testing.T, body string, delay time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(delay):
			}
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

const okManifest = `{"version":"9.9.9","changelog":"x","assets":[
{"os":"linux","format":"deb","url":"http://x/a.deb","size":11,"sha256":"aa","signature":"sa"},
{"os":"windows","url":"http://x/a.exe","size":22,"sha256":"bb","signature":"sb"}]}`

// 首个源不可用时照常从次源取到（并发探测下失败的源只是不参与取最大）
func TestSources_FailoverToNext(t *testing.T) {
	bad := manifestServer(t, "", 0)
	bad.Close() // 立刻关站：连接必失败
	good := manifestServer(t, okManifest, 0)
	s := NewSources([]Source{{Name: "github", ManifestURL: bad.URL}, {Name: "gitee", ManifestURL: good.URL}})
	rel, err := s.FetchLatest(context.Background())
	if err != nil {
		t.Fatalf("应切到第二个源并成功，实得 %v", err)
	}
	if rel.Source != "gitee" {
		t.Errorf("命中源名 = %q，期望 gitee", rel.Source)
	}
	if rel.URL != "http://x/a.deb" || rel.SHA256 != "aa" || rel.Signature != "sa" || rel.Size != 11 {
		t.Errorf("未按本机平台解析安装包: %+v", rel)
	}
}

// 单源卡住超过超时即放弃该源（不因一个源拖死整次检查）
func TestSources_SlowSourceTimesOut(t *testing.T) {
	slow := manifestServer(t, okManifest, 300*time.Millisecond)
	fast := manifestServer(t, `{"version":"8.8.8","url":"http://y/b","size":5,"sha256":"cc","signature":"sc"}`, 0)
	s := NewSources([]Source{{Name: "github", ManifestURL: slow.URL}, {Name: "gitee", ManifestURL: fast.URL}})
	s.timeout = 30 * time.Millisecond
	rel, err := s.FetchLatest(context.Background())
	if err != nil {
		t.Fatalf("慢源超时后应落到快源，实得 %v", err)
	}
	if rel.Source != "gitee" || rel.Version != "8.8.8" {
		t.Errorf("落到 %s / %s，期望 gitee / 8.8.8", rel.Source, rel.Version)
	}
}

// 镜像源常滞后于原始发布：配置靠前的 gitee 版本更低时，仍要取到 github 那份更高的
func TestSources_PicksNewestAcrossSources(t *testing.T) {
	old := manifestServer(t, `{"version":"0.1.41","url":"http://x/old","size":1,"sha256":"aa","signature":"sa"}`, 0)
	newer := manifestServer(t, `{"version":"0.1.42","url":"http://x/new","size":2,"sha256":"bb","signature":"sb"}`, 0)
	s := NewSources([]Source{{Name: "gitee", ManifestURL: old.URL}, {Name: "github", ManifestURL: newer.URL}})
	rel, err := s.FetchLatest(context.Background())
	if err != nil {
		t.Fatalf("实得 %v", err)
	}
	if rel.Version != "0.1.42" || rel.Source != "github" || rel.URL != "http://x/new" || rel.Size != 2 {
		t.Errorf("未跨源取版本最大: %s / %s / %s / %d", rel.Version, rel.Source, rel.URL, rel.Size)
	}
}

// 版本并列时取配置顺序靠前的一份（数组顺序的残留语义：并列裁决与报错点名顺序）
func TestSources_EqualVersionKeepsConfigOrder(t *testing.T) {
	a := manifestServer(t, `{"version":"9.9.9","url":"http://x/a","size":1,"sha256":"aa","signature":"sa"}`, 0)
	b := manifestServer(t, `{"version":"9.9.9","url":"http://x/b","size":1,"sha256":"bb","signature":"sb"}`, 0)
	s := NewSources([]Source{{Name: "github", ManifestURL: a.URL}, {Name: "gitee", ManifestURL: b.URL}})
	rel, err := s.FetchLatest(context.Background())
	if err != nil {
		t.Fatalf("实得 %v", err)
	}
	if rel.Source != "github" || rel.URL != "http://x/a" {
		t.Errorf("并列版本应取配置靠前的一份，实得 %s / %s", rel.Source, rel.URL)
	}
}

// 超时是每源独立并同时起算：三源全挂约一个超时即收口，不叠加成 3 倍等待
func TestSources_TimeoutsDoNotStack(t *testing.T) {
	const timeout = 300 * time.Millisecond
	var list []Source
	for i := 0; i < 3; i++ {
		srv := manifestServer(t, okManifest, 10*time.Second) // 永不响应（客户端先超时）
		list = append(list, Source{Name: fmt.Sprintf("source%d", i), ManifestURL: srv.URL})
	}
	s := NewSources(list)
	s.timeout = timeout
	start := time.Now()
	_, err := s.FetchLatest(context.Background())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("全部源超时必须报错")
	}
	if elapsed >= 2*timeout {
		t.Errorf("超时按源叠加了（串行 ≈3×%v），期望并发 ≈%v，实得 %v", timeout, timeout, elapsed)
	}
}

// 全部源失败：错误必须逐个点名，用户才知道该改哪一条配置
func TestSources_AllFailNamesEverySource(t *testing.T) {
	a := manifestServer(t, "", 0)
	b := manifestServer(t, `not json`, 0)
	s := NewSources([]Source{{Name: "github", ManifestURL: a.URL + "/x"}, {Name: "gitee", ManifestURL: b.URL}})
	a.Close() // 让 github 连接失败
	_, err := s.FetchLatest(context.Background())
	if err == nil {
		t.Fatal("全败必须报错")
	}
	msg := err.Error()
	for _, want := range []string{"所有发布源均不可用", "github:", "gitee:", "解析发布清单失败"} {
		if !strings.Contains(msg, want) {
			t.Errorf("错误缺少 %q：%s", want, msg)
		}
	}
}

func TestSources_EmptyList(t *testing.T) {
	if _, err := (&Sources{}).FetchLatest(context.Background()); err == nil || !strings.Contains(err.Error(), "未配置任何发布源") {
		t.Fatalf("空源列表应报「未配置任何发布源」，实得 %v", err)
	}
}

// 清单 version 缺席等于「永远没更新」，必须当失败处理、由其余源顶上
func TestSources_ManifestWithoutVersionFailsOver(t *testing.T) {
	bad := manifestServer(t, `{"url":"http://x/a"}`, 0)
	good := manifestServer(t, okManifest, 0)
	s := NewSources([]Source{{Name: "github", ManifestURL: bad.URL}, {Name: "gitee", ManifestURL: good.URL}})
	rel, err := s.FetchLatest(context.Background())
	if err != nil {
		t.Fatalf("实得 %v", err)
	}
	if rel.Source != "gitee" {
		t.Errorf("命中 %s，期望 gitee", rel.Source)
	}
}

func TestResolvePlatform(t *testing.T) {
	assets := []Asset{
		{OS: "linux", Format: "rpm", URL: "u.rpm", Size: 1, SHA256: "s.rpm"},
		{OS: "Linux", Format: "deb", URL: "u.deb", Size: 2, SHA256: "s.deb"},
		{OS: "darwin", Format: "dmg", URL: "u.dmg", Size: 3, SHA256: "s.dmg"},
	}
	cases := []struct {
		name string
		p    platform
		want string
	}{
		{"linux 取 deb", platform{"linux", "deb"}, "u.deb"},
		{"linux 取 rpm", platform{"linux", "rpm"}, "u.rpm"},
		{"darwin 忽略大小写", platform{"darwin", ""}, "u.dmg"},
	}
	for _, tc := range cases {
		r := Release{Version: "1.0.0", Assets: assets}
		if err := r.resolvePlatform(tc.p); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if r.URL != tc.want {
			t.Errorf("%s: 选到 %s，期望 %s", tc.name, r.URL, tc.want)
		}
	}
	// 本机平台没有精确格式时放宽到同 os 的任意一份
	r := Release{Version: "1.0.0", Assets: []Asset{{OS: "linux", Format: "appimage", URL: "u.AppImage"}}}
	if err := r.resolvePlatform(platform{"linux", "deb"}); err != nil || r.URL != "u.AppImage" {
		t.Errorf("应放宽到同 os 的一份: %v / %s", err, r.URL)
	}
	// 完全不匹配要点名本机平台
	r = Release{Version: "1.0.0", Assets: []Asset{{OS: "windows", URL: "u.exe"}}}
	err := r.resolvePlatform(platform{"linux", "deb"})
	if err == nil || !strings.Contains(err.Error(), "linux/deb") {
		t.Errorf("应报无本机平台，实得 %v", err)
	}
	// 旧式单包清单（无 assets）顶层字段原样可用
	r = Release{Version: "1.0.0", URL: "u.bin", Size: 9}
	if err := r.resolvePlatform(platform{"linux", "deb"}); err != nil || r.URL != "u.bin" {
		t.Errorf("单包清单应原样可用: %v / %s", err, r.URL)
	}
	r = Release{Version: "1.0.0"}
	if err := r.resolvePlatform(platform{"linux", "deb"}); err == nil {
		t.Error("既无 assets 又无 url 应报错")
	}
}

// Checker 把命中源名随 update:available 广播出去（界面「更新源」唯一来源）
func TestCheckEmitsSource(t *testing.T) {
	srv := manifestServer(t, okManifest, 0)
	em := &capEmitter{}
	c := NewChecker("0.1.0", NewSources([]Source{{Name: "gitee", ManifestURL: srv.URL}}), em)
	if _, newer, err := c.Check(context.Background()); err != nil || !newer {
		t.Fatalf("newer=%v err=%v", newer, err)
	}
	var got model.UpdateAvailable
	for _, e := range em.Capture() {
		if e.Name == "update:available" {
			got, _ = e.Payload.(model.UpdateAvailable)
		}
	}
	if got.Source != "gitee" {
		t.Errorf("载荷未带源名: %+v", got)
	}
}
