// cache 组规则（2）：offline-prune · cache-import
package preflight

import (
	"phpo/internal/config"
	"phpo/pkg/errs"
)

func (r *run) offlinePrune() {
	c := r.c
	for _, x := range r.w.Offline {
		if x.Kind == c.Svc && x.Version == c.Ver {
			return
		}
	}
	r.errf("%s: %s/%s", errs.OfflineMissing, c.Svc, c.Ver)
}

// cacheImport 手工导入缓存（需求 1：读写任意缓存文件）。
// 唯一裁决是「导入到哪一条缓存」的坐标合法：kind/version 与安装同判据，extType 限定三种落点；
// 源文件是否存在由导入步骤给出人话报错（preflight 不碰文件系统，§0.2 #14 只裁决规则）。
func (r *run) cacheImport() {
	c := r.c
	if !isSvc(c.Kind) {
		r.errf("%s", errs.SvcMissing)
		return
	}
	if vv := config.ValidateVersion(c.Kind, c.Version); !vv.Ok {
		r.errf("%s", vv.Msg)
		return
	}
	switch c.Field {
	case "image":
	case "apk", "pecl":
		if c.Kind != "php" {
			r.errf("%s 扩展只能导入到 php 缓存", c.Field)
		}
	default:
		r.errf("缓存文件类型只能是 image / apk / pecl，得 %s", c.Field)
	}
}
