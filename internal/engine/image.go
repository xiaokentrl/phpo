// 镜像 load / save / pull / remove 封装：基于 Docker SDK，进度流式上报
package engine

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/docker/docker/api/types/image"

	"phpo/internal/util"
)

// ImageLoad 从 tar 路径加载镜像（零网络）；resp.Body 必须读完再关，否则加载不完成
func (c *Client) ImageLoad(ctx context.Context, tarPath string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("打开镜像 tar 失败: %w", err)
	}
	defer f.Close()

	resp, err := c.cli.ImageLoad(ctx, f)
	if err != nil {
		return fmt.Errorf("docker load 失败: %w", err)
	}
	defer resp.Body.Close()
	// 读取到 EOF 触发实际导入
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("docker load 读取失败: %w", err)
	}
	return nil
}

// ImageSave 将镜像导出为 tar 到 outPath（先写临时再原子重命名，避免半截文件）
func (c *Client) ImageSave(ctx context.Context, ref, outPath string) error {
	rc, err := c.cli.ImageSave(ctx, []string{ref})
	if err != nil {
		return fmt.Errorf("docker save 失败: %w", err)
	}
	defer rc.Close()

	tmp := outPath + ".tmp"
	f, err := util.Create(tmp)
	if err != nil {
		return fmt.Errorf("创建导出文件失败: %w", err)
	}
	if _, err := io.Copy(f, rc); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("docker save 写入失败: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, outPath)
}

// ImagePull 拉取镜像；onProgress 每收到一行 JSON 进度回调一次（nil 则忽略）
func (c *Client) ImagePull(ctx context.Context, ref string, onProgress func(line string)) error {
	rc, err := c.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("docker pull 失败: %w", err)
	}
	defer rc.Close()

	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if onProgress != nil {
			onProgress(sc.Text())
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("读取 pull 进度失败: %w", err)
	}
	return nil
}

// ImageTag 给本机已有的镜像引用再打一个名字（镜像源拉回来的带源前缀名字，换回原始引用）
func (c *Client) ImageTag(ctx context.Context, src, dst string) error {
	if err := c.cli.ImageTag(ctx, src, dst); err != nil {
		return fmt.Errorf("docker tag 失败: %w", err)
	}
	return nil
}

// ImageRemove 删除镜像（force）；幂等：不存在不报错
func (c *Client) ImageRemove(ctx context.Context, ref string) error {
	if _, err := c.cli.ImageRemove(ctx, ref, image.RemoveOptions{Force: true}); err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("docker rmi 失败: %w", err)
	}
	return nil
}

// isNotFound 判断 SDK 错误是否为「资源不存在」
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "no such image") || strings.Contains(msg, "no such")
}
