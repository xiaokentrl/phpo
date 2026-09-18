// 服务生命周期前端 API：install/start/stop/remove 的 Wails 绑定薄封装（T504 接线点）。
// 仅封装真实调用；纯 Vite demo 的 mock runTask 分支留在调用方（对齐 api/site.ts 约定）。
import * as app from '../../bindings/phpo/app.js'
import { ServiceKind as BindKind } from '../../bindings/phpo/internal/model/models.js'

export function installService(kind: string, version: string): Promise<void> {
  return app.Install(kind as BindKind, version)
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
