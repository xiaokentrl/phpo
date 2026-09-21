// 回收站文件操作（§5.13.7）：把站点根目录移入 `<用户数据目录>/trash` 并可恢复；到期清理由 store 记录的 ExpiresAt 驱动
package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Trash 绑定一个回收站根目录
type Trash struct {
	Root string
}

// NewTrash 指定回收站根（通常 `<用户数据目录>/trash`）
func NewTrash(root string) *Trash { return &Trash{Root: root} }

// Move 把 origPath 移入回收站，返回落地路径。幂等：origPath 不存在则视为已回收，返回既有/预期目标。
// 目标重名时追加时间戳后缀，避免覆盖。
func (t *Trash) Move(origPath string) (string, error) {
	if t.Root == "" {
		return "", fmt.Errorf("回收站根目录未配置")
	}
	if err := os.MkdirAll(t.Root, 0o755); err != nil {
		return "", fmt.Errorf("创建回收站失败: %w", err)
	}
	dest := filepath.Join(t.Root, filepath.Base(origPath))
	if _, err := os.Stat(origPath); err != nil {
		if os.IsNotExist(err) {
			return dest, nil // 已被回收：幂等
		}
		return "", err
	}
	if _, err := os.Stat(dest); err == nil {
		dest = fmt.Sprintf("%s.%d", dest, time.Now().UnixNano())
	}
	if err := os.Rename(origPath, dest); err != nil {
		return "", fmt.Errorf("移入回收站失败 %s → %s: %w", origPath, dest, err)
	}
	return dest, nil
}

// Restore 把回收站条目移回原始位置（幂等：目标已存在则视为已恢复）
func (t *Trash) Restore(trashPath, origPath string) error {
	if _, err := os.Stat(origPath); err == nil {
		return nil // 已在原位
	}
	if err := os.MkdirAll(filepath.Dir(origPath), 0o755); err != nil {
		return err
	}
	if err := os.Rename(trashPath, origPath); err != nil {
		return fmt.Errorf("从回收站恢复失败 %s → %s: %w", trashPath, origPath, err)
	}
	return nil
}

// Purge 永久删除回收站内的路径（到期清理用）
func (t *Trash) Purge(trashPath string) error {
	if err := os.RemoveAll(trashPath); err != nil {
		return fmt.Errorf("清空回收站条目失败 %s: %w", trashPath, err)
	}
	return nil
}
