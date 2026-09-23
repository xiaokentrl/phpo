// 宿主 ↔ 容器文件搬运（§5.14.3 扩展包离线化）：PHP 容器的挂载表里没有 ext/ 这一档（§4.2 挂载只覆盖
// 源码/conf/logs），唯一字节通道是 Docker archive API——容器暂存目录里的包文件取回宿主临时目录，
// 缓存里已有的包回填进容器暂存目录，之后即零网络复用。
package engine

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"

	"phpo/internal/util"
)

// CopyTo 把宿主文件打进内存 tar 落到容器 dstDir。tar 内只留 basename：既免掉 dstDir 前缀二义，
// 也让宿主路径不可能穿越进容器（扩展包名已过 ValidateExt，此处仍按不可信输入处置）。
// 包文件量级为 KB～MB，整读进内存可比「跨副本根 rename」省掉一整层临时文件管理。
func (c *Client) CopyTo(ctx context.Context, name, dstDir string, hostFiles ...string) error {
	if len(hostFiles) == 0 {
		return nil
	}
	id, err := c.containerID(ctx, name)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, f := range hostFiles {
		b, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("读取待拷贝文件失败: %w", err)
		}
		hdr := &tar.Header{Name: path.Base(f), Mode: 0o644, Size: int64(len(b)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(b); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	// CopyToContainer 要求目标目录已存在，故先在容器内建好
	if err := c.ExecStream(ctx, name, []string{"mkdir", "-p", dstDir}, io.Discard, io.Discard); err != nil {
		return fmt.Errorf("容器内建暂存目录失败: %w", err)
	}
	if err := c.cli.CopyToContainer(ctx, id, dstDir, &buf, container.CopyToContainerOptions{}); err != nil {
		return fmt.Errorf("拷贝进容器失败: %w", err)
	}
	return nil
}

// CopyFrom 把容器 srcDir 内的常规文件取回宿主 dstDir，返回落地的文件名（升序）。
// srcDir 不存在视作「无产物」返回空表：apk 依赖早已在镜像内时 apk add 不再下载，这是正常路径，
// 不该判死本次扩展集；其余错误一律上抛（把一次正常在机的目录说成空，比不报更糟）。
func (c *Client) CopyFrom(ctx context.Context, name, srcDir, dstDir string) ([]string, error) {
	id, err := c.containerID(ctx, name)
	if err != nil {
		return nil, err
	}
	rc, _, err := c.cli.CopyFromContainer(ctx, id, srcDir)
	if err != nil {
		if isNotFound(err) || strings.Contains(err.Error(), "Could not find the file") {
			return nil, nil
		}
		return nil, fmt.Errorf("从容器取回文件失败: %w", err)
	}
	defer rc.Close()
	if err := util.MkdirAll(dstDir); err != nil {
		return nil, err
	}
	var out []string
	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取容器 tar 失败: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue // 目录/符号链接不进缓存
		}
		base := path.Base(hdr.Name)
		if base == "" || base == "." || base == "/" {
			continue
		}
		if err := writeFileFromReader(filepath.Join(dstDir, base), tr); err != nil {
			return nil, err
		}
		out = append(out, base)
	}
	sort.Strings(out)
	return out, nil
}

func writeFileFromReader(dst string, r io.Reader) error {
	f, err := util.Create(dst)
	if err != nil {
		return fmt.Errorf("写入取回文件失败: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("写入取回文件失败: %w", err)
	}
	return f.Close()
}
