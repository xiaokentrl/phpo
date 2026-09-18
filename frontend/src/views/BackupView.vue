<script setup lang="ts">
// Backup 视图：1:1 迁移原型 renderBackup（2694–2696）
import { useI18n } from '@/composables/useI18n'
import { useAppState } from '@/stores/appState'
import { useModals } from '@/composables/useModals'
import PathInfoBar from '@/components/common/PathInfoBar.vue'

const { t } = useI18n()
const state = useAppState()
const modals = useModals()
</script>

<template>
  <div class="view-inner">
    <header class="view-header">
      <div>
        <h1>{{ t('backup.title') }}</h1>
        <p class="view-sub">{{ t('backup.subtitle') }}</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-primary" data-action="run-task" data-args="backup" :data-label="t('backup.nowTask')" data-pf-action="backup" @click="modals.runGuardedTask('backup', {}, ['backup'], t('backup.nowTask'))">{{ t('backup.now') }}</button>
      </div>
    </header>

    <PathInfoBar :label="t('backup.pathLabel')" :path="state.env.BACKUP_ROOT + '/'" />
    <div class="alert alert-warn" style="margin-bottom: 16px">{{ t('backup.warning') }}</div>

    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th style="width: 32%">{{ t('backup.col.file') }}</th>
            <th style="width: 10%">{{ t('backup.col.size') }}</th>
            <th style="width: 18%">{{ t('backup.col.time') }}</th>
            <th style="width: 10%">{{ t('backup.col.content') }}</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="b in state.backups" :key="b.file">
            <td><span class="mono" style="font-size: 12.5px">{{ b.file }}</span></td>
            <td><span class="mono" style="color: var(--text-dim)">{{ b.size }}</span></td>
            <td><span class="mono" style="color: var(--text-mute); font-size: 12px">{{ b.at }}</span></td>
            <td><span class="chip">{{ b.items }} {{ t('backup.col.items') }}</span></td>
            <td>
              <div class="row-actions">
                <button class="btn btn-sm" data-action="download-backup" :data-file="b.file">
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><path d="M7 10l5 5 5-5M12 15V3" /></svg>
                  {{ t('backup.download') }}
                </button>
                <button class="btn btn-sm" data-action="restore" :data-file="b.file" @click="modals.openRestoreModal(b.file)">{{ t('backup.restore') }}</button>
                <button class="btn btn-sm btn-danger" data-action="delete-backup" :data-file="b.file" @click="modals.openDeleteBackupModal(b.file)">{{ t('backup.delete') }}</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
