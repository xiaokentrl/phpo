// resolver 按引擎现取分岔 + 引擎结论的并发承载：识别晚于装配落定，所以「装配时抄一份定值」是禁项——
// 那会把 Podman 机器的 vhost 写成 Docker 的内嵌 DNS，站点解析坏掉且只有重绑对象图才自愈。
package app

import (
	"context"
	"errors"
	"testing"

	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/pkg/dockerutil"
)

func TestEngineResolverFunc_SwitchesWhenKindSettles(t *testing.T) {
	var gatewayCalls int
	gateway := func(context.Context, string) (string, error) {
		gatewayCalls++
		return "10.89.0.5", nil
	}
	kind := engine.EngineDocker
	r := engineResolverFunc(func() engine.EngineKind { return kind }, gateway)

	if got := r(); got != "127.0.0.11" {
		t.Fatalf("Docker 分支恒为内嵌 DNS，实得 %q", got)
	}
	if gatewayCalls != 0 {
		t.Fatalf("Docker 分支不该去问网络网关，实得 %d 次", gatewayCalls)
	}

	// 装配之后识别结论才落定：下一次取 resolver 必须已经是 Podman 分支
	kind = engine.EnginePodman
	if got := r(); got != "10.89.0.5" {
		t.Fatalf("换成 Podman 后必须立刻走网关分支（不得留在装配时的旧值），实得 %q", got)
	}
	if gatewayCalls != 1 {
		t.Fatalf("Podman 分支要现取网关，实得 %d 次", gatewayCalls)
	}
}

func TestEngineResolverFunc_UnknownIsNotPodman(t *testing.T) {
	r := engineResolverFunc(func() engine.EngineKind { return engine.EngineUnknown },
		func(context.Context, string) (string, error) { return "10.89.0.5", nil })
	if got := r(); got != "127.0.0.11" {
		t.Fatalf("问不出血统时按 Docker 口径给内嵌 DNS（与默认端口取 80 同档），实得 %q", got)
	}
}

func TestEngineResolverFunc_PodmanFallbacks(t *testing.T) {
	gatewayErr := true
	r := engineResolverFunc(
		func() engine.EngineKind { return engine.EnginePodman },
		func(context.Context, string) (string, error) {
			if gatewayErr {
				return "", errors.New("network inspect 失败")
			}
			return "10.89.0.7", nil
		},
	)
	if got := r(); got != "10.89.0.1" {
		t.Fatalf("问不出网关要退 podman 默认首网网关，实得 %q", got)
	}
	gatewayErr = false
	if got := r(); got != "10.89.0.7" {
		t.Fatalf("网关恢复后用现取值，实得 %q", got)
	}
	gatewayErr = true
	if got := r(); got != "10.89.0.7" {
		t.Fatalf("再问失败时退回上次成功值，实得 %q", got)
	}
}

func TestEngineResolverFunc_UsesPhpoNetworkName(t *testing.T) {
	var got string
	r := engineResolverFunc(
		func() engine.EngineKind { return engine.EnginePodman },
		func(_ context.Context, name string) (string, error) { got = name; return "10.89.0.9", nil },
	)
	_ = r()
	if got != dockerutil.NetworkName {
		t.Fatalf("网关只能问 phpo 自家网络，实得 %q", got)
	}
}

func TestEngineID_KindStaysEmptyUntilSettle(t *testing.T) {
	e := &engineID{info: model.EngineInfo{Endpoint: "/run/user/1000/podman/podman.sock", Rootless: true}}
	got := e.get()
	if got.Kind != "" {
		t.Fatalf("未识别时 Kind 必须留空（不得冒充 docker/podman），实得 %q", got.Kind)
	}
	if !got.Rootless || got.Endpoint == "" {
		t.Fatalf("端点与 rootless 不需拨号，装配期就要给快照：%+v", got)
	}
	e.set("podman", "5.7.0")
	after := e.get()
	if after.Kind != "podman" || after.Version != "5.7.0" {
		t.Fatalf("后台识别完成后要填上种类与版本，实得 %+v", after)
	}
	if after.Endpoint != got.Endpoint || after.Rootless != got.Rootless {
		t.Fatalf("填结论不得抹掉端点与 rootless：%+v", after)
	}
}
