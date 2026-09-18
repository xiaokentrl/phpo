<script setup lang="ts">
// 模态外壳：1:1 对应原型 openModalShell + setupModalResize（1502–1532）
// 遮罩点击关闭 / ESC 关闭（danger 时禁用）/ 右下角拖拽 resize（缩放补偿）
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useResize } from '@/composables/useResize'
import { useI18n } from '@/composables/useI18n'

const props = withDefaults(defineProps<{ size?: '' | 'lg' | 'xl'; danger?: boolean; bodyConfig?: boolean }>(), { size: '', danger: false, bodyConfig: false })
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()

const modalEl = ref<HTMLElement | null>(null)
const sizeStyle = ref<Record<string, string>>({})

const { startResize } = useResize({
  minWidth: 360,
  minHeight: 240,
  getSize: () => {
    const el = modalEl.value
    return { w: el?.offsetWidth ?? 480, h: el?.offsetHeight ?? 320 }
  },
  setSize: (w, h) => {
    sizeStyle.value = { width: w + 'px', height: h + 'px', maxWidth: 'none', maxHeight: 'none' }
  },
})

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && !props.danger) emit('close')
}
onMounted(() => document.addEventListener('keydown', onKey))
onBeforeUnmount(() => document.removeEventListener('keydown', onKey))

function onMask(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('modal-root')) emit('close')
}
</script>

<template>
  <div class="modal-root open" @click="onMask">
    <div ref="modalEl" class="modal" :class="{ 'modal-lg': size === 'lg', 'modal-xl': size === 'xl' }" role="dialog" aria-modal="true" :style="sizeStyle">
      <button class="modal-close-x" type="button" :aria-label="t('common.cancel')" @click="emit('close')">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12" /></svg>
      </button>
      <div v-if="$slots.head" class="modal-head"><slot name="head" /></div>
      <div v-if="$slots.body" class="modal-body" :class="{ 'config-body': bodyConfig }"><slot name="body" /></div>
      <slot />
      <div v-if="$slots.foot" class="modal-foot"><slot name="foot" /></div>
      <div class="modal-resizer" @pointerdown="startResize" />
    </div>
  </div>
</template>
