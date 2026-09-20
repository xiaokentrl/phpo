<script setup lang="ts">
// DockerGate：首启 Docker 可用性提醒（硬红线 7，非阻断）。
// 探测不可用时：先弹「可关闭」提醒弹窗（×/ESC/遮罩/知道了 均可关），关闭后顶部常驻红色横幅；
// 横幅带 × 可关闭。关闭后本轮持续掉线不再打扰，直到检测到 ok→掉线的新跃迁才重新弹出。
// 真正容器操作仍由后端 preflight/硬红线 7 权威拦截报错——此组件只做前置提醒，不替代后端裁决。
import { ref, watch } from 'vue'
import ModalShell from '@/components/common/ModalShell.vue'
import { useAppState } from '@/stores/appState'
import { useI18n } from '@/composables/useI18n'
import { refreshDocker } from '@/composables/useDockerPreflight'

const app = useAppState()
const { t } = useI18n()
const dismissed = ref(false) // 弹窗已关 → 展示横幅
const bannerClosed = ref(false) // 横幅已关 → 本轮不再展示
const rechecking = ref(false)

// 装机向导同为不可关闭硬门禁；目录未就绪时让向导占屏，Docker 提醒延后到目录就绪
const active = () => app.docker.checked && !app.docker.canStart && app.homeReady
const showDialog = () => active() && !dismissed.value
const showBanner = () => active() && dismissed.value && !bannerClosed.value

// 掉线态上升沿（ok/未探测 → 掉线）：重置关闭状态，重新走「弹窗 → 横幅」提醒
watch(active, (on) => { if (on) { dismissed.value = false; bannerClosed.value = false } })

async function recheck(): Promise<void> {
  if (rechecking.value) return
  rechecking.value = true
  await refreshDocker()
  rechecking.value = false
}
</script>

<template>
  <ModalShell v-if="showDialog()" @close="dismissed = true">
    <template #head>
      <h3>{{ t('docker.gateTitle') }}</h3>
      <p>{{ t('docker.gateSubtitle') }}</p>
    </template>
    <div class="docker-gate-body">
      <div class="docker-gate-icon">🐳</div>
      <div class="docker-gate-msg">{{ app.docker.message || t('docker.' + app.docker.status) }}</div>
      <div v-if="app.docker.hint" class="docker-gate-hint">{{ app.docker.hint }}</div>
    </div>
    <template #foot>
      <button class="btn" type="button" @click="dismissed = true">{{ t('docker.gotIt') }}</button>
      <button class="btn btn-primary" type="button" :disabled="rechecking" @click="recheck">{{ rechecking ? t('docker.checking') : t('docker.recheck') }}</button>
    </template>
  </ModalShell>

  <div v-else-if="showBanner()" class="docker-banner">
    <span class="docker-banner-dot" />
    <span class="docker-banner-text">🐳 {{ app.docker.message || t('docker.' + app.docker.status) }}<template v-if="app.docker.hint"> · {{ app.docker.hint }}</template></span>
    <button class="btn btn-sm" type="button" :disabled="rechecking" @click="recheck">{{ rechecking ? t('docker.checking') : t('docker.recheck') }}</button>
    <button class="docker-banner-x" type="button" :aria-label="t('docker.close')" @click="bannerClosed = true">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12" /></svg>
    </button>
  </div>
</template>

<style scoped>
.docker-gate-body {
  padding: 8px 4px;
  text-align: center;
}
.docker-gate-icon {
  font-size: 40px;
  line-height: 1;
}
.docker-gate-msg {
  margin-top: 12px;
  font-weight: 600;
}
.docker-gate-hint {
  margin-top: 8px;
  color: var(--text-dim);
  font-size: 13px;
  word-break: break-all;
}
.docker-banner {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 16px;
  background: var(--err-bg, rgba(224, 82, 82, 0.14));
  color: var(--err, #e05252);
  border-bottom: 1px solid var(--err, #e05252);
  font-size: 13px;
}
.docker-banner-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--err, #e05252);
  flex: none;
  animation: docker-pulse 1.4s ease-in-out infinite;
}
.docker-banner-text {
  flex: 1;
  min-width: 0;
}
.docker-banner-x {
  flex: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: inherit;
  cursor: pointer;
}
.docker-banner-x:hover {
  background: rgba(224, 82, 82, 0.22);
}
@keyframes docker-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
}
</style>
