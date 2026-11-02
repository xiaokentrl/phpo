<script setup lang="ts">
// 根布局壳：侧栏导航（对齐原型 NAV 三段分组）+ 视图出口
// T102 10 路由；T103 i18n；T104 主题应用+持久化；T110 事件订阅启动
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from '@/composables/useI18n'
import { refreshServices, runSync, startStateSync, stopStateSync, bootstrapState } from '@/composables/useStateSync'
import { subscribeUpdater, unsubscribeUpdater } from '@/composables/useUpdater'
import { subscribeCache, unsubscribeCache } from '@/composables/useCache'
import { startDockerPreflight, stopDockerPreflight } from '@/composables/useDockerPreflight'
import { useAppState } from '@/stores/appState'
import { hasBackend, waitForBackend } from '@/api/site'
import { useLayoutStore } from '@/stores/layoutStore'
import { usePrefsStore } from '@/stores/prefsStore'
import { useModals } from '@/composables/useModals'
import { toast } from '@/composables/useToast'
import ModalRoot from '@/components/common/ModalRoot.vue'
import ToastHost from '@/components/common/ToastHost.vue'
import CmdPalette from '@/components/business/CmdPalette.vue'
import TaskDrawer from '@/components/business/TaskDrawer.vue'
import AppTrayMenu from '@/components/business/AppTrayMenu.vue'
import DockerGate from '@/components/business/DockerGate.vue'
import UpdateBadge from '@/components/business/UpdateBadge.vue'

const { t } = useI18n()
const app = useAppState()
const { openHomeSetupWizard } = useModals()
const layout = useLayoutStore() // 实例化即应用 --sidebar-width / --ui-scale 与 documentElement.zoom
const prefs = usePrefsStore()
const router = useRouter()
let stopRouteWatch: (() => void) | null = null

onMounted(async () => {
  // 先等宿主 Core 就绪（有界超时）：window._wails 的宿主字段在 WindowLoadFinished 才注入，
  // 可能晚于本挂载钩子。未就绪时 hasBackend 误判为 false 会走 demo、跳过真实 Docker 探测。
  await waitForBackend()
  // 宿主已确认：store 建立早于 window._wails 注入时会误判 demo 铺下占位数据，此处一次性收掉再等快照
  if (hasBackend()) app.enterRealHost()
  await prefs.refreshTrayStatus()
  prefs.syncTrayPrefs() // 宿主就绪后才把托盘偏好投影到原生外壳；此后勾选变化由 store 自动跟随
  startStateSync() // 订阅后端 §5.6 全量事件；此后状态变化只来自事件落地
  subscribeUpdater() // 订阅 update:* 事件：后台发现新版本即时提示（硬红线 4）
  subscribeCache() // 订阅 6 类 cache:* 事件：缓存命中/未命中/提升/损坏/清理/临时目录清空落地 cacheStore
  await bootstrapState() // 启动权威快照（T607/硬红线 4）：dirReady/env 以 config.yaml + DB 为准；后端未就绪时有界重试，不再把首屏留在占位态（demo 无宿主单次即返）
  // 首启引导：真实宿主下主目录/网站目录未初始化 → 弹出装机向导。可关闭（右上 X），
  // 关闭后任何写操作仍由后端 preflight 权威拦截（目录未就绪报错），不放水；再次触发安装/建站会重新弹出。
  if (hasBackend() && !app.homeReady) openHomeSetupWizard()
  startDockerPreflight() // 首启探测 + 定时轮询 Docker 可用性（硬红线 7 门禁，两段式引导）；同一拍顺带轻量刷新服务清单
  // 切菜单、窗口重新拿到焦点：各做一次轻量刷新，让服务页对着「Docker 上此刻实际有什么」，
  // 而不是等用户去点「同步状态」。没变化时后端不发事件，界面不会动。
  window.addEventListener('focus', refreshServices)
  stopRouteWatch = router.afterEach(() => void refreshServices())
  if (import.meta.env.DEV) await import('@/api/mockEvents') // 开发期 mock 发射驱动（构建产物不含）
})
onBeforeUnmount(() => {
  stopStateSync()
  unsubscribeUpdater()
  unsubscribeCache()
  stopDockerPreflight()
  window.removeEventListener('focus', refreshServices)
  stopRouteWatch?.()
  stopRouteWatch = null
})

const sections: { titleKey: string; items: { id: string; labelKey: string; icon: string }[] }[] = [
  { titleKey: 'nav.business', items: [{ id: 'sites', labelKey: 'nav.sites', icon: '🔗' }] },
  {
    titleKey: 'nav.services',
    items: [
      { id: 'php', labelKey: 'nav.php', icon: '🐘' },
      { id: 'mysql', labelKey: 'nav.mysql', icon: '🐬' },
      { id: 'pgsql', labelKey: 'nav.pgsql', icon: '🐘' },
      { id: 'redis', labelKey: 'nav.redis', icon: '⚡' },
      { id: 'nginx', labelKey: 'nav.nginx', icon: '🌐' },
    ],
  },
  {
    titleKey: 'nav.ops',
    items: [
      { id: 'backup', labelKey: 'nav.backup', icon: '📦' },
      { id: 'offline', labelKey: 'nav.offline', icon: '🗄️' },
      { id: 'overview', labelKey: 'nav.overview', icon: '◈' },
    ],
  },
]

