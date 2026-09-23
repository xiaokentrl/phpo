// 服务配置保存步骤（T505）：先备份全部原文件，再逐个写入；任一步失败即回滚已写文件（§5.13.3 原子性）
// 备份与写入分两趟：备份趟任一读失败（非缺失）在写任何文件前中止，避免半写状态
package steps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"phpo/internal/model"
	"phpo/internal/task"
	"phpo/internal/util"
)

// ConfigFile 一个待写入配置：Host 为宿主绝对路径，Content 为新内容
type ConfigFile struct {
	Host    string
	Content string
}

// SaveConfigStep 多文件原子写入：备份 → 写入 → 失败逆序回滚
type SaveConfigStep struct {
	task.BaseStep
	files []ConfigFile
	stash map[string]*string // Host → 原文（nil 表示原本不存在）
	wrote []string           // 已尝试写入的 Host，按序，供逆序回滚
}

func NewSaveConfigStep(stepName string, files []ConfigFile) *SaveConfigStep {
	return &SaveConfigStep{
		BaseStep: task.BaseStep{StepName: stepName},
		files:    files,
	}
}

func (s *SaveConfigStep) Execute(ctx context.Context, log task.StepLog) error {
	// 备份趟：全部读入内存；非缺失类读错误即刻中止（此时尚未写任何文件）
	s.stash = make(map[string]*string, len(s.files))
	for _, f := range s.files {
		if err := ctx.Err(); err != nil {
			return err
		}
		b, err := os.ReadFile(f.Host)
		switch {
		case err == nil:
			c := string(b)
			s.stash[f.Host] = &c
		case errors.Is(err, os.ErrNotExist):
			s.stash[f.Host] = nil
		default:
			return fmt.Errorf("备份原配置失败 %s: %w", f.Host, err)
		}
	}

	// 写入趟：先记 Host 再写，失败即返回错误交引擎回滚
	for _, f := range s.files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := util.MkdirAll(filepath.Dir(f.Host)); err != nil {
			return fmt.Errorf("创建配置目录失败 %s: %w", filepath.Dir(f.Host), err)
		}
		s.wrote = append(s.wrote, f.Host)
		if err := util.WriteFile(f.Host, []byte(f.Content)); err != nil {
			return fmt.Errorf("写入配置失败 %s: %w", f.Host, err)
		}
		log.Log(string(model.LogOk), "已写入 "+f.Host)
	}
	return nil
}

// Rollback 逆序恢复：原本存在则写回原文，原本不存在则删除
func (s *SaveConfigStep) Rollback(context.Context) error {
	var errs []error
	for i := len(s.wrote) - 1; i >= 0; i-- {
		host := s.wrote[i]
		orig := s.stash[host]
		if orig == nil {
			if err := os.Remove(host); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
			continue
		}
		if err := util.WriteFile(host, []byte(*orig)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
