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
	// conflictAdvance 改已有站点的端口：占用则顺延 [1,65535] 首个可用，不报错。
	// 这一档站点占用照常参与判定——用户要的是「给我一个能用的口」，别的站点已经站在那一口上，就该往后让。
	conflictAdvance portConflict = iota
	// conflictBlock 服务端口：占用即报 portInUse 阻断，不顺延。占用者只认「一个真的应用程序」——
	// 数据服务（mysql/pgsql/redis）的宿主端口。站点端口不算占用：那口是自家 Nginx 发布出来的，
	// 装 Nginx 时它本来就该继续归 Nginx（同一颗进程、同一口，按 server_name 分流服务那些站点）。
	// 真被外部程序占着的情况由「创建并启动」那一步被引擎如实拒绝，不在这里凭空拦。
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
	// 建站与装服务这两路：站点那个宿主端口本来就是自家 Nginx 发布出来的，同一颗 Nginx 按 server_name
	// 分流即可复用，所以「站点占用」在这两路都不算占用——拿它拦服务端口，等于让自家的东西占自家的口。
	// 剔掉之后剩下的只有数据服务（mysql/pgsql/redis）的端口；真被本机别的程序听着的口，
	// 不在这里凭空拦，由「创建并启动」那一步被引擎如实拒绝。
	// 改已有站点端口要顺延的那一路不剔：别的站点正站在那一口上，就是该往后让的正当理由。
	if conflict == conflictKeepWarn || conflict == conflictBlock {
		for p, owner := range used {
			if strings.HasPrefix(owner, "site ") {
				delete(used, p)
			}
		}
	}
	return port.Validate(raw, used, opts)
}
