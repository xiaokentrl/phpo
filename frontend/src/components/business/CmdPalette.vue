<script setup lang="ts">
// 命令面板：1:1 迁移原型 CMD_ITEMS + openCmdPalette/renderCmdList/dispatch（3233–3254, 3343–3353）
// 全局快捷键：⌘/Ctrl+K 开关 · ⌘1–9 跳转路由 · ⌘+/-/0 缩放 · ⌘R 同步提示 · Esc 关闭。
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useCmdPalette } from '@/composables/useCmdPalette'
import { useModals } from '@/composables/useModals'
import { useLayoutStore } from '@/stores/layoutStore'
import { CMD_ITEMS, type CmdItem } from '@/constants/cmd'
import { router } from '@/router'
import { toast } from '@/composables/useToast'
import { runTask } from '@/composables/useTask'

const { t } = useI18n()
const palette = useCmdPalette()
const modals = useModals()
const layout = useLayoutStore()

const query = ref('')
const inputEl = ref<HTMLInputElement | null>(null)

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return CMD_ITEMS
  return CMD_ITEMS.filter((it) => t(it.labelKey).toLowerCase().includes(q) || it.labelKey.toLowerCase().includes(q))
})

// 忠实原型 dispatch（3254）：路由 / 动作 / 布局 / 预检任务 / 任务
function dispatch(it: CmdItem): void {
  palette.hide()
  if (it.route) {
    void router.push({ name: it.route }).then(() => scrollToTop())
    return
  }
  switch (it.action) {
    case 'site-add': modals.openSiteAddModal(); return
    case 'theme': modals.openThemePicker(); return
    case 'zoom-in': layout.zoomIn(); return
    case 'zoom-out': layout.zoomOut(); return
    case 'zoom-reset': layout.zoomReset(); return
  }
  if (it.layout === 'reset') { layout.reset(); return }
  if (it.layout) { layout.applyPreset(it.layout); return }
  if (it.task) {
    const [args, labelKey] = it.task
    const label = t(labelKey)
    if (it.pf) modals.runGuardedTask(it.pf, {}, args.split(','), label)
    else runTask(args.split(','), label)
  }
}

function scrollToTop(): void {
  document.querySelector('.app-main')?.scrollTo({ top: 0 })
}

function onEnter(): void {
  const first = filtered.value[0]
  if (first) dispatch(first)
}

async function onOpened(): Promise<void> {
  query.value = ''
  await nextTick()
  inputEl.value?.focus()
}

function onKeydown(e: KeyboardEvent): void {
  if (e.key === 'Escape' && palette.open.value) { palette.hide(); return }
  const mod = e.ctrlKey || e.metaKey
  if (!mod) return
  const k = e.key.toLowerCase()
  if (k === 'k') { e.preventDefault(); palette.toggle(); if (palette.open.value) void onOpened(); return }
  if (k === 'r') { e.preventDefault(); toast(t('common.refresh'), 'ok', 1200); return }
  if (e.key === '=' || e.key === '+') { e.preventDefault(); layout.zoomIn(); return }
  if (e.key === '-' || e.key === '_') { e.preventDefault(); layout.zoomOut(); return }
  if (e.key === '0') { e.preventDefault(); layout.zoomReset(); return }
  if (/^[1-9]$/.test(e.key)) {
    const routes = ['sites', 'php', 'mysql', 'pgsql', 'redis', 'nginx', 'backup', 'overview', 'settings']
    const name = routes[parseInt(e.key, 10) - 1]
    if (name) { e.preventDefault(); void router.push({ name }).then(() => scrollToTop()) }
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <div class="cmd-palette" :class="{ open: palette.open.value }" @click.self="palette.hide()">
    <div class="cmd-palette-box">
      <input ref="inputEl" v-model="query" type="text" autocomplete="off" :placeholder="t('common.cmdPalette') + '…'" @keydown.enter.prevent="onEnter">
      <div class="cmd-palette-list">
        <div v-for="it in filtered" :key="it.labelKey" class="cmd-palette-item" @click="dispatch(it)">
          <span>{{ t(it.labelKey) }}</span>
          <span v-if="it.kbd" class="kbd">{{ it.kbd }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.cmd-palette {
  position: fixed;
  inset: 0;
  display: none;
  align-items: flex-start;
  justify-content: center;
  padding-top: 15vh;
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(3px);
  z-index: 150;
}
.cmd-palette.open {
  display: flex;
}
.cmd-palette-box {
  width: 100%;
  max-width: 520px;
  margin: 0 20px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 14px;
  box-shadow: var(--shadow);
  overflow: hidden;
}
.cmd-palette-box input {
  width: 100%;
  padding: 16px 20px;
  font-size: 15px;
  background: transparent;
  border: none;
  border-bottom: 1px solid var(--border);
  color: var(--text);
  font-family: var(--sans);
  outline: none;
}
.cmd-palette-list {
  max-height: 320px;
  overflow-y: auto;
  padding: 8px;
}
.cmd-palette-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
  border-radius: var(--r-sm);
  color: var(--text-dim);
  font-size: 13.5px;
  cursor: pointer;
}
.cmd-palette-item:hover {
  background: var(--accent-bg);
  color: var(--accent);
}
.cmd-palette-item .kbd {
  margin-left: auto;
  font-family: var(--mono);
  font-size: 11px;
  padding: 2px 7px;
  border-radius: 5px;
  background: var(--surface-2);
  color: var(--text-mute);
}
</style>
