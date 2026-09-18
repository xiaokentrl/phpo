// 状态校准：以 Docker 实际运行态为准回写期望运行集，并产出完整漂移报告（§5.13.9 一致性；硬红线 4）
// Calibrate 为纯逻辑无 IO，供 T306 生命周期服务在启动 / 每任务后 / 手动三处复用同一判定口径。
package engine

// Correction 一次校准对单容器「运行态」的修正：Docker 实际与期望不符时，把权威视图拉齐到实际。
// 方向依据：进程是否真在运行由 Docker 决定，SQLite 记录须被校准到与之一致。
type Correction struct {
	Ref     ContainerRef
	Running bool // 纠正后的运行态（= Docker 实际）
}

// CalibrateResult 校准产物：可落库的运行态修正 + 完整漂移（含不自动处置的存在性差异）。
type CalibrateResult struct {
	Corrections []Correction
	Drift       Drift
}

// Changed 是否需要写回（有运行态修正或存在性漂移）。
func (r CalibrateResult) Changed() bool {
	return len(r.Corrections) > 0 || !r.Drift.Empty()
}

// Calibrate 比对期望（installed/running，running ⊆ installed）与 Docker 实际态：
//   - 对「期望已安装且实际存在、但运行态不符」者产出 Correction（把 running 拉齐到实际）；
//   - 存在性漂移（Missing 未落地 / Extra 残留孤儿）仅上报 Drift，不自动删建，交用户或清理线决策，避免误删数据。
func Calibrate(installed, running []ContainerRef, actual []ActualState) CalibrateResult {
	wantRunning := nameSet(running)

	byName := make(map[string]ActualState, len(actual))
	for _, a := range actual {
		byName[a.Ref.Name()] = a
	}

	var corrections []Correction
	for _, ref := range installed {
		n := ref.Name()
		a, exists := byName[n]
		if !exists {
			continue // 存在性漂移，由 Drift.Missing 上报，不改运行态
		}
		if wantRunning[n] != a.Running {
			corrections = append(corrections, Correction{Ref: ref, Running: a.Running})
		}
	}

	return CalibrateResult{
		Corrections: corrections,
		Drift:       VerifyState(installed, running, actual),
	}
}
