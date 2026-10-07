// preflight 内部复用助手：端口校验（接 store 逻辑占用表）、服务存在性判定
package preflight

import (
	"strings"

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

// portConflict 端口占用处置策略（§5.8 三档：改已有站点端口才顺延 / 服务端口占用报错 / 新建站点占用只告警）
type portConflict int

const (
	// conflictAdvance 改已有站点的端口：占用则顺延 [1,65535] 首个可用，不报错
	conflictAdvance portConflict = iota
	// conflictBlock 服务端口：占用即报 portInUse 阻断，不顺延
	conflictBlock
	// conflictKeepWarn 新建站点：端口原样保留、站点照常创建、配置照常落盘，只把这一个端口暂不发布（站点降级），不阻断。
	// 被自己的 Nginx 发布过的端口不算占用——同一颗 Nginx 按 server_name 分流即可复用，连告警都不给。
	conflictKeepWarn
)

// validatePort 组合逻辑占用表（服务端口 + 站点端口）后走 port.Validate，按 conflict 决定占用处置
func (r *run) validatePort(raw string, excludePorts []int, excludeDomains []string, conflict portConflict) port.Result {
	opts := port.Options{Exclude: excludePorts}
	switch conflict {
	case conflictAdvance:
		opts.AutoAdvance = true
	case conflictKeepWarn:
		opts.KeepOnConflict = true
	}
	used := store.CollectUsedPorts(r.w.Snap, excludeDomains)
	// 建站那一路：别的站点已经把这个端口交给自己的 Nginx 发布了，同一颗 Nginx 按 server_name 分流即可复用，
	// 所以「站点占用」不算占用（只在本地这一份抄件里剔掉，共用占用表一字不动——服务端口占用报错那条还靠它）。
	// 数据服务占用的端口、以及本机其它进程占用的端口仍然照报。
	if conflict == conflictKeepWarn {
		for p, owner := range used {
			if strings.HasPrefix(owner, "site ") {
				delete(used, p)
			}
		}
	}
	return port.Validate(raw, used, opts)
}
