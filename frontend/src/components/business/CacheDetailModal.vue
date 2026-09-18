<script setup lang="ts">
// 缓存详情弹窗：M6 才接真实逻辑，原型 renderOffline 有 demo 数据但无独立详情模态源码、亦无 cache.* i18n 冻结键。
// 依「唯一真实来源=原型」——此处仅留最小外壳占位，不虚构界面与键。真实实现见 M6（manifest/SHA256/清理模式）。
import ModalShell from '@/components/common/ModalShell.vue'
defineProps<{ svc?: string; ver?: string }>()
const emit = defineEmits<{ close: [] }>()
</script>

<template>
  <ModalShell>
    <template #body>
      <p style="margin: 0">CacheDetailModal — 占位（M6 接入）</p>
    </template>
    <template #foot>
      <button class="btn" type="button" @click="emit('close')">Close</button>
    </template>
  </ModalShell>
</template>
