// tar.gz 归档纯函数（T602 备份/恢复底座）：多宿主路径 → 单一 tar.gz；反向解包与顶层条目列举。
// 硬红线 3：解包拒绝绝对路径与 `..` 穿越，所有落点必须在目标目录内。
// 不含任何业务语义：备份服务决定打包哪些路径、归档命名，本包只做字节级打包/解包。
package archive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Source 一个待打包条目：ArcPrefix 归档内相对前缀（如 "home"/"www"/"offline"/"db"），HostPath 宿主绝对路径（目录或文件）。
// HostPath 不存在则跳过（不计入 items），空目录也跳过。
type Source struct {
	ArcPrefix string
	HostPath  string
}

// Create 把 sources 逐个打入 dst 指向的 tar.gz；返回实际贡献条目的顶层前缀（去重、保序）。
// dst 的父目录若不存在会自动创建。
func Create(dst string, sources []Source) ([]string, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, fmt.Errorf("创建归档目录失败: %w", err)
	}
	f, err := os.Create(dst)
	if err != nil {
		return nil, fmt.Errorf("创建归档文件失败: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	seen := map[string]bool{}
	var tops []string
	for _, s := range sources {
		prefix := strings.Trim(s.ArcPrefix, "/")
		if prefix == "" {
			return nil, errors.New("归档前缀不能为空")
		}
		info, err := os.Lstat(s.HostPath)
		if err != nil {
			continue // 源不存在：跳过，不算错误（异机/未装服务场景）
		}
		added := false
		if !info.IsDir() {
			if err := appendFile(tw, prefix, s.HostPath, info); err != nil {
				return nil, err
			}
			added = true
		} else {
			if err := appendDir(tw, prefix, s.HostPath, &added); err != nil {
				return nil, err
			}
		}
		if added && !seen[prefix] {
			seen[prefix] = true
			tops = append(tops, prefix)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("收尾 tar 失败: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("收尾 gzip 失败: %w", err)
	}
	return tops, nil
}

// appendDir 递归把 dir 内容以 arcBase 前缀写入 tar；found 标记是否至少写入一个文件
func appendDir(tw *tar.Writer, arcBase, dir string, found *bool) error {
	return filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := path.Join(arcBase, filepath.ToSlash(rel))
		switch {
		case info.IsDir():
			return nil // 目录由文件路径隐式表达，不单独写 dirHeader（解包时按需 MkdirAll）
		case info.Mode().IsRegular():
			if err := writeHeader(tw, name, info); err != nil {
				return err
			}
			return copyFileBody(tw, p, found)
		default:
			return nil // 跳过符号链接/设备/管道：跨平台不确定且非备份目标
		}
	})
}

// appendFile 把单个宿主文件以 arcName 写入 tar
func appendFile(tw *tar.Writer, arcName, hostPath string, info os.FileInfo) error {
	if err := writeHeader(tw, arcName, info); err != nil {
		return err
	}
	var found bool
	if err := copyFileBody(tw, hostPath, &found); err != nil {
		return err
	}
	return nil
}

func writeHeader(tw *tar.Writer, name string, info os.FileInfo) error {
	return tw.WriteHeader(&tar.Header{
		Name:    name,
		Mode:    int64(info.Mode().Perm()),
		Size:    info.Size(),
		ModTime: info.ModTime(),
	})
}

func copyFileBody(tw *tar.Writer, hostPath string, found *bool) error {
	in, err := os.Open(hostPath)
	if err != nil {
		return err
	}
	defer in.Close()
	if _, err := io.Copy(tw, in); err != nil {
		return err
	}
	*found = true
	return nil
}

// Extract 把 tar.gz 解到 dstDir 内；拒绝穿越 dstDir 的条目名（硬红线 3）。返回写入的文件数。
func Extract(src, dstDir string) (int, error) {
	f, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("打开归档失败: %w", err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return 0, fmt.Errorf("解压 gzip 失败: %w", err)
	}
	defer gr.Close()

	cleanDst := filepath.Clean(dstDir)
	tr := tar.NewReader(gr)
	count := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return count, fmt.Errorf("读取 tar 头失败: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		target, err := safeJoin(cleanDst, hdr.Name)
		if err != nil {
			return count, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return count, err
		}
		mode := os.FileMode(hdr.Mode).Perm()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			return count, err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return count, err
		}
		out.Close()
		count++
	}
	return count, nil
}

// TopLevel 列举归档内的顶层前缀（首段路径），去重保序，供 items 计数。只读头不解压内容。
func TopLevel(src string) ([]string, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	seen := map[string]bool{}
	var tops []string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		first := strings.SplitN(strings.Trim(hdr.Name, "/"), "/", 2)[0]
		if first != "" && !seen[first] {
			seen[first] = true
			tops = append(tops, first)
		}
	}
	return tops, nil
}

// safeJoin 把归档内条目名安全拼到 dstDir 之下；绝对路径或越界（..）一律拒绝。
func safeJoin(dstDir, name string) (string, error) {
	if strings.HasPrefix(name, "/") || filepath.IsAbs(name) {
		return "", fmt.Errorf("拒绝绝对路径条目: %s", name)
	}
	cleaned := filepath.Clean(filepath.Join(dstDir, name))
	if cleaned != dstDir && !strings.HasPrefix(cleaned, dstDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("拒绝路径穿越条目: %s", name)
	}
	return cleaned, nil
}
