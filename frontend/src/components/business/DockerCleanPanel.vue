<script setup lang="ts">
// 总览页底部的「其他 · Docker 全量资源清理」面板。
// 界面结构照原型：可折叠面板 → 清理范围两张卡 → 状态条 → 工具条 → 分组表 → 底部两颗按钮。
// 本轮只做界面与勾选手感（三处勾选入口行为必须一致），扫描/预览/清空的真实调用留给后续逻辑轮。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { toast } from '@/composables/useToast'
import { humanSize } from '@/composables/useCleanup'
import { useModalStore } from '@/stores/modalStore'
import { cleanHostSupported, previewClean, refreshCleanRow, scanClean } from '@/api/dockerClean'
import { EVENT, onEvent } from '@/api/events'
import DangerConfirm from '@/components/business/DangerConfirm.vue'
import DockerCleanPreviewModal from '@/components/business/DockerCleanPreviewModal.vue'
import type { CleanRow } from '@/types'

const { t } = useI18n()
const modal = useModalStore()

// 面板永远默认折叠，也不记上次是开着还是关着。
const open = ref(false)
const rows = ref<CleanRow[]>([])
const scanning = ref(false)
const deepReady = ref(false)
const retrying = ref('')
const errText = ref('')
const progress = ref<{ step: number; total: number; stage: string } | null>(null)

// 勾选手感：保留模式下 phpo 相关项默认灰掉，要先看完后果才算勾上。
const scopeMode = ref<'keep' | 'remove'>('keep')
const picked = ref<Set<string>>(new Set())
const unlocked = ref<Set<string>>(new Set())
const collapsedGroups = ref<Record<string, boolean>>({})
const bannerDetailOpen = ref(false)

const offProgress = onEvent(EVENT.DockerCleanup, (payload: unknown) => {
  const p = payload as { stage?: string; step?: number; total?: number }
  if (typeof p?.step === 'number' && typeof p?.total === 'number') {
    progress.value = { step: p.step, total: p.total, stage: p.stage ?? '' }
  }
})
onBeforeUnmount(() => offProgress())

watch(open, (v) => {
  if (v && rows.value.length === 0 && !scanning.value) void load(false)
})

const groups = computed(() => {
  const out: { key: string; rows: CleanRow[] }[] = []
  for (const r of rows.value) {
    const last = out[out.length - 1]
    if (last && last.key === r.group) last.rows.push(r)
    else out.push({ key: r.group, rows: [r] })
  }
  return out
})

const phpoCount = computed(() => rows.value.filter((r) => r.isPhpo).length)
const otherCount = computed(() => rows.value.length - phpoCount.value)

// 保留模式 + 该项属于 phpo + 还没解锁 = 灰掉（.locked-badge 那颗锁）。
function rowLocked(r: CleanRow): boolean {
  return scopeMode.value === 'keep' && r.isPhpo && !unlocked.value.has(r.key)
}

function rowText(r: CleanRow): string {
  if (r.status !== 'ok') return '—'
  const n = t('clean.panel.countUnit', { n: r.count })
  return r.hasBytes ? `${n} · ${humanSize(r.bytes)}` : n
}

const statusPill: Record<string, string> = {
  ok: 'pill-ok', no_perm: 'pill-warn', not_supported: 'pill-off', unavailable: 'pill-warn',
}
const riskPill: Record<string, string> = {
  low: 'pill-ok', medium: 'pill-warn', high: 'pill-danger', critical: 'pill-danger',
}

function groupCollapsed(key: string): boolean {
  return collapsedGroups.value[key] !== false
}

function toggleGroup(key: string) {
  collapsedGroups.value = { ...collapsedGroups.value, [key]: !groupCollapsed(key) }
}

