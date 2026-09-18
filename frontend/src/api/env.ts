// 服务 env 前端 API：密码/端口的读写薄封装（T504）。
// 硬红线 4/5：真实链路只发起后端调用，状态由后端 state:changed 回流；纯 Vite demo 无宿主时回落本地 mock。
import * as app from '../../bindings/phpo/app.js'
import { ServiceKind as BindKind } from '../../bindings/phpo/internal/model/models.js'
import { hasBackend } from '@/api/site'
import { useAppState } from '@/stores/appState'
import { DEFAULT_PASSWORD } from '@/utils/str'

// envKeyPort / envKeyPassword：与后端 store.EnvKeyPort / EnvKeyPassword 逐字对齐（大写种类 + 去点版本）
function keyOf(kind: string, version: string, suffix: string): string {
  return `${kind.toUpperCase()}_${version.replace(/\./g, '')}_${suffix}`
}
export function envKeyPassword(kind: string, version: string): string {
  return keyOf(kind, version, 'PASSWORD')
}
export function envKeyPort(kind: string, version: string): string {
  return keyOf(kind, version, 'PORT')
}

// 读取回显值：真实快照经 state:changed 落到 state.env；未设置回落默认密码。
export function passwordFromEnv(kind: string, version: string): string {
  const env = useAppState().env
  return env[envKeyPassword(kind, version)] ?? DEFAULT_PASSWORD
}
export function portFromEnv(kind: string, version: string): string {
  const env = useAppState().env
  return env[envKeyPort(kind, version)] ?? ''
}

// setPassword 明文写入（空串/任意长度合法，零校验零加密 §1.5）。mock 下直写本地 env 供 demo 回显。
export async function setPassword(kind: string, version: string, password: string): Promise<void> {
  if (!hasBackend()) {
    useAppState().env[envKeyPassword(kind, version)] = password
    return
  }
  await app.SetServicePassword(kind as BindKind, version, password)
}

export async function setPort(kind: string, version: string, port: number): Promise<void> {
  if (!hasBackend()) {
    useAppState().env[envKeyPort(kind, version)] = String(port)
    return
  }
  await app.SetServicePort(kind as BindKind, version, port)
}
