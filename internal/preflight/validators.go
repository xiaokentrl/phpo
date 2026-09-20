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

// portConflict 端口占用处置策略（§5.8 三档）
type portConflict int

const (
	// conflictAdvance 改已有站点的端口：占用则顺延 [1,65535] 首个可用，不报错
	conflictAdvance portConflict = iota
	// conflictBlock 服务端口：占用即报 portInUse 阻断，不顺延
	conflictBlock
	// conflictKeepWarn 新建站点：占用只回传告警，用户所填端口原样保留（站点降级，不阻断建站）
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
	return port.Validate(raw, used, opts)
}
