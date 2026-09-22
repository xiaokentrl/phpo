// root 组规则（1）：root-set —— 自定义缓存根 / 备份根 / 每服务版本数据目录（需求 1/2/7/8）
// 唯一限制是路径安全（硬红线 3）：不要求绝对路径、不要求已存在、不限字符集；置空即回落默认根
package preflight

import (
	"phpo/internal/config"
	"phpo/pkg/errs"
)

func (r *run) rootSet() {
	c := r.c
	// 数据目录挂在具体服务版本上，kind/version 与安装同判据（路径安全）；缓存根/备份根是全局根，不涉及
	if c.Field == "data_dir" {
		if !isSvc(c.Kind) {
			r.errf("%s", errs.SvcMissing)
			return
		}
		if vv := config.ValidateVersion(c.Kind, c.Version); !vv.Ok {
			r.errf("%s", vv.Msg)
			return
		}
	}
	if _, err := config.ValidateRootPath(asString(c.NewValue)); err != nil {
		r.errf("%s", errs.PathTraversal)
		return
	}
	if c.Field == "data_dir" && r.w.isRunning(c.Kind, c.Version) {
		r.warnf("%s %s 正在运行，数据目录要重建容器后才生效", c.Kind, c.Version)
	}
}
