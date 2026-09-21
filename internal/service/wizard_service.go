// T607 · 装机向导服务：把原型 openHomeSetupWizard 的「验证 / 确认」两步真化到后端。
// HomeVerify 只读预检（校验路径安全 + 判存在 + 按权限位判可写），**不创建任何目录、不落任何文件、不落库、不发事件**；
// 目录与文件的创建只发生在用户点「确认并创建」→ HomeEnsure。
// HomeEnsure 走三段式任务：创建工作目录子树 → Apply 把两根目录写入 config.yaml（ConfigStore）→ 广播 state:changed。
// 遵循硬红线 3（.. 路径穿越拒绝）、硬红线 4（前端只读快照，不本地乐观更新）、硬红线 5（写操作三段式）。
// 先决检测：HomeEnsure 进来先问 Configured()——两根「已持久化 + 目录已存在」即为已设置，一律跳过建树只广播，禁止重复创建工作目录。
// dirReady 不再落库：由 config.yaml 两根 + 目录存在性在快照内派生（见 store.BuildSnapshot）。
package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"phpo/internal/config"
	"phpo/internal/model"
	"phpo/internal/task"
	"phpo/pkg/errs"
)

// WizardConfig 装机根目录落库子集（*config.ConfigStore 满足）：写两根 + 判两根就绪
type WizardConfig interface {
	SetRoots(home, www string) error
	RootsReady() (home, www bool)
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

// Configured 工作目录是否「已设置」：两根已写入 config.yaml 且两个目录实际存在（与快照 dirReady 同判据，不另立标准）。
// 这是装机向导的先决检测：已设置即禁止任何重复创建。
func (s *WizardService) Configured() bool {
	home, www := s.cfg.RootsReady()
	return home && www
}

// HomeEnsure 装机确认：三段式任务创建工作目录子树并把两根目录落地 config.yaml，随后广播 state:changed（硬红线 5）
// 先决检测：工作目录已设置（两根已持久化且目录已存在）→ 一律跳过创建，只广播权威快照令前端即时归位；
// 即使本次请求的路径与已设目录不同，也不建第二套工作目录、不改写 config.yaml（禁止重复创建）。
func (s *WizardService) HomeEnsure(ctx context.Context, home, www string) error {
	h, w, verr := validateHomeWww(home, www)
	if verr != "" {
		return fmt.Errorf("%s", verr)
	}
	if s.Configured() {
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
					lv := model.LogOk
					if strings.HasPrefix(ln, "⚠") {
						lv = model.LogDim // 告警行降级为次要信息，不计入错误
					}
					log.Log(string(lv), ln)
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
	if w := gitWorkTreeWarning(hRoot); w != "" {
		lines = append(lines, w)
	}
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
	if w := gitWorkTreeWarning(hRoot); w != "" {
		lines = append(lines, w)
	}
	return lines, errsList
}

// gitWorkTreeWarning 工作根位于 git 工作树内时的告警行（自工作根向上找 .git，目录或文件均算——worktree/submodule 的 .git 是文件）；
// 不在工作树内返回空串。**只告警不阻断**（§0.2-16 能警告的不要阻止）：工作目录里的镜像/数据/日志会污染仓库工作区，
// 但把选择权留给用户——有人刻意把 PHPO_HOME 放在项目内（如 画像 D）。返回的行以 ⚠ 起始，供前端按告警着色。
func gitWorkTreeWarning(root string) string {
	abs := config.ExpandHome(root)
	for d := abs; ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return fmt.Sprintf("⚠ %s 位于 git 工作树 %s 内：镜像、数据与日志会污染仓库工作区，建议改用仓库外的目录", filepath.ToSlash(abs), filepath.ToSlash(d))
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
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

// persistEnv 把规范化 home/www 写入 config.yaml（ConfigStore 仅存两根，派生路径现算）。
// 写盘即令快照 dirReady 派生为双 true（preflight NEEDS_HOME 随之放行），无需任何落库标记。
func (s *WizardService) persistEnv(home, www string) error {
	return s.cfg.SetRoots(home, www)
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
