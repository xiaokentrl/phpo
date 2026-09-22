<script setup lang="ts">
// 托盘（浏览器内仿真 tray-sim/tray-menu，逐字迁移原型 3206–3231）
// 生产版原生托盘见 internal/ui/tray.go + menu.go；其动作以 ui:command 广播，本组件订阅并复用同一套处理。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useAppState } from '@/stores/appState'
import { usePrefsStore, type ThemeId } from '@/stores/prefsStore'
import { useTaskStore, TASK_BUSY } from '@/stores/taskStore'
import { useModals } from '@/composables/useModals'
import { toast } from '@/composables/useToast'
import { quitApp as nativeQuit } from '@/api/state'
import { THEMES } from '@/constants/themes'
import { onEvent, UI_COMMAND, UICmd } from '@/api/events'
import type { ServiceKind } from '@/types'

const { t } = useI18n()
const state = useAppState()
const prefs = usePrefsStore()
const task = useTaskStore()
const modals = useModals()

const menuOpen = ref(false)
const simEl = ref<HTMLElement | null>(null)
const menuEl = ref<HTMLElement | null>(null)

const currentTheme = computed(() => THEMES.find((th) => th.id === prefs.theme) ?? THEMES[0])
const trayLabel = computed(() => `phpo · ${t(currentTheme.value.nameKey)}`)
const dotStyle = computed(() => ({ background: currentTheme.value.colors[2] }))
const dotClass = computed(() => {
  const s = task.task?.status
  if (s === 'running') return 'running'
  if (s === 'failed') return 'error'
  return ''
})

// 托盘服务行：对齐原型，取 php/mysql/nginx 首个已安装版本，最多 5 行
// 运行状态取自权威快照的 running 分区（硬红线 4）：停止的服务不得显示为运行中
const svcRows = computed(() =>
  (
    [
      ['🐘', 'PHP', 'php'],
      ['🐬', 'MySQL', 'mysql'],
      ['🌐', 'Nginx', 'nginx'],
    ] as [string, string, ServiceKind][]
  )
    .map(([icon, name, kind]) => {
      const version = state.installed[kind][0] || ''
      return { icon, name, version, running: state.isServiceRunning(kind, version) }
    })
    .filter((r) => r.version !== '')
    .slice(0, 5),
)

function toggleMenu(): void {
  menuOpen.value = !menuOpen.value
}

function closeMenu(): void {
  menuOpen.value = false
}

function applyTheme(id: ThemeId): void {
  if (id === prefs.theme) return
  prefs.setTheme(id)
}

function openLogs(): void {
  closeMenu()
  task.setExpanded(true)
}

function backupNow(): void {
  closeMenu()
  // 走 useModals.runBackup：预检 → 警告危险确认 → 后端三段式任务（队列/日志/成败由事件回流）
  modals.runBackup()
}

async function quitApp(): Promise<void> {
  closeMenu()
  if (task.queueRunning) {
    // 忙锁：任务运行中不得退出（原生托盘菜单同样将 CmdQuit 广播到这里，两条入口共用同一裁决）
    toast(TASK_BUSY, 'err', 2200)
    return
  }
  // 真实宿主：调门面 Quit → 原生外壳退出进程；无宿主的 demo 通道没有可退出的进程，静默跳过
  await nativeQuit().catch((e: unknown) => toast(String(e), 'err', 4600))
}

function onDocClick(e: MouseEvent): void {
  const target = e.target as HTMLElement | null
  if (!target) return
  if (simEl.value?.contains(target) || menuEl.value?.contains(target)) return
  closeMenu()
}

let offUICommand: (() => void) | null = null
onMounted(() => {
  document.addEventListener('click', onDocClick)
  // 原生托盘菜单动作 → 复用同一套处理
  offUICommand = onEvent(UI_COMMAND, (cmd: unknown) => {
    if (cmd === UICmd.OpenLogs) openLogs()
    else if (cmd === UICmd.BackupNow) backupNow()
    else if (cmd === UICmd.Quit) quitApp()
  })
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  offUICommand?.()
})
</script>

<template>
  <div v-show="prefs.tray.enabled" ref="simEl" class="tray-sim" :title="t('settings.tray')" @click="toggleMenu">
    <span class="tray-dot" :class="dotClass" :style="dotStyle"></span>
    <span class="tray-label">{{ trayLabel }}</span>
  </div>

  <div ref="menuEl" class="tray-menu" :class="{ open: menuOpen }">
    <div class="tray-themes" :title="t('common.theme')">
      <button
        v-for="th in THEMES"
        :key="th.id"
        type="button"
        class="tray-theme-dot"
        :class="{ active: th.id === prefs.theme }"
        :title="t(th.nameKey)"
        :aria-label="t(th.nameKey)"
        :style="{ '--dot-bg': th.colors[0], '--dot-accent': th.colors[2] }"
        @click.stop="applyTheme(th.id as ThemeId)"
      ></button>
    </div>
    <div class="tray-menu-sep"></div>

    <div v-for="r in svcRows" :key="r.name" class="tray-svc">
      <span>{{ r.icon }}</span>
      <span class="name">{{ r.name }} {{ r.version }}</span>
      <span class="status" :class="{ 'st-off': !r.running }">● {{ t(r.running ? 'svc.running' : 'svc.stopped') }}</span>
    </div>
    <div class="tray-menu-sep"></div>

    <div class="tray-menu-item" @click="openLogs">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M4 6h16M4 12h16M4 18h10" /></svg>
      {{ t('tray.openLogs') }}
    </div>
    <div class="tray-menu-item" @click="backupNow">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><path d="M7 10l5 5 5-5M12 15V3" /></svg>
      {{ t('tray.createBackup') }}
    </div>

    <template v-if="task.task">
      <div class="tray-menu-sep"></div>
      <div class="tray-menu-item">
        <span>{{ task.task.status === 'success' ? '✓' : '⚠' }}</span>
        <span>{{ task.task.label }}</span>
      </div>
    </template>

    <div class="tray-menu-sep"></div>
    <div class="tray-menu-item" @click="quitApp">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" /></svg>
      {{ t('tray.quit') }}
    </div>
  </div>
</template>
