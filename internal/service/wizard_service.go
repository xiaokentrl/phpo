// T607 · 装机向导服务：把原型 openHomeSetupWizard 的「验证 / 确认」两步真化到后端。
// HomeVerify 只读预检（校验路径安全 + 判存在 + 按权限位判可写），**不创建任何目录、不落任何文件、不落库、不发事件**；
// 目录与文件的创建只发生在用户点「确认并创建」→ HomeEnsure。
// HomeEnsure 走三段式任务：创建工作目录子树 → Apply 把两根目录写入 config.yaml（ConfigStore）→ 广播 state:changed。
// 遵循硬红线 3（.. 路径穿越拒绝）、硬红线 4（前端只读快照，不本地乐观更新）、硬红线 5（写操作三段式）。
// 幂等：HomeEnsure 先检测两根是否「已持久化 + 目录已存在」，已设置则跳过建树直接广播，禁止重复创建工作目录。
// dirReady 不再落库：由 config.yaml 两根 + 目录存在性在快照内派生（见 store.BuildSnapshot）。
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

// WizardStore 权威快照回流子集（*store.Store 满足）：两根落库后据此广播就绪态
type WizardStore interface {
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

// HomeVerify 只读预检工作目录：判路径安全、判存在、判可写，**不创建目录也不写探测文件**；
// 仅返回逐条预检行与错误行，不持久化、不发事件（供向导「验证」按钮实时反馈）。
func (s *WizardService) HomeVerify(ctx context.Context, home, www string) (model.HomeVerifyResult, error) {
	h, w, verr := validateHomeWww(home, www)
	if verr != "" {
		return model.HomeVerifyResult{Errors: []string{verr}}, nil
	}
	lines, fsErrs := probeTree(h, w)
	return model.HomeVerifyResult{OK: len(fsErrs) == 0, Lines: lines, Errors: fsErrs}, nil
}

// HomeEnsure 装机确认：三段式任务创建工作目录子树并把两根目录落地 config.yaml，随后广播 state:changed（硬红线 5）
// 先决检测：两根目录已在 config.yaml 持久化且实际存在 → 视为「已设置」，广播权威快照以即时更新 UI，
// 跳过目录子树创建，禁止重复创建（幂等）。
func (s *WizardService) HomeEnsure(ctx context.Context, home, www string) error {
	h, w, verr := validateHomeWww(home, www)
	if verr != "" {
		return fmt.Errorf("%s", verr)
	}
	if s.alreadyReady(h, w) {
		return s.emit() // 已设置：仅广播权威快照令前端即时更新就绪态，不重复建树、不重复落库
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

// ensureTree 逐级创建 PHPO_HOME 子树与 WWW_ROOT 并试写探测可写；返回逐条成功行与错误行（幂等，目录已存在即空操作）。
// **只允许 HomeEnsure（用户点「确认并创建」）调用**：验证阶段不得落盘。
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

// probeTree 只读预检工作目录树：判存在、判可写，**不创建目录、不写探测文件**（创建只在「确认并创建」时发生）。
// 已存在且可写 → ✓；尚不存在但最近已存在祖先可写 → ○（确认后将创建）；其余 → ✗。
// 权限位只是预览启发式（Windows 只读目录回 0555，类 Unix 看属主 0o200）；权威判定在 HomeEnsure 实建时以真实写入为准。
func probeTree(home, www string) (lines, errsList []string) {
	hRoot := config.ExpandHome(home)
	wRoot := config.ExpandHome(www)
	check := func(label, p string) {
		switch st, err := os.Stat(p); {
		case err == nil && st.IsDir():
			if writableByPerm(p) {
				lines = append(lines, fmt.Sprintf("✓ %s %s", label, filepath.ToSlash(p)))
			} else {
				errsList = append(errsList, fmt.Sprintf("✗ 不可写：%s", filepath.ToSlash(p)))
			}
		case err == nil:
			errsList = append(errsList, fmt.Sprintf("✗ 同名文件已存在，不能作为目录：%s", filepath.ToSlash(p)))
		case os.IsNotExist(err):
			if writableByPerm(nearestExistingDir(p)) {
				lines = append(lines, fmt.Sprintf("○ %s %s（确认后将创建）", label, filepath.ToSlash(p)))
			} else {
				errsList = append(errsList, fmt.Sprintf("✗ 上级目录不可写，无法创建：%s", filepath.ToSlash(p)))
			}
		default:
			errsList = append(errsList, fmt.Sprintf("✗ 无法访问 %s：%v", filepath.ToSlash(p), err))
		}
	}
	check("PHPO_HOME", hRoot)
	for _, sd := range config.HomeSubdirs {
		check("子目录", filepath.Join(hRoot, filepath.FromSlash(sd.Path)))
	}
	check("WWW_ROOT", wRoot)
	return lines, errsList
}

// nearestExistingDir 自父级向上找第一个已存在的目录（文件系统根恒存在，故必返回非空）
func nearestExistingDir(p string) string {
	for d := filepath.Dir(p); ; {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return d
		}
		d = parent
	}
}

// writableByPerm 按属主权限位判目录可写；路径不存在或非目录即 false
func writableByPerm(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir() && st.Mode().Perm()&0o200 != 0
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

// persistEnv 把规范化 home/www 写入 config.yaml（ConfigStore 仅存两根，派生路径现算）。
// 写盘即令快照 dirReady 派生为双 true（preflight NEEDS_HOME 随之放行），无需任何落库标记。
func (s *WizardService) persistEnv(home, www string) error {
	return s.cfg.SetRoots(home, www)
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
