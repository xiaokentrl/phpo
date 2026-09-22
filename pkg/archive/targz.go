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

// Skip 一个未能进入归档的条目。Reason 是人话原因，由调用方逐条报给用户——
// 归档缺了什么必须看得见，不得静默。
type Skip struct {
	Path   string
	Reason string
}

// Create 把 sources 逐个打入 dst 指向的 tar.gz；返回实际贡献条目的顶层前缀（去重、保序）与被跳过的条目。
// dst 的父目录若不存在会自动创建。
//
// 单个条目读不动（权限不足、打包期间消失）不判死整包：宿主数据目录里 mysql 的 ibdata1、pgsql 的整个
// data/ 常由容器内 uid 拥有且 0700/0600，冷拷贝本就拿不到，这部分内容由调用方的逻辑导出兜住。
// 但写入 tar 中途失败必须上抛：header 已声明的 Size 无法回收，硬跳过会产出解包即坏的归档。
func Create(dst string, sources []Source) ([]string, []Skip, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, nil, fmt.Errorf("创建归档目录失败: %w", err)
	}
	f, err := os.Create(dst)
	if err != nil {
		return nil, nil, fmt.Errorf("创建归档文件失败: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	seen := map[string]bool{}
	var tops []string
	var skipped []Skip
	for _, s := range sources {
		prefix := strings.Trim(s.ArcPrefix, "/")
		if prefix == "" {
			return nil, skipped, errors.New("归档前缀不能为空")
		}
		info, err := os.Lstat(s.HostPath)
		if err != nil {
			continue // 源不存在：跳过，不算错误（异机/未装服务场景）
		}
		added := false
		if !info.IsDir() {
			if err := appendFile(tw, prefix, s.HostPath, info, &added, &skipped); err != nil {
				return nil, skipped, err
			}
		} else {
			if err := appendDir(tw, prefix, s.HostPath, &added, &skipped); err != nil {
				return nil, skipped, err
			}
		}
		if added && !seen[prefix] {
			seen[prefix] = true
			tops = append(tops, prefix)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, skipped, fmt.Errorf("收尾 tar 失败: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, skipped, fmt.Errorf("收尾 gzip 失败: %w", err)
	}
	return tops, skipped, nil
}

// appendDir 递归把 dir 内容以 arcBase 前缀写入 tar；found 标记是否至少写入一个文件
func appendDir(tw *tar.Writer, arcBase, dir string, found *bool, skipped *[]Skip) error {
	return filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			*skipped = append(*skipped, Skip{p, whySkipped(err)})
			if info != nil && info.IsDir() {
				return filepath.SkipDir // 进不去的目录只跳过它自己，不牵连整包
			}
			return nil
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
			return appendEntry(tw, name, p, info, found, skipped)
		default:
			return nil // 跳过符号链接/设备/管道：跨平台不确定且非备份目标
		}
	})
}

// appendFile 把单个宿主文件以 arcName 写入 tar
func appendFile(tw *tar.Writer, arcName, hostPath string, info os.FileInfo, found *bool, skipped *[]Skip) error {
	return appendEntry(tw, arcName, hostPath, info, found, skipped)
}

// appendEntry 写入一个常规文件：先 open、成功才落 header——反过来会在 tar 流里留下声明了字节的空洞，整包报废
func appendEntry(tw *tar.Writer, arcName, hostPath string, info os.FileInfo, found *bool, skipped *[]Skip) error {
	in, err := os.Open(hostPath)
	if err != nil {
		*skipped = append(*skipped, Skip{hostPath, whySkipped(err)})
		return nil
	}
	defer in.Close()
	if err := writeHeader(tw, arcName, info); err != nil {
		return err
	}
	if _, err := io.Copy(tw, in); err != nil {
		return fmt.Errorf("读取 %s 失败: %w", hostPath, err)
	}
	*found = true
	return nil
}

// whySkipped 把 skip 的技术错误翻成人话原因
func whySkipped(err error) string {
	switch {
	case errors.Is(err, os.ErrPermission):
		return "权限不足（属主多为容器内 uid）"
	case errors.Is(err, os.ErrNotExist):
		return "打包期间已消失"
	default:
		return err.Error()
	}
}

func writeHeader(tw *tar.Writer, name string, info os.FileInfo) error {
	return tw.WriteHeader(&tar.Header{
		Name:    name,
		Mode:    int64(info.Mode().Perm()),
		Size:    info.Size(),
		ModTime: info.ModTime(),
	})
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
