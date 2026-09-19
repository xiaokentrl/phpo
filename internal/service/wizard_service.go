// T607 · 装机向导服务：把原型 openHomeSetupWizard 的「验证 / 确认」两步真化到后端。
// HomeVerify 纯探测（校验路径安全 + 逐级创建 PHPO_HOME 子树与 WWW_ROOT + 可写测试），不落库、不发事件；
// HomeEnsure 走三段式任务：创建工作目录子树 → Apply 把派生路径写入 env 表并置 dirReady[PHPO_HOME]=true → 广播 state:changed。
// 遵循硬红线 3（.. 路径穿越拒绝）、硬红线 4（前端只读快照，不本地乐观更新）、硬红线 5（写操作三段式）。
// 幂等：重复 ensure 只是重复 MkdirAll（目录已存在即空操作）与覆盖写 env/dirReady。
package service

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"

	"phpo/internal/config"
	"phpo/internal/model"
	"phpo/internal/task"
	"phpo/pkg/errs"
)

// WizardStore 装机落库子集（*store.Store 满足）：派生路径写 env + 就绪标记 + 权威快照回流
type WizardStore interface {
	SetEnv(key, value string) error
	SetDirReady(key string, ready bool) error
	BuildSnapshot() (*model.Snapshot, error)
}

// WizardService 装机向导门面
type WizardService struct {
	store WizardStore
	em    Emitter
	tasks *task.Manager
	seq   atomic.Uint64
}

func NewWizardService(st WizardStore, em Emitter, tm *task.Manager) *WizardService {
	return &WizardService{store: st, em: em, tasks: tm}
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
func (s *WizardService) HomeEnsure(ctx context.Context, home, www string) error {
	h, w, verr := validateHomeWww(home, www)
	if verr != "" {
		return fmt.Errorf("%s", verr)
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

// persistEnv 把规范化 home/www 派生的全部路径键写入 env 表，并置 dirReady[PHPO_HOME]=true（供 Snapshot 回流）
func (s *WizardService) persistEnv(home, www string) error {
	e := config.DerivePaths(home, www)
	keys := [][2]string{
		{"PHPO_HOME", e.PHPOHome}, {"WWW_ROOT", e.WWWRoot}, {"PHP_ROOT", e.PHPRoot},
		{"NGINX_ROOT", e.NginxRoot}, {"NGINX_SITES_ROOT", e.NginxSitesRoot},
		{"MYSQL_ROOT", e.MysqlRoot}, {"PGSQL_ROOT", e.PgsqlRoot}, {"REDIS_ROOT", e.RedisRoot},
		{"BACKUP_ROOT", e.BackupRoot}, {"OFFLINE_ROOT", e.OfflineRoot},
	}
	for _, kv := range keys {
		if err := s.store.SetEnv(kv[0], kv[1]); err != nil {
			return err
		}
	}
	return s.store.SetDirReady("PHPO_HOME", true)
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
