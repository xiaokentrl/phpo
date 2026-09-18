// 升级包下载器（§5.9 / T604）：流式下载到 dst.part → 进度回调 → 成功原子 rename；取消/失败即清理临时分片
// 硬红线 6：本文件只负责搬运字节，完整性由 verifier 把关（SHA256 + Ed25519 缺一不可）
package updater

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// ProgressFunc 下载进度回调：done/total 均为已下载/总字节数；total<=0 表示长度未知
type ProgressFunc func(done, total int64)

// Downloader 抽象一次下载，便于单测注入替身；onProgress 可为 nil
type Downloader interface {
	Download(ctx context.Context, url, dst string, onProgress ProgressFunc) error
}

// HTTPDownloader 默认真实下载器；Client 为 nil 时使用无总超时的默认客户端（大文件由 ctx 控制取消）
type HTTPDownloader struct {
	Client *http.Client
}

// Download 把 url 内容原子写入 dst：先写 dst.part，校验长度后 rename；任何失败路径都清掉 part
func (d HTTPDownloader) Download(ctx context.Context, url, dst string, onProgress ProgressFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c := d.Client
	if c == nil {
		c = &http.Client{} // 不设总超时：升级包可能较大，取消由 ctx 驱动
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("updater: 下载返回 %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	part := dst + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	copyErr := copyWithProgress(f, resp.Body, resp.ContentLength, ctx, onProgress)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(part) // 失败/取消：清理分片，绝不污染下次（对齐临时目录必清原则）
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	return os.Rename(part, dst)
}

// copyWithProgress 分块拷贝：每块前检查 ctx 取消，累计字节后回调进度
func copyWithProgress(dst io.Writer, src io.Reader, total int64, ctx context.Context, onProgress ProgressFunc) error {
	buf := make([]byte, 64*1024)
	var done int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			if onProgress != nil {
				onProgress(done, total)
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}