async function load(deep: boolean) {
  if (scanning.value) return
  scanning.value = true
  errText.value = ''
  progress.value = { step: 0, total: 0, stage: t('clean.panel.preparing') }
  try {
    const rep = await scanClean(deep)
    if (!rep) {
      toast(t('clean.panel.noBackend'), 'info', 3600)
      return
    }
    rows.value = rep.rows ?? []
    for (const g of new Set(rows.value.map((r) => r.group))) {
      if (collapsedGroups.value[g] === undefined) collapsedGroups.value[g] = true
    }
    // 重新数一次不该把用户手工勾的那一半洗掉：一个都没勾时才照范围卡铺一遍。
    if (picked.value.size === 0) applyScope()
    else prunePicked()
    if (rep.warnings?.length) errText.value = rep.warnings.join(' ')
  } catch (e) {
    setError(String(e))
  } finally {
    scanning.value = false
    progress.value = null
  }
}

function setError(msg: string) {
  errText.value = msg
}

// 照当前范围卡重算选中的那一套：保留模式只选非 phpo 的行（加上这一档里刚知情解锁过的），清理模式全选（危险项除外）。
// 切范围卡时 unlocked 已经清空，所以这里算出来的就是这一档的默认；
// 「重新数一次」（load）走这一条时不清 unlocked，避免把用户手工解锁的那一半洗掉。
function applyScope() {
  const next = new Set<string>()
  for (const r of rows.value) {
    if (!selectable(r)) continue
    if (scopeMode.value === 'keep' && r.isPhpo && !unlocked.value.has(r.key)) continue
    next.add(r.key)
  }
  picked.value = next
}

// 重数之后只摘掉「这一轮点不动了」的那几颗勾（数不到、不给删的行），
// 已经知情勾上的危险项照原样留着——它们是用户一颗一颗点出来的，不是全选带进来的。
function prunePicked() {
  const deletable = new Set(rows.value.filter((r) => r.deletable).map((r) => r.key))
  const next = new Set<string>()
  picked.value.forEach((k) => {
    if (deletable.has(k)) next.add(k)
  })
  picked.value = next
}

// 点「保留 phpo 服务」/「一起清理 phpo 服务」这两张范围卡时，列表里的勾选永远回到这一档的默认那一套：
// 保留 = 只勾不属于 phpo 的那些行；清理 = 全勾（危险项、以及「只说清去哪儿删、不给按钮」的那些行除外）。
// 不管切换前勾了什么：手工勾的、手工取消的、点过「全选」带上来的一律洗掉；
// 连「我已知情」解锁过的那些 phpo 行也一起洗——换一张范围卡等于换过一次口径，那份知情不再记账。
// 点的是不是当前已经亮着的那张卡也照样重置：否则「全选 → 清空选择 → 再点这张卡」会什么都回不来。
// 怎么回来：在新这一档里重新点那一行 phpo 项，后果确认会再问一遍，点了「确定要选上」它才又勾上。
// 注意这里和「重新数一次」（load）是两种刻意不同的行为：重数不洗勾选，只有点范围卡才洗。
function setScopeMode(mode: 'keep' | 'remove') {
  scopeMode.value = mode
  unlocked.value = new Set()
  applyScope()
}

// 「有按钮可点」的行还要再收一道：危险项只能一行一行自己点。
function selectable(r: CleanRow): boolean {
  return r.deletable && !r.dangerous
}

function toggleRow(r: CleanRow) {
  if (!r.deletable) return
  if (rowLocked(r)) {
    // 灰掉的 phpo 项：先把后果摆完，点了「确定要选上」才算解锁并勾上。
    modal.open(DangerConfirm, {
      title: t('clean.ui.unlockTitle'),
      description: t('clean.ui.unlockOne', { name: t(`clean.${r.key}.label`) }),
      checkbox: { label: t('clean.ui.unlockText') },
      confirmLabel: t('clean.danger.confirm'),
      onConfirm: () => {
        unlock([r.key])
        flip(r.key, true)
        toast(t('clean.ui.unlockOne', { name: t(`clean.${r.key}.label`) }), 'ok', 3200)
      },
    })
    return
  }
  if (r.dangerous && !picked.value.has(r.key)) {
    modal.open(DangerConfirm, {
      title: t('clean.danger.title', { name: t(`clean.${r.key}.label`) }),
      description: t('clean.' + r.key + '.desc'),
      warnings: [{ text: t('clean.danger.warn') }],
      confirmLabel: t('clean.danger.confirm'),
      onConfirm: () => flip(r.key, true),
    })
    return
  }
  flip(r.key, !picked.value.has(r.key))
}

