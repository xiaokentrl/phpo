<script setup lang="ts">
// pgsql 服务视图：复用 ServiceView（原型 renderService('pgsql')）
import ServiceView from './ServiceView.vue'
</script>

<template>
  <ServiceView kind="pgsql" />
</template>