// resync：手动「同步状态」= 全量口径（§5.19）——容器 / 基座镜像 / 扩展固化镜像三类缺失一次核清，
// 缺失逐行进抽屉日志并随快照 gaps 回流；复用公共 runSync 入口（硬红线 4：值只来自后端）
async function resync(): Promise<void> {
  await runSync()
  toast(t('common.refresh'), 'ok', 1400)
}

// 侧栏自由拖宽：按住右缘手柄横向拖，setSidebar 自带 64–380px 夹取，watchEffect 落 CSS 变量并持久化。
// pointer capture 让拖出窗口也不丢事件；拖拽期间给 body 加类锁光标和文本选中，避免拖出一片蓝。
const resizing = ref(false)
function onResizeDown(e: PointerEvent): void {
  e.preventDefault()
  resizing.value = true
  ;(e.currentTarget as HTMLElement).dataset.startX = String(e.clientX)
  ;(e.currentTarget as HTMLElement).dataset.startW = String(layout.sidebarWidth)
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  document.body.classList.add('sidebar-resizing')
}
function onResizeMove(e: PointerEvent): void {
  if (!resizing.value) return
  const el = e.currentTarget as HTMLElement
  layout.setSidebar(Number(el.dataset.startW) + (e.clientX - Number(el.dataset.startX)))
}
function onResizeUp(e: PointerEvent): void {
  if (!resizing.value) return
  resizing.value = false
  ;(e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId)
  document.body.classList.remove('sidebar-resizing')
}
</script>

<template>
  <div class="app-shell">
    <div class="app-sidebar-wrap">
      <aside class="app-sidebar">
        <div class="app-brand">phpo</div>
        <UpdateBadge />
        <nav>
          <section v-for="s in sections" :key="s.titleKey">
            <h3 class="nav-section">{{ t(s.titleKey) }}</h3>
            <RouterLink v-for="it in s.items" :key="it.id" :to="{ name: it.id }" class="nav-item">
              <span class="nav-icon">{{ it.icon }}</span>
              <span>{{ t(it.labelKey) }}</span>
            </RouterLink>
          </section>
        </nav>
        <div class="sidebar-foot">
          <button class="btn-ghost foot-btn" type="button" id="refresh-btn" @click="resync">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-2.64-6.36M21 3v6h-6"/></svg>
            <span>{{ t('common.refresh') }}</span>
          </button>
          <RouterLink class="btn-ghost foot-btn" id="settings-btn" :to="{ name: 'settings' }">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
            <span>{{ t('nav.settings') }}</span>
          </RouterLink>
        </div>
      </aside>
      <div
        class="sidebar-resizer"
        :class="{ active: resizing }"
        role="separator"
        aria-orientation="vertical"
        @pointerdown="onResizeDown"
        @pointermove="onResizeMove"
        @pointerup="onResizeUp"
        @pointercancel="onResizeUp"
      />
    </div>
    <main class="app-main">
      <DockerGate />
      <div class="view">
        <RouterView />
      </div>
      <TaskDrawer />
    </main>
    <ModalRoot />
    <ToastHost />
    <CmdPalette />
    <AppTrayMenu />
  </div>
</template>

<style scoped>
.app-shell {
  display: flex;
  height: 100vh;
  width: 100vw;
  background: var(--bg);
  color: var(--text);
}
/* 侧栏外壳：宽度挂在 wrap 上，滚动放进内层 aside——拖宽手柄贴在 wrap 右缘，不随内容滚走 */
.app-sidebar-wrap {
  position: relative;
  width: var(--sidebar-width, 200px);
  flex: none;
  height: 100%;
}
.app-sidebar {
  height: 100%;
  background: var(--surface);
  border-right: 1px solid var(--border);
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}
/* 拖宽手柄：骑在边框上（左 -3px 起 7px 宽），悬停/拖拽中亮起 */
.sidebar-resizer {
  position: absolute;
  top: 0;
  bottom: 0;
  right: -3px;
  width: 7px;
  cursor: col-resize;
  z-index: 20;
  touch-action: none;
}
.sidebar-resizer:hover,
.sidebar-resizer.active {
  background: var(--accent-bg);
}
/* 拖拽期间锁光标与选中（scoped 下打 :global 落到 body） */
:global(body.sidebar-resizing) {
  cursor: col-resize;
  user-select: none;
}
.app-brand {
  font-weight: 700;
  padding: 12px 16px;
}
.app-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  background: var(--bg);
}
.app-main .view {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 16px 24px;
}
.nav-section {
  font-size: 11px;
  text-transform: uppercase;
  color: var(--text-mute);
  padding: 12px 16px 4px;
  margin: 0;
}
.nav-item {
  display: flex;
  gap: 8px;
  align-items: center;
  padding: 8px 16px;
  text-decoration: none;
  color: var(--text-dim);
}
.nav-item.router-link-active {
  background: var(--accent-bg);
  color: var(--accent);
  font-weight: 600;
}
/* 底部两颗左右平铺、随侧栏宽度自适应：各占一半，窄到极限时文字省略不换行 */
.sidebar-foot {
  margin-top: auto;
  padding: 12px 10px;
  display: flex;
  gap: 6px;
}
.foot-btn {
  flex: 1;
  min-width: 0;
  display: flex;
  gap: 6px;
  align-items: center;
  justify-content: center;
  padding: 8px 6px;
}
.foot-btn span {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
</style>
