<script setup lang="ts">
// 站点 nginx 配置弹窗：忠实迁移原型 openSiteConfigModal（2849–2875）
// 编辑 vhost 正文；脏态启用保存/重置；保存走 preflight('site-vhost')
import { computed, ref } from 'vue'
import ModalShell from '@/components/common/ModalShell.vue'
import { useI18n } from '@/composables/useI18n'
import { usePreflight } from '@/composables/usePreflight'
import { toast } from '@/composables/useToast'
import { runTask } from '@/composables/useTask'
import { useAppState } from '@/stores/appState'
import { computeVhost } from '@/utils/vhost'
import { setSiteVhostContent, hasBackend } from '@/api/site'

const props = defineProps<{ domain: string }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { preflight } = usePreflight()
const app = useAppState()

const site = computed(() => app.sites.find((s) => s.domain === props.domain))
const original = computed(() => (site.value ? computeVhost(app.env, site.value) : ''))
const content = ref(original.value)
const sitesRoot = computed(() => app.env.NGINX_SITES_ROOT || `${app.env.PHPO_HOME}/nginx/sites`)
const confPath = computed(() => `${sitesRoot.value}/${props.domain}.conf`)

const dirty = computed(() => content.value !== original.value)

function onTab(e: KeyboardEvent): void {
  if (e.key !== 'Tab') return
  e.preventDefault()
  const ta = e.target as HTMLTextAreaElement
  const s = ta.selectionStart
  const en = ta.selectionEnd
  content.value = ta.value.slice(0, s) + '    ' + ta.value.slice(en)
  requestAnimationFrame(() => { ta.selectionStart = ta.selectionEnd = s + 4 })
}

function onSave(): void {
  const check = preflight('site-vhost', { domain: props.domain, content: content.value, php: site.value?.php })
  if (!check.ok) { toast(check.errors.join('\n'), 'err', 4600); return }
  emit('close')
  if (!hasBackend()) {
    runTask(['site', 'vhost', 'save', props.domain], `${props.domain} · nginx 配置`, { type: 'site-vhost', domain: props.domain, content: content.value })
    toast(t('siteConfig.saved', { domain: props.domain }), 'ok', 2400)
    return
  }
  setSiteVhostContent(props.domain, content.value)
    .then(() => toast(t('siteConfig.saved', { domain: props.domain }), 'ok', 2400))
    .catch((e) => toast(String(e), 'err', 4600))
}
</script>

<template>
  <ModalShell size="xl" @close="emit('close')">
    <template #head>
      <h3>{{ domain }} · {{ t('siteConfig.title') }}</h3>
      <p>{{ t('siteConfig.subtitle') }}</p>
    </template>
    <template #body>
      <div class="config-editor-wrap">
        <div class="config-editor-head">
          <span class="path-text">{{ confPath }}</span>
          <span style="font-size: 11.5px; color: var(--text-mute); flex-shrink: 0">{{ t('config.editorHint') }}</span>
        </div>
        <textarea v-model="content" class="config-editor" spellcheck="false" @keydown="onTab" />
      </div>
      <div class="config-hint">
        <div class="alert alert-info">{{ t('siteConfig.hint') }}</div>
      </div>
    </template>
    <template #foot>
      <span class="foot-status" :class="{ dirty }">{{ dirty ? t('siteConfig.unsaved') : '' }}</span>
      <button class="btn" type="button" :disabled="!dirty" @click="content = original">{{ t('siteConfig.reset') }}</button>
      <button class="btn" type="button" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" type="button" :disabled="!dirty" @click="onSave">{{ t('siteConfig.save') }}</button>
    </template>
  </ModalShell>
</template>
