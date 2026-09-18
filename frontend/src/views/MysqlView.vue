<script setup lang="ts">
// mysql 服务视图：复用 ServiceView（原型 renderService('mysql')）
import ServiceView from './ServiceView.vue'
</script>

<template>
  <ServiceView kind="mysql" />
</template>
