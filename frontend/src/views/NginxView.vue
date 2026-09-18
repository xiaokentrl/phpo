<script setup lang="ts">
// nginx 服务视图：复用 ServiceView（原型 renderService('nginx')）
import ServiceView from './ServiceView.vue'
</script>

<template>
  <ServiceView kind="nginx" />
</template>
