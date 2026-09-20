// T607 · 装机向导服务：把原型 openHomeSetupWizard 的「验证 / 确认」两步真化到后端。
// HomeVerify 纯探测（校验路径安全 + 逐级创建 PHPO_HOME 子树与 WWW_ROOT + 可写测试），不落库、不发事件；
// HomeEnsure 走三段式任务：创建工作目录子树 → Apply 把两根目录写入 config.yaml（ConfigStore）并置 dirReady=true → 广播 state:changed。
// 遵循硬红线 3（.. 路径穿越拒绝）、硬红线 4（前端只读快照，不本地乐观更新）、硬红线 5（写操作三段式）。
// 幂等：HomeEnsure 先检测两根是否「已持久化 + 目录已存在」，已设置则跳过建树、仅校正 dirReady 并广播，禁止重复创建工作目录。
package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"phpo/internal/config"
	"phpo/internal/model"
	"phpo/internal/task"
	"phpo/pkg/errs"
)

// WizardConfig 装机根目录落库子集（*config.ConfigStore 满足）：写两根 + 读原始根
type WizardConfig interface {
	SetRoots(home, www string) error
	Roots() (home, www string)
}

// WizardStore 就绪标记 + 权威快照回流子集（*store.Store 满足）：dirReady 属运行态，仍留 SQLite
type WizardStore interface {
	SetDirReady(key string, ready bool) error
	BuildSnapshot() (*model.Snapshot, error)
}

// WizardService 装机向导门面
type WizardService struct {
	cfg   WizardConfig
	store WizardStore
	em    Emitter
	tasks *task.Manager
	seq   atomic.Uint64
}

func NewWizardService(cfg WizardConfig, st WizardStore, em Emitter, tm *task.Manager) *WizardService {
	return &WizardService{cfg: cfg, store: st, em: em, tasks: tm}
}

// HomeVerify 校验并创建工作目录子树 + 可写探测；仅返回逐条进度与错误，不做任何持久化（供向导「验证」按钮实时反馈）
func (s *WizardService) HomeVerify(ctx context.Context, home, www string) (model.HomeVerifyResult, error) {
	h, w, verr := validateHomeWww(home, www)
	if verr != "" {
		return model.HomeVerifyResult{Errors: []string{verr}}, nil
	}
	lines, fsErrs := ensureTree(h, w)
	return model.HomeVerifyResult{OK: len(fsErrs) == 0, Lines: lines, Errors: fsErrs}, nil
}

// HomeEnsure 装机确认：三段式任务创建工作目录子树并落地 env / dirReady，随后广播 state:changed（硬红线 5）
// 先决检测：两根目录已在 config.yaml 持久化且实际存在 → 视为「已设置」，只校正就绪标记并广播以即时更新 UI，
// 跳过目录子树创建，禁止重复创建（幂等）。
func (s *WizardService) HomeEnsure(ctx context.Context, home, www string) error {
	h, w, verr := validateHomeWww(home, www)
	if verr != "" {
		return fmt.Errorf("%s", verr)
	}
	if s.alreadyReady(h, w) {
		if err := s.markReady(); err != nil {
			return err
		}
		return s.emit()
	}
	t := &task.Task{
		ID:    s.newID("home-ensure"),
		Label: "初始化工作目录",
		Meta:  model.TaskMeta{Type: "home-ensure"},
		Steps: []task.Step{
			&task.FuncStep{StepName: "创建工作目录子树", Exec: func(_ context.Context, log task.StepLog) error {
				lines, fsErrs := ensureTree(h, w)
				for _, ln := range lines {
					log.Log(string(model.LogOk), ln)
				}
				if len(fsErrs) > 0 {
					return fmt.Errorf("%s", fsErrs[0])
				}
				return nil
			}},
		},
		Apply: func() error {
			if err := s.persistEnv(h, w); err != nil {
				return err
			}
			return s.emit()
		},
	}
	_, err := s.tasks.Run(ctx, t)
	return err
}

// ---- 内部助手 ----

