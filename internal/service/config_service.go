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
	"phpo/pkg/dockerutil"
)

// ConfigFile 前端提交的单个配置改动：仅文件名 + 新内容（路径由后端按模板解析，不接受客户端路径）
type ConfigFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// ConfigService 组合 env（路径解析）与 task.Manager（三段式）
type ConfigService struct {
	env     config.Env
	tasks   *task.Manager
	seq     atomic.Uint64
	restart ContainerRestarter // php 容器重启（php.ini / fpm 池仅启动期读取）；nil 视为无法重启
	reload  NginxReloader      // nginx 平滑重载（nginx.conf 变更）；nil 视为无需重载
}

// ContainerRestarter 重启容器并等稳定运行（engine.Client 满足）；测试注入替身
type ContainerRestarter interface {
	RestartContainer(ctx context.Context, name string) error
	ContainerRunning(ctx context.Context, name string) (bool, error)
}

// NginxReloader 重载 nginx（vhost.NewNginxReloader 满足）；测试注入替身
type NginxReloader interface {
	Reload(ctx context.Context) error
}

func NewConfigService(env config.Env, tm *task.Manager, restart ContainerRestarter, reload NginxReloader) *ConfigService {
	return &ConfigService{env: env, tasks: tm, restart: restart, reload: reload}
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
		Steps: append([]task.Step{steps.NewSaveConfigStep("写入配置文件", files)}, s.effectSteps(kind, version)...),
	}
	_, err := s.tasks.Run(ctx, t)
	return err
}

// effectSteps 保存后的「生效步」：谁的脸变了动谁、用最轻的手段（§0.2 规则 16 能警告的不要阻止）。
//   - php：php.ini / fpm 池只在进程启动期读取 → 重启 php 容器（未运行不强行拉起，下次启动自然生效）
//   - nginx：nginx.conf 变更 reload 平滑生效，无需重建容器
//   - mysql/pgsql/redis：数据服务不自动重启（避免打断依赖它的站点），落一行说明
//
// 生效步失败一律不判死整单：文件已落盘，任务回滚会把刚保存的内容退回去，反而把
// 「保存成功但重载失败」变成「保存失败」（§5.16.2 后置环节不撤回先例），落 err 行说明即可。
func (s *ConfigService) effectSteps(kind model.ServiceKind, version string) []task.Step {
	switch kind {
	case model.KindPHP:
		return []task.Step{s.phpRestartStep(version)}
	case model.KindNginx:
		return []task.Step{s.nginxReloadStep()}
	default:
		return []task.Step{&task.FuncStep{
			BaseStep: task.BaseStep{StepName: "配置生效说明"},
			Exec: func(_ context.Context, log task.StepLog) error {
				log.Log(string(model.LogDim), dockerutil.ContainerName(string(kind), version)+
					" 不自动重启：这类配置在容器下次启动时生效（数据服务不打断依赖它的站点）")
				return nil
			},
		}}
	}
}

// phpRestartStep 重启 php 容器使新配置生效；未运行/无法确认状态/重启失败都只落日志不失败
func (s *ConfigService) phpRestartStep(version string) task.Step {
	name := dockerutil.ContainerName(string(model.KindPHP), version)
	return &task.FuncStep{
		BaseStep: task.BaseStep{StepName: "重启 " + name + " 使配置生效"},
		Exec: func(ctx context.Context, log task.StepLog) error {
			if s.restart == nil {
				return nil
			}
			running, err := s.restart.ContainerRunning(ctx, name)
			if err != nil {
				log.Log(string(model.LogDim), "无法确认 "+name+" 状态（"+err.Error()+"），跳过重启；配置将在下次启动生效")
				return nil
			}
			if !running {
				log.Log(string(model.LogDim), name+" 未运行，配置将在下次启动生效")
				return nil
			}
			if err := s.restart.RestartContainer(ctx, name); err != nil {
				log.Log(string(model.LogErr), "重启 "+name+" 失败（配置已保存，重启成功后生效）："+err.Error())
				return nil
			}
			log.Log(string(model.LogOk), name+" 已重启，新配置已生效")
			return nil
		},
	}
}

// nginxReloadStep 重载 nginx 使 nginx.conf 变更生效；失败只落 err 行不判死（理由见 effectSteps）
func (s *ConfigService) nginxReloadStep() task.Step {
	return &task.FuncStep{
		BaseStep: task.BaseStep{StepName: "重载 nginx 使配置生效"},
		Exec: func(ctx context.Context, log task.StepLog) error {
			if s.reload == nil {
				return nil
			}
			if err := s.reload.Reload(ctx); err != nil {
				log.Log(string(model.LogErr), "重载 nginx 失败（配置已保存，重载成功后生效）："+err.Error())
				return nil
			}
			log.Log(string(model.LogOk), "nginx 已重载，新配置已生效")
			return nil
		},
	}
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
