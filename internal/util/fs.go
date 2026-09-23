// 文件系统工具：原子写与权限归一（临时文件 + rename），供 vhost/hosts/配置等落盘复用
package util

import (
	"os"
	"path/filepath"
)

// phpo 产出物的权限（总纲「文件权限策略」）：目录与文件一律 0777。
// 本产品定位是程序员，不替用户限制访问；权限位不靠 MkdirAll/WriteFile 的入参生效——
// 它会被进程 umask 削掉（022 → 0755），因此每次写入后都显式 chmod 归一。
const (
	DirPerm  = 0o777
	FilePerm = 0o777
)

// MkdirAll 逐级创建目录，并把本次新建的各级与叶子目录归一到 DirPerm；
// 已存在但属他人（如自定义数据目录）时 chmod 失败按 best-effort 忽略。
func MkdirAll(path string) error {
	missing := missingDirs(path)
	if err := os.MkdirAll(path, DirPerm); err != nil {
		return err
	}
	for _, d := range missing {
		_ = os.Chmod(d, DirPerm)
	}
	// 叶子总是归一：旧装机留下的 0755 目录要在下一次写入时修好
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		_ = os.Chmod(path, DirPerm)
	}
	return nil
}

// WriteFile 写文件（必要时建父目录）并归一到 FilePerm
func WriteFile(path string, content []byte) error {
	if err := MkdirAll(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.WriteFile(path, content, FilePerm); err != nil {
		return err
	}
	return os.Chmod(path, FilePerm)
}

// Create 建/截断文件并归一到 FilePerm（os.Create 的 0666 会被 umask 削成 0644）；
// 覆盖写已存在的旧文件时也显式 chmod，否则旧权限位原样留着。
func Create(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, FilePerm)
	if err != nil {
		return nil, err
	}
	_ = f.Chmod(FilePerm)
	return f, nil
}

// AtomicWrite 原子写入 path：先写同目录临时文件再 rename；失败清理临时文件
func AtomicWrite(path string, content []byte) error {
	if err := MkdirAll(filepath.Dir(path)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".phpo-tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Chmod(FilePerm); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// missingDirs 自叶子向上收集尚不存在的各级目录（最浅的缺失祖先即 MkdirAll 往下的第一层）
func missingDirs(path string) []string {
	var out []string
	p := filepath.Clean(path)
	for {
		if _, err := os.Stat(p); err == nil {
			return out
		}
		parent := filepath.Dir(p)
		if parent == p {
			return out
		}
		out = append(out, p)
		p = parent
	}
}
