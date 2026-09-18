<script setup lang="ts">
// Overview 视图：1:1 迁移原型 renderOverview（2714–2727）
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from '@/composables/useI18n'
import { useAppState } from '@/stores/appState'
import { runTask } from '@/composables/useTask'
import { SVC_META } from '@/constants/service'
import type { ServiceKind } from '@/types'

const { t } = useI18n()
const state = useAppState()
const router = useRouter()

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
      <div v-if="all.length > 0" class="header-actions">
        <button class="btn" data-action="run-task" data-args="doctor" :data-label="t('overview.doctorTask')" @click="runTask(['doctor'], t('overview.doctorTask'))">{{ t('overview.doctor') }}</button>
      </div>
    </header>

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
