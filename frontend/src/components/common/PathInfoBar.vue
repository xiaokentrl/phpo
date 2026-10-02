<script setup lang="ts">
// 路径信息条：1:1 迁移原型 pathInfoBar()（1140–1149）
import { useI18n } from '@/composables/useI18n'
import { copyText } from '@/utils/str'
import { toast } from '@/composables/useToast'
const { t } = useI18n()
const props = defineProps<{ label: string; path: string }>()

// 那颗按钮只复制完整路径：成功与失败各给一句提示，不改任何其他状态
function onCopy(): void {
  copyText(props.path).then((ok) => toast(ok ? t('common.copied') : t('common.copyFailed'), ok ? 'ok' : 'err', 2400))
}
</script>

<template>
  <div class="path-info-bar">
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /></svg>
    <span class="pib-label">{{ label }}</span>
    <code class="pib-path" :title="path">{{ path }}</code>
    <button class="pib-copy" type="button" @click="onCopy" :title="t('common.copy')">
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="9" y="9" width="12" height="12" rx="2" /><path d="M5 15V5a2 2 0 0 1 2-2h10" /></svg>
    </button>
  </div>
</template>