// validateHomeWww 归一两路径并做装机级校验：非空 / 无 .. 穿越（硬红线 3）/ 二者不相同；返回规范化值与单条错误文案
func validateHomeWww(home, www string) (h, w, errMsg string) {
	h = config.NormPath(home)
	w = config.NormPath(www)
	switch {
	case h == "":
		return h, w, "PHPO_HOME 不能为空"
	case w == "":
		return h, w, "WWW_ROOT 不能为空"
	case config.HasTraversal(h) || config.HasTraversal(w):
		return h, w, errs.PathTraversal
	case config.ExpandHome(h) == config.ExpandHome(w):
		return h, w, "PHPO_HOME 与 WWW_ROOT 不能相同"
	}
	return h, w, ""
}

// ensureTree 逐级创建 PHPO_HOME 子树与 WWW_ROOT 并试写探测可写；返回逐条成功行与错误行（幂等，目录已存在即空操作）
func ensureTree(home, www string) (lines, errsList []string) {
	hRoot := config.ExpandHome(home)
	wRoot := config.ExpandHome(www)
	probe := func(label, p string) {
		if dirWritable(p) {
			lines = append(lines, fmt.Sprintf("✓ %s %s", label, filepath.ToSlash(p)))
		} else {
			errsList = append(errsList, fmt.Sprintf("✗ 不可写：%s", filepath.ToSlash(p)))
		}
	}
	probe("PHPO_HOME", hRoot)
	for _, sd := range config.HomeSubdirs {
		probe("子目录", filepath.Join(hRoot, filepath.FromSlash(sd.Path)))
	}
	probe("WWW_ROOT", wRoot)
	return lines, errsList
}

// alreadyReady 判定请求的两根目录是否「已设置」：config.yaml 已持久化非空根、展开后与请求一致、且两目录实际存在。
// 命中即说明工作目录早已建好，HomeEnsure 据此跳过重复创建。
func (s *WizardService) alreadyReady(home, www string) bool {
	ph, pw := s.cfg.Roots()
	if ph == "" || pw == "" {
		return false
	}
	sameHome := config.ExpandHome(config.NormPath(ph)) == config.ExpandHome(home)
	sameWww := config.ExpandHome(config.NormPath(pw)) == config.ExpandHome(www)
	return sameHome && sameWww && dirExists(config.ExpandHome(home)) && dirExists(config.ExpandHome(www))
}

// markReady 置双 dirReady=true（供 Snapshot 回流 + preflight NEEDS_HOME 放行）；仅在两根目录已实际存在时调用。
func (s *WizardService) markReady() error {
	if err := s.store.SetDirReady("PHPO_HOME", true); err != nil {
		return err
	}
	return s.store.SetDirReady("WWW_ROOT", true)
}

// persistEnv 把规范化 home/www 写入 config.yaml（ConfigStore，仅存两根，派生路径现算），并置双 dirReady=true（供 Snapshot 回流）
func (s *WizardService) persistEnv(home, www string) error {
	if err := s.cfg.SetRoots(home, www); err != nil {
		return err
	}
	// 主目录与网站目录一并建树成功 → 双就绪标记（供 Snapshot 回流 + preflight NEEDS_HOME 放行）
	return s.markReady()
}

// RefreshDirReady 启动时按「config.yaml 已持久化 + 目录实际存在」重算双就绪标记：任一缺失即置 false（永久阻断写操作）。
// 首启两根为空 → 双 false → 弹装机向导；曾就绪但目录被删 → 回落 false 重新拦截。返回是否有降级变化。
func (s *WizardService) RefreshDirReady() (bool, error) {
	changed := false
	roots := map[string]string{"PHPO_HOME": "", "WWW_ROOT": ""}
	roots["PHPO_HOME"], roots["WWW_ROOT"] = s.cfg.Roots()
	for _, key := range []string{"PHPO_HOME", "WWW_ROOT"} {
		raw := roots[key]
		ready := raw != "" && dirExists(config.ExpandHome(raw))
		if err := s.store.SetDirReady(key, ready); err != nil {
			return false, err
		}
		changed = changed || !ready
	}
	return changed, nil
}

// dirExists 目录存在且为目录（跟随符号链接）
func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// emit 拉取权威快照并广播 state:changed（前端据此落地 env 与 dirReady，无乐观更新）
func (s *WizardService) emit() error {
	snap, err := s.store.BuildSnapshot()
	if err != nil {
		return err
	}
	s.em.Emit("state:changed", map[string]any{"snapshot": snap})
	return nil
}

func (s *WizardService) newID(op string) string {
	return fmt.Sprintf("%s-%d", op, s.seq.Add(1))
}
