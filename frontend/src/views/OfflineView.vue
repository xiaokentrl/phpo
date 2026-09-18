<script setup lang="ts">
// Offline 视图：1:1 迁移原型 renderOffline（2698–2704）
import { useI18n } from '@/composables/useI18n'
import { useAppState } from '@/stores/appState'
import { useModals } from '@/composables/useModals'
import { runTask } from '@/composables/useTask'
import { SVC_ICON } from '@/constants/service'
import PathInfoBar from '@/components/common/PathInfoBar.vue'

const { t } = useI18n()
const state = useAppState()
const modals = useModals()
</script>

<template>
  <div class="view-inner">
    <header class="view-header">
      <div>
        <h1>{{ t('offline.title') }}</h1>
        <p class="view-sub">{{ t('offline.subtitle') }}</p>
      </div>
      <div class="header-actions">
        <button class="btn" data-action="run-task" data-args="offline,verify" :data-label="t('offline.verifyAll')" @click="runTask(['offline','verify'], t('offline.verifyAll'))">{{ t('offline.verifyAll') }}</button>
      </div>
    </header>

    <PathInfoBar :label="t('offline.pathLabel')" :path="state.env.OFFLINE_ROOT + '/'" />

    <div class="summary">
      <div class="summary-item"><div class="summary-num">{{ state.offline.total }}</div><div class="summary-label">{{ t('offline.total') }}</div></div>
      <div class="summary-item"><div class="summary-num">{{ state.offline.trees.length }}</div><div class="summary-label">{{ t('offline.entries') }}</div></div>
      <div class="summary-item"><div class="summary-num" style="color: var(--ok)">{{ state.offline.trees.length }}</div><div class="summary-label">{{ t('offline.verified') }}</div></div>
      <div class="summary-item"><div class="summary-num" style="color: var(--warn)">2.0 GB</div><div class="summary-label">{{ t('offline.threshold') }}</div></div>
    </div>

    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th style="width: 14%">{{ t('offline.col.svc') }}</th>
            <th style="width: 10%">{{ t('offline.col.ver') }}</th>
            <th style="width: 26%">{{ t('offline.col.path') }}</th>
            <th style="width: 10%">{{ t('offline.col.size') }}</th>
            <th style="width: 8%">{{ t('offline.col.items') }}</th>
            <th style="width: 16%">{{ t('offline.col.lastVerify') }}</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="tr in state.offline.trees" :key="`${tr.svc}/${tr.ver}`">
            <td>
              <div class="svc-cell"><span class="svc-icon">{{ SVC_ICON[tr.svc] || '📦' }}</span>{{ tr.svc }}</div>
            </td>
            <td><span class="chip chip-accent">{{ tr.ver }}</span></td>
            <td>
              <code class="mono" style="font-size: 11px; color: var(--text-dim); word-break: break-all; line-height: 1.4" :title="`${state.env.OFFLINE_ROOT}/${tr.svc}/${tr.ver}/`">{{ `${state.env.OFFLINE_ROOT}/${tr.svc}/${tr.ver}/` }}</code>
            </td>
            <td><span class="mono" style="color: var(--text-dim)">{{ tr.size }}</span></td>
            <td><span class="mono" style="color: var(--text-mute)">{{ tr.items }}</span></td>
            <td><span class="mono" style="color: var(--text-mute); font-size: 12px">{{ tr.verified }}</span></td>
            <td>
              <div class="row-actions">
                <button class="btn btn-sm" data-action="run-task" :data-args="`offline,verify,${tr.svc},${tr.ver}`" :data-label="`verify ${tr.svc} ${tr.ver}`" @click="runTask(['offline','verify',tr.svc,tr.ver], 'verify '+tr.svc+' '+tr.ver)">{{ t('offline.verify') }}</button>
                <button class="btn btn-sm btn-danger" data-action="offline-prune" :data-svc="tr.svc" :data-ver="tr.ver" :data-size="tr.size" @click="modals.openOfflinePruneModal(tr.svc, tr.ver, String(tr.size))">{{ t('offline.prune') }}</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
