<script setup lang="ts">
// 主题选择弹窗：忠实迁移原型 openThemePicker（2790–2797）
// 网格卡片；点击即时切换主题（prefsStore 负责落地 + 持久化）
import ModalShell from '@/components/common/ModalShell.vue'
import { useI18n } from '@/composables/useI18n'
import { usePrefsStore, type ThemeId } from '@/stores/prefsStore'
import { THEMES } from '@/constants/themes'

const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const prefs = usePrefsStore()
</script>

<template>
  <ModalShell size="lg">
    <template #head>
      <h3>{{ t('theme.title') }}</h3>
      <p>{{ t('theme.subtitle') }}</p>
    </template>
    <template #body>
      <div class="theme-grid">
        <div
          v-for="th in THEMES"
          :key="th.id"
          class="theme-card"
          :class="{ selected: th.id === prefs.theme }"
          @click="prefs.setTheme(th.id as ThemeId)"
        >
          <div class="theme-preview">
            <div class="theme-swatch theme-swatch-bg" :style="{ background: th.colors[0] }" />
            <div v-for="(c, i) in th.colors.slice(1)" :key="i" class="theme-swatch" :style="{ background: c }" />
          </div>
          <div class="theme-name">{{ t(th.nameKey) }}</div>
          <div class="theme-desc">{{ t(th.descKey) }}</div>
        </div>
      </div>
    </template>
    <template #foot>
      <button class="btn" type="button" @click="emit('close')">{{ t('theme.done') }}</button>
    </template>
  </ModalShell>
</template>
