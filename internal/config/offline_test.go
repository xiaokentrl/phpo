// 离线缓存路径契约测试（§5.14.2 目录结构冻结，禁止漂移）
package config

import "testing"

func TestOfflinePaths(t *testing.T) {
	e := DerivePaths("", "")
	cases := []struct{ got, want string }{
		{e.OfflineImageTar("php", "8.4"), "~/phpo/offline/php/8.4/image.tar"},
		{e.OfflineManifestFile("mysql", "8.4"), "~/phpo/offline/mysql/8.4/manifest.json"},
		{e.OfflineExtDir("php", "8.4", ExtTypePECL), "~/phpo/offline/php/8.4/pecl"},
		{e.TempExtDir("php", "8.4"), "~/phpo/php/8.4/ext"},
		{e.TempExtTypeDir("php", "8.4", ExtTypeAPK), "~/phpo/php/8.4/ext/apk"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("路径 = %q, 期望 %q", c.got, c.want)
		}
	}
}
