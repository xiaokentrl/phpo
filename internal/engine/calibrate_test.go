// T306 验收：Calibrate 纯逻辑——docker kill 后检出运行态漂移并产出回写修正；存在性漂移仅上报不误改
package engine

import "testing"

func php84() ContainerRef { return ContainerRef{Kind: "php", Version: "8.4"} }

func TestCalibrate_NoDrift(t *testing.T) {
	res := Calibrate([]ContainerRef{php84()}, []ContainerRef{php84()},
		[]ActualState{{Ref: php84(), Running: true}})
	if res.Changed() {
		t.Fatalf("期望一致应无变化，得 %+v", res)
	}
	if len(res.Corrections) != 0 {
		t.Fatalf("无修正，实得 %+v", res.Corrections)
	}
}

// 核心场景：容器被外部 docker kill —— 期望运行、实际停止 → 产出「拉齐到停止」的修正
func TestCalibrate_KilledContainerProducesCorrection(t *testing.T) {
	res := Calibrate([]ContainerRef{php84()}, []ContainerRef{php84()},
		[]ActualState{{Ref: php84(), Running: false}})
	if !res.Changed() {
		t.Fatal("kill 后应检出漂移")
	}
	if len(res.Corrections) != 1 || res.Corrections[0].Ref != php84() || res.Corrections[0].Running != false {
		t.Fatalf("修正错误: %+v", res.Corrections)
	}
	if len(res.Drift.Stopped) != 1 || res.Drift.Stopped[0] != "phpo-php-8.4" {
		t.Fatalf("漂移应记 Stopped=[phpo-php-8.4]，实得 %+v", res.Drift)
	}
}

// 意外运行：期望停止、实际在跑 → 产出「拉齐到运行」修正，并记 Unexpected
func TestCalibrate_UnexpectedRunning(t *testing.T) {
	res := Calibrate([]ContainerRef{php84()}, nil, // 期望未运行
		[]ActualState{{Ref: php84(), Running: true}})
	if len(res.Corrections) != 1 || !res.Corrections[0].Running {
		t.Fatalf("应产出 running=true 修正，实得 %+v", res.Corrections)
	}
	if len(res.Drift.Unexpected) != 1 || res.Drift.Unexpected[0] != "phpo-php-8.4" {
		t.Fatalf("应记 Unexpected，实得 %+v", res.Drift)
	}
}

// 存在性漂移（幽灵：SQLite 有、Docker 无）：仅上报 Missing，不产出运行态修正（不误删）
func TestCalibrate_MissingReportOnly(t *testing.T) {
	res := Calibrate([]ContainerRef{php84()}, []ContainerRef{php84()}, nil)
	if len(res.Corrections) != 0 {
		t.Fatalf("缺失不应产运行修正，实得 %+v", res.Corrections)
	}
	if !res.Changed() {
		t.Fatal("存在性漂移应判为已变化")
	}
	if len(res.Drift.Missing) != 1 || res.Drift.Missing[0] != "phpo-php-8.4" {
		t.Fatalf("应记 Missing，实得 %+v", res.Drift)
	}
}

// 孤儿（Docker 有、SQLite 无）：仅上报 Extra
func TestCalibrate_ExtraReportOnly(t *testing.T) {
	orphan := ContainerRef{Kind: "redis", Version: "7"}
	res := Calibrate(nil, nil, []ActualState{{Ref: orphan, Running: true}})
	if len(res.Corrections) != 0 {
		t.Fatalf("孤儿不应产运行修正")
	}
	if len(res.Drift.Extra) != 1 || res.Drift.Extra[0] != "phpo-redis-7" {
		t.Fatalf("应记 Extra，实得 %+v", res.Drift)
	}
}
