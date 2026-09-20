// collectUsedPorts 直译（原型 1629–1644 行）：mysql/pgsql/redis 服务端口 + 站点端口，可按域名排除
package store

import (
	"strconv"

	"phpo/internal/config"
	"phpo/internal/model"
	"phpo/pkg/port"
)

// CollectServicePorts 数据服务（mysql/pgsql/redis）已占用的宿主端口（先到先得，与占用表同源）
func CollectServicePorts(snap *model.Snapshot) port.Used {
	used := port.Used{}
	for _, kind := range []string{"mysql", "pgsql", "redis"} {
		for _, v := range snap.Installed[kind] {
			raw, ok := snap.Env[config.EnvKeyPort(kind, v)]
			if !ok {
				continue
			}
			p, err := strconv.Atoi(raw)
			if err != nil {
				continue
			}
			if _, exists := used[p]; !exists {
				used[p] = kind + " " + v
			}
		}
	}
	return used
}

// CollectUsedPorts 从快照构造逻辑占用表；先到先得语义与原型一致（服务先、站点后，跳过已占用）
func CollectUsedPorts(snap *model.Snapshot, excludeDomains []string) port.Used {
	used := CollectServicePorts(snap)
	excluded := map[string]bool{}
	for _, d := range excludeDomains {
		excluded[d] = true
	}
	for _, st := range snap.Sites {
		if excluded[st.Domain] {
			continue
		}
		p := st.Port
		if _, exists := used[p]; !exists {
			used[p] = "site " + st.Domain
		}
	}
	return used
}
