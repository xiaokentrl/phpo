// T304：容器/网络/卷装配的纯函数单测（命名、挂载、别名、标签、端口映射、重启策略）
package engine

import (
	"testing"

	"github.com/docker/docker/api/types/container"
	"phpo/internal/config"
	"phpo/pkg/dockerutil"
)

func testEnv() config.Env { return config.DerivePaths("~/phpo", "~/www") }

func TestOwnershipLabels(t *testing.T) {
	got := OwnershipLabels("php", "8.4")
	if got["phpo.managed"] != "true" || got["phpo.kind"] != "php" || got["phpo.version"] != "8.4" {
		t.Errorf("归属标签不完整: %v", got)
	}
	net := OwnershipLabels("", "")
	if net["phpo.kind"] != "" || net["phpo.version"] != "" || net["phpo.managed"] != "true" {
		t.Errorf("网络标签应仅含 managed: %v", net)
	}
}

func TestBuildBindsPHP(t *testing.T) {
	binds := BuildBinds(testEnv(), "php", "8.4")
	if len(binds) != 4 {
		t.Fatalf("php 8.4 应有 4 条 bind，实得 %d: %v", len(binds), binds)
	}
	want := []string{
		"~/www:/var/www:rw",
		"~/phpo/php/8.4/conf/php.ini:/usr/local/etc/php/conf.d/zz-phpo.ini:ro",
		"~/phpo/php/8.4/conf/php-fpm.conf:/usr/local/etc/php-fpm.d/zz-phpo.conf:ro",
		"~/phpo/php/8.4/logs:/var/log/php-fpm:rw",
	}
	for i, w := range want {
		if binds[i] != w {
			t.Errorf("bind[%d] = %q，期望 %q", i, binds[i], w)
		}
	}
	if got := BuildBinds(testEnv(), "unknown", "1"); len(got) != 0 {
		t.Errorf("未知服务应无 bind，实得 %v", got)
	}
}

func TestBuildContainerSpecs(t *testing.T) {
	spec := ContainerSpec{
		Kind:    "php",
		Version: "8.4",
		Image:   "php:8.4-fpm",
		Env:     []string{"PHP_IDE_CONFIG=x"},
		Exposed: []string{"9000/tcp"},
		PortMap: map[string]string{"9000/tcp": "9000"},
		Labels:  map[string]string{"extra": "yes"},
	}
	cfg, host, netc := buildContainerSpecs(testEnv(), spec)

	if cfg.Image != "php:8.4-fpm" {
		t.Errorf("镜像 = %q", cfg.Image)
	}
	if cfg.Labels["phpo.kind"] != "php" || cfg.Labels["extra"] != "yes" {
		t.Errorf("标签合并失败: %v", cfg.Labels)
	}
	if _, ok := cfg.ExposedPorts["9000/tcp"]; !ok {
		t.Errorf("暴露端口缺失: %v", cfg.ExposedPorts)
	}
	if host.RestartPolicy.Name != container.RestartPolicyUnlessStopped {
		t.Errorf("重启策略 = %q", host.RestartPolicy.Name)
	}
	if len(host.Binds) != 4 {
		t.Errorf("bind 数 = %d，期望 4", len(host.Binds))
	}
	b, ok := host.PortBindings["9000/tcp"]
	if !ok || len(b) != 1 || b[0].HostPort != "9000" {
		t.Errorf("端口映射错误: %v", host.PortBindings)
	}

	ep, ok := netc.EndpointsConfig[dockerutil.NetworkName]
	if !ok {
		t.Fatalf("未挂到 %s 网络", dockerutil.NetworkName)
	}
	// 硬红线 1：PHP 网络别名必须精确为 php-8.4-fpm
	if len(ep.Aliases) != 1 || ep.Aliases[0] != "php-8.4-fpm" {
		t.Errorf("PHP 网络别名 = %v，期望 [php-8.4-fpm]", ep.Aliases)
	}
}

func TestBuildContainerSpecsMySQLAlias(t *testing.T) {
	_, _, netc := buildContainerSpecs(testEnv(), ContainerSpec{Kind: "mysql", Version: "8.4"})
	ep := netc.EndpointsConfig[dockerutil.NetworkName]
	if len(ep.Aliases) != 1 || ep.Aliases[0] != "mysql-8.4" {
		t.Errorf("非 PHP 别名 = %v，期望 [mysql-8.4]", ep.Aliases)
	}
}

func TestMergeLabels(t *testing.T) {
	base := map[string]string{"a": "1", "b": "2"}
	got := mergeLabels(base, map[string]string{"b": "9", "c": "3"})
	if got["a"] != "1" || got["b"] != "9" || got["c"] != "3" {
		t.Errorf("merge 结果错误: %v", got)
	}
	// 不改动入参
	if base["b"] != "2" {
		t.Errorf("merge 不应修改 base: %v", base)
	}
	if same := mergeLabels(base, nil); len(same) != 2 {
		t.Errorf("extra 为空应原样返回 base: %v", same)
	}
}