function flip(key: string, on: boolean) {
  const next = new Set(picked.value)
  if (on) next.add(key)
  else next.delete(key)
  picked.value = next
}

function unlock(keys: string[]) {
  const next = new Set(unlocked.value)
  for (const k of keys) next.add(k)
  unlocked.value = next
}

// 组勾选：这一组里如果有还没解锁的 phpo 项，整组勾选也要先过后果确认（三处入口一致）。
function toggleGroupRows(g: { key: string; rows: CleanRow[] }) {
  const clickable = g.rows.filter((r) => r.deletable && !r.dangerous)
  const locked = clickable.filter(rowLocked)
  if (locked.length) {
    modal.open(DangerConfirm, {
      title: t('clean.ui.unlockTitle'),
      description: t('clean.ui.unlockGroup', { name: t(`clean.group.${g.key}.label`) }),
      checkbox: { label: t('clean.ui.unlockText') },
      confirmLabel: t('clean.danger.confirm'),
      onConfirm: () => {
        unlock(locked.map((r) => r.key))
        for (const r of clickable) flip(r.key, true)
        toast(t('clean.ui.unlockGroup', { name: t(`clean.group.${g.key}.label`) }), 'ok', 3200)
      },
    })
    return
  }
  const allOn = clickable.length > 0 && clickable.every((r) => picked.value.has(r.key))
  for (const r of clickable) flip(r.key, !allOn)
}

function groupChecked(g: { key: string; rows: CleanRow[] }): boolean {
  const clickable = g.rows.filter((r) => r.deletable && !r.dangerous && !rowLocked(r))
  return clickable.length > 0 && clickable.every((r) => picked.value.has(r.key))
}

function groupSomeChecked(g: { key: string; rows: CleanRow[] }): boolean {
  const clickable = g.rows.filter((r) => r.deletable && !r.dangerous && !rowLocked(r))
  return clickable.some((r) => picked.value.has(r.key)) && !groupChecked(g)
}

// 全选：保留模式下会把 phpo 项一起带走，所以照样要先过后果确认。
function selectAll() {
  const locked = rows.value.filter((r) => r.deletable && !r.dangerous && rowLocked(r))
  if (locked.length) {
    modal.open(DangerConfirm, {
      title: t('clean.ui.unlockTitle'),
      description: t('clean.ui.unlockAll'),
      checkbox: { label: t('clean.ui.unlockText') },
      confirmLabel: t('clean.danger.confirm'),
      onConfirm: () => {
        unlock(locked.map((r) => r.key))
        applyScope()
        toast(t('clean.ui.unlockAll'), 'ok', 3200)
      },
    })
    return
  }
  const next = new Set(picked.value)
  rows.value.forEach((r) => { if (selectable(r)) next.add(r.key) })
  picked.value = next
}

function clearPick() {
  picked.value = new Set()
}

const allChecked = computed(() => {
  const clickable = rows.value.filter((r) => r.deletable && !r.dangerous && !rowLocked(r))
  return clickable.length > 0 && clickable.every((r) => picked.value.has(r.key))
})
const someChecked = computed(() => picked.value.size > 0 && !allChecked.value)

function onSelectAllClick() {
  if (picked.value.size > 0 || rowsLockedCount.value === 0) {
    if (allChecked.value) clearPick()
    else void selectAll()
    return
  }
  void selectAll()
}

