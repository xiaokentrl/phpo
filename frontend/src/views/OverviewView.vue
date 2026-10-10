<script setup lang="ts">
// Overview 视图：1:1 迁移原型 renderOverview（2714–2727）；doctor 接真（T603 §5.7）
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from '@/composables/useI18n'
import { useAppState } from '@/stores/appState'
import { toast } from '@/composables/useToast'
import { backendMsg } from '@/utils/backendMsg'
import { runDoctor, fixDoctor } from '@/api/doctor'
import { useModals } from '@/composables/useModals'
import { SVC_META } from '@/constants/service'
import DockerCleanPanel from '@/components/business/DockerCleanPanel.vue'
import type { DoctorReport, DoctorStatus, ServiceKind } from '@/types'

const { t } = useI18n()
const state = useAppState()
const router = useRouter()
const { openCleanupModal, openTrashModal, openInstallModal } = useModals()

const report = ref<DoctorReport | null>(null)
const diagnosing = ref(false)
const fixing = ref('')

async function runDiagnose() {
  if (diagnosing.value) return
  diagnosing.value = true
  try {
    const r = await runDoctor()
    report.value = r
    if (!r) toast(t('doctor.demoHint'), 'info', 3200)
  } catch (e) {
    toast(backendMsg(String(e)), 'err', 4600)
  } finally {
    diagnosing.value = false
  }
}

async function onFix(id: string) {
  if (fixing.value) return
  fixing.value = id
  try {
    await fixDoctor(id)
    const r = await runDoctor()
    report.value = r
    toast(t('doctor.fix') + ' ✓', 'ok', 2200)
  } catch (e) {
    toast(backendMsg(String(e)), 'err', 4600)
  } finally {
    fixing.value = ''
  }
}

const pillOf: Record<DoctorStatus, string> = { ok: 'pill-ok', warn: 'pill-warn', err: 'pill-err' }

interface Row {
  kind: ServiceKind
  version: string
  running: boolean
}

const all = computed<Row[]>(() => {
  const out: Row[] = []
  ;(Object.keys(state.installed) as ServiceKind[]).forEach((kind) => {
    state.installed[kind].forEach((version) => out.push({ kind, version, running: state.isServiceRunning(kind, version) }))
  })
  return out
})
const running = computed(() => all.value.filter((x) => x.running).length)
const stopped = computed(() => all.value.length - running.value)
const lines = computed(() => new Set(all.value.map((x) => x.kind)).size)

function jump(kind: ServiceKind) {
  router.push('/' + kind)
}

// ---- 批量操作（需求）：表格左侧多选列，支持全选/反选；对选中项批量启动/停止/删除（走任务队列） ----
const { batchStartService, batchStopService, batchRemoveService } = useModals()

const selected = ref<Set<string>>(new Set())
const keyOf = (r: Row): string => `${r.kind}/${r.version}`
const selectedRows = computed(() => all.value.filter((r) => selected.value.has(keyOf(r))))
const allSelected = computed(() => all.value.length > 0 && selectedRows.value.length === all.value.length)
const someSelected = computed(() => selectedRows.value.length > 0 && !allSelected.value)
// 启停按状态过滤适用行：停止只对运行中的、启动只对已停止的——对不在状态里的服务入队只会换来一颗报错任务
const stoppable = computed(() => selectedRows.value.filter((r) => r.running))
const startable = computed(() => selectedRows.value.filter((r) => !r.running))

function toggleAll(): void {
  selected.value = allSelected.value ? new Set() : new Set(all.value.map(keyOf))
}
function toggleRow(r: Row): void {
  const s = new Set(selected.value)
  if (s.has(keyOf(r))) s.delete(keyOf(r))
  else s.add(keyOf(r))
  selected.value = s
}
</script>

