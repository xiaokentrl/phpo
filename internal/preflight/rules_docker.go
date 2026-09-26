// docker 组规则（1）：docker-source-set —— 配置 Docker 镜像源清单（一行一个主机名）
// 唯一限制是地址合形（只认「主机[:端口]」）；可达性不在此裁决——选了连不上的源，拉取时逐项点名后回落原始镜像名
package preflight

import (
	"phpo/internal/config"
)

func (r *run) dockerSourceSet() {
	// 与写路径共用同一判据（config.ValidateRegistryHosts：忽略空行、逐行合形、去重），preflight 不另立标准。
	// 这里只裁决「地址合不合形」；可达性不在此判——选了连不上的源，拉取时逐项点名后回落原始镜像名。
	hosts, err := config.ValidateRegistryHosts(r.c.Sources)
	if err != nil {
		r.errf("%s", err.Error())
		return
	}
	// 空清单合法：语义即「不改写镜像名、直连官方源」，与「清除自定义根」同口径
	if len(hosts) == 0 {
		r.warnf("未填写镜像源，拉取将直连官方（docker.io）")
	}
}
