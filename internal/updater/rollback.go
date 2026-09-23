// 升级回滚（§5.9 / T604）：安装前写 pending 标记，下次启动自检决定「确认成功」或「回滚旧版」
// 语义：标记存在且运行版本==目标版本 → 新构建已接管 → 清标记；标记存在但运行版本仍是旧版 → 上次装到一半被打断 → 恢复备份并清标记
package updater

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"phpo/internal/util"
)

// PendingUpdate 一次进行中的升级标记；随恢复语义使用
type PendingUpdate struct {
	Target string `json:"target"` // 目标版本
	Backup string `json:"backup"` // 旧二进制/安装物备份路径（可为空表示无需恢复文件）
}

// Rollback 以 updates 目录下的 marker 文件承载待确认状态
type Rollback struct {
	dir string
}

func NewRollback(dir string) *Rollback { return &Rollback{dir: dir} }

func (r *Rollback) markerPath() string { return filepath.Join(r.dir, "pending-update.json") }

// Begin 写/覆盖 pending 标记（安装前调用；幂等）
func (r *Rollback) Begin(p PendingUpdate) error {
	if err := util.MkdirAll(r.dir); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return util.WriteFile(r.markerPath(), b)
}

// Load 读取当前标记；无标记返回 (nil,false,nil)
func (r *Rollback) Load() (*PendingUpdate, bool, error) {
	b, err := os.ReadFile(r.markerPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var p PendingUpdate
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, false, err
	}
	return &p, true, nil
}

// Complete 清除 pending 标记；不存在视为已完成
func (r *Rollback) Complete() error {
	if err := os.Remove(r.markerPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// RestoreFunc 由平台侧提供的恢复动作：用备份路径覆盖当前安装物；backup 为空时可返回 nil（无需文件级恢复）
type RestoreFunc func(backupPath string) error

// RecoverOnStartup 启动自检：
//   - 无标记 → 正常，(false,nil)
//   - Target==current → 新构建已接管 → 清标记（确认成功），(false,nil)
//   - Target!=current → 上次升级被中断 → restore 备份 + 清标记（已回滚），(true,err)
func (r *Rollback) RecoverOnStartup(current string, restore RestoreFunc) (bool, error) {
	p, ok, err := r.Load()
	if err != nil || !ok {
		return false, err
	}
	if p.Target == current {
		return false, r.Complete() // 新版本已成功运行：确认并清标记
	}
	if restore != nil {
		if rerr := restore(p.Backup); rerr != nil {
			return true, rerr // 回滚动作失败：保留标记，下次启动继续尝试
		}
	}
	return true, r.Complete()
}
