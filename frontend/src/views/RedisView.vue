<script setup lang="ts">
// redis 服务视图：复用 ServiceView（原型 renderService('redis')）
import ServiceView from './ServiceView.vue'
</script>

<template>
  <ServiceView kind="redis" />
</template>
