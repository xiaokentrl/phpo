<script setup lang="ts">
// 装机向导：忠实迁移原型 openHomeSetupWizard（1959–2132），三步 + 目录树预览 + 校验
// 验证/确认走后端 HomeVerify / HomeEnsure（硬红线 4/5：确认后不本地乐观更新，等 state:changed 回流）
// 无宿主（纯 Vite demo）时回退到原型动画 + 本地写 env/dirReady
import { computed, onMounted, ref } from 'vue'
import { Dialogs } from '@wailsio/runtime'
import ModalShell from '@/components/common/ModalShell.vue'
import { useI18n } from '@/composables/useI18n'
import { toast } from '@/composables/useToast'
import { useAppState } from '@/stores/appState'
import { HOME_SUBDIRS } from '@/constants/home'
import { DEFAULT_HOME, DEFAULT_WWW, derivePaths } from '@/utils/path'
import { hasBackend } from '@/api/site'
import { getHomeDefaults, homeEnsure, homeVerify } from '@/api/wizard'

const props = defineProps<{ locked?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const app = useAppState()
const canBrowse = hasBackend() // 有宿主才提供原生目录选择器（沿用 SiteAddModal 同套 Dialogs.OpenFile）

const step = ref(1)
const verified = ref(false)
const verifying = ref(false)
// env 解析出的默认目录（快照 env > config.yaml 的 phpo_home > ~/phpo）：作输入框预填与浏览起始目录的兜底源
const envHome = ref('')
const envWww = ref('')
// 初值取快照 env（首启为空），随后 onMounted 用后端 HomeDefaults 覆盖为 config.yaml 定义的目录
const homeVal = ref(app.env.PHPO_HOME || DEFAULT_HOME)
const wwwVal = ref(app.env.WWW_ROOT || DEFAULT_WWW)
const homeDirty = ref(false)
const wwwDirty = ref(false)
// alreadySet：后端先决检测「工作目录已设置」→ 只回显两根 + 「完成」，不提供三步设置、验证与确认并创建（禁止重复创建）
const alreadySet = ref(false)

// normHome/normWww：trim 去尾斜杠；输入框为空时回落 env 定义目录，再退 DEFAULT_*
const normHome = computed(() => homeVal.value.trim().replace(/\/+$/, '') || envHome.value || DEFAULT_HOME)
const normWww = computed(() => wwwVal.value.trim().replace(/\/+$/, '') || envWww.value || DEFAULT_WWW)

// 载入后端解析的工作目录默认值：config.yaml 预置自定义根目录时优先于硬编码 ~/phpo（需求 1）
// configured 为真即工作目录早已设置完毕：本向导不再允许重复创建，就地展示已设置态
onMounted(async () => {
  const d = await getHomeDefaults()
  if (!d) return
  envHome.value = d.home
  envWww.value = d.www
  alreadySet.value = d.configured
  if (!homeDirty.value && d.home) homeVal.value = d.home
  if (!wwwDirty.value && d.www) wwwVal.value = d.www
})

// browseDir 打开原生选目录框，选中绝对路径回填对应输入框（home=PHPO_HOME / www=WWW_ROOT）
// 起始目录：用户已填则打开到该目录，否则打开到 env 定义目录（需求 2）
async function browseDir(target: 'home' | 'www'): Promise<void> {
  const startDir = (target === 'home' ? homeVal.value.trim() : wwwVal.value.trim())
    || (target === 'home' ? envHome.value : envWww.value)
    || (target === 'home' ? DEFAULT_HOME : DEFAULT_WWW)
  const picked = await Dialogs.OpenFile({
    Title: t(target === 'home' ? 'wiz.s1.title' : 'wiz.s2.title'),
    CanChooseDirectories: true,
    CanChooseFiles: false,
    Directory: startDir,
  })
  const abs = String(picked || '').replace(/\/+$/, '')
  if (!abs) return
  if (target === 'home') {
    homeVal.value = abs
    homeDirty.value = true
  } else {
    wwwVal.value = abs
    wwwDirty.value = true
  }
}

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
const promptSeg = (h: string, w: string): Seg => ({ c: 'prompt', x: `$ phpo home ensure ${h} --www ${w}` })

// runDemoVerify 无宿主回退：复刻原型逐行动画，纯展示不落库
async function runDemoVerify(h: string, w: string): Promise<void> {
  verifyLog.value = []
  const push = (s: Seg) => { verifyLog.value = [...verifyLog.value, s] }
  const d0 = HOME_SUBDIRS.filter((s) => s.depth === 0).length
  push(promptSeg(h, w))
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
  verified.value = true
}

async function doVerify(): Promise<void> {
  if (verifying.value) return
  const h = normHome.value
  const w = normWww.value
  if (!h) { toast(t('wiz.needHome'), 'err'); return }
  if (!w) { toast(t('wiz.needWww'), 'err'); return }
  if (h === w) { toast(t('wiz.samePath'), 'err'); return }
  verifying.value = true
  try {
    const r = await homeVerify(h, w)
    if (r === null) { await runDemoVerify(h, w); return }
    const segs: Seg[] = [promptSeg(h, w)]
    for (const ln of r.lines) segs.push({ c: 'ok', x: ln })
    for (const e of r.errors) segs.push({ c: 'err', x: e })
    verifyLog.value = segs
    verified.value = r.ok
    if (!r.ok && r.errors[0]) toast(r.errors[0], 'err')
  } catch (e) {
    verifyLog.value = [promptSeg(h, w), { c: 'err', x: String((e as Error)?.message ?? e) }]
    verified.value = false
  } finally {
    verifying.value = false
  }
}

const confirming = ref(false)
const done = ref(false) // 目录设置成功后置真：向导内展示成功提示，用户点「完成」才关闭返回主界面
async function doConfirm(): Promise<void> {
  if (confirming.value) return
  confirming.value = true
  const h = normHome.value
  const w = normWww.value
  try {
    await homeEnsure(h, w) // 有宿主：只有这一步才真正建目录 → 写 config.yaml → 广播 state:changed（dirReady 由快照派生，硬红线 4/5）
    if (!hasBackend()) { // 无宿主：本地占位落地
      Object.assign(app.env, derivePaths(h, w))
      app.dirReady.PHPO_HOME = true
      app.dirReady.WWW_ROOT = true
    }
    done.value = true // 成功后就地提示；不接续任何后续写操作（禁止目录设置与安装/建站连续操作）
  } catch (e) {
    toast(String((e as Error)?.message ?? e), 'err')
  } finally {
    confirming.value = false
  }
}
function finish(): void { emit('close') } // 用户确认成功后关闭向导返回主界面
</script>

<template>
  <ModalShell size="lg" :locked="props.locked" @close="emit('close')">
    <template #head>
      <h3>{{ t('wiz.title') }}</h3>
      <p>{{ t('wiz.subtitle') }}</p>
    </template>
    <div v-if="!done && !alreadySet" class="wiz-bar">
      <div class="wiz-steps">
        <div class="wiz-step" :class="step > 1 ? 'done' : step === 1 ? 'active' : ''"><span class="wiz-num">{{ step > 1 ? '✓' : '1' }}</span><span class="wiz-label">{{ t('wiz.step1') }}</span></div>
        <div class="wiz-line" :class="{ done: step > 1 }" />
        <div class="wiz-step" :class="step > 2 ? 'done' : step === 2 ? 'active' : ''"><span class="wiz-num">{{ step > 2 ? '✓' : '2' }}</span><span class="wiz-label">{{ t('wiz.step2') }}</span></div>
        <div class="wiz-line" :class="{ done: step > 2 }" />
        <div class="wiz-step" :class="step === 3 ? 'active' : ''"><span class="wiz-num">3</span><span class="wiz-label">{{ t('wiz.step3') }}</span></div>
      </div>
    </div>
    <template #body>
      <template v-if="done || alreadySet">
        <div class="wiz-hero"><div class="wiz-hero-title">{{ alreadySet ? t('wiz.already.title') : t('wiz.done.title') }}</div><div class="wiz-hero-desc">{{ alreadySet ? t('wiz.already.desc') : t('wiz.done.desc') }}</div></div>
        <div class="wiz-confirm-kv">
          <div class="row"><span class="k">PHPO_HOME</span><span class="v">{{ alreadySet ? envHome : normHome }}</span></div>
          <div class="row"><span class="k">WWW_ROOT</span><span class="v">{{ alreadySet ? envWww : normWww }}</span></div>
        </div>
      </template>
      <template v-else-if="step === 1">
        <div class="wiz-hero"><div class="wiz-hero-title">{{ t('wiz.s1.title') }}</div><div class="wiz-hero-desc">{{ t('wiz.s1.desc') }}</div></div>
        <div class="field">
          <label class="mono" style="font-size: 12px">PHPO_HOME</label>
          <div class="input-with-action">
            <input v-model="homeVal" type="text" spellcheck="false" autocomplete="off" @input="homeDirty = true">
            <button v-if="canBrowse" class="input-action-btn" type="button" @click="browseDir('home')">{{ t('siteAdd.browse') }}</button>
          </div>
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
          <div class="input-with-action">
            <input v-model="wwwVal" type="text" spellcheck="false" autocomplete="off" @input="wwwDirty = true">
            <button v-if="canBrowse" class="input-action-btn" type="button" @click="browseDir('www')">{{ t('siteAdd.browse') }}</button>
          </div>
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
      <button v-if="done || alreadySet" class="btn btn-primary" type="button" @click="finish">{{ t('wiz.done.btn') }}</button>
      <template v-else>
        <button v-if="step > 1" class="btn" type="button" @click="prev">← {{ t('wiz.prev') }}</button>
        <button v-else-if="!props.locked" class="btn" type="button" @click="emit('close')">{{ t('common.cancel') }}</button>
        <button v-if="step < 3" class="btn btn-primary" type="button" @click="next">{{ t('wiz.next') }} →</button>
        <button v-else-if="verified" class="btn btn-primary" type="button" :disabled="confirming" @click="doConfirm">{{ t('dir.confirm') }}</button>
        <button v-else class="btn btn-primary" type="button" :disabled="verifying" @click="doVerify">{{ t('dir.verify') }}</button>
      </template>
    </template>
  </ModalShell>
</template>
