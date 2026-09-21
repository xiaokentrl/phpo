<script setup lang="ts">
// TaskDrawer：任务实时反馈抽屉（T109 + Q6）。
// 队列详情（运行中 / 排队中可撤回 / 历史记录）、逐行日志、步骤进度、成功/失败与失败原因——
// 全部来自后端权威快照与 task:* 事件（硬红线 4：仅回放展示，前端不造状态）。
import { computed, nextTick, ref, watch } from 'vue'
import { useTaskStore } from '@/stores/taskStore'
import { useAppState } from '@/stores/appState'
import { useLayoutStore } from '@/stores/layoutStore'
import { useI18n } from '@/composables/useI18n'
import { toast } from '@/composables/useToast'
import { copyText } from '@/utils/str'
import type { ServiceKind, TaskStatus } from '@/types'

const store = useTaskStore()
const app = useAppState()
const layout = useLayoutStore()
const { t } = useI18n()

const logRef = ref<HTMLElement | null>(null)

const task = computed(() => store.task)
const lines = computed(() => store.visibleLines)
const isRunning = computed(() => store.isRunning)
const expanded = computed(() => store.expanded)
const progress = computed(() => store.progress)
// errorText：失败原因（终态 failed 时由 err 日志收口；成功/取消不显示）
const errorText = computed(() => (task.value?.status === 'failed' ? task.value.error || '' : ''))

const labelText = computed(() => task.value?.label || t('task.none'))
// cmdText：等效命令仅在「本次会话由前端提交、且后端任务标签与提交标签一致」时可得；
// 后端自发起的任务（如校准、重发布 nginx）没有等效命令，留空而不是渲染一个孤零零的 "phpo"。
const cmdText = computed(() => {
  const args = task.value?.args ?? []
  return args.length ? 'phpo ' + args.join(' ') : ''
})

const dotClass = computed(() => {
  const s = task.value?.status
  let c = 'drawer-dot'
  if (s === 'running') c += ' running'
  else if (s === 'success') c += ' success'
  else if (s === 'failed') c += ' failed'
  else if (s === 'cancelled') c += ' cancelled'
  return c
})

const statusText = computed(() => {
  const tk = task.value
  if (!tk) return ''
  if (tk.status === 'running') return t('task.running')
  if (tk.status === 'success') {
    // 耗时以后端 task:done 的权威值为准；事件未到齐时退化为本地起止差
    const ms = tk.durationMs ?? (tk.endedAt ?? Date.now()) - tk.startedAt
    return t('task.complete', { dur: (ms / 1000).toFixed(1) })
  }
  if (tk.status === 'cancelled') return t('task.cancelled')
  if (tk.status === 'failed') return t('task.failed')
  return ''
})

// —— 队列详情（运行中 + 排队中 + 本次会话记录）——
interface QueueItem {
  id: string
  label: string
  kind: 'running' | 'queued' | 'record'
  step: number
  total: number
  status: TaskStatus
  withdrawable: boolean
}

// QUEUE_CHIPS 队列条最多铺这几个 chip：队列再长也保证抽屉不被记录挤掉日志区
const QUEUE_CHIPS = 14
const queueItems = computed<QueueItem[]>(() => {
  const items: QueueItem[] = []
  const run = store.runningBrief
  if (run) {
    // 运行中项的步骤以事件回流的记录为准（task:progress 即时到达），快照 brief 仅作首帧
    const rec = store.records.find((r) => r.id === run.id)
    items.push({
      id: run.id,
      label: run.label,
      kind: 'running',
      step: rec?.step ?? run.step,
      total: rec?.total ?? run.total,
      status: 'running',
      withdrawable: false,
    })
  }
  for (const p of store.pendingBriefs) {
    if (items.length >= QUEUE_CHIPS) return items
    items.push({ id: p.id, label: p.label, kind: 'queued', step: 0, total: p.total, status: 'running', withdrawable: true })
  }
  const inQueue = new Set(items.map((i) => i.id))
  for (const r of store.records) {
    if (items.length >= QUEUE_CHIPS) break
    if (inQueue.has(r.id) || r.status === 'running') continue // 运行中项已由队列首项表达
    items.push({ id: r.id, label: r.label, kind: 'record', step: r.step, total: r.total, status: r.status, withdrawable: false })
  }
  return items
})

