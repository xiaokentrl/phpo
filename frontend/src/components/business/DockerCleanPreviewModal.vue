<script setup lang="ts">
// 「彻底清空」分两步走，跟原型一个样：
//   第一步只看——把要动的东西一项一项摊出来，这一屏一个删除动作都不会发生。
//   第二步才点头——看清后果那一屏，勾里有危险项时「我知道后果」必须真点一下，「同时卸载这几个 phpo 版本」默认不勾。
// 为什么非要两屏：一屏既能勾又能提交，用户容易在没看完清单时就按下去；删掉的东西里如果有正在跑的服务，
// 界面只会留下「缺失态」，恢复得靠用户自己点「启用」。所以删除动作只挂在第二屏那颗按钮上。
import { computed, ref } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { toast } from '@/composables/useToast'
import { backendMsg } from '@/utils/backendMsg'
import { humanSize } from '@/composables/useCleanup'
import { executeClean } from '@/api/dockerClean'
import ModalShell from '@/components/common/ModalShell.vue'
import { syncState } from '@/composables/useStateSync'
import type { CleanPreview, CleanTarget } from '@/types'

const props = defineProps<{ preview: CleanPreview; onDone?: () => void }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()

// preview = 看清单那一屏；confirm = 认后果那一屏。
const stage = ref<'preview' | 'confirm'>('preview')
const chosen = ref<Set<string>>(new Set(props.preview.targets.map((x) => x.id)))
// consentRequired 为真时这一颗起始是「没勾」，必须用户亲手点。
const consent = ref(!props.preview.consentRequired)
const uninstall = ref(false)
const running = ref(false)

const targets = computed(() => props.preview.targets ?? [])
const sizeSum = computed(() => targets.value.reduce((s, x) => s + (chosen.value.has(x.id) ? x.size : 0), 0))
const canNext = computed(() => chosen.value.size > 0)
const canSubmit = computed(() => chosen.value.size > 0 && consent.value && !running.value)

// 「将删除对象：」按行归堆，一行一堆名字——只列勾上的那几项。
const scopeGroups = computed(() => {
  const map = new Map<string, CleanTarget[]>()
  for (const tg of targets.value) {
    if (!chosen.value.has(tg.id)) continue
    const list = map.get(tg.row)
    if (list) list.push(tg)
    else map.set(tg.row, [tg])
  }
  return [...map.entries()]
})

