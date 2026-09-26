// 服务种类 → Docker 镜像引用映射（官方镜像惯例；版本开放输入，版本串即 tag）
package engine

import (
	"fmt"
	"strings"

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

// MirrorRef 把原始镜像引用改写成「从某个镜像源拉」的形态：
//
//	php:8.4-fpm        + docker.m.daocloud.io → docker.m.daocloud.io/library/php:8.4-fpm
//	phpo/php:8.4       + docker.m.daocloud.io → docker.m.daocloud.io/phpo/php:8.4
//
// 单段仓库名（php / mysql / nginx…）必须补 library/ 这一层——Docker Hub 的官方镜像真身在
// library/ 命名空间下，镜像源转发时同样按这段路径找，不补就是 404。host 为空即原样返回（直连官方）。
// 引用本身不带 tag 时补 latest，与 docker pull 的默认一致。
func MirrorRef(host, ref string) string {
	if host == "" {
		return ref
	}
	name, tag := splitRef(ref)
	if !strings.Contains(name, "/") {
		name = "library/" + name
	}
	return host + "/" + name + ":" + tag
}

// splitRef 拆出「仓库名」与「tag」。注意 host:5000/img:tag 这种形态里第一个冒号是端口，
// 所以只在最后一个斜杠之后找冒号。无 tag 时回落 latest。
func splitRef(ref string) (name, tag string) {
	slash := strings.LastIndex(ref, "/")
	colon := strings.LastIndex(ref, ":")
	if colon <= slash {
		return ref, "latest"
	}
	return ref[:colon], ref[colon+1:]
}
