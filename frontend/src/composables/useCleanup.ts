// useCleanup（T605 / §5.13.6-7-10）：清理面板的接线层——扫描/清理/回收站/审计动作。
// 硬红线 4：清理为写操作走后端三段式；打开面板与每次写完成后重新拉权威孤儿/回收站列表，前端不做乐观更新。
// 硬红线 §5.14.13：激进模式含数据卷删除，须先经前端 DangerConfirm 二次确认再由调用方触发 clean('aggressive')。
import { computed } from 'vue'
import { useCleanupStore } from '@/stores/cleanupStore'
import {
  scanOrphans, runCleanup, cleanCache, listTrash, restoreTrash, emptyExpired, listOperations,
} from '@/api/cleanup'
import { toast } from '@/composables/useToast'
import { t } from '@/composables/useI18n'

export function useCleanup() {
  const s = useCleanupStore()

  const orphanTotal = computed(() => {
    const o = s.orphans
    return o.containers.length + o.volumes.length + o.networks.length + o.images.length
  })

  async function scan(): Promise<void> {
    if (s.scanning) return
    s.scanning = true
    try {
      const o = await scanOrphans()
      if (o) s.setOrphans(o)
    } catch (e) {
      toast(String(e), 'err', 4600)
    } finally {
      s.scanning = false
    }
  }

  // clean：按模式删除孤儿；成功后重扫 + 刷新回收站（到期项会被顺带清空）
  async function clean(mode: string): Promise<void> {
    if (s.cleaning) return
    s.cleaning = true
    try {
      const rep = await runCleanup(mode)
      if (!rep) return
      const summary = t('cleanup.done', { removed: rep.removed, failed: rep.failed, size: humanSize(rep.freedBytes) })
      if (rep.failed > 0) toast(summary, 'err', 4200)
      else toast(summary, 'ok', 3200)
      await scan()
      await loadTrash()
    } catch (e) {
      toast(String(e), 'err', 4600)
    } finally {
      s.cleaning = false
    }
  }

  async function purgeCache(mode: string): Promise<void> {
    try {
      const freed = await cleanCache(mode)
      toast(t('cleanup.cacheDone', { size: humanSize(freed) }), 'ok', 3200)
    } catch (e) {
      toast(String(e), 'err', 4600)
    }
  }

  async function loadTrash(): Promise<void> {
    const list = await listTrash()
    if (list) s.setTrash(list)
  }

  async function restore(id: number): Promise<void> {
    try {
      await restoreTrash(id)
      toast(t('cleanup.restored'), 'ok', 2600)
      await loadTrash()
    } catch (e) {
      toast(String(e), 'err', 4600)
    }
  }

  async function clearExpired(): Promise<void> {
    try {
      const n = await emptyExpired()
      toast(t('cleanup.expiredCleared', { n }), 'ok', 2600)
      await loadTrash()
    } catch (e) {
      toast(String(e), 'err', 4600)
    }
  }

  async function loadOperations(): Promise<void> {
    const ops = await listOperations(50)
    if (ops) s.setOperations(ops)
  }

  return {
    store: s, orphanTotal,
    scan, clean, purgeCache, loadTrash, restore, clearExpired, loadOperations,
  }
}

// humanSize 字节转人类可读（与展示层一致的极简格式化）
export function humanSize(b: number): string {
  if (!b || b <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let n = b
  let i = 0
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}
