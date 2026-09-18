// T503 验收：Redis 装配的 ContainerSpec——密码走容器 env（与 mysql/pgsql 一致），命令以 --requirepass 覆盖配置文件占位；
// 空密码附 --protected-mode no 以允许跨容器（php→redis 网络别名）连接；端口读 store 回落默认。
package service

import (
	"strings"
	"testing"

	"phpo/internal/config"
)

func redisSpec(t *testing.T, reader EnvReader, version string) specShape {
	t.Helper()
	sp, err := NewRedisService(reader).ContainerSpec(version, config.DerivePaths("~/phpo", "~/www"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sp.Command) != 3 || sp.Command[0] != "sh" || sp.Command[1] != "-c" {
		t.Fatalf("redis 应以 sh -c 包装命令以展开密码，实得 %v", sp.Command)
	}
	return specShape{image: sp.Image, env: sp.Env, script: sp.Command[2], portMap: sp.PortMap, exposed: sp.Exposed}
}

func TestRedisSpec_DefaultPassword(t *testing.T) {
	got := redisSpec(t, fakePasswords{}, "7")
	if got.image != "redis:7" {
		t.Fatalf("镜像应 redis:7，实得 %s", got.image)
	}
	if v, ok := envValue(got.env, "REDIS_PASSWORD"); !ok || v != config.DefaultPassword {
		t.Fatalf("未设密码应回落默认，实得 %q", v)
	}
	if !strings.Contains(got.script, `--requirepass "$REDIS_PASSWORD"`) {
		t.Fatalf("命令应含 --requirepass 环境变量引用，实得 %s", got.script)
	}
	if strings.Contains(got.script, "protected-mode no") {
		t.Fatal("有密码时不应关闭 protected-mode")
	}
	if got.portMap["6379/tcp"] != "6379" {
		t.Fatalf("默认应发布 6379，实得 %+v", got.portMap)
	}
}

func TestRedisSpec_SetPassword(t *testing.T) {
	got := redisSpec(t, fakePasswords{m: map[string]pw{"redis/7": {"p@ss word", true}}}, "7")
	if v, _ := envValue(got.env, "REDIS_PASSWORD"); v != "p@ss word" {
		t.Fatalf("含空格/特殊字符密码应原样进 env，实得 %q", v)
	}
}

func TestRedisSpec_EmptyPasswordAllowsProtectedModeOff(t *testing.T) {
	got := redisSpec(t, fakePasswords{m: map[string]pw{"redis/7": {"", true}}}, "7")
	if v, ok := envValue(got.env, "REDIS_PASSWORD"); !ok || v != "" {
		t.Fatalf("空密码应写空串，实得 %q(ok=%v)", v, ok)
	}
	if !strings.Contains(got.script, "--protected-mode no") || !strings.Contains(got.script, `--requirepass ""`) {
		t.Fatalf("空密码应关 protected-mode 且 requirepass 空，实得 %s", got.script)
	}
}

func TestRedisSpec_PerVersionPort(t *testing.T) {
	got := redisSpec(t, fakePasswords{ports: map[string]int{"redis/7": 6380}}, "7")
	if got.portMap["6379/tcp"] != "6380" {
		t.Fatalf("应发布到落库端口 6380，实得 %+v", got.portMap)
	}
}

// specShape 提取 ContainerSpec 关键面，便于断言
type specShape struct {
	image   string
	env     []string
	script  string
	portMap map[string]string
	exposed []string
}
