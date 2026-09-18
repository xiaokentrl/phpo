// 挂载装配：把 config.MOUNTS 解析出的宿主挂载翻译为 Docker bind 规范 "host:container:mode"
package engine

import (
	"phpo/internal/config"
)

// BuildBinds 依 MOUNTS 表生成 bind 挂载串；跳过解析后宿主路径为空的条目
func BuildBinds(env config.Env, kind, version string) []string {
	mounts := env.ResolveMounts(kind, version)
	binds := make([]string, 0, len(mounts))
	for _, m := range mounts {
		if m.Host == "" || m.To == "" {
			continue
		}
		mode := m.Mode
		if mode == "" {
			mode = "rw"
		}
		binds = append(binds, m.Host+":"+m.To+":"+mode)
	}
	return binds
}
