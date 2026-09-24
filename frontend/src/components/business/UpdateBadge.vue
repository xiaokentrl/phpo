<script setup lang="ts">
// 左上角「更新中心」徽标：可用更新 / 后台下载进度 / 升级结果都在这里一句话给出。
// 状态只来自 updaterStore（update:* 事件落地），不做乐观更新（硬红线 4）；
// 「忽略此版本」后 store.hasUpdate 为假即隐藏，但下载中/已出结果仍继续显示到本次收口。
import { computed } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useUpdater } from '@/composables/useUpdater'
import { useModals } from '@/composables/useModals'

const { t } = useI18n()
const { store, percent, busy } = useUpdater()
const { openUpdateModal } = useModals()

const source = computed(() => store.available?.source ?? '')
const visible = computed(() => busy.value || store.done !== null || store.hasUpdate)
const label = computed(() => {
  if (store.done?.status === 'success') return t('update.centerDone')
  if (store.done?.status === 'failed') return t('update.centerFailed')
  if (busy.value) return t('update.downloading', { percent: percent.value })
  // 后端每次只报一个最新可用版本，故计数恒为 1（有更新才渲染）
  return source.value ? t('update.centerSource', { count: 1, source: source.value }) : t('update.center', { count: 1 })
})
const tone = computed(() => (store.done?.status === 'failed' ? 'err' : store.done?.status === 'success' ? 'ok' : 'avail'))

// 点击即回到升级弹窗看版本详情与三选项；下载进行中同样可开（弹窗内进度条承载）
function onClick(): void {
  openUpdateModal()
}
</script>

<template>
  <button v-if="visible" class="upd-badge" :class="'upd-badge-' + tone" type="button" @click="onClick">
    <span class="upd-badge-dot" />
    <span class="upd-badge-text">{{ label }}</span>
  </button>
</template>

<style scoped>
/* inline 之外的样式只写在本组件的 scoped 块：不改 base.css 与冻结原型 SSOT（§5.6.4 同口径） */
.upd-badge {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0 12px 8px;
  padding: 5px 9px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--accent-bg);
  color: var(--accent);
  font: inherit;
  font-size: 12px;
  line-height: 1.3;
  text-align: left;
  cursor: pointer;
}
.upd-badge-dot {
  flex: none;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--accent);
}
.upd-badge-text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.upd-badge-ok { background: var(--surface); color: var(--ok); }
.upd-badge-ok .upd-badge-dot { background: var(--ok); }
.upd-badge-err { background: var(--surface); color: var(--danger); }
.upd-badge-err .upd-badge-dot { background: var(--danger); }
</style>