const rowsLockedCount = computed(() => rows.value.filter((r) => r.deletable && !r.dangerous && rowLocked(r)).length)

async function onRetry(r: CleanRow) {
  if (retrying.value) return
  retrying.value = r.key
  try {
    const fresh = await refreshCleanRow(r.key)
    if (!fresh) return
    const i = rows.value.findIndex((x) => x.key === r.key)
    if (i >= 0) rows.value.splice(i, 1, fresh)
    if (fresh.status !== 'ok') toast(t('clean.panel.retryStill', { msg: fresh.message ?? t('clean.panel.unknown') }), 'info', 5200)
  } catch (e) {
    toast(String(e), 'err', 5200)
  } finally {
    retrying.value = ''
  }
}

async function onDeep() {
  if (!deepReady.value) {
    toast(t('clean.panel.deepUnsupported'), 'info', 4200)
    return
  }
  await load(true)
}

async function checkDeepReady() {
  deepReady.value = await cleanHostSupported()
}
onMounted(() => void checkDeepReady())

async function onClean() {
  if (picked.value.size === 0) return
  try {
    const pv = await previewClean([...picked.value])
    if (!pv) return
    modal.open(DockerCleanPreviewModal, { preview: pv, onDone: () => void load(false) })
  } catch (e) {
    toast(String(e), 'err', 6000)
  }
}

function bannerDetailRows(g: { key: string; rows: CleanRow[] }) {
  return g.rows.filter((r) => r.isPhpo)
}

const detailGroups = computed(() => groups.value.filter((g) => bannerDetailRows(g).length > 0))
</script>

