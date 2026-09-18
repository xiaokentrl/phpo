// 服务配置前端 API（T505）：读回显 + 原子保存的薄封装。
// 硬红线 4/5：真实链路只发起后端调用（后端备份→写→失败回滚）；纯 Vite demo 无宿主时回落本地 mock。
import * as app from '../../bindings/phpo/app.js'
import { ServiceKind as BindKind } from '../../bindings/phpo/internal/model/models.js'
import { ConfigFile as BindConfigFile } from '../../bindings/phpo/internal/service/models.js'
import { hasBackend } from '@/api/site'
import { getDefaultFiles, type ConfigFile } from '@/constants/configs'

// getConfigFiles 返回该服务版本配置：真实读宿主（缺失回落模板）；mock 读本地默认模板
export async function getConfigFiles(kind: string, version: string): Promise<ConfigFile[]> {
  if (!hasBackend()) return getDefaultFiles(kind, version)
  const files = await app.ConfigGetFiles(kind as BindKind, version)
  return (files ?? []).map((f) => ({ name: f.name, path: f.path, content: f.content }))
}

// saveConfigFiles 提交改动文件（仅 name+content，路径由后端按模板解析）；mock 下由调用方本地处理
export async function saveConfigFiles(kind: string, version: string, files: ConfigFile[]): Promise<void> {
  if (!hasBackend()) return
  const payload = files.map((f) => new BindConfigFile({ name: f.name, content: f.content }))
  await app.ConfigSaveFiles(kind as BindKind, version, payload)
}
