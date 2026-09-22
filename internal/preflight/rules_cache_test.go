// cache-import 规则单测（需求 1）：手工把任意文件导入为一条离线缓存
// 口径：只裁决「导入坐标」——kind/version 与安装同判据、extType 限三种落点；源文件是否存在交导入步骤报错
package preflight

import (
	"strings"
	"testing"

	"phpo/pkg/errs"
)

func TestCacheImport_AcceptsValidCoordinate(t *testing.T) {
	w := readyWorld()
	for _, c := range []Ctx{
		{Kind: "php", Version: "8.4", Field: "image", NewValue: "/tmp/php-8.4.tar"},
		{Kind: "mysql", Version: "8.4", Field: "image", NewValue: "/tmp/mysql.tar"},
		{Kind: "php", Version: "8.4", Field: "apk", NewValue: "/tmp/libzip.apk"},
		{Kind: "php", Version: "8.4", Field: "pecl", NewValue: "/tmp/redis.tgz"},
	} {
		res := Run(ActCacheImport, c, w)
		if !res.Ok {
			t.Errorf("%s/%s %s 应放行，得 %+v", c.Kind, c.Version, c.Field, res.Errors)
		}
	}
}

func TestCacheImport_RejectsBadCoordinate(t *testing.T) {
	w := readyWorld()
	res := Run(ActCacheImport, Ctx{Kind: "oracle", Version: "23", Field: "image"}, w)
	if res.Ok || firstErr(res) != errs.SvcMissing {
		t.Errorf("未知服务应报 svcMissing，得 %+v", res.Errors)
	}
	res = Run(ActCacheImport, Ctx{Kind: "php", Version: "../8.4", Field: "image"}, w)
	if res.Ok || !strings.Contains(res.Errors[0], errs.VersionInvalid) {
		t.Errorf("穿越版本应拒绝，得 %+v", res.Errors)
	}
	res = Run(ActCacheImport, Ctx{Kind: "php", Version: "8.4", Field: "deb"}, w)
	if res.Ok || !strings.Contains(firstErr(res), "image / apk / pecl") {
		t.Errorf("未知落点应拒绝，得 %+v", res.Errors)
	}
	// 扩展包只属于 php 缓存：apk/pecl 落在 mysql 会写到一条永不读取的目录
	res = Run(ActCacheImport, Ctx{Kind: "mysql", Version: "8.4", Field: "pecl"}, w)
	if res.Ok || !strings.Contains(firstErr(res), "只能导入到 php") {
		t.Errorf("非 php 服务不得导入 apk/pecl，得 %+v", res.Errors)
	}
}

// 两根未就绪不得往缓存根写文件（与 offline-prune 同组，违反首启零落盘）
func TestCacheImport_NeedsHome(t *testing.T) {
	w := readyWorld()
	w.Snap.DirReady["WWW_ROOT"] = false
	res := Run(ActCacheImport, Ctx{Kind: "php", Version: "8.5", Field: "image"}, w)
	if res.Ok || firstErr(res) != errs.HomeNotReady {
		t.Fatalf("HOME 未就绪应报 homeNotReady，得 %+v", res.Errors)
	}
}
