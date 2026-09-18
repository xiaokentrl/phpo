<script setup lang="ts">
// 模态宿主：渲染 modalStore.current 指向的模态组件（全局单例）
import { useModalStore } from '@/stores/modalStore'
const modal = useModalStore()
</script>

<template>
  <component :is="modal.current" v-if="modal.current" v-bind="modal.props" @close="modal.close()" />
</template>
