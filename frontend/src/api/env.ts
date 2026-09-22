// 服务 env 前端 API：密码/端口/数据目录 与 自定义根（缓存根·备份根）的读写薄封装（T504 / 需求 1/7/8）。
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
export function envKeyDataDir(kind: string, version: string): string {
  return keyOf(kind, version, 'DATA_DIR')
}

// trimRoot 去掉尾部斜杠，使「生效根 === 默认根」可直接比较（后端派生值同样已裁剪）
function trimRoot(s: string): string {
  return s.replace(/\/+$/, '')
}
function envRoot(key: string): string {
  return trimRoot(useAppState().env[key] || '')
}

// 默认根/默认数据目录：一律从当前 PHPO_HOME、{KIND_ROOT} 派生（§0.1.1：`./` 即 PHPO_HOME 根）
export function defaultOfflineRoot(): string {
  return envRoot('PHPO_HOME') + '/offline'
}
export function defaultBackupRoot(): string {
  return envRoot('PHPO_HOME') + '/backups'
}
export function defaultDataDir(kind: string, version: string): string {
  return envRoot(kind.toUpperCase() + '_ROOT') + '/' + version + '/data'
}

// 生效路径：自定义根互斥取代默认根，快照未给出时回落默认派生值（等价于「未自定义」）
export function offlineRoot(): string {
  return envRoot('OFFLINE_ROOT') || defaultOfflineRoot()
}
export function backupRoot(): string {
  return envRoot('BACKUP_ROOT') || defaultBackupRoot()
}
export function dataDirOf(kind: string, version: string): string {
  return envRoot(envKeyDataDir(kind, version)) || defaultDataDir(kind, version)
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

// setOfflineRoot / setBackupRoot 自定义缓存根、备份根（需求 1/8）：
// 传入非空即**完全取代**默认根，传空串即回落默认——两者任一时刻只有一个生效。
// 后端落库后会重绑装配层并推 state:changed，这里不发本地乐观更新（硬红线 4）。
export async function setOfflineRoot(root: string): Promise<void> {
  if (!hasBackend()) {
    useAppState().env.OFFLINE_ROOT = trimRoot(root) || defaultOfflineRoot()
    return
  }
  await app.SetOfflineRoot(root)
}
export async function setBackupRoot(root: string): Promise<void> {
  if (!hasBackend()) {
    useAppState().env.BACKUP_ROOT = trimRoot(root) || defaultBackupRoot()
    return
  }
  await app.SetBackupRoot(root)
}

// setServiceDataDir 某服务版本的数据目录（需求 7）：空串即回落 {KIND_ROOT}/{version}/data
export async function setServiceDataDir(kind: string, version: string, dir: string): Promise<void> {
  if (!hasBackend()) {
    useAppState().env[envKeyDataDir(kind, version)] = trimRoot(dir) || defaultDataDir(kind, version)
    return
  }
  await app.SetServiceDataDir(kind as BindKind, version, dir)
}