function queueTitle(item: QueueItem): string {
  if (item.kind === 'queued') return t('task.queued')
  if (item.total) return item.label + ' · ' + item.step + '/' + item.total
  return item.label
}

// drawer-services 面板（原型 renderDrawerServices，2773–2788）：读取 appState 展示各服务版本运行态
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

// copyLog：复制当前可见日志正文；无任务时提示暂无日志
async function copyLog(): Promise<void> {
  if (!task.value) {
    toast(t('task.noLog'), 'info', 1200)
    return
  }
  const ok = await copyText(lines.value.map((l) => l.s).join('\n'))
  toast(ok ? t('task.copyLog') : t('common.copyFailed'), ok ? 'ok' : 'err', 1600)
}

// §1.4：等效命令仅展示，点击复制给出「本产品不提供 CLI」
function onCmdClick(): void {
  toast(t('drawer.noCli'), 'info', 1600)
}

function onCancel(): void {
  store.cancel()
}

function onSelect(id: string): void {
  store.select(id)
}

// onWithdraw：撤回排队项（未执行 ⇒ 无需回滚）；命中后由后端队列变化把该行从快照里摘掉
async function onWithdraw(id: string): Promise<void> {
  await store.withdraw(id)
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
          v-if="cmdText"
          class="drawer-cmd"
          :title="`${t('drawer.cmd')} · ${t('drawer.cmdOnly')}`"
          style="cursor: pointer"
          @click="onCmdClick"
          >{{ cmdText }}</span
        >
        <span v-if="cmdText" class="chip" style="font-size: 10px; padding: 1px 6px" :title="t('drawer.cmdOnly')">{{ t('drawer.cmdOnly') }}</span>
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
        <span v-if="progress" class="chip" style="font-size: 10px; padding: 1px 6px" :title="t('task.progressTitle')">{{ progress.step }}/{{ progress.total }}</span>
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
    <div v-if="queueItems.length" class="drawer-queue">
      <span class="dq-title">{{ t('task.queue') }}</span>
      <span
        v-for="q in queueItems"
        :key="q.id"
        class="dq-chip"
        :class="{ active: q.id === store.activeId, queued: q.kind === 'queued', plain: q.kind === 'queued' }"
        :title="queueTitle(q)"
        @click="onSelect(q.id)"
        >
        <span class="dq-dot" :class="q.kind === 'queued' ? '' : q.status"></span>
        <span class="dq-text">{{ q.label }}</span>
        <span v-if="q.total" class="dq-step">{{ q.step }}/{{ q.total }}</span>
        <span v-if="q.kind === 'queued'" class="dq-step">{{ t('task.queued') }}</span>
        <button v-if="q.withdrawable" class="dq-withdraw" type="button" :title="t('task.withdraw')" @click.stop="onWithdraw(q.id)">
          <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12" /></svg>
        </button>
      </span>
    </div>
    <div v-if="progress" class="drawer-progress"><div class="drawer-progress-bar" :style="{ width: progress.percent + '%' }"></div></div>
    <div v-if="errorText" class="drawer-error">
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="flex-shrink: 0"><circle cx="12" cy="12" r="9" /><path d="M12 7v6M12 16.5v.5" /></svg>
      <span class="drawer-error-text" :title="errorText">{{ t('task.reason') }}: {{ errorText }}</span>
    </div>
    <pre ref="logRef" class="drawer-log"><div v-for="(l, i) in lines" :key="i" class="log-line" :class="l.t">{{ l.s || ' ' }}</div></pre>
  </section>
</template>
