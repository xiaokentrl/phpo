<script setup lang="ts">
// DockerGate：容器引擎可用性门禁（硬红线 7，非阻断）。
// 探测不可用 → 先弹可关闭提醒（含引擎状态 + 安装引导/修复命令），关闭后顶部常驻横幅；
// 横幅带 × 可关闭，本轮持续掉线不再打扰，直到 ok→掉线的新跃迁才重新弹出。
// 真正的容器操作仍由后端 preflight / 硬红线 7 权威拦截——此组件只做前置提醒，不替代后端裁决。
import { ref, watch, computed } from 'vue'
import ModalShell from '@/components/common/ModalShell.vue'
import { useAppState } from '@/stores/appState'
import { useI18n } from '@/composables/useI18n'
import { refreshDocker } from '@/composables/useDockerPreflight'

const app = useAppState()
const { t } = useI18n()
const dismissed = ref(false) // 弹窗已关 → 展示横幅
const bannerClosed = ref(false) // 横幅已关 → 本轮不再展示
const rechecking = ref(false)
const copiedCmd = ref<string | null>(null)

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

// engineLabel 返回引擎的显示名（未识别返回空，隐藏徽标）；品牌名不翻译
const engineLabel = (): string => {
  if (!app.engine?.kind) return ''
  return app.engine.kind === 'podman' ? 'Podman' : 'Docker'
}

// 主文案优先走前端 i18n（用户语言），后端 message 仅用于提取技术详情：
// 后端 health.go 的 Message 是硬编码中文，不能直接当多语言界面文案使用（§5.25）。
const parsed = computed(() => {
  const main = t('docker.' + app.docker.status) || ''
  const raw = (app.docker.message || '').trim()
  const m = raw.match(/^(.+?[。.!?])\s*[（(](.+?)[）)]\s*$/)
  const detail = m ? m[2].trim() : raw
  return { main, detail }
})

// 未安装状态时不再展示 hint（避免与安装卡片命令重复）
const showHint = computed(() => !!app.docker.hint && app.docker.status !== 'not_installed')

// 安装命令从 i18n 读取：单数据源，中英两侧同步维护
const dockerCmds = computed<string[]>(() => [t('engine.installDockerCmd')])
const podmanCmds = computed<string[]>(() => t('engine.installPodmanCmd').split('\n'))

async function copyCmd(text: string): Promise<void> {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    copiedCmd.value = text
    window.setTimeout(() => {
      if (copiedCmd.value === text) copiedCmd.value = null
    }, 1500)
  } catch {
    /* 剪贴板不可用时静默失败，用户仍可手动选择文本 */
  }
}
</script>

<template>
  <ModalShell v-if="showDialog()" @close="dismissed = true">
    <template #head>
      <h3>{{ t('docker.gateTitle') }}</h3>
      <p>{{ t('docker.gateSubtitle') }}</p>
    </template>

    <div class="gate">
      <!-- ============ 状态区 ============ -->
      <section class="gate-status">
        <div class="gate-status-head">
          <span class="gate-status-pulse" />
          <span class="gate-status-title">{{ parsed.main }}</span>
        </div>

        <div v-if="app.engine?.kind" class="gate-status-sub">
          <span class="gate-status-engine" :class="app.engine.kind">
            {{ app.engine.kind === 'podman' ? '🐘' : '🐳' }}
            {{ engineLabel() }}
          </span>
          <span v-if="app.engine.version" class="gate-status-ver">v{{ app.engine.version }}</span>
          <span v-if="app.engine.rootless" class="gate-status-tag">rootless</span>
        </div>

        <details v-if="parsed.detail" class="gate-detail">
          <summary>{{ t('docker.viewDetail') }}</summary>
          <pre class="gate-detail-pre">{{ parsed.detail }}</pre>
        </details>
      </section>

      <!-- ============ 修复建议（非未安装时展示） ============ -->
      <section v-if="showHint" class="gate-hint">
        <span class="gate-hint-icon">💡</span>
        <span>{{ app.docker.hint }}</span>
      </section>

      <!-- ============ 安装引导 ============ -->
      <section v-if="app.docker.status === 'not_installed'" class="gate-install">
        <div class="gate-install-title">{{ t('engine.installTitle') }}</div>

        <div class="gate-cards">
          <!-- Docker -->
          <div class="gate-card docker">
            <div class="gate-card-head">
              <span class="gate-card-badge">🐳</span>
              <span class="gate-card-name">Docker</span>
            </div>
            <div class="gate-cmds">
              <div v-for="cmd in dockerCmds" :key="cmd" class="gate-cmd">
                <code>{{ cmd }}</code>
                <button
                  class="gate-cmd-copy"
                  :class="{ copied: copiedCmd === cmd }"
                  type="button"
                  :aria-label="copiedCmd === cmd ? t('docker.copied') : t('docker.copyCmd')"
                  @click="copyCmd(cmd)"
                >
                  <svg v-if="copiedCmd !== cmd" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <rect x="9" y="9" width="13" height="13" rx="2" ry="2"/>
                    <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>
                  </svg>
                  <svg v-else width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                    <polyline points="20 6 9 17 4 12"/>
                  </svg>
                </button>
              </div>
            </div>
            <div class="gate-card-foot">
              <a href="https://docs.docker.com/engine/install/" target="_blank" rel="noopener">{{ t('docker.installDoc') }} ↗</a>
            </div>
          </div>

          <!-- Podman -->
          <div class="gate-card podman">
            <div class="gate-card-head">
              <span class="gate-card-badge">🐘</span>
              <span class="gate-card-name">Podman</span>
            </div>
            <div class="gate-cmds">
              <div v-for="cmd in podmanCmds" :key="cmd" class="gate-cmd">
                <code>{{ cmd }}</code>
                <button
                  class="gate-cmd-copy"
                  :class="{ copied: copiedCmd === cmd }"
                  type="button"
                  :aria-label="copiedCmd === cmd ? t('docker.copied') : t('docker.copyCmd')"
                  @click="copyCmd(cmd)"
                >
                  <svg v-if="copiedCmd !== cmd" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <rect x="9" y="9" width="13" height="13" rx="2" ry="2"/>
                    <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>
                  </svg>
                  <svg v-else width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                    <polyline points="20 6 9 17 4 12"/>
                  </svg>
                </button>
              </div>
            </div>
            <div class="gate-card-foot">
              <span>{{ t('docker.podmanNote') }}</span>
            </div>
          </div>
        </div>
      </section>
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
    <span class="gate-banner-text">{{ parsed.main }}</span>
    <button class="btn btn-sm gate-banner-btn" type="button" :disabled="rechecking" @click="recheck">
      {{ rechecking ? t('docker.checking') : t('docker.recheck') }}
    </button>
    <button class="gate-banner-x" type="button" :aria-label="t('docker.close')" @click="bannerClosed = true">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12" /></svg>
    </button>
  </div>
