// QualifyImageRef 的用例锁：短名补全的两条规则 + 三类原样返回（全名/带端口/digest）
package engine

import "testing"

func TestQualifyImageRef(t *testing.T) {
	cases := []struct{ in, want string }{
		// 单段仓库名：补 docker.io/library/
		{"mysql:5.7", "docker.io/library/mysql:5.7"},
		{"php", "docker.io/library/php"},
		{"nginx:alpine", "docker.io/library/nginx:alpine"},
		// 一段命名空间：补 docker.io/
		{"phpo/php:8.4", "docker.io/phpo/php:8.4"},
		{"myteam/app", "docker.io/myteam/app"},
		// 带主机端口：原样（first 段含 . 或 :）
		{"docker.m.daocloud.io/library/mysql:5.7", "docker.m.daocloud.io/library/mysql:5.7"},
		{"localhost/mysql:5.7", "localhost/mysql:5.7"},
		{"registry:5000/app:1", "registry:5000/app:1"},
		// 全名：原样
		{"docker.io/library/mysql:5.7", "docker.io/library/mysql:5.7"},
		{"docker.io/phpo/php:8.4", "docker.io/phpo/php:8.4"},
		// digest 引用与空串：原样
		{"mysql@sha256:5107333e08a8", "mysql@sha256:5107333e08a8"},
		{"", ""},
	}
	for _, c := range cases {
		if got := QualifyImageRef(c.in); got != c.want {
			t.Errorf("QualifyImageRef(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}
