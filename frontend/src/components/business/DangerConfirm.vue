<script setup lang="ts">
// 危险确认模态：1:1 迁移 openDangerConfirm（1550–1573）；被 5 类删除/危险操作复用
// §1.4（总纲 v2.9.11）：界面不展示 `phpo …` 伪命令行——确认信息由标题、说明与 warnings 的人话文本承载。
import { computed, ref } from 'vue'
import { useI18n } from '@/composables/useI18n'
import ModalShell from '@/components/common/ModalShell.vue'

interface Warning {
  text: string
  keep?: boolean
}
interface Checkbox {
  label: string
}
const props = defineProps<{
  title: string
  description?: string
  warnings?: Warning[]
  confirmLabel: string
  checkbox?: Checkbox
  onConfirm?: () => void
}>()
const emit = defineEmits<{ close: []; confirm: [] }>()
const { t } = useI18n()

const checked = ref(!props.checkbox)
const canConfirm = computed(() => checked.value)

function onCheck(e: Event) {
  checked.value = (e.target as HTMLInputElement).checked
}
function confirm() {
  if (!canConfirm.value) return
  emit('close')
  emit('confirm')
  props.onConfirm?.()
}
</script>

<template>
  <ModalShell danger @close="emit('close')">
    <template #head>
      <div class="danger-header">
        <div class="danger-icon">⚠</div>
        <div style="flex: 1">
          <h3>{{ title }}</h3>
          <p v-if="description">{{ description }}</p>
        </div>
      </div>
    </template>
    <template #body>
      <ul v-if="warnings && warnings.length" class="danger-list">
        <li v-for="(w, i) in warnings" :key="i" :class="{ keep: w.keep }">{{ w.text }}</li>
      </ul>
      <label v-if="checkbox" class="danger-check" :class="{ checked }">
        <input type="checkbox" :checked="checked" @change="onCheck" />
        <span class="check-label">{{ checkbox.label }}</span>
      </label>
    </template>
    <template #foot>
      <button class="btn" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button class="btn btn-danger" :disabled="!canConfirm" style="background: var(--danger-bg); border-color: var(--danger-bg)" @click="confirm">{{ confirmLabel }}</button>
    </template>
  </ModalShell>
</template>
