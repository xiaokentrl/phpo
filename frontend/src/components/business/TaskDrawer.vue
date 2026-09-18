<script setup lang="ts">
// TaskDrawer：日志抽屉（T109）。5 类型日志行逐行回放 + 4 态状态点 + 等效命令「仅展示」+ 取消/复制。
// 硬红线 4：仅回放展示；状态落地/服务实时状态由后端事件驱动（T110）。
import { computed, ref, watch, nextTick } from 'vue'
import { useTaskStore } from '@/stores/taskStore'
import { useAppState } from '@/stores/appState'
import { useLayoutStore } from '@/stores/layoutStore'
import { useI18n } from '@/composables/useI18n'
import { toast } from '@/composables/useToast'
import { copyText } from '@/utils/str'
import type { ServiceKind } from '@/types'

const store = useTaskStore()
const app = useAppState()
const layout = useLayoutStore()
const { t } = useI18n()

const logRef = ref<HTMLElement | null>(null)

const task = computed(() => store.task)
const lines = computed(() => store.visibleLines)
const isRunning = computed(() => store.isRunning)
const expanded = computed(() => store.expanded)

const labelText = computed(() => task.value?.label || t('task.none'))
const cmdText = computed(() => (task.value ? 'phpo ' + task.value.args.join(' ') : ''))

const dotClass = computed(() => {
  const s = task.value?.status
  let c = 'drawer-dot'
  if (s === 'running') c += ' running'
  else if (s === 'success') c += ' success'
  else if (s === 'failed') c += ' failed'
  return c
})

const statusText = computed(() => {
  const tk = task.value
  if (!tk) return ''
  if (tk.status === 'running') return t('task.running')
  if (tk.status === 'success') {
    const dur = (((tk.endedAt ?? Date.now()) - tk.startedAt) / 1000).toFixed(1)
    return t('task.complete', { dur })
  }
  if (tk.status === 'cancelled') return t('task.cancelled')
  if (tk.status === 'failed') return t('task.failed')
  return ''
})

// drawer-services 面板（原型 renderDrawerServices，2773–2788）：读取 mock appState 展示各服务版本运行态
const SVC_ORDER: ServiceKind[] = ['php', 'mysql', 'pgsql', 'redis', 'nginx']
const SVC_NAME: Record<string, string> = { php: 'PHP', mysql: 'MySQL', pgsql: 'PostgreSQL', redis: 'Redis', nginx: 'Nginx' }
const svcGroups = computed(() =>
  SVC_ORDER.map((kind) => ({
    kind,
    name: SVC_NAME[kind],
    versions: (app.installed[kind] || []).map((v) => ({ v, running: app.isServiceRunning(kind, v) })),
  })).filter((g) => g.versions.length)
)

// 新行滚动到底（原型 playTask 每行 log.scrollTop = scrollHeight）
watch(
  () => lines.value.length,
  () => {
    nextTick(() => {
      const el = logRef.value
      if (el) el.scrollTop = el.scrollHeight
    })
  }
)

function onResizerDown(e: PointerEvent): void {
  if (!store.expanded) return
  e.preventDefault()
  const zoom = layout.scale || 1
  const startY = e.clientY
  const startH = layout.drawerHeight
  const onMove = (ev: PointerEvent) => {
    const dy = (startY - ev.clientY) / zoom
    layout.setDrawer(startH + dy)
  }
  const onUp = () => {
    window.removeEventListener('pointermove', onMove)
    window.removeEventListener('pointerup', onUp)
  }
  window.addEventListener('pointermove', onMove)
  window.addEventListener('pointerup', onUp)
}

// copyLog：复制已回放的日志正文（原型 3338），无任务时提示暂无日志
async function copyLog(): Promise<void> {
  const tk = task.value
  if (!tk) {
    toast(t('task.noLog'), 'info', 1200)
    return
  }
  const txt = tk.lines.slice(0, tk.cursor).map((l) => l.s).join('\n')
  const ok = await copyText(txt)
  toast(ok ? t('task.copyLog') : t('common.copyFailed'), ok ? 'ok' : 'err', 1600)
}

// §1.4：等效命令仅展示，点击复制给出「本产品不提供 CLI」
function onCmdClick(): void {
  toast(t('drawer.noCli'), 'info', 1600)
}

function onCancel(): void {
  store.cancel()
}
</script>

<template>
  <section class="drawer" :class="{ expanded }">
    <div class="drawer-resizer" title="Drag to resize (expanded only)" @pointerdown="onResizerDown" @dblclick="layout.setDrawer(160)"></div>
    <header class="drawer-head">
      <div class="drawer-left">
        <span :class="dotClass"></span>
        <span class="drawer-label">{{ labelText }}</span>
        <span
          class="drawer-cmd"
          :title="`${t('drawer.cmd')} · ${t('drawer.cmdOnly')}`"
          style="cursor: pointer"
          @click="onCmdClick"
          >{{ cmdText }}</span
        >
        <span class="chip" style="font-size: 10px; padding: 1px 6px" :title="t('drawer.cmdOnly')">{{ t('drawer.cmdOnly') }}</span>
      </div>
      <div class="drawer-services">
        <span v-for="g in svcGroups" :key="g.kind" class="dsvc">
          <span class="dsvc-name">{{ g.name }}</span>
          <span class="dsvc-vers"
            >[<template v-for="(item, i) in g.versions" :key="item.v"
              ><span v-if="i > 0" class="dsvc-sep">,</span><span class="dsvc-ver" :title="`${g.name} ${item.v} · ${item.running ? t('svc.running') : t('svc.stopped')}`"
                ><span class="dsvc-dot" :class="item.running ? 'on' : 'off'"></span><span class="dsvc-vtext">{{ item.v }}</span></span
              ></template
            >]</span
          >
        </span>
      </div>
      <div class="drawer-right">
        <span class="drawer-status">{{ statusText }}</span>
        <button v-show="isRunning" class="icon-btn" :title="t('drawer.cancelTitle')" @click="onCancel">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12" /></svg>
        </button>
        <button class="icon-btn" :title="t('drawer.copyTitle')" @click="copyLog">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="9" y="9" width="12" height="12" rx="2" /><path d="M5 15V5a2 2 0 0 1 2-2h10" /></svg>
        </button>
        <button class="icon-btn" :title="t('drawer.expandTitle')" @click="store.toggle()">
          <svg class="drawer-toggle-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M18 15l-6-6-6 6" /></svg>
        </button>
      </div>
    </header>
    <pre ref="logRef" class="drawer-log"><div v-for="(l, i) in lines" :key="i" class="log-line" :class="l.t">{{ l.s || ' ' }}</div></pre>
  </section>
</template>
