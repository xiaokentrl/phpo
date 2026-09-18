// 服务种类 → Docker 镜像引用映射（官方镜像惯例；版本开放输入，版本串即 tag）
package engine

import (
	"fmt"

	"phpo/internal/model"
)

// 官方 Docker Hub 仓库名。pgsql 服务对应官方 postgres 镜像。
var imageRepos = map[model.ServiceKind]string{
	model.KindPHP:   "php",
	model.KindMySQL: "mysql",
	model.KindPgsql: "postgres",
	model.KindRedis: "redis",
	model.KindNginx: "nginx",
}

// ImageTagFor 计算某服务版本的镜像 tag：php 追加 -fpm 后缀，其余原样
func ImageTagFor(kind, version string) string {
	if model.ServiceKind(kind) == model.KindPHP {
		return version + "-fpm"
	}
	return version
}

// ImageRefFor 拼接完整镜像引用，如 php/8.4 → php:8.4-fpm，nginx/alpine → nginx:alpine
func ImageRefFor(kind, version string) (string, error) {
	repo, ok := imageRepos[model.ServiceKind(kind)]
	if !ok {
		return "", fmt.Errorf("未知服务种类: %s", kind)
	}
	if version == "" {
		return "", fmt.Errorf("版本不能为空")
	}
	return repo + ":" + ImageTagFor(kind, version), nil
}

// CommittedPHPRef 扩展经内置工具编译安装后，用 docker commit 固化出的 phpo 专用镜像引用（phpo/php:{version}）。
// 与官方基座 php:{version}-fpm 区分：启用扩展的 php 版本运行此镜像，重装同配置时零网络加载。
func CommittedPHPRef(version string) string { return "phpo/php:" + version }
