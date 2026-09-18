// 备份前端 API（T602）：列表/创建/恢复/删除/下载的薄封装。
// 硬红线 4/5：真实链路只发起后端调用（写操作走三段式），列表/展示以后端为准；纯 Vite demo 无宿主时返回 null 让视图保留本地 mock。
import * as app from '../../bindings/phpo/app.js'
import { Dialogs } from '@wailsio/runtime'
import { hasBackend } from '@/api/site'
import type { Backup } from '@/types'

// listBackups 拉取权威备份列表；无宿主返回 null（视图维持 mock 不覆盖）
export async function listBackups(): Promise<Backup[] | null> {
  if (!hasBackend()) return null
  const rows = await app.BackupList()
  return (rows ?? []).map((r) => ({ file: r.file, size: r.size, at: r.at, items: r.items }))
}

// createBackup 创建备份并返回新条目；无宿主返回 null
export async function createBackup(): Promise<Backup | null> {
  if (!hasBackend()) return null
  const r = await app.BackupCreate()
  return { file: r.file, size: r.size, at: r.at, items: r.items }
}

// restoreBackup 异机恢复：清空命名空间 → 解包落盘 → 逻辑重放 SQLite → 重建容器
export async function restoreBackup(file: string): Promise<void> {
  if (!hasBackend()) return
  await app.BackupRestore(file)
}

// deleteBackup 删除归档
export async function deleteBackup(file: string): Promise<void> {
  if (!hasBackend()) return
  await app.BackupDelete(file)
}

// downloadBackup 经原生保存框选目标后导出归档；无宿主直接返回（mock 环境不提供下载）
export async function downloadBackup(file: string): Promise<void> {
  if (!hasBackend()) return
  const dst = await Dialogs.SaveFile({ Filename: file })
  if (!dst) return // 用户取消
  await app.BackupSaveAs(file, dst)
}
