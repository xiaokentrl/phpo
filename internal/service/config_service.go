// ConfigService：服务配置文件读写（T505）
// GetFiles 读宿主既有配置（缺失回落模板默认）；Save 经 task.Manager 三段式原子写入（备份→写→失败回滚，硬红线 5）
// 仅接受模板已知文件名（按名映射回模板权威路径，杜绝客户端传任意路径造成穿越，硬红线 3）
package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"phpo/internal/config"
	"phpo/internal/model"
	"phpo/internal/task"
	"phpo/internal/task/steps"
	"phpo/internal/template"
)

// ConfigFile 前端提交的单个配置改动：仅文件名 + 新内容（路径由后端按模板解析，不接受客户端路径）
type ConfigFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// ConfigService 组合 env（路径解析）与 task.Manager（三段式）
type ConfigService struct {
	env   config.Env
	tasks *task.Manager
	seq   atomic.Uint64
}

func NewConfigService(env config.Env, tm *task.Manager) *ConfigService {
	return &ConfigService{env: env, tasks: tm}
}

// GetFiles 返回该服务版本的配置文件清单：宿主已存在则回显其内容，否则回落模板默认
func (s *ConfigService) GetFiles(kind model.ServiceKind, version string) ([]template.File, error) {
	defs, err := template.FilesFor(string(kind), version)
	if err != nil {
		return nil, err
	}
	out := make([]template.File, len(defs))
	copy(out, defs)
	for i := range out {
		b, err := os.ReadFile(s.hostPath(defs[i].Path))
		if err != nil {
			continue // 缺失或不可读：保留模板默认内容
		}
		out[i].Content = string(b)
	}
	return out, nil
}

// Save 原子保存勾选的配置：白名单校验 → 解析宿主路径 → 经 task.Manager 执行 SaveConfigStep
func (s *ConfigService) Save(ctx context.Context, kind model.ServiceKind, version string, in []ConfigFile) error {
	if len(in) == 0 {
		return nil
	}
	allowed := map[string]string{} // name → 模板权威相对路径
	for _, def := range mustFiles(string(kind), version) {
		allowed[def.Name] = def.Path
	}
	files := make([]steps.ConfigFile, 0, len(in))
	for _, f := range in {
		rel, ok := allowed[f.Name]
		if !ok {
			return fmt.Errorf("未知配置文件名: %s", f.Name)
		}
		files = append(files, steps.ConfigFile{Host: s.hostPath(rel), Content: f.Content})
	}
	t := &task.Task{
		ID:    s.newID("config-save"),
		Label: fmt.Sprintf("保存 %s/%s 配置（%d 个文件）", kind, version, len(files)),
		Meta:  model.TaskMeta{Type: "config-save", Kind: string(kind), Version: version},
		Steps: []task.Step{steps.NewSaveConfigStep("写入配置文件", files)},
	}
	_, err := s.tasks.Run(ctx, t)
	return err
}

// hostPath 把模板相对路径解析为 PHPO_HOME 下宿主绝对路径（与 prepareService 落盘位置一致）
func (s *ConfigService) hostPath(rel string) string {
	return filepath.Join(s.env.PHPOHome, filepath.FromSlash(rel))
}

func (s *ConfigService) newID(op string) string {
	return fmt.Sprintf("%s-%d", op, s.seq.Add(1))
}

// mustFiles 取模板文件名白名单；渲染错误时返回空（Save 将因白名单为空拒绝所有输入）
func mustFiles(kind, version string) []template.File {
	defs, err := template.FilesFor(kind, version)
	if err != nil {
		return nil
	}
	return defs
}
