<script setup lang="ts">
// 装机向导：忠实迁移原型 openHomeSetupWizard（1959–2132），三步 + 目录树预览 + 校验动画
// 完成 → 写 env（derivePaths）+ dirReady.PHPO_HOME=true → 回调 onReady
import { computed, ref } from 'vue'
import ModalShell from '@/components/common/ModalShell.vue'
import { useI18n } from '@/composables/useI18n'
import { toast } from '@/composables/useToast'
import { useAppState } from '@/stores/appState'
import { HOME_SUBDIRS } from '@/constants/home'
import { DEFAULT_HOME, DEFAULT_WWW, derivePaths } from '@/utils/path'

const props = defineProps<{ onReady?: () => void }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const app = useAppState()

const step = ref(1)
const verified = ref(false)
const verifying = ref(false)
const homeVal = ref(app.env.PHPO_HOME || DEFAULT_HOME)
const wwwVal = ref(app.env.WWW_ROOT || DEFAULT_WWW)

const normHome = computed(() => homeVal.value.trim().replace(/\/+$/, '') || DEFAULT_HOME)
const normWww = computed(() => wwwVal.value.trim().replace(/\/+$/, '') || DEFAULT_WWW)

interface Seg { c: string; x: string }
const pad = (p: string) => ' '.repeat(Math.max(2, 22 - p.length))

const homeTree = computed<Seg[][]>(() => {
  const h = normHome.value
  const lines: Seg[][] = [[{ c: 'tree', x: `${h}/` }]]
  const depth0 = HOME_SUBDIRS.filter((s) => s.depth === 0)
  let idx0 = 0
  for (const sd of HOME_SUBDIRS) {
    if (sd.depth === 0) {
      const isLast = idx0 === depth0.length - 1
      lines.push([
        { c: 'tree', x: isLast ? '└── ' : '├── ' },
        { c: 'leaf', x: `${sd.path}/` },
        { c: 'tree', x: pad(sd.path) + sd.label },
      ])
      idx0++
    } else if (sd.depth === 1) {
      lines.push([
        { c: 'tree', x: '│   └── ' },
        { c: 'leaf', x: `${sd.path.split('/').pop()}/` },
        { c: 'tree', x: pad(sd.path) + sd.label },
      ])
    }
  }
  return lines
})
const wwwTree = computed<Seg[][]>(() => [
  [{ c: 'tree', x: `${normWww.value}/` }],
  [{ c: 'tree', x: '├── ' }, { c: 'leaf', x: 'demo.test/' }, { c: 'tree', x: '     → /var/www/demo.test' }],
  [{ c: 'tree', x: '├── ' }, { c: 'leaf', x: 'blog.test/' }, { c: 'tree', x: '     → /var/www/blog.test' }],
  [{ c: 'tree', x: '└── ' }, { c: 'leaf', x: '…' }],
])

const verifyLog = ref<Seg[]>([])
const showLog = computed(() => verifyLog.value.length > 0)

function next(): void {
  if (step.value === 1) {
    if (!homeVal.value.trim()) { toast(t('wiz.needHome'), 'err'); return }
    step.value = 2
    return
  }
  if (step.value === 2) {
    if (!wwwVal.value.trim()) { toast(t('wiz.needWww'), 'err'); return }
    if (normHome.value === normWww.value) { toast(t('wiz.samePath'), 'err'); return }
    step.value = 3
    verified.value = false
  }
}
function prev(): void {
  if (step.value > 1) { step.value--; verified.value = false; verifyLog.value = [] }
}

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms))
async function doVerify(): Promise<void> {
  if (verifying.value) return
  const h = normHome.value
  const w = normWww.value
  if (!h) { toast(t('wiz.needHome'), 'err'); return }
  if (!w) { toast(t('wiz.needWww'), 'err'); return }
  if (h === w) { toast(t('wiz.samePath'), 'err'); return }
  verifying.value = true
  const log = verifyLog.value
  log.length = 0
  log.push({ c: 'dim', x: '' })
  verifyLog.value = [{ c: 'prompt', x: `$ phpo home ensure ${h} --www ${w}` }]
  const push = (s: Seg) => { verifyLog.value = [...verifyLog.value, s] }
  const d0 = HOME_SUBDIRS.filter((s) => s.depth === 0).length
  await wait(200)
  push({ c: 'ok', x: `${t('wiz.s3.homeReady')}：${h}` })
  await wait(200)
  push({ c: 'ok', x: `${t('wiz.s3.wwwReady')}：${w}  → /var/www` })
  await wait(200)
  push({ c: 'ok', x: `✓ ${t('wiz.s3.subStart', { n: d0 })}` })
  for (const sd of HOME_SUBDIRS) { push({ c: 'ok', x: `    ✓ ${h}/${sd.path}/   ${sd.label}` }); await wait(60) }
  push({ c: 'ok', x: `✓ ${t('wiz.s3.wwwCreate')}：${w}` })
  await wait(200)
  push({ c: 'ok', x: '✓ ' + t('wiz.s3.chmod') + '   u+rwX,go+rX' })
  await wait(200)
  push({ c: 'ok', x: '✓ ' + t('wiz.s3.rw') + '   write / read / delete OK' })
  verifying.value = false
  verified.value = true
}

