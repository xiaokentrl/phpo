<script setup lang="ts">
// 根布局壳：侧栏导航（对齐原型 NAV 三段分组）+ 视图出口
// T102 10 路由；T103 i18n；T104 主题应用+持久化；T110 事件订阅启动
import { onBeforeUnmount, onMounted } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { runSync, startStateSync, stopStateSync, syncState } from '@/composables/useStateSync'
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
const { openThemePicker, openHomeSetupWizard } = useModals()
useLayoutStore() // 实例化即应用 --sidebar-width / --ui-scale 与 documentElement.zoom
const prefs = usePrefsStore()

onMounted(async () => {
  // 先等宿主 Core 就绪（有界超时）：window._wails 的宿主字段在 WindowLoadFinished 才注入，
  // 可能晚于本挂载钩子。未就绪时 hasBackend 误判为 false 会走 demo、跳过真实 Docker 探测。
  await waitForBackend()
  // 宿主已确认：store 建立早于 window._wails 注入时会误判 demo 铺下占位数据，此处一次性收掉再等快照
  if (hasBackend()) app.enterRealHost()
  prefs.syncTrayPrefs() // 宿主就绪后才把托盘偏好投影到原生外壳；此后勾选变化由 store 自动跟随
  startStateSync() // 订阅后端 §5.6 全量事件；此后状态变化只来自事件落地
  subscribeUpdater() // 订阅 update:* 事件：后台发现新版本即时提示（硬红线 4）
  subscribeCache() // 订阅 6 类 cache:* 事件：缓存命中/未命中/提升/损坏/清理/临时目录清空落地 cacheStore
  await syncState() // 启动权威快照（T607/硬红线 4）：dirReady/env 以 config.yaml + DB 为准；无宿主则不动占位值
  // 首启引导：真实宿主下主目录/网站目录未初始化 → 弹出装机向导。可关闭（右上 X），
  // 关闭后任何写操作仍由后端 preflight 权威拦截（目录未就绪报错），不放水；再次触发安装/建站会重新弹出。
  if (hasBackend() && !app.homeReady) openHomeSetupWizard()
  startDockerPreflight() // 首启探测 + 定时轮询 Docker 可用性（硬红线 7 门禁，两段式引导）
  if (import.meta.env.DEV) await import('@/api/mockEvents') // 开发期 mock 发射驱动（构建产物不含）
})
onBeforeUnmount(() => {
  stopStateSync()
  unsubscribeUpdater()
  unsubscribeCache()
  stopDockerPreflight()
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
      { id: 'settings', labelKey: 'nav.settings', icon: '⚙️' },
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
</script>

<template>
  <div class="app-shell">
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
        <button class="btn-ghost" type="button" id="refresh-btn" @click="resync">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-2.64-6.36M21 3v6h-6"/></svg>
          <span>{{ t('common.refresh') }}</span>
        </button>
        <button class="btn-ghost" type="button" id="theme-btn" @click="openThemePicker">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>
          <span>{{ t('common.theme') }}</span>
        </button>
      </div>
    </aside>
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
.app-sidebar {
  width: var(--sidebar-width, 200px);
  flex: none;
  background: var(--surface);
  border-right: 1px solid var(--border);
  overflow-y: auto;
  display: flex;
  flex-direction: column;
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
.sidebar-foot {
  margin-top: auto;
  padding: 12px 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
</style>
