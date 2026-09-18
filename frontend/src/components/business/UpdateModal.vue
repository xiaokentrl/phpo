<script setup lang="ts">
// 升级弹窗（T604 / §5.9）：应用版本检查 + 三段式升级（下载→SHA256+Ed25519 双校验→备份→安装，失败自动回滚）。
// 硬红线 4/6：可用版本/进度/结果全部来自后端 update:* 事件（App.vue 已全局订阅）；双校验缺一即拒绝安装。
import { computed, onMounted, ref } from 'vue'
import ModalShell from '@/components/common/ModalShell.vue'
import { useI18n } from '@/composables/useI18n'
import { useUpdater } from '@/composables/useUpdater'
import { currentVersion } from '@/api/updater'
import type { UpdateStage } from '@/types'

const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { store, percent, stage, busy, check, apply } = useUpdater()
const cur = ref('')

onMounted(async () => {
  cur.value = await currentVersion()
  if (!store.available) check()
})

const sizeLabel = computed(() => {
  const b = store.available?.size ?? 0
  if (b <= 0) return ''
  const units = ['B', 'KB', 'MB', 'GB']
  let n = b
  let i = 0
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++ }
  return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
})

const stageLabel = computed(() => t(`update.stage.${stage.value as UpdateStage}`))
const doneOk = computed(() => store.done?.status === 'success')
const doneErr = computed(() => store.done?.status === 'failed')
const canApply = computed(() => !!store.available && !busy.value && !doneOk.value)
</script>

<template>
  <ModalShell>
    <template #head>
      <h3>{{ t('update.title') }}</h3>
      <p>{{ t('update.subtitle', { version: cur }) }}</p>
    </template>
    <template #body>
      <div class="upd">
        <!-- 升级结果优先展示 -->
        <div v-if="doneOk" class="upd-done ok">
          <div class="upd-done-title">{{ t('update.doneOk') }}</div>
          <p class="upd-done-note">{{ t('update.restartHint', { version: store.done?.version ?? '' }) }}</p>
        </div>
        <div v-else-if="doneErr" class="upd-done err">
          <div class="upd-done-title">{{ t('update.doneErr') }}</div>
          <p class="upd-done-note">{{ t('update.rollbackHint') }}</p>
        </div>

        <!-- 进行中：分阶段进度条 -->
        <div v-else-if="busy" class="upd-progress">
          <div class="upd-stage">{{ stageLabel }}</div>
          <div class="upd-bar"><div class="upd-bar-fill" :style="{ width: percent + '%' }" /></div>
          <div class="upd-pct">{{ percent }}%</div>
        </div>

        <!-- 有更新：版本 + 变更 + 体积 + 双校验说明 -->
        <div v-else-if="store.available" class="upd-info">
          <div class="upd-row"><span class="upd-k">{{ t('update.newVersion') }}</span><b>{{ store.available.version }}</b></div>
          <div class="upd-row"><span class="upd-k">{{ t('update.size') }}</span><span>{{ sizeLabel }}</span></div>
          <pre v-if="store.available.changelog" class="upd-changelog">{{ store.available.changelog }}</pre>
          <p class="upd-verify">{{ t('update.verifyNote') }}</p>
        </div>

        <!-- 无更新 / 检查中 -->
        <div v-else class="upd-idle">{{ store.checking ? t('update.checking') : t('update.latestShort') }}</div>
      </div>
    </template>
    <template #foot>
      <button class="btn" type="button" :disabled="store.checking || busy" @click="check()">{{ t('update.check') }}</button>
      <button class="btn btn-primary" type="button" :disabled="!canApply" @click="apply()">{{ t('update.apply') }}</button>
      <button class="btn" type="button" @click="emit('close')">{{ t('common.cancel') }}</button>
    </template>
  </ModalShell>
</template>

<style scoped>
.upd { display: flex; flex-direction: column; gap: 12px; min-height: 96px; }
.upd-row { display: flex; align-items: baseline; gap: 8px; }
.upd-k { color: var(--text-mute); min-width: 72px; }
.upd-changelog { margin: 0; padding: 10px 12px; background: var(--surface); border: 1px solid var(--border); border-radius: 6px; max-height: 160px; overflow: auto; white-space: pre-wrap; font-size: 12px; }
.upd-verify { margin: 0; color: var(--text-mute); font-size: 12px; }
.upd-idle { color: var(--text-mute); }
.upd-progress { display: flex; flex-direction: column; gap: 8px; }
.upd-stage { color: var(--text-mute); font-size: 13px; }
.upd-bar { height: 8px; background: var(--surface); border: 1px solid var(--border); border-radius: 999px; overflow: hidden; }
.upd-bar-fill { height: 100%; background: var(--ok); transition: width .2s ease; }
.upd-pct { font-size: 12px; color: var(--text-mute); }
.upd-done-title { font-weight: 600; }
.upd-done.ok .upd-done-title { color: var(--ok); }
.upd-done.err .upd-done-title { color: var(--danger); }
.upd-done-note { margin: 6px 0 0; color: var(--text-mute); font-size: 13px; }
</style>
