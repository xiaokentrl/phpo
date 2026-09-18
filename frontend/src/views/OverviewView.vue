<script setup lang="ts">
// Overview 视图：1:1 迁移原型 renderOverview（2714–2727）；doctor 接真（T603 §5.7）
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from '@/composables/useI18n'
import { useAppState } from '@/stores/appState'
import { toast } from '@/composables/useToast'
import { runDoctor, fixDoctor } from '@/api/doctor'
import { SVC_META } from '@/constants/service'
import type { DoctorReport, DoctorStatus, ServiceKind } from '@/types'

const { t } = useI18n()
const state = useAppState()
const router = useRouter()

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
    toast(String(e), 'err', 4600)
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
    toast(String(e), 'err', 4600)
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
</script>

<template>
  <div class="view-inner">
    <header class="view-header">
      <div>
        <h1>{{ t('overview.title') }}</h1>
        <p class="view-sub">{{ t('overview.subtitle') }}</p>
      </div>
      <div class="header-actions">
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
      <button class="btn btn-primary" data-action="install" data-kind="php">{{ t('php.empty.action') }}</button>
    </div>

    <template v-else>
      <div class="summary">
        <div class="summary-item"><div class="summary-num">{{ all.length }}</div><div class="summary-label">{{ t('overview.instances') }}</div></div>
        <div class="summary-item"><div class="summary-num" style="color: var(--ok)">{{ running }}</div><div class="summary-label">{{ t('overview.running') }}</div></div>
        <div class="summary-item"><div class="summary-num">{{ stopped }}</div><div class="summary-label">{{ t('overview.stopped') }}</div></div>
        <div class="summary-item"><div class="summary-num">{{ lines }}</div><div class="summary-label">{{ t('overview.lines') }}</div></div>
      </div>

      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th style="width: 22%">{{ t('overview.col.svc') }}</th>
              <th style="width: 16%">{{ t('overview.col.ver') }}</th>
              <th style="width: 18%">{{ t('overview.col.status') }}</th>
              <th style="width: 30%">{{ t('overview.col.container') }}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in all" :key="`${item.kind}/${item.version}`">
              <td>
                <div class="svc-cell"><span class="svc-icon">{{ SVC_META[item.kind].icon }}</span>{{ t(SVC_META[item.kind].titleKey) }}</div>
              </td>
              <td><span class="chip chip-accent">{{ item.version }}</span></td>
              <td>
                <span v-if="item.running" class="status-pill pill-ok"><span class="pill-dot"></span>{{ t('svc.running') }}</span>
                <span v-else class="status-pill pill-off"><span class="pill-dot"></span>{{ t('svc.stopped') }}</span>
              </td>
              <td><span class="mono" style="font-size: 11.5px; color: var(--text-mute)">phpo-{{ item.kind }}-{{ item.version }}</span></td>
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
</style>
