<script setup lang="ts">
// 根布局壳：侧栏导航（对齐原型 NAV 三段分组）+ 视图出口
// T102 10 路由；T103 i18n；T104 主题应用+持久化；T110 事件订阅启动
import { onBeforeUnmount, onMounted } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { startStateSync, stopStateSync } from '@/composables/useStateSync'
import { usePrefsStore } from '@/stores/prefsStore'
import { useLayoutStore } from '@/stores/layoutStore'
import ModalRoot from '@/components/common/ModalRoot.vue'
import ToastHost from '@/components/common/ToastHost.vue'
import CmdPalette from '@/components/business/CmdPalette.vue'
import TaskDrawer from '@/components/business/TaskDrawer.vue'
import AppTrayMenu from '@/components/business/AppTrayMenu.vue'

const { locale, t, setLocale } = useI18n()
const prefs = usePrefsStore()
useLayoutStore() // 实例化即应用 --sidebar-width / --ui-scale 与 documentElement.zoom

onMounted(async () => {
  startStateSync() // 订阅后端 §5.6 全量事件；此后状态变化只来自事件落地
  if (import.meta.env.DEV) await import('@/api/mockEvents') // 开发期 mock 发射驱动（构建产物不含）
})
onBeforeUnmount(() => stopStateSync())

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

function toggleLang() {
  setLocale(locale.value === 'zh-CN' ? 'en-US' : 'zh-CN')
}
</script>

<template>
  <div class="app-shell">
    <aside class="app-sidebar">
      <div class="app-brand">phpo</div>
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
        <label class="pref-row">
          <span>{{ t('settings.appearance') }}</span>
          <select :value="prefs.theme" @change="prefs.setTheme(($event.target as HTMLSelectElement).value as any)">
            <option v-for="id in prefs.THEME_IDS" :key="id" :value="id">{{ t('theme.' + id) }}</option>
          </select>
        </label>
        <button class="lang-toggle" @click="toggleLang">
          {{ locale === 'zh-CN' ? 'English' : '中文' }}
        </button>
      </div>
    </aside>
    <main class="app-main">
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
.pref-row {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  color: var(--text-dim);
}
select {
  background: var(--surface-2);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 4px 6px;
}
.lang-toggle {
  padding: 6px 10px;
}
</style>
