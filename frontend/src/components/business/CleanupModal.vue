<script setup lang="ts">
// 清理弹窗（T605 / §5.13.6-7）：外壳承载 CleanupView（孤儿三模式 + 回收站入口 + 审计）。
// 硬红线 4：面板数据只来自后端读接口与 docker:* 事件；无独立 NAV 路由（§0.3 路由数=10），仅从 doctor 入口打开。
import ModalShell from '@/components/common/ModalShell.vue'
import CleanupView from '@/views/CleanupView.vue'
import { useI18n } from '@/composables/useI18n'

const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
</script>

<template>
  <ModalShell>
    <template #head>
      <h3>{{ t('cleanup.title') }}</h3>
      <p>{{ t('cleanup.subtitle') }}</p>
    </template>
    <template #body>
      <CleanupView />
    </template>
    <template #foot>
      <button class="btn" type="button" @click="emit('close')">{{ t('common.close') }}</button>
    </template>
  </ModalShell>
</template>
