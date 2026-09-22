<script setup lang="ts">
// TaskDrawer：任务实时反馈抽屉（T109 + Q6 + §5.6.1）。
// 展开后左右两栏：**左栏日志默认占 70%**（当前选中任务的逐行日志 / 进度 / 失败原因），
// **右栏任务队列默认占 30%**（竖排列表，最新提交永远在最上面，每行给出明确显示态）。
// 两栏之间中缝可左右拖拽改占比（需求 4），双击复位 70%／30%；偏好仅存 localStorage（§3.1 原则 5）。
// 队列详情、日志、进度、终态与失败原因全部来自后端权威快照与 task:* 事件（硬红线 4：仅回放展示，前端不造状态）。
import { computed, nextTick, ref, watch } from 'vue'
import { DISPLAY_LABEL, useTaskStore } from '@/stores/taskStore'
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
const bodyRef = ref<HTMLElement | null>(null)

const task = computed(() => store.task)
const lines = computed(() => store.visibleLines)
const isRunning = computed(() => store.isRunning)
const expanded = computed(() => store.expanded)
const display = computed(() => store.display)
const progress = computed(() => store.progress)
// errorText：失败原因（终态 failed 时由 err 日志收口；成功/取消不显示）
const errorText = computed(() => (task.value?.status === 'failed' ? task.value.error || '' : ''))

// 需求 5（§5.6.1 头部三区口径）：头部左侧只有状态点 + 固定标签「服务」——标签取 t('nav.services')，
// 不随选中任务变化（任务名在右栏队列每行给出，头部只作区块标题，避免日志/队列切换时标题跳动）。
// §1.4（v2.9.11）：等效命令不再在界面上任何位置展示——本产品不提供 CLI，把 `phpo …` 形态的文本
// 摆在界面上等于暗示存在命令行；任务参数仍记在 taskStore（demo 回放与日志正文要用），只是不外显。

// dotClass / statusText：一律由显示态派生（§5.6.1），不再看记录内部 status 字段
const dotClass = computed(() => 'drawer-dot ' + display.value)

const statusText = computed(() => {
  const tk = task.value
  if (!tk) return ''
  const ds = display.value
  if (ds === 'done') {
    // 耗时以后端 task:done 的权威值为准；事件未到齐时退化为本地起止差
    const ms = tk.durationMs ?? (tk.endedAt ?? Date.now()) - tk.startedAt
    return t('task.complete', { dur: (ms / 1000).toFixed(1) })
  }
  return t(DISPLAY_LABEL[ds])
})

// —— 队列详情（右栏 30%）——
// 行内容（最新在顶的排序、显示态、可撤回判定）全部在 taskStore.queue 里派生（§5.6.1），
// 组件只做渲染：不重排、不补状态、不因「看起来该结束了」推断终态。
const queue = computed(() => store.queue)

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

// onSplitterDown：需求 4——日志／队列两栏除默认 70%／30% 外可左右拖拽。
// 按 .drawer-body 实际矩形算百分比：占比与像素缩放无关，故不需要 zoom 补偿。
function onSplitterDown(e: PointerEvent): void {
  if (!store.expanded) return
  e.preventDefault()
  const el = bodyRef.value
  if (!el) return
  const rect = el.getBoundingClientRect()
  if (rect.width <= 0) return
  const onMove = (ev: PointerEvent) => {
    layout.setSplit(Math.round(((ev.clientX - rect.left) / rect.width) * 100))
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

// §1.4（v2.9.11）：等效命令的展示与复制入口一并移除（界面不再出现 `phpo …` 文本），
// 故此处不再需要「本产品不提供 CLI」的复制提示。

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
        <span class="drawer-label">{{ t('nav.services') }}</span>
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
    <!-- §5.6.1：展开体两栏——左默认 70% 日志、右默认 30% 任务队列（竖排、最新在顶）；中缝可左右拖拽（需求 4） -->
    <div ref="bodyRef" class="drawer-body">
      <div class="drawer-main">
        <div v-if="progress" class="drawer-progress"><div class="drawer-progress-bar" :style="{ width: progress.percent + '%' }"></div></div>
        <div v-if="errorText" class="drawer-error">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="flex-shrink: 0"><circle cx="12" cy="12" r="9" /><path d="M12 7v6M12 16.5v.5" /></svg>
          <span class="drawer-error-text" :title="errorText">{{ t('task.reason') }}: {{ errorText }}</span>
        </div>
        <pre ref="logRef" class="drawer-log"><div v-for="(l, i) in lines" :key="i" class="log-line" :class="l.t">{{ l.s || ' ' }}</div><div v-if="!lines.length" class="log-line dim">{{ task ? t('task.noLog') : t('task.sysEmpty') }}</div></pre>
      </div>
      <div class="drawer-splitter" :title="t('drawer.splitTitle')" @pointerdown="onSplitterDown" @dblclick="layout.setSplit(70)"></div>
      <aside class="drawer-queue">
        <div class="dq-head">
          <span class="dq-title">{{ t('task.queue') }}</span>
          <span class="dq-count">{{ queue.length }}</span>
        </div>
        <div class="dq-list">
          <div
            v-for="q in queue"
            :key="q.id"
            class="dq-row"
            :class="{ active: q.active, waiting: q.withdrawable }"
            :title="q.label + ' · ' + q.statusText"
            @click="onSelect(q.id)"
          >
            <span class="dq-dot" :class="q.display"></span>
            <span class="dq-text">{{ q.label }}</span>
            <span v-if="q.total && !q.withdrawable" class="dq-step">{{ q.step }}/{{ q.total }}</span>
            <span class="dq-status">{{ q.statusText }}</span>
            <button v-if="q.withdrawable" class="dq-withdraw" type="button" :title="t('task.withdraw')" @click.stop="onWithdraw(q.id)">
              <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12" /></svg>
            </button>
          </div>
          <div v-if="!queue.length" class="dq-empty">{{ t('task.none') }}</div>
        </div>
      </aside>
    </div>
  </section>
</template>