function toggle(id: string) {
  const next = new Set(chosen.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  chosen.value = next
}

function back() {
  stage.value = 'preview'
}

async function run() {
  if (!canSubmit.value) return
  running.value = true
  try {
    const rep = await executeClean({
      token: props.preview.token,
      ids: [...chosen.value],
      consent: consent.value,
      uninstall: uninstall.value,
    })
    if (rep) {
      toast(t('clean.exec.done', {
        removed: rep.removed, failed: rep.failed, skipped: rep.skipped, size: humanSize(rep.freedBytes),
      }), rep.failed > 0 ? 'err' : 'ok', 6400)
      if (rep.trashed?.length) toast(t('clean.exec.trashed', { n: rep.trashed.length }), 'info', 5200)
    }
    // 等权威快照回流后再交回按钮：先复能会让界面多亮一帧旧状态（§5.6.4）。
    await syncState()
    props.onDone?.()
    emit('close')
  } catch (e) {
    // 一次性凭据过期 / 危险项没确认 / 清单外的 ID——后端那句人话原样贴出来，用户才知道下一步点哪里。
    toast(backendMsg(String(e)), 'err', 6800)
    // 后端拦下来说明清单可能已经过期，退回第一屏让用户重新看一遍再决定。
    stage.value = 'preview'
  } finally {
    running.value = false
  }
}
</script>

<template>
  <ModalShell size="lg" danger @close="emit('close')">
    <template #head>
      <h3>{{ stage === 'preview' ? t('clean.preview.title') : t('clean.ui.confirmTitle') }}</h3>
      <p>{{ t('clean.preview.subtitle', { rows: preview.rows.length }) }}</p>
    </template>

    <template #body>
      <div v-if="preview.warnings?.length" class="cp-warns">
        <p v-for="(w, i) in preview.warnings" :key="i" class="cp-warn">{{ w }}</p>
      </div>

      <div class="cp-tools">
        <span class="chip chip-accent">{{ t('clean.preview.count', { n: chosen.size, size: humanSize(sizeSum) }) }}</span>
        <template v-if="stage === 'preview'">
          <button class="btn btn-sm" @click="chosen = new Set(targets.map((x) => x.id))">{{ t('clean.preview.all') }}</button>
          <button class="btn btn-sm" @click="chosen = new Set()">{{ t('clean.preview.none') }}</button>
        </template>
      </div>

      <!-- 第一屏：勾与看都在这里；第二屏只读，改动得先退回第一屏。 -->
      <ul class="cp-list" :class="{ readonly: stage === 'confirm' }">
        <li v-for="tg in targets" :key="tg.id" class="cp-item">
          <label>
            <input type="checkbox" :checked="chosen.has(tg.id)" :disabled="stage === 'confirm'" @change="toggle(tg.id)" />
            <span class="cp-name">{{ tg.name }}</span>
            <span class="cp-row">{{ t('clean.' + tg.row + '.label') }}</span>
            <span v-if="tg.inUse" class="badge">{{ t('clean.preview.inUse') }}</span>
            <span v-if="tg.foreign" class="badge">{{ t('clean.preview.foreign') }}</span>
            <span v-if="tg.needsRoot" class="badge badge-warn">{{ t('clean.preview.needsRoot') }}</span>
            <span v-if="tg.size > 0" class="cp-size">{{ humanSize(tg.size) }}</span>
          </label>
        </li>
      </ul>

      <!-- 后果那一屏：把勾中的对象按行归堆再念一遍，让用户在点头前最后一次对上号。 -->
      <div v-if="stage === 'confirm'" class="cp-scope">
        <p class="cp-scope-title">{{ t('clean.ui.scopeObjs') }}</p>
        <div v-for="[row, list] in scopeGroups" :key="row" class="cp-cat">
          <span class="cp-cat-head">{{ t('clean.' + row + '.label') }} <em class="cp-cat-count">{{ list.length }}</em></span>
          <ul class="plain-tags">
            <li v-for="tg in list" :key="tg.id">{{ tg.name }}</li>
          </ul>
        </div>
      </div>

      <p v-if="preview.consentRequired" class="cp-consent">
        <label>
          <input v-model="consent" type="checkbox" :disabled="stage === 'preview'" />
          {{ t('clean.preview.consent') }}
        </label>
      </p>
      <p v-if="preview.installed?.length" class="cp-uninstall">
        <label>
          <input v-model="uninstall" type="checkbox" />
          {{ t('clean.preview.uninstall', { list: preview.installed.map((x) => x.kind + ' ' + x.version).join('、') }) }}
        </label>
        <em>{{ t('clean.preview.uninstallHint') }}</em>
      </p>
    </template>

    <template #foot>
      <div class="cp-foot">
        <button class="btn" :disabled="running" @click="emit('close')">{{ t('common.cancel') }}</button>
        <button v-if="stage === 'confirm'" class="btn" :disabled="running" @click="back">{{ t('clean.ui.backStep') }}</button>
        <button v-if="stage === 'preview'" class="btn" :disabled="!canNext" @click="stage = 'confirm'">
          {{ t('clean.ui.nextStep') }}
        </button>
        <button v-else class="btn danger" :disabled="!canSubmit" @click="run">
          {{ running ? t('clean.preview.running') : t('clean.preview.submit') }}
        </button>
      </div>
    </template>
  </ModalShell>
</template>

<style scoped>
.cp-warns { margin-bottom: 10px; }
.cp-warn { margin: 0 0 4px; font-size: 12px; color: var(--warn); }
.cp-tools { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.cp-list { list-style: none; margin: 0; padding: 0; max-height: 46vh; overflow: auto; }
.cp-list.readonly { opacity: .8; }
.cp-item { padding: 5px 0; border-bottom: 1px solid var(--border); }
.cp-item label { display: flex; align-items: center; gap: 8px; cursor: pointer; }
.cp-item input:disabled { cursor: default; }
.cp-name { font-size: 12.5px; word-break: break-all; }
.cp-row { font-size: 11.5px; color: var(--text-mute); }
.cp-size { margin-left: auto; font-size: 11.5px; color: var(--text-mute); font-variant-numeric: tabular-nums; }
.cp-scope { margin-top: 12px; padding-top: 10px; border-top: 1px solid var(--border); }
.cp-scope-title { margin: 0 0 6px; font-size: 12.5px; color: var(--text-dim); }
.cp-cat { margin-bottom: 8px; }
.cp-cat-head { font-size: 11.5px; color: var(--text-mute); }
.cp-cat-count { font-style: normal; }
.plain-tags { list-style: none; display: flex; flex-wrap: wrap; gap: 4px 6px; margin: 4px 0 0; padding: 0; }
.plain-tags li {
  font-size: 11.5px;
  padding: 1px 6px;
  border: 1px solid var(--border);
  border-radius: var(--r-xs);
  background: var(--surface-2);
  word-break: break-all;
}
.cp-consent, .cp-uninstall { margin: 12px 0 0; font-size: 12.5px; }
.cp-consent input:disabled { cursor: default; }
.cp-uninstall em { display: block; margin-top: 4px; font-size: 11.5px; color: var(--text-mute); font-style: normal; }
.cp-foot { display: flex; justify-content: flex-end; gap: 8px; }
.badge {
  padding: 0 6px;
  font-size: 11px;
  border: 1px solid var(--border);
  border-radius: 999px;
  color: var(--text-mute);
}
.badge-warn { color: var(--warn); border-color: var(--warn); }
</style>