</template>

<style scoped>
/* ============================================================
   设计令牌：显式颜色，不依赖全局 --border 之类的弱色
   ============================================================ */
.gate {
  --g-text: #1a1f2b;
  --g-text-2: #4a5568;
  --g-text-3: #6b7280;
  --g-text-4: #8a93a0;

  --g-line: #e1e5ea;          /* 常规分隔 / 命令块边 */
  --g-line-2: #cdd4dc;        /* 卡片外框（关键：比常规深一档） */

  --g-surface: #ffffff;
  --g-surface-2: #eef1f5;     /* 命令块背景（比之前更深） */

  --g-err: #d94444;
  --g-err-ink: #7a2828;
  --g-err-soft: #fdf3f3;
  --g-err-line: #f0c8c8;

  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 2px 0 4px;
}

/* ---------- 状态区 ---------- */
.gate-status {
  position: relative;
  padding: 12px 14px 12px 17px;
  border-radius: 8px;
  background: var(--g-err-soft);
  border: 1px solid var(--g-err-line);
  overflow: hidden;
}
.gate-status::before {
  content: '';
  position: absolute;
  inset: 0 auto 0 0;
  width: 3px;
  background: var(--g-err);
}
.gate-status-head {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}
.gate-status-pulse {
  flex: none;
  margin-top: 6px;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--g-err);
  box-shadow: 0 0 0 3px rgba(217, 68, 68, 0.18);
  animation: gate-pulse 1.6s ease-in-out infinite;
}
.gate-status-title {
  flex: 1;
  min-width: 0;
  font-size: 13.5px;
  font-weight: 600;
  line-height: 1.5;
  color: var(--g-err-ink);
  word-break: break-word;
}
.gate-status-sub {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
  padding-left: 16px;
  font-size: 12px;
  color: #8a5a5a;
}
.gate-status-engine {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-weight: 500;
}
.gate-status-engine.docker { color: #1a7bc4; }
.gate-status-engine.podman { color: #7c4fe0; }
.gate-status-ver { font-family: var(--mono); font-size: 11.5px; }
.gate-status-tag {
  font-size: 10.5px;
  padding: 1px 6px;
  border-radius: 4px;
  background: rgba(124, 79, 224, 0.12);
  color: #7c4fe0;
  font-family: var(--mono);
}

/* ---------- 技术详情折叠 ---------- */
.gate-detail {
  margin-top: 8px;
  margin-left: 16px;
  font-size: 12px;
}
.gate-detail > summary {
  cursor: pointer;
  color: #a06868;
  list-style: none;
  user-select: none;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 0;
}
.gate-detail > summary::before {
  content: '▸';
  font-size: 10px;
  transition: transform 0.15s ease;
}
.gate-detail[open] > summary::before { transform: rotate(90deg); }
.gate-detail > summary::-webkit-details-marker { display: none; }
.gate-detail > summary:hover { color: var(--g-err); }
.gate-detail-pre {
  margin: 8px 0 0;
  padding: 10px 12px;
  border-radius: 6px;
  background: #ffffff;
  border: 1px solid var(--g-err-line);
  font-family: var(--mono);
  font-size: 11px;
  line-height: 1.55;
  color: var(--g-text-2);
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 140px;
  overflow: auto;
}

/* ---------- 修复建议 ---------- */
.gate-hint {
  display: flex;
  gap: 8px;
  padding: 10px 12px;
  border-radius: 8px;
  background: var(--g-surface-2);
  border: 1px solid var(--g-line);
  font-size: 12.5px;
  line-height: 1.6;
  color: var(--g-text-2);
  word-break: break-word;
}
.gate-hint-icon { flex: none; }

/* ---------- 安装引导 ---------- */
.gate-install {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-top: 2px;
}
/* 关键：缩进 13px，与卡片内 padding 对齐 —— 标题与卡片内容同一条竖线 */
.gate-install-title {
  padding-left: 13px;
  font-size: 12px;
  font-weight: 600;
  color: var(--g-text-3);
  letter-spacing: 0.02em;
}

.gate-cards {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
  align-items: stretch;
}

.gate-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 13px;
  border-radius: 9px;
  background: var(--g-surface);
  border: 1px solid var(--g-line-2);
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
  min-width: 0;
}
.gate-card.docker:hover {
  border-color: #2496ed;
  box-shadow: 0 0 0 3px rgba(36, 150, 237, 0.1);
}
.gate-card.podman:hover {
  border-color: #9461f4;
  box-shadow: 0 0 0 3px rgba(148, 97, 244, 0.1);
}

.gate-card-head {
  display: flex;
  align-items: center;
  gap: 8px;
}
.gate-card-badge {
  flex: none;
  width: 28px;
  height: 28px;
  border-radius: 7px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 15px;
  background: var(--g-surface-2);
  border: 1px solid var(--g-line);
}
.gate-card.docker .gate-card-badge {
  background: rgba(36, 150, 237, 0.1);
  border-color: rgba(36, 150, 237, 0.3);
}
.gate-card.podman .gate-card-badge {
  background: rgba(148, 97, 244, 0.1);
  border-color: rgba(148, 97, 244, 0.3);
}
.gate-card-name {
  font-weight: 600;
  font-size: 13.5px;
  color: var(--g-text);
}

/* 命令列表容器 */
.gate-cmds {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

/* 命令块：flex 横排 —— code 占满，右侧复制按钮 */
.gate-cmd {
  position: relative;
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
  padding: 6px 6px 6px 11px;
  border-radius: 5px;
  background: var(--g-surface-2);
  border: 1px solid var(--g-line);
}
.gate-cmd::before {
  content: '';
  position: absolute;
  inset: 6px auto 6px 0;
  width: 2px;
  border-radius: 2px;
  background: var(--g-line-2);
}
.gate-cmd code {
  flex: 1;
  min-width: 0;
  display: block;
  font-family: var(--mono);
  font-size: 11.5px;
  line-height: 1.5;
  letter-spacing: -0.01em;
  color: var(--g-text);
  word-break: break-word;
  overflow-wrap: anywhere;
  user-select: all;
}

/* 复制按钮 */
.gate-cmd-copy {
  flex: none;
  width: 22px;
  height: 22px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid transparent;
  border-radius: 4px;
  background: transparent;
  color: var(--g-text-4);
  cursor: pointer;
  transition: background 0.15s ease, color 0.15s ease, border-color 0.15s ease;
}
.gate-cmd-copy:hover {
  background: var(--g-surface);
  border-color: var(--g-line-2);
  color: var(--g-text-2);
}
.gate-cmd-copy:active {
  background: var(--g-line);
}
.gate-cmd-copy.copied {
  color: #2ba471;
}
.gate-cmd-copy.copied:hover {
  background: rgba(43, 164, 113, 0.08);
  border-color: rgba(43, 164, 113, 0.25);
  color: #2ba471;
}

.gate-card-foot {
  margin-top: auto;
  padding-top: 2px;
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--g-text-4);
}
.gate-card-foot a {
  color: #2496ed;
  text-decoration: none;
}
.gate-card-foot a:hover { text-decoration: underline; }

/* ============================================================
   顶部横幅
   ============================================================ */
.gate-banner {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 14px 9px 16px;
  background: var(--g-err-soft, #fdf3f3);
  color: var(--g-err-ink, #7a2828);
  border-bottom: 1px solid var(--g-err-line, #f0c8c8);
  font-size: 13px;
}
.gate-banner-dot {
  flex: none;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--g-err, #d94444);
  box-shadow: 0 0 0 3px rgba(217, 68, 68, 0.18);
  animation: gate-pulse 1.4s ease-in-out infinite;
}
.gate-banner-text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 500;
}
.gate-banner-btn { flex: none; }
.gate-banner-x {
  flex: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: inherit;
  cursor: pointer;
  transition: background 0.15s ease;
}
.gate-banner-x:hover { background: rgba(217, 68, 68, 0.15); }

@keyframes gate-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

/* ============================================================
   窄屏降级：安装卡片改单列
   ============================================================ */
@media (max-width: 520px) {
  .gate-cards { grid-template-columns: 1fr; }
}
</style>