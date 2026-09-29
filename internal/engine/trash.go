// 回收站文件操作（§5.13.7）：把站点根目录移入 `<用户数据目录>/trash` 并可恢复；到期清理由 store 记录的 ExpiresAt 驱动
package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"phpo/internal/util"
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
	if err := util.MkdirAll(t.Root); err != nil {
		return "", fmt.Errorf("创建回收站失败: %w", err)
	}
	if _, err := os.Stat(origPath); err != nil {
		if os.IsNotExist(err) {
			// 已被回收：幂等，给出当初那个预期落点（不加后缀——没搬过就不该多出一个时间戳名）
			return filepath.Join(t.Root, filepath.Base(origPath)), nil
		}
		return "", err
	}
	dest := t.destFor(origPath)
	if err := os.Rename(origPath, dest); err != nil {
		return "", fmt.Errorf("移入回收站失败 %s → %s: %w", origPath, dest, err)
	}
	return dest, nil
}

// destFor 给出这份东西在回收站里该落在哪：同名就追加纳秒后缀。
//
// 单独抽出来是因为「提权挪」那一条路（MoveElevated）必须在**发起授权之前**就把目标定好——
// `mv -n` 撞名时静默退出码 0 却不搬，用它就等于「以为收走了，其实还在原处」。
func (t *Trash) destFor(origPath string) string {
	dest := filepath.Join(t.Root, filepath.Base(origPath))
	if _, err := os.Stat(dest); err == nil {
		dest = fmt.Sprintf("%s.%d", dest, time.Now().UnixNano())
	}
	return dest
}

// MoveElevated 把 origPath 用提权方式挪进回收站，返回落地路径。
//
// 为什么要它：Docker 卷的数据目录属主是容器内那个 uid（rootful 守护进程下即 root），
// 宿主用户自己的 `os.Rename` 对它无效——而这类东西按需求必须先进回收站留 7 天，不能直接删。
// 走的是 `pkexec mv -- 源 目标`，argv 传入、不经 shell。
//
// 两条要交代给用户的代价（调用方负责写进日志）：
//   - 挪进去的那份内容属主仍是 root，phpo 自己既读不动也删不动，
//     因此「从回收站恢复」与「到期清理」这两步同样得再授权一次。
//   - 跨文件系统时 mv 是复制+删除，大卷会等上一阵子。
func (t *Trash) MoveElevated(ctx context.Context, origPath string) (string, error) {
	if t.Root == "" {
		return "", fmt.Errorf("回收站根目录未配置")
	}
	if err := util.MkdirAll(t.Root); err != nil {
		return "", fmt.Errorf("创建回收站失败: %w", err)
	}
	if _, err := os.Stat(origPath); err != nil {
		if os.IsNotExist(err) {
			// 已经不在了：给既有/预期目标，与 Move 同口径（幂等）
			return filepath.Join(t.Root, filepath.Base(origPath)), nil
		}
		return "", err
	}
	dest := t.destFor(origPath)
	if _, err := runDeleteCmd(ctx, true, "mv", "--", origPath, dest); err != nil {
		return "", fmt.Errorf("提权移入回收站失败 %s → %s: %w", origPath, dest, err)
	}
	return dest, nil
}

// Restore 把回收站条目移回原始位置（幂等：目标已存在则视为已恢复）
func (t *Trash) Restore(trashPath, origPath string) error {
	if _, err := os.Stat(origPath); err == nil {
		return nil // 已在原位
	}
	if err := util.MkdirAll(filepath.Dir(origPath)); err != nil {
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