<template>
  <section class="table-wrap" id="otherPanel">
    <div class="other-head" :class="{ collapsed: !open }" id="otherHead" @click="open = !open">
      <div class="other-head-left">
        <div class="collapse-arrow" :class="{ collapsed: !open }" id="panelArrow">▾</div>
        <div class="other-head-text">
          <h3>{{ t('clean.ui.groupTitle') }}</h3>
          <p class="other-desc">{{ t('clean.ui.desc') }}</p>
        </div>
      </div>
      <div class="other-head-right" id="headRightActions">
        <button class="btn btn-sm danger" :disabled="picked.size === 0" @click.stop="onClean">
          {{ t('clean.ui.cleanBtn') }}
        </button>
      </div>
    </div>

    <div class="collapse-body" :class="{ collapsed: !open }" id="panelBody">
      <div class="error-box" id="errorBox" v-show="!!errText">
        <strong>{{ t('clean.ui.errTitle') }}</strong>{{ errText }}
      </div>

      <div class="scope-section">
        <div class="scope-title">{{ t('clean.ui.scopeTitle') }}</div>
        <div class="scope-subtitle">{{ t('clean.ui.scopeSub') }}</div>
        <div class="scope-cards">
          <div class="scope-card" :class="{ active: scopeMode === 'keep' }" data-mode="keep" id="cardKeep" @click="setScopeMode('keep')">
            <div class="card-icon">🔒</div>
            <div class="card-body">
              <div class="card-title">{{ t('clean.ui.keepTitle') }}</div>
              <div class="card-desc">
                {{ t('clean.ui.keepDesc') }}<br>
                <span class="hi-accent">{{ t('clean.ui.keepDescHi') }}</span>
              </div>
            </div>
            <div class="card-check">✓</div>
          </div>
          <div class="scope-card" :class="{ active: scopeMode === 'remove' }" data-mode="remove" id="cardRemove" @click="setScopeMode('remove')">
            <div class="card-icon">🔓</div>
            <div class="card-body">
              <div class="card-title">{{ t('clean.ui.removeTitle') }}</div>
              <div class="card-desc">
                <span class="hi-danger">{{ t('clean.ui.removeDescHi') }}</span>{{ t('clean.ui.removeDesc') }}
              </div>
            </div>
            <div class="card-check">✓</div>
          </div>
        </div>
      </div>

      <div class="status-banner" :class="scopeMode" id="statusBanner">
        <div class="banner-row-main">
          <span class="banner-icon" id="bannerIcon">{{ scopeMode === 'keep' ? '🔒' : '🔓' }}</span>
          <span class="banner-title" id="bannerTitle">
            {{ scopeMode === 'keep' ? t('clean.ui.modeKeep') : t('clean.ui.modeRemove') }}
          </span>
          <span class="banner-summary" id="bannerSummary">
            <span v-if="scanning" class="loading-text">
              {{ progress && progress.total
                ? t('clean.ui.scanProgress', { stage: progress.stage, step: progress.step, total: progress.total })
                : t('clean.ui.loading') }}
            </span>
            <span v-else>{{ t('clean.ui.phpoCount', { n: phpoCount, m: otherCount }) }}</span>
          </span>
          <span class="banner-actions">
            <button class="btn" @click.stop="bannerDetailOpen = !bannerDetailOpen">
              {{ bannerDetailOpen ? t('clean.ui.detailClose') : t('clean.ui.detailOpen') }}
            </button>
            <button class="btn" @click.stop="void load(false)">{{ t('clean.ui.research') }}</button>
          </span>
        </div>
        <div class="banner-detail" :class="{ open: bannerDetailOpen }" id="bannerDetail">
          <div class="detail-note">{{ t('clean.ui.detailNote') }}</div>
          <div id="bannerDetailBody">
            <div v-if="!detailGroups.length" class="loading-text">{{ t('clean.ui.noPhpo') }}</div>
            <div v-for="g in detailGroups" :key="g.key" class="detail-cat">
              <div class="cat-head">
                <span>{{ t('clean.group.' + g.key + '.label') }}</span>
                <span class="count">{{ bannerDetailRows(g).length }}</span>
              </div>
              <div class="cat-items">
                <span v-for="r in bannerDetailRows(g)" :key="r.key" class="tag">
                  {{ t('clean.' + r.key + '.label') }}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div class="table-toolbar">
        <button class="btn btn-sm" :disabled="scanning" @click.stop="selectAll">{{ t('clean.panel.selectAll') }}</button>
        <button class="btn btn-sm" :disabled="scanning" @click.stop="clearPick">{{ t('clean.panel.clear') }}</button>
        <button class="btn btn-sm" :disabled="scanning" @click.stop="void load(false)">{{ t('clean.panel.refresh') }}</button>
        <button
          class="btn btn-sm"
          :disabled="scanning"
          :title="deepReady ? t('clean.panel.deepTip') : t('clean.panel.deepUnsupported')"
          @click.stop="onDeep"
        >{{ t('clean.panel.deepScan') }}</button>
        <span class="chip chip-accent" id="selectedCount">{{ t('clean.ui.selectedCount', { n: picked.size }) }}</span>
        <div class="spacer"></div>
      </div>

      <table>
        <thead>
          <tr>
            <th style="width:42px;">
              <input
                type="checkbox"
                id="selectAllCheckbox"
                :checked="allChecked"
                :indeterminate="someChecked"
                @click.stop="onSelectAllClick"
              />
            </th>
            <th style="width:18%;">{{ t('clean.panel.col.row') }}</th>
            <th style="width:30%;">{{ t('clean.panel.col.desc') }}</th>
            <th style="width:13%;">{{ t('clean.panel.col.amount') }}</th>
            <th style="width:9%;">{{ t('clean.panel.col.risk') }}</th>
            <th style="width:10%;">{{ t('clean.panel.col.status') }}</th>
            <th style="width:90px; text-align:right;">{{ t('clean.ui.colAction') }}</th>
          </tr>
        </thead>
        <tbody id="otherTableBody">
          <tr v-if="scanning && !rows.length" class="loading-row">
            <td colspan="7">{{ t('clean.ui.loading') }}</td>
          </tr>
          <tr v-else-if="!rows.length && !scanning" class="loading-row">
            <td colspan="7">{{ t('clean.panel.empty') }}</td>
          </tr>

          <template v-for="g in groups" :key="g.key">
            <tr class="group-row" :class="{ collapsed: groupCollapsed(g.key) }" :data-group="g.key">
              <td colspan="7">
                <div class="group-toggle" @click.stop="toggleGroup(g.key)">
                  <span class="group-arrow">▾</span>
                  <input
                    type="checkbox"
                    :checked="groupChecked(g)"
                    :indeterminate="groupSomeChecked(g)"
                    @click.stop="toggleGroupRows(g)"
                  />
                  <span class="group-icon">📁</span>
                  <span class="group-label">{{ t('clean.group.' + g.key + '.label') }}</span>
                  <span
                    class="group-count"
                    :class="{ selected: g.rows.some((r) => picked.has(r.key)) }"
                  >{{ g.rows.filter((r) => picked.has(r.key)).length }}/{{ g.rows.length }}</span>
                </div>
              </td>
            </tr>
            <tr
              v-for="r in g.rows"
              :key="r.key"
              class="item-row"
              :class="{ 'hidden-by-group': groupCollapsed(g.key), 'locked-phpo': rowLocked(r) && r.deletable }"
            >
              <td>
                <input
                  type="checkbox"
                  :disabled="!r.deletable"
                  :checked="picked.has(r.key)"
                  @click.stop="toggleRow(r)"
                />
              </td>
              <td class="mono">
                {{ t('clean.' + r.key + '.label') }}
                <span v-if="r.dangerous" class="badge badge-danger">{{ t('clean.panel.dangerBadge') }}</span>
                <span v-if="rowLocked(r)" class="locked-badge">{{ t('clean.ui.locked') }}</span>
                <span v-if="r.tier === 3" class="badge">{{ t('clean.panel.infoBadge') }}</span>
              </td>
              <td>
                <div class="desc-clip" :title="t('clean.' + r.key + '.desc')">{{ t('clean.' + r.key + '.desc') }}</div>
              </td>
              <td class="amount">{{ rowText(r) }}</td>
              <td>
                <span class="status-pill" :class="riskPill[r.risk] ?? 'pill-warn'">
                  <span class="pill-dot"></span>{{ t('clean.risk.' + r.risk) }}
                </span>
              </td>
              <td>
                <span class="status-pill" :class="statusPill[r.status] ?? 'pill-warn'" :title="r.message ?? ''">
                  <span class="pill-dot"></span>{{ t('clean.status.' + r.status) }}
                </span>
              </td>
              <td class="row-actions">
                <button v-if="r.status !== 'ok'" class="btn btn-sm" :disabled="retrying === r.key" @click.stop="onRetry(r)">
                  {{ retrying === r.key ? t('clean.panel.retrying') : t('clean.panel.retry') }}
                </button>
                <button v-else class="icon-btn" :data-tip="t('clean.ui.previewBtn')" @click.stop="onClean">
                  <svg viewBox="0 0 24 24"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" /><circle cx="12" cy="12" r="3" /></svg>
                </button>
              </td>
            </tr>
          </template>
        </tbody>
      </table>

      <div class="other-actions">
        <button class="btn btn-sm" :disabled="scanning || picked.size === 0" @click.stop="onClean">{{ t('clean.ui.previewBtn') }}</button>
        <button class="btn btn-sm danger" :disabled="scanning || picked.size === 0" @click.stop="onClean">{{ t('clean.ui.cleanBtn') }}</button>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* 原型外观搬进应用自身主题：变量一律用 base.css 的那几颗，不动全局样式表。 */
