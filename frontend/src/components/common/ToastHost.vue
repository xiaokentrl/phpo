<script setup lang="ts">
// Toast 容器：忠实渲染原型 #toast-root 的 .toast-root / .toast.{kind}
import { useToast } from '@/composables/useToast'
const { toasts } = useToast()
</script>

<template>
  <div class="toast-root">
    <div v-for="it in toasts" :key="it.id" class="toast" :class="it.kind">{{ it.msg }}</div>
  </div>
</template>
