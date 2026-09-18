// preflight 内部复用助手：端口校验（接 store 逻辑占用表）、服务存在性判定
package preflight

import (
	"phpo/internal/model"
	"phpo/internal/store"
	"phpo/pkg/port"
)

// isSvc：SVC_META 五种服务存在性（原型 SVC_META[kind]）
func isSvc(kind string) bool {
	for _, k := range model.AllKinds {
		if string(k) == kind {
			return true
		}
	}
	return false
}

// validatePort 组合逻辑占用表（服务端口 + 站点端口）后走 port.Validate 顺延链
func (r *run) validatePort(raw string, excludePorts []int, excludeDomains []string, autoAdvance bool) port.Result {
	used := store.CollectUsedPorts(r.w.Snap, excludeDomains)
	return port.Validate(raw, used, port.Options{
		Exclude:     excludePorts,
		AutoAdvance: autoAdvance,
	})
}