.table-wrap { background: var(--surface); border: 1px solid var(--border); border-radius: var(--r); padding: 0; overflow: hidden; }
.other-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 14px 16px; cursor: pointer; }
.other-head-left { display: flex; align-items: center; gap: 10px; }
.other-head-text h3 { margin: 0; font-size: 14px; }
.other-desc { margin: 2px 0 0; font-size: 12px; color: var(--text-mute); }
.other-head-right { display: flex; align-items: center; gap: 8px; }
.collapse-arrow { font-size: 12px; color: var(--text-mute); transition: transform var(--motion-fast); width: 14px; }
.collapse-arrow.collapsed { transform: rotate(-90deg); }
.collapse-body { padding: 0 16px 16px; }
.collapse-body.collapsed { display: none; }

.error-box {
  display: flex; flex-wrap: wrap; gap: 6px; align-items: center;
  margin-bottom: 12px; padding: 10px 12px; font-size: 12.5px;
  border: 1px solid var(--danger); border-radius: var(--r-xs); background: var(--danger-bg); color: var(--danger);
}
.error-box strong { color: var(--danger); }

.scope-section { margin-bottom: 14px; }
.scope-title { font-size: 12.5px; font-weight: 600; margin-bottom: 2px; }
.scope-subtitle { font-size: 12px; color: var(--text-mute); margin-bottom: 10px; }
.scope-cards { display: flex; gap: 12px; flex-wrap: wrap; }
.scope-card {
  position: relative; flex: 1 1 260px; display: flex; gap: 10px; align-items: flex-start;
  padding: 12px 14px; border: 1px solid var(--border); border-radius: var(--r-sm);
  background: var(--surface-2); cursor: pointer; transition: border-color var(--motion-fast), background var(--motion-fast);
}
.scope-card:hover { border-color: var(--border-2); }
.scope-card.active { border-color: var(--accent); background: var(--accent-bg); }
.scope-card[data-mode="remove"].active { border-color: var(--danger); background: var(--danger-bg); }
.card-icon { font-size: 16px; line-height: 1.2; }
.card-body { flex: 1; }
.card-title { font-size: 12.5px; font-weight: 600; margin-bottom: 3px; }
.card-desc { font-size: 11.5px; color: var(--text-dim); line-height: 1.6; }
.card-check { font-size: 12px; color: var(--accent); opacity: 0; }
.scope-card.active .card-check { opacity: 1; }
.scope-card[data-mode="remove"].active .card-check { color: var(--danger); }
.hi-accent { color: var(--accent); }
.hi-danger { color: var(--danger); font-weight: 600; }