function doConfirm(): void {
  Object.assign(app.env, derivePaths(normHome.value, normWww.value))
  app.dirReady.PHPO_HOME = true
  emit('close')
  props.onReady?.()
}
</script>

<template>
  <ModalShell size="lg">
    <template #head>
      <h3>{{ t('wiz.title') }}</h3>
      <p>{{ t('wiz.subtitle') }}</p>
    </template>
    <div class="wiz-bar">
      <div class="wiz-steps">
        <div class="wiz-step" :class="step > 1 ? 'done' : step === 1 ? 'active' : ''"><span class="wiz-num">{{ step > 1 ? '✓' : '1' }}</span><span class="wiz-label">{{ t('wiz.step1') }}</span></div>
        <div class="wiz-line" :class="{ done: step > 1 }" />
        <div class="wiz-step" :class="step > 2 ? 'done' : step === 2 ? 'active' : ''"><span class="wiz-num">{{ step > 2 ? '✓' : '2' }}</span><span class="wiz-label">{{ t('wiz.step2') }}</span></div>
        <div class="wiz-line" :class="{ done: step > 2 }" />
        <div class="wiz-step" :class="step === 3 ? 'active' : ''"><span class="wiz-num">3</span><span class="wiz-label">{{ t('wiz.step3') }}</span></div>
      </div>
    </div>
    <template #body>
      <template v-if="step === 1">
        <div class="wiz-hero"><div class="wiz-hero-title">{{ t('wiz.s1.title') }}</div><div class="wiz-hero-desc">{{ t('wiz.s1.desc') }}</div></div>
        <div class="field">
          <label class="mono" style="font-size: 12px">PHPO_HOME</label>
          <input v-model="homeVal" type="text" spellcheck="false" autocomplete="off">
          <div class="hint">💡 {{ t('wiz.s1.hint') }}</div>
        </div>
        <div class="field"><label>{{ t('wiz.s1.tree') }}</label>
          <div class="dir-log"><div v-for="(ln, i) in homeTree" :key="i"><span v-for="(s, j) in ln" :key="j" :class="s.c">{{ s.x }}</span></div></div>
        </div>
      </template>
      <template v-else-if="step === 2">
        <div class="wiz-hero"><div class="wiz-hero-title">{{ t('wiz.s2.title') }}</div><div class="wiz-hero-desc">{{ t('wiz.s2.desc') }}</div></div>
        <div class="field">
          <label class="mono" style="font-size: 12px">WWW_ROOT</label>
          <input v-model="wwwVal" type="text" spellcheck="false" autocomplete="off">
          <div class="hint">💡 {{ t('wiz.s2.hint') }}</div>
        </div>
        <div class="field"><label>{{ t('wiz.s2.tree') }}</label>
          <div class="dir-log"><div v-for="(ln, i) in wwwTree" :key="i"><span v-for="(s, j) in ln" :key="j" :class="s.c">{{ s.x }}</span></div></div>
        </div>
      </template>
      <template v-else>
        <div class="wiz-hero"><div class="wiz-hero-title">{{ t('wiz.s3.title') }}</div><div class="wiz-hero-desc">{{ t('wiz.s3.desc') }}</div></div>
        <div class="field"><label>{{ t('wiz.s3.summary') }}</label>
          <div class="wiz-confirm-kv">
            <div class="row"><span class="k">PHPO_HOME</span><span class="v">{{ normHome }}</span></div>
            <div class="row"><span class="k">WWW_ROOT</span><span class="v">{{ normWww }}</span></div>
          </div>
        </div>
        <div v-if="showLog" class="field"><label>{{ t('wiz.s3.log') }}</label>
          <div class="dir-log" style="max-height: 220px"><div v-for="(s, i) in verifyLog" :key="i" :class="s.c">{{ s.x }}</div></div>
        </div>
      </template>
    </template>
    <template #foot>
      <button v-if="step > 1" class="btn" type="button" @click="prev">← {{ t('wiz.prev') }}</button>
      <button v-else class="btn" type="button" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button v-if="step < 3" class="btn btn-primary" type="button" @click="next">{{ t('wiz.next') }} →</button>
      <button v-else-if="verified" class="btn btn-primary" type="button" @click="doConfirm">{{ t('dir.confirm') }}</button>
      <button v-else class="btn btn-primary" type="button" :disabled="verifying" @click="doVerify">{{ t('dir.verify') }}</button>
    </template>
  </ModalShell>
</template>
