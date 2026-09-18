<script setup lang="ts">
// 伪静态弹窗：忠实迁移原型 openRewriteModal（2805–2847）
// 框架网格 + 规则编辑器 + 折叠预览 + 脏态应用；应用后走 preflight('rewrite')
import { computed, ref } from 'vue'
import ModalShell from '@/components/common/ModalShell.vue'
import { useI18n } from '@/composables/useI18n'
import { usePreflight } from '@/composables/usePreflight'
import { toast } from '@/composables/useToast'
import { runTask } from '@/composables/useTask'
import { setSiteRewrite, hasBackend } from '@/api/site'
import { useAppState } from '@/stores/appState'
import { REWRITE_PRESETS } from '@/constants/rewrite'

const props = defineProps<{ domain: string }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { preflight } = usePreflight()
const app = useAppState()

const site = computed(() => app.sites.find((s) => s.domain === props.domain))
const sitesRoot = computed(() => app.env.NGINX_SITES_ROOT || `${app.env.PHPO_HOME}/nginx/sites`)
const siteRoot = computed(() => site.value?.root || '')

const initialPreset = (site.value?.rewrite || 'none') as string
const initialRule = (typeof site.value?.rewriteRule === 'string' && site.value.rewriteRule.trim())
  ? site.value.rewriteRule
  : (REWRITE_PRESETS[initialPreset]?.rule || REWRITE_PRESETS.custom.rule)

const selected = ref(initialPreset)
const rule = ref(initialRule)
const previewOpen = ref(false)

const dirty = computed(() => selected.value !== initialPreset || rule.value !== initialRule)
const presets = computed(() => Object.entries(REWRITE_PRESETS))

function pick(key: string): void {
  selected.value = key
  rule.value = REWRITE_PRESETS[key].rule
}

function onApply(): void {
  const check = preflight('rewrite', { domain: props.domain, rule: rule.value })
  if (!check.ok) { toast(check.errors.join('\n'), 'err', 4600); return }
  const preset = REWRITE_PRESETS[selected.value]
  const ruleToStore = finalRuleEqualsPreset(preset.rule) ? '' : rule.value
  emit('close')
  if (!hasBackend()) {
    runTask(['site', 'rewrite', props.domain, '--preset', selected.value], `${props.domain} · ${t(preset.nameKey)}`, { type: 'rewrite', domain: props.domain, preset: selected.value, rule: ruleToStore })
    return
  }
  setSiteRewrite(props.domain, selected.value, ruleToStore).catch((e: unknown) => toast(String(e), 'err', 4600))
}
function finalRuleEqualsPreset(presetRule: string): boolean {
  return rule.value === presetRule
}
</script>

<template>
  <ModalShell size="lg">
    <template #head>
      <h3>{{ domain }} · {{ t('rw.title') }}</h3>
      <p>{{ t('rw.subtitle') }}</p>
    </template>
    <template #body>
      <div class="field">
        <label>{{ t('rw.select') }}</label>
        <div class="fw-grid">
          <div
            v-for="[key, p] in presets"
            :key="key"
            class="fw-card"
            :class="{ selected: key === selected }"
            @click="pick(key)"
          >
            <div class="fw-icon">{{ p.icon }}</div>
            <div class="fw-name">{{ t(p.nameKey) }}</div>
            <div class="fw-tag">{{ p.tag }}</div>
          </div>
        </div>
      </div>
      <div class="field">
        <label>{{ t('rw.rule') }} <span style="font-weight: 400; color: var(--text-mute); font-size: 11.5px">{{ t('rw.rule.hint') }}</span></label>
        <textarea v-model="rule" class="rw-editor" spellcheck="false" />
        <div class="hint">{{ t('rw.writeHint', { domain, root: siteRoot }) }}</div>
      </div>
      <div class="field">
        <button class="rw-collapse-btn" :class="{ collapsed: !previewOpen }" type="button" @click="previewOpen = !previewOpen">
          <span class="arrow">▼</span>
          <span>{{ previewOpen ? t('rw.hidePreview') : t('rw.showPreview') }}</span>
        </button>
        <div v-if="previewOpen" class="cmd-preview" style="white-space: pre-wrap; max-height: 240px; overflow-y: auto">{{ rule }}</div>
      </div>
      <div class="alert alert-info">{{ t('rw.applyHint') }}</div>
    </template>
    <template #foot>
      <button class="btn" type="button" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" type="button" :disabled="!dirty" @click="onApply">{{ dirty ? t('rw.apply') : t('rw.current') }}</button>
    </template>
  </ModalShell>
</template>