<template>
  <div class="view-inner">
    <header class="view-header">
      <div>
        <h1>{{ t('overview.title') }}</h1>
        <p class="view-sub">{{ t('overview.subtitle') }}</p>
      </div>
      <div class="header-actions">
        <button class="btn" @click="openCleanupModal">{{ t('cleanup.title') }}</button>
        <button class="btn" @click="openTrashModal">{{ t('cleanup.trash.title') }}</button>
        <button class="btn" :disabled="diagnosing" @click="runDiagnose">{{ diagnosing ? t('doctor.diagnosing') : (report ? t('doctor.recheck') : t('doctor.run')) }}</button>
      </div>
    </header>

    <div v-if="report" class="doctor-card">
      <div class="doctor-head">
        <strong>{{ t('doctor.section') }}</strong>
        <span v-if="report.errors === 0 && report.warnings === 0" class="doctor-good">{{ t('doctor.allGood') }}</span>
        <span v-else class="doctor-sum">{{ t('doctor.summary', { ok: report.ok, warnings: report.warnings, errors: report.errors }) }}</span>
      </div>
      <ul class="doctor-list">
        <li v-for="c in report.checks" :key="c.id" class="doctor-row">
          <span class="status-pill" :class="pillOf[c.status]"><span class="pill-dot"></span>{{ c.title }}</span>
          <span class="doctor-detail">{{ c.detail }}<span v-if="c.hint" class="doctor-hint"> · {{ c.hint }}</span></span>
          <button v-if="c.fix" class="btn btn-sm" :disabled="fixing === c.fix" @click="onFix(c.fix)">{{ t('doctor.fix.' + c.fix) }}</button>
        </li>
      </ul>
    </div>

    <div v-if="all.length === 0" class="empty">
      <div class="empty-icon">🚀</div>
      <h2>{{ t('overview.empty.title') }}</h2>
      <p>{{ t('overview.empty.desc') }}</p>
      <button class="btn btn-primary" data-action="install" data-kind="php" @click="openInstallModal('php')">{{ t('php.empty.action') }}</button>
    </div>

    <template v-else>
      <div class="summary">
        <div class="summary-item"><div class="summary-num">{{ all.length }}</div><div class="summary-label">{{ t('overview.instances') }}</div></div>
        <div class="summary-item"><div class="summary-num ov-summary-ok">{{ running }}</div><div class="summary-label">{{ t('overview.running') }}</div></div>
        <div class="summary-item"><div class="summary-num">{{ stopped }}</div><div class="summary-label">{{ t('overview.stopped') }}</div></div>
        <div class="summary-item"><div class="summary-num">{{ lines }}</div><div class="summary-label">{{ t('overview.lines') }}</div></div>
      </div>

      <!-- 批量操作栏：有勾选才出现（需求）——删除是危险操作，弹窗里另有勾选确认 -->
      <div v-if="selectedRows.length" class="ov-batch-bar">
        <span class="ov-batch-count">{{ t('overview.batch.selected', { n: selectedRows.length }) }}</span>
        <button class="btn btn-sm" :disabled="!startable.length" @click="batchStartService(startable)">{{ t('overview.batch.start') }}</button>
        <button class="btn btn-sm" :disabled="!stoppable.length" @click="batchStopService(stoppable)">{{ t('overview.batch.stop') }}</button>
        <button class="btn btn-sm btn-danger" @click="batchRemoveService(selectedRows)">{{ t('overview.batch.delete') }}</button>
      </div>

      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th class="ov-th-check">
                <input
                  type="checkbox"
                  :checked="allSelected"
                  :indeterminate="someSelected"
                  :aria-label="t('overview.batch.selectAll')"
                  @change="toggleAll"
                />
              </th>
              <th class="ov-th-svc">{{ t('overview.col.svc') }}</th>
              <th class="ov-th-ver">{{ t('overview.col.ver') }}</th>
              <th class="ov-th-status">{{ t('overview.col.status') }}</th>
              <th class="ov-th-container">{{ t('overview.col.container') }}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in all" :key="`${item.kind}/${item.version}`">
              <td class="ov-td-check">
                <input
                  type="checkbox"
                  :checked="selected.has(`${item.kind}/${item.version}`)"
                  :aria-label="`${t(SVC_META[item.kind].titleKey)} ${item.version}`"
                  @change="toggleRow(item)"
                />
              </td>
              <td>
                <div class="svc-cell"><span class="svc-icon">{{ SVC_META[item.kind].icon }}</span>{{ t(SVC_META[item.kind].titleKey) }}</div>
              </td>
              <td><span class="chip chip-accent">{{ item.version }}</span></td>
              <td>
                <span v-if="item.running" class="status-pill pill-ok"><span class="pill-dot"></span>{{ t('svc.running') }}</span>
                <span v-else class="status-pill pill-off"><span class="pill-dot"></span>{{ t('svc.stopped') }}</span>
              </td>
              <td><span class="mono ov-td-container">phpo-{{ item.kind }}-{{ item.version }}</span></td>
              <td>
                <div class="row-actions">
                  <button class="btn btn-sm" @click="jump(item.kind)">{{ t('overview.manage') }}</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <DockerCleanPanel />
  </div>
</template>

<style scoped>
.doctor-card {
  margin-bottom: 18px;
  padding: 14px 16px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 10px;
}
.doctor-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}
.doctor-sum {
  font-size: 12.5px;
  color: var(--text-mute);
}
.doctor-good {
  font-size: 12.5px;
  color: var(--ok);
}
.doctor-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.doctor-row {
  display: flex;
  align-items: center;
  gap: 12px;
}
.doctor-detail {
  flex: 1;
  font-size: 12.5px;
  color: var(--text);
}
.doctor-hint {
  color: var(--text-mute);
}
.ov-summary-ok {
  color: var(--ok);
}
.ov-th-svc {
  width: 22%;
}
.ov-th-check {
  width: 36px;
}
.ov-td-check {
  text-align: center;
}
.ov-td-check input,
.ov-th-check input {
  accent-color: var(--accent, var(--text));
  cursor: pointer;
}
.ov-batch-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0 0 10px;
  padding: 6px 10px;
  border: 1px solid var(--border-2);
  border-radius: var(--r-xs, 6px);
  background: var(--surface-2);
}
.ov-batch-count {
  font-size: 12px;
  color: var(--text);
  margin-right: auto;
}
.ov-th-ver {
  width: 16%;
}
.ov-th-status {
  width: 18%;
}
.ov-th-container {
  width: 30%;
}
.ov-td-container {
  font-size: 11.5px;
  color: var(--text-mute);
}
</style>
