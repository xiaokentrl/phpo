// 服务生命周期前端 API：install/reinstall/start/stop/remove 的 Wails 绑定薄封装（T504 接线点）。
// 仅封装真实调用；纯 Vite demo 的 mock runTask 分支留在调用方（对齐 api/site.ts 约定）。
import * as app from '../../bindings/phpo/app.js'
import { InstallOptions, ServiceKind as BindKind } from '../../bindings/phpo/internal/model/models.js'

// InstallCfg 安装期端口/密码（后端 model.InstallOptions 同构）。
// 必须由 install 携带：二者只在建容器那一刻被读走，而未安装态过不了 update-config 守卫（§5.8）。
export interface InstallCfg {
  port?: number
  password?: string
  hasPassword?: boolean
}

export function installService(kind: string, version: string, cfg: InstallCfg = {}): Promise<void> {
  return app.Install(kind as BindKind, version, InstallOptions.createFrom(cfg))
}
// reinstallService 重建容器：服务端口与密码只在建容器时落定，改完必须重建才生效（数据卷保留）
export function reinstallService(kind: string, version: string): Promise<void> {
  return app.Reinstall(kind as BindKind, version)
}
export function startService(kind: string, version: string): Promise<void> {
  return app.Start(kind as BindKind, version)
}
export function stopService(kind: string, version: string): Promise<void> {
  return app.Stop(kind as BindKind, version)
}
export function removeService(kind: string, version: string): Promise<void> {
  return app.Remove(kind as BindKind, version)
}
