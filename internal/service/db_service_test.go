// T501/T502 验收：数据服务装配的 ContainerSpec——密码 env（设置/空/未设回落）+ 宿主端口发布 + 镜像引用
// 数据持久由 config.MOUNTS 经 engine.BuildBinds 自动生成 bind，此处不重复验证挂载
package service

import (
	"testing"

	"phpo/internal/config"
	"phpo/internal/model"
)

// fakePasswords 实现 EnvReader：按 {kind,version} 返回预置密码/端口；exists=false 模拟未设置
type fakePasswords struct {
	m     map[string]pw
	ports map[string]int
}

type pw struct {
	value  string
	exists bool
}

func (f fakePasswords) GetPassword(kind, version string) (string, bool, error) {
	e, ok := f.m[kind+"/"+version]
	if !ok {
		return "", false, nil
	}
	return e.value, e.exists, nil
}

func (f fakePasswords) GetServicePort(kind, version string) (int, bool, error) {
	p, ok := f.ports[kind+"/"+version]
	if !ok {
		return 0, false, nil
	}
	return p, true, nil
}

func envValue(env []string, key string) (string, bool) {
	for _, kv := range env {
		if k, v, ok := splitKV(kv); ok && k == key {
			return v, true
		}
	}
	return "", false
}

func splitKV(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

func TestDBSpec_MySQL_PasswordSet(t *testing.T) {
	svc := NewDBService(model.KindMySQL, fakePasswords{m: map[string]pw{"mysql/8.4": {"s3cr3t", true}}})
	spec, err := svc.ContainerSpec("8.4", config.DerivePaths("~/phpo", "~/www"))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := envValue(spec.Env, "MYSQL_ROOT_PASSWORD"); !ok || got != "s3cr3t" {
		t.Fatalf("应注入 MYSQL_ROOT_PASSWORD=s3cr3t，实得 %q(ok=%v)", got, ok)
	}
	if _, ok := envValue(spec.Env, "MYSQL_ALLOW_EMPTY_PASSWORD"); ok {
		t.Fatal("已设密码时不应允许空密码")
	}
	if spec.Image != "mysql:8.4" {
		t.Fatalf("镜像引用错误: %s", spec.Image)
	}
	if spec.PortMap["3306/tcp"] != "3306" {
		t.Fatalf("应发布 3306→3306，实得 %+v", spec.PortMap)
	}
}

func TestDBSpec_MySQL_EmptyPassword(t *testing.T) {
	svc := NewDBService(model.KindMySQL, fakePasswords{m: map[string]pw{"mysql/8.4": {"", true}}})
	spec, err := svc.ContainerSpec("8.4", config.DerivePaths("~/phpo", "~/www"))
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := envValue(spec.Env, "MYSQL_ALLOW_EMPTY_PASSWORD"); !ok || v != "yes" {
		t.Fatalf("空密码应设 MYSQL_ALLOW_EMPTY_PASSWORD=yes，实得 %q(ok=%v)", v, ok)
	}
	if _, ok := envValue(spec.Env, "MYSQL_ROOT_PASSWORD"); ok {
		t.Fatal("空密码时不应设 MYSQL_ROOT_PASSWORD")
	}
}

func TestDBSpec_MySQL_UnsetFallsBackToDefault(t *testing.T) {
	svc := NewDBService(model.KindMySQL, fakePasswords{})
	spec, err := svc.ContainerSpec("8.4", config.DerivePaths("~/phpo", "~/www"))
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := envValue(spec.Env, "MYSQL_ROOT_PASSWORD"); !ok || v != config.DefaultPassword {
		t.Fatalf("未设密码应回落默认 %s，实得 %q", config.DefaultPassword, v)
	}
}

func TestDBSpec_Pgsql_PasswordSetAndEmpty(t *testing.T) {
	env := config.DerivePaths("~/phpo", "~/www")

	set := NewDBService(model.KindPgsql, fakePasswords{m: map[string]pw{"pgsql/17": {"pw with space", true}}})
	spec, err := set.ContainerSpec("17", env)
	if err != nil {
		t.Fatal(err)
	}
	// 含空格密码原样进 env（Docker env 不经 shell，任意字符合法，§1.5）
	if v, ok := envValue(spec.Env, "POSTGRES_PASSWORD"); !ok || v != "pw with space" {
		t.Fatalf("应注入 POSTGRES_PASSWORD，实得 %q", v)
	}
	if spec.Image != "postgres:17" {
		t.Fatalf("pgsql 镜像应为 postgres:17，实得 %s", spec.Image)
	}
	if spec.PortMap["5432/tcp"] != "5432" {
		t.Fatalf("应发布 5432→5432，实得 %+v", spec.PortMap)
	}
	// pgsql 必须显式指向挂载的配置文件，否则官方镜像忽略 /etc/postgresql/postgresql.conf
	wantCmd := []string{"postgres", "-c", "config_file=/etc/postgresql/postgresql.conf"}
	if len(spec.Command) != len(wantCmd) {
		t.Fatalf("pgsql Command 异常: %v", spec.Command)
	}
	for i := range wantCmd {
		if spec.Command[i] != wantCmd[i] {
			t.Fatalf("pgsql Command[%d]=%q，期望 %q", i, spec.Command[i], wantCmd[i])
		}
	}

	empty := NewDBService(model.KindPgsql, fakePasswords{m: map[string]pw{"pgsql/17": {"", true}}})
	spec2, err := empty.ContainerSpec("17", env)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := envValue(spec2.Env, "POSTGRES_HOST_AUTH_METHOD"); !ok || v != "trust" {
		t.Fatalf("pgsql 空密码应设 POSTGRES_HOST_AUTH_METHOD=trust，实得 %q(ok=%v)", v, ok)
	}
	if _, ok := envValue(spec2.Env, "POSTGRES_PASSWORD"); ok {
		t.Fatal("空密码时不应设 POSTGRES_PASSWORD")
	}
}

// 多版本并存：落库端口优先于注册表默认，使同服务不同版本各占宿主端口（§5.8 服务端口占用报错、不顺延）
func TestDBSpec_MySQL_PerVersionHostPort(t *testing.T) {
	svc := NewDBService(model.KindMySQL, fakePasswords{
		m:     map[string]pw{"mysql/8.4": {"pw", true}, "mysql/8.0": {"pw", true}},
		ports: map[string]int{"mysql/8.0": 3307}, // 仅 8.0 落库自定义端口；8.4 回落默认 3306
	})
	env := config.DerivePaths("~/phpo", "~/www")

	spec80, err := svc.ContainerSpec("8.0", env)
	if err != nil {
		t.Fatal(err)
	}
	if spec80.PortMap["3306/tcp"] != "3307" {
		t.Fatalf("8.0 应发布到落库端口 3307，实得 %+v", spec80.PortMap)
	}

	spec84, err := svc.ContainerSpec("8.4", env)
	if err != nil {
		t.Fatal(err)
	}
	if spec84.PortMap["3306/tcp"] != "3306" {
		t.Fatalf("8.4 未落库端口应回落注册表默认 3306，实得 %+v", spec84.PortMap)
	}
}
