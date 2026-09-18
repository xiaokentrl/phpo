<script setup lang="ts">
// 升级弹窗：M6 才接真实逻辑，原型无此模态源码、亦无 update.* i18n 冻结键。
// 依 §0.2「唯一真实来源=原型」「禁止臆测数字/文案」——此处仅留最小外壳占位，不虚构界面与键。
// 真实实现见 T107→M6（internal/updater + 检查/下载/SHA256+签名/安装）。
import ModalShell from '@/components/common/ModalShell.vue'
const emit = defineEmits<{ close: [] }>()
</script>

<template>
  <ModalShell>
    <template #body>
      <p style="margin: 0">UpdateModal — 占位（M6 接入）</p>
    </template>
    <template #foot>
      <button class="btn" type="button" @click="emit('close')">Close</button>
    </template>
  </ModalShell>
</template>