.status-banner { margin-bottom: 14px; padding: 10px 12px; border-radius: var(--r-sm); border: 1px solid var(--accent); background: var(--accent-bg); }
.status-banner.remove { border-color: var(--danger); background: var(--danger-bg); }
.banner-row-main { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.banner-icon { font-size: 14px; }
.banner-title { font-size: 12.5px; font-weight: 600; }
.banner-summary { flex: 1; font-size: 12px; color: var(--text-dim); }
.banner-actions { display: flex; gap: 6px; }
.banner-detail { display: none; margin-top: 10px; padding-top: 10px; border-top: 1px solid var(--border); }
.banner-detail.open { display: block; }
.detail-note { font-size: 11.5px; color: var(--text-mute); margin-bottom: 8px; }
.detail-cat { margin-bottom: 10px; }
.cat-head { display: flex; align-items: center; gap: 6px; font-size: 11.5px; color: var(--text-dim); margin-bottom: 5px; }
.cat-head .count { color: var(--text-mute); }
.cat-items { display: flex; flex-wrap: wrap; gap: 6px; }
.tag { padding: 1px 7px; font-size: 11px; border: 1px solid var(--border); border-radius: 999px; background: var(--surface-3); color: var(--text-dim); }

.table-toolbar { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; flex-wrap: wrap; }
.table-toolbar .spacer { flex: 1; }
.loading-row td { text-align: center; color: var(--text-mute); font-size: 12px; padding: 18px 0; }
.loading-text { color: var(--text-mute); font-size: 12px; }

table { width: 100%; border-collapse: collapse; }
thead th {
  text-align: left; font-size: 11.5px; font-weight: 600; color: var(--text-mute);
  padding: 8px 10px; border-bottom: 1px solid var(--border); background: var(--surface-2);
}
tbody td { padding: 8px 10px; font-size: 12.5px; border-bottom: 1px solid var(--border); vertical-align: top; }

.group-row td { padding: 0; background: var(--surface-2); border-bottom: 1px solid var(--border); }
.group-toggle { display: flex; align-items: center; gap: 8px; padding: 8px 10px; cursor: pointer; font-size: 12px; }
.group-arrow { font-size: 11px; color: var(--text-mute); transition: transform var(--motion-fast); transform: rotate(90deg); }
.group-row.collapsed .group-arrow { transform: rotate(0deg); }
.group-icon { font-size: 12px; }
.group-label { color: var(--text-dim); }
.group-count { padding: 0 6px; font-size: 11px; border-radius: 999px; border: 1px solid var(--border); color: var(--text-mute); }
.group-count.selected { border-color: var(--accent); color: var(--accent); }

.item-row.hidden-by-group { display: none; }
.item-row.locked-phpo td { opacity: .55; }
.mono { font-variant-numeric: tabular-nums; }
.amount { font-variant-numeric: tabular-nums; }
.desc-clip { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; font-size: 12px; color: var(--text-mute); }

.badge {
  display: inline-block; margin-left: 6px; padding: 0 6px; font-size: 11px;
  border: 1px solid var(--border); border-radius: 999px; color: var(--text-mute);
}
.badge-danger { color: var(--danger); border-color: var(--danger); }
.locked-badge {
  display: inline-block; margin-left: 6px; padding: 0 6px; font-size: 11px;
  border: 1px dashed var(--border-2); border-radius: 999px; color: var(--text-mute);
}

.status-pill { display: inline-flex; align-items: center; gap: 5px; font-size: 11.5px; white-space: nowrap; }
.pill-dot { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.pill-ok { color: var(--ok); }
.pill-warn { color: var(--warn); }
.pill-danger { color: var(--danger); }
.pill-off { color: var(--text-mute); }

.row-actions { text-align: right; white-space: nowrap; }
.icon-btn {
  position: relative; display: inline-flex; align-items: center; justify-content: center;
  width: 24px; height: 24px; padding: 0; margin-left: 4px;
  background: transparent; border: 1px solid var(--border); border-radius: var(--r-xs); color: var(--text-dim); cursor: pointer;
}
.icon-btn:hover { color: var(--accent); border-color: var(--accent); }
.icon-btn svg { width: 13px; height: 13px; fill: none; stroke: currentColor; stroke-width: 2; }
.icon-btn::after {
  content: attr(data-tip); position: absolute; right: 100%; top: 50%; transform: translateY(-50%);
  margin-right: 6px; padding: 3px 7px; font-size: 11px; white-space: nowrap;
  background: var(--surface-3); color: var(--text); border: 1px solid var(--border); border-radius: var(--r-xs);
  opacity: 0; pointer-events: none; transition: opacity var(--motion-fast);
}
.icon-btn:hover::after { opacity: 1; }

.other-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 12px; }
.btn.danger { background: var(--danger); border-color: var(--danger); color: #fff; }
.btn.danger:disabled { opacity: .5; }

input[type="checkbox"] { width: 15px; height: 15px; accent-color: var(--accent); cursor: pointer; vertical-align: middle; }

@media (max-width: 760px) {
  .scope-cards { flex-direction: column; }
  .banner-row-main { align-items: flex-start; }
  .banner-actions { width: 100%; justify-content: flex-end; }
  .other-head { flex-direction: column; align-items: flex-start; }
  .table-toolbar { gap: 6px; }
}
</style>
