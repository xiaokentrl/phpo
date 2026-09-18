<script setup lang="ts">
// 挂载列表：忠实渲染原型 mountListHtml / mountRowHtml（.mount-list / .mount-row）
import { computed } from 'vue'
import { useAppState } from '@/stores/appState'
import { resolveMounts } from '@/constants/mounts'
import type { ServiceKind } from '@/types'

const props = defineProps<{ kind: string; version: string }>()
const app = useAppState()

const rows = computed(() => resolveMounts(app.env, props.kind as ServiceKind, props.version))
</script>

<template>
  <div v-if="rows.length" class="mount-list">
    <div v-for="(m, i) in rows" :key="i" class="mount-row">
      <span class="m-host">{{ m.host }}</span>
      <span class="m-arrow">→</span>
      <span class="m-cont">{{ m.to }}</span>
      <span :class="m.mode === 'ro' ? 'm-mode ro' : 'm-mode'">{{ m.mode }}</span>
      <span class="m-label">{{ m.label || '' }}</span>
    </div>
  </div>
</template>
