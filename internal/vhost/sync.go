// vhost 文件同步：把正文原子写入 {NGINX_SITES_ROOT}/{domain}.conf；删除时移除文件
// 硬红线 2：落盘前/后由注入的 Validator 跑 nginx -t，失败则不落地（内容校验）或回滚原内容（磁盘校验）
package vhost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"phpo/internal/util"
)

// Save 落盘站点 conf（权限归一到 util.FilePerm），并按校验器类型执行硬红线 2：
//   - FileValidator：需 conf 在磁盘才能核验 → 先写盘再校验，失败回滚为原内容或删除新文件
//   - 其余 Validator：内容级校验先过再写盘；失败不触碰磁盘
func (m *Manager) Save(ctx context.Context, v Validator, domain, content string) error {
	path := m.Path(domain)
	if fv, ok := v.(FileValidator); ok {
		prev, had := readFile(path)
		if err := util.AtomicWrite(path, []byte(content)); err != nil {
			return fmt.Errorf("落盘 vhost 失败: %w", err)
		}
		if err := fv.ValidateFile(ctx, path); err != nil {
			restoreFile(path, prev, had) // 校验未过：复原写盘前状态
			return fmt.Errorf("nginx 配置校验未通过，已回滚 %s: %w", domain, err)
		}
		return nil
	}
	if v != nil {
		if err := v.Validate(ctx, domain, content); err != nil {
			return fmt.Errorf("nginx 配置校验未通过，未写入 %s: %w", domain, err)
		}
	}
	if err := util.AtomicWrite(path, []byte(content)); err != nil {
		return fmt.Errorf("落盘 vhost 失败: %w", err)
	}
	return nil
}

// DeleteFile 移除站点 conf 文件（不存在视为成功）
func (m *Manager) DeleteFile(domain string) error {
	if err := os.Remove(m.Path(domain)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Path 返回某域名的 vhost 文件路径
func (m *Manager) Path(domain string) string {
	return filepath.Join(m.env.NginxSitesRoot, domain+".conf")
}

// readFile 读取路径内容并报告是否存在（读错误按不存在处理，交由写覆盖）
func readFile(path string) ([]byte, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return b, true
}

// restoreFile 回滚：原先存在则写回旧内容，原先不存在则删除
func restoreFile(path string, prev []byte, had bool) {
	if had {
		_ = util.AtomicWrite(path, prev)
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
}
