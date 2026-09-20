<script setup lang="ts">
// DockerGate：首启 Docker 可用性两段式门禁（硬红线 7）。
// 第一段：探测不可用时弹出不可关闭引导（locked，无 X/ESC/遮罩关闭），提供「重新检测」与「稍后再说」。
// 第二段：「稍后再说」后转为常驻红色横幅，持续提示直到 Docker 就绪；就绪后自动收起。
// 写操作在门禁期仍由后端 preflight/硬红线 7 权威拦截（此组件只做前置引导，不替代后端裁决）。
import { ref, watch } from 'vue'
import ModalShell from '@/components/common/ModalShell.vue'
import { useAppState } from '@/stores/appState'
import { useI18n } from '@/composables/useI18n'
import { refreshDocker } from '@/composables/useDockerPreflight'

const app = useAppState()
const { t } = useI18n()
const dismissed = ref(false)
const rechecking = ref(false)

// Docker 就绪后复位「稍后再说」，下次掉线重新引导
watch(() => app.docker.canStart, (ok) => { if (ok) dismissed.value = false })

const blocked = () => app.docker.checked && !app.docker.canStart
// 不可关闭引导仅在装机向导（同为不可关闭硬门禁）已就绪后弹出，避免两层遮罩堆叠；
// 未就绪时以常驻横幅提示，向导完成后轮询会自动升级为首屏引导。
const showDialog = () => blocked() && app.dirReady.PHPO_HOME && !dismissed.value

async function recheck(): Promise<void> {
  if (rechecking.value) return
  rechecking.value = true
  await refreshDocker()
  rechecking.value = false
}
</script>

<template>
  <ModalShell v-if="showDialog()" locked>
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
      <button class="btn" type="button" @click="dismissed = true">{{ t('docker.later') }}</button>
      <button class="btn btn-primary" type="button" :disabled="rechecking" @click="recheck">{{ rechecking ? t('docker.checking') : t('docker.recheck') }}</button>
    </template>
  </ModalShell>

  <div v-else-if="blocked()" class="docker-banner">
    <span class="docker-banner-dot" />
    <span class="docker-banner-text">🐳 {{ app.docker.message || t('docker.' + app.docker.status) }}<template v-if="app.docker.hint"> · {{ app.docker.hint }}</template></span>
    <button class="btn btn-sm" type="button" :disabled="rechecking" @click="recheck">{{ rechecking ? t('docker.checking') : t('docker.recheck') }}</button>
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
@keyframes docker-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
}
</style>
