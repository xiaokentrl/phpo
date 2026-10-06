<script setup lang="ts">
// DockerGate：容器引擎可用性门禁（硬红线 7，非阻断）。
// 探测不可用 → 先弹可关闭提醒（含引擎状态 + 安装引导/修复命令），关闭后顶部常驻横幅；
// 横幅带 × 可关闭，本轮持续掉线不再打扰，直到 ok→掉线的新跃迁才重新弹出。
// 真正的容器操作仍由后端 preflight / 硬红线 7 权威拦截——此组件只做前置提醒，不替代后端裁决。
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

// 装机向导同为不可关闭硬门禁；目录未就绪时让向导占屏，引擎提醒延后到目录就绪
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

// engineLabel 返回引擎的显示名（未识别返回空，隐藏徽标）
const engineLabel = (): string => {
  if (!app.engine?.kind) return ''
  return app.engine.kind === 'podman' ? 'Podman' : 'Docker'
}
</script>

<template>
  <ModalShell v-if="showDialog()" @close="dismissed = true">
    <template #head>
      <h3>{{ t('docker.gateTitle') }}</h3>
      <p>{{ t('docker.gateSubtitle') }}</p>
    </template>
    <div class="gate-body">
      <!-- 引擎徽标：已识别的引擎及其运行模式 -->
      <div v-if="app.engine?.kind" class="gate-engine">
        <span class="gate-engine-dot" :class="app.engine.kind" />
        <span class="gate-engine-name">{{ engineLabel() }}</span>
        <template v-if="app.engine.version"><span class="gate-engine-ver">{{ app.engine.version }}</span></template>
        <span v-if="app.engine.rootless" class="gate-engine-tag">rootless</span>
      </div>

      <!-- 状态消息 + 建议 -->
      <div class="gate-msg">{{ app.docker.message || t('docker.' + app.docker.status) }}</div>
      <div v-if="app.docker.hint" class="gate-hint">{{ app.docker.hint }}</div>

      <!-- 双列安装引导（仅未安装时显示） -->
      <div v-if="app.docker.status === 'not_installed'" class="gate-install">
        <div class="gate-install-title">{{ t('engine.installTitle') }}</div>
        <div class="gate-cards">
          <div class="gate-card">
            <div class="gate-card-head"><span class="gate-card-icon">🐳</span> Docker</div>
            <code>sudo apt install docker.io</code>
            <div class="gate-card-note">{{ t('docker.cardDockerNote') }}</div>
          </div>
          <div class="gate-card">
            <div class="gate-card-head"><span class="gate-card-icon">🐘</span> Podman</div>
            <code>sudo apt install podman</code>
            <code>systemctl --user enable --now podman.socket</code>
            <div class="gate-card-note">{{ t('docker.cardPodmanNote') }}</div>
          </div>
        </div>
      </div>
    </div>
    <template #foot>
      <button class="btn" type="button" @click="dismissed = true">{{ t('docker.gotIt') }}</button>
      <button class="btn btn-primary" type="button" :disabled="rechecking" @click="recheck">
        {{ rechecking ? t('docker.checking') : t('docker.recheck') }}
      </button>
    </template>
  </ModalShell>

  <div v-else-if="showBanner()" class="gate-banner">
    <span class="gate-banner-dot" />
    <span class="gate-banner-text">{{ app.docker.message || t('docker.' + app.docker.status) }}</span>
    <button class="btn btn-sm gate-banner-btn" type="button" :disabled="rechecking" @click="recheck">
      {{ rechecking ? t('docker.checking') : t('docker.recheck') }}
    </button>
    <button class="gate-banner-x" type="button" :aria-label="t('docker.close')" @click="bannerClosed = true">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12" /></svg>
    </button>
  </div>
</template>

<style scoped>
/* ---- 弹窗 ---- */
.gate-body {
  padding: 4px 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.gate-engine {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  border-radius: var(--r-sm);
  background: var(--surface-2);
  border: 1px solid var(--border);
  font-size: 13px;
}
.gate-engine-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex: none;
}
.gate-engine-dot.podman { background: #9461f4; }
.gate-engine-dot.docker { background: #2496ed; }
.gate-engine-name { font-weight: 600; }
.gate-engine-ver { color: var(--text-mute); font-family: var(--mono); font-size: 12px; }
.gate-engine-tag {
  font-size: 10.5px;
  padding: 1px 6px;
  border-radius: 3px;
  background: var(--accent-bg);
  color: var(--accent);
  font-family: var(--mono);
}
.gate-msg {
  font-weight: 600;
  font-size: 14px;
}
.gate-hint {
  color: var(--text-dim);
  font-size: 13px;
  line-height: 1.6;
  word-break: break-all;
}
/* ---- 双列安装引导 ---- */
.gate-install {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.gate-install-title {
  font-size: 12.5px;
  color: var(--text-dim);
}
.gate-cards {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.gate-card {
  border: 1px solid var(--border);
  border-radius: var(--r);
  padding: 14px;
  background: var(--bg);
  display: flex;
  flex-direction: column;
  gap: 6px;
  transition: border-color var(--motion-fast);
}
.gate-card:hover { border-color: var(--accent); }
.gate-card-head {
  font-weight: 600;
  font-size: 13.5px;
  display: flex;
  align-items: center;
  gap: 6px;
}
.gate-card-icon { font-size: 16px; }
.gate-card code {
  display: block;
  font-family: var(--mono);
  font-size: 11.5px;
  line-height: 1.55;
  white-space: pre-line;
  word-break: break-all;
  color: var(--text-dim);
  background: var(--surface-2);
  border-radius: 4px;
  padding: 6px 8px;
}
.gate-card-note {
  font-size: 11px;
  color: var(--text-mute);
}
/* ---- 横幅 ---- */
.gate-banner {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 16px;
  background: var(--err-bg, rgba(224, 82, 82, 0.14));
  color: var(--err, #e05252);
  border-bottom: 1px solid var(--err, #e05252);
  font-size: 13px;
}
.gate-banner-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--err, #e05252);
  flex: none;
  animation: gate-pulse 1.4s ease-in-out infinite;
}
.gate-banner-text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.gate-banner-btn { flex: none; }
.gate-banner-x {
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
.gate-banner-x:hover { background: rgba(224, 82, 82, 0.22); }
@keyframes gate-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
}
</style>
