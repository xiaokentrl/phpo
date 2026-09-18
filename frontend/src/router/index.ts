// 路由表：10 条对齐原型 NAV（sites/php/mysql/pgsql/redis/nginx/backup/offline/settings/overview）
// §0.3 权威值：路由数 = 10。桌面应用用 hash 模式，避免 file:// 下 history 路径问题。
import { createRouter, createWebHashHistory, type RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  { path: '/', redirect: '/overview' },
  { path: '/sites', name: 'sites', component: () => import('@/views/SitesView.vue'), meta: { icon: '🔗' } },
  { path: '/php', name: 'php', component: () => import('@/views/PhpView.vue'), meta: { icon: '🐘' } },
  { path: '/mysql', name: 'mysql', component: () => import('@/views/MysqlView.vue'), meta: { icon: '🐬' } },
  { path: '/pgsql', name: 'pgsql', component: () => import('@/views/PgsqlView.vue'), meta: { icon: '🐘' } },
  { path: '/redis', name: 'redis', component: () => import('@/views/RedisView.vue'), meta: { icon: '⚡' } },
  { path: '/nginx', name: 'nginx', component: () => import('@/views/NginxView.vue'), meta: { icon: '🌐' } },
  { path: '/backup', name: 'backup', component: () => import('@/views/BackupView.vue'), meta: { icon: '📦' } },
  { path: '/offline', name: 'offline', component: () => import('@/views/OfflineView.vue'), meta: { icon: '🗄️' } },
  { path: '/settings', name: 'settings', component: () => import('@/views/SettingsView.vue'), meta: { icon: '⚙️' } },
  { path: '/overview', name: 'overview', component: () => import('@/views/OverviewView.vue'), meta: { icon: '◈' } },
]

export const router = createRouter({
  history: createWebHashHistory(),
  routes,
})
