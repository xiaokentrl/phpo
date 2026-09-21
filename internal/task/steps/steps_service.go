// 服务安装步骤（T303）：Docker 可用性门禁 → 缓存优先保证镜像就绪（§5.14.3）
// 容器/网络/卷装配属 T304；此处只负责把 kind/version 的镜像落到本地 Docker store
package steps

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"phpo/internal/cache"
	"phpo/internal/config"
	"phpo/internal/engine"
	"phpo/internal/model"
	"phpo/internal/task"
)

// dockerBackend 把 engine.Client 适配到 cache.DockerBackend（层间桥接，置于 task/steps 以避免 engine→cache 反向依赖）
type dockerBackend struct{ cli *engine.Client }

func (d dockerBackend) PullImage(ctx context.Context, ref string) error {
	return d.cli.ImagePull(ctx, ref, nil)
}

func (d dockerBackend) SaveImage(ctx context.Context, ref, dstTar string) error {
	return d.cli.ImageSave(ctx, ref, dstTar)
}

func (d dockerBackend) LoadImage(ctx context.Context, tarPath string) error {
	return d.cli.ImageLoad(ctx, tarPath)
}

// Download 拉取远程文件到 dstPath（扩展包 apk/pecl 用；M6 T601 复用）
func (d dockerBackend) Download(ctx context.Context, url, dstPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败 %s: HTTP %d", url, resp.StatusCode)
	}
	tmp := dstPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dstPath)
}

// NewCacheManager 生产装配：用真实 Docker 客户端构建缓存编排器
func NewCacheManager(env config.Env, em cache.Emitter, cli *engine.Client) *cache.Manager {
	return cache.NewManager(env, em, dockerBackend{cli: cli})
}

// InstallImageStep 保证服务版本镜像就绪（缓存优先，硬红线 7 前置门禁）
type InstallImageStep struct {
	task.BaseStep
	cm      ImageEnsurer
	probe   engine.Probe
	env     config.Env
	kind    string
	version string
}

// ImageEnsurer 抽出 EnsureImage 便于注入 fake Manager 测试。
// 缓存命中/未命中/提升由 cache 层以 cache:* 事件如实上报（§5.6 既有事件名），步骤不再重复播报。
type ImageEnsurer interface {
	EnsureImage(ctx context.Context, kind, version, ref string) error
}

func NewInstallImageStep(stepName string, cm ImageEnsurer, probe engine.Probe, env config.Env, kind, version string) *InstallImageStep {
	return &InstallImageStep{
		BaseStep: task.BaseStep{StepName: stepName},
		cm:       cm,
		probe:    probe,
		env:      env,
		kind:     kind,
		version:  version,
	}
}

// Cancelable pull 过程可取消（取消时 cache 层 defer 清空临时目录）
func (s *InstallImageStep) Cancelable() bool { return true }

func (s *InstallImageStep) Execute(ctx context.Context, log task.StepLog) error {
	// 硬红线 7：Docker 未装/未运行不得启动任何服务
	if h := engine.Check(ctx, s.probe); !h.CanStart {
		msg := h.Message
		if h.Hint != "" {
			msg += " " + h.Hint
		}
		log.Log(string(model.LogErr), msg)
		return fmt.Errorf("%s", msg)
	}

	ref, err := engine.ImageRefFor(s.kind, s.version)
	if err != nil {
		log.Log(string(model.LogErr), err.Error())
		return err
	}
	log.Log(string(model.LogCmd), "确保镜像就绪（缓存优先）: "+ref)

	if err := s.cm.EnsureImage(ctx, s.kind, s.version, ref); err != nil {
		log.Log(string(model.LogErr), "镜像安装失败: "+err.Error())
		return err
	}
	log.Log(string(model.LogOk), "镜像就绪: "+ref)
	return nil
}

// Rollback 镜像安装是加法且缓存/镜像与业务解耦（§5.14.13），回滚不删镜像；容器清理由 T304/T305 负责
func (s *InstallImageStep) Rollback(context.Context) error { return nil }
