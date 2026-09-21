<script setup lang="ts">
// 站点 nginx 配置弹窗：忠实迁移原型 openSiteConfigModal（2849–2875）
// 编辑 vhost 正文；脏态启用保存/重置；保存走 preflight('site-vhost')
// 保存 = 后端 writeVHost（写入 → nginx -t → 失败自动回滚 → 才 reload），故校验在落盘与重载之前
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import ModalShell from '@/components/common/ModalShell.vue'
import { useI18n } from '@/composables/useI18n'
import { usePreflight } from '@/composables/usePreflight'
import { toast } from '@/composables/useToast'
import { runTask } from '@/composables/useTask'
import { syncState } from '@/composables/useStateSync'
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
const saving = ref(false)
// 校验失败详情：非空即弹框。草稿必须留在编辑器里，故不能沿用「先关窗再写」的路径
const errText = ref('')

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
  if (!check.ok) { errText.value = check.errors.join('\n'); return }
  if (!hasBackend()) {
    emit('close')
    runTask(['site', 'vhost', 'save', props.domain], `${props.domain} · nginx 配置`, { type: 'site-vhost', domain: props.domain, content: content.value })
    toast(t('siteConfig.saved', { domain: props.domain }), 'ok', 2400)
    return
  }
  saving.value = true
  setSiteVhostContent(props.domain, content.value)
    .then(() => {
      emit('close')
      toast(t('siteConfig.saved', { domain: props.domain }), 'ok', 2400)
    })
    .catch((e: unknown) => {
      errText.value = String(e)
    })
    .finally(() => {
      saving.value = false
      // 写后拉权威快照收口：成功时健康/落盘状态与失败时后端回滚结果都以快照为准（硬红线 4）
      syncState()
    })
}

// 弹框开着时接管 ESC：只关弹框，不冒泡到外壳（否则整窗关闭、草稿丢失）
function onEsc(e: KeyboardEvent): void {
  if (e.key !== 'Escape') return
  e.stopPropagation()
  errText.value = ''
}
watch(errText, (v) => {
  if (v) document.addEventListener('keydown', onEsc, true)
  else document.removeEventListener('keydown', onEsc, true)
})
onBeforeUnmount(() => document.removeEventListener('keydown', onEsc, true))
</script>

<template>
  <ModalShell size="xl" tall body-config @close="emit('close')">
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
      <button class="btn" type="button" :disabled="!dirty || saving" @click="content = original">{{ t('siteConfig.reset') }}</button>
      <button class="btn" type="button" :disabled="saving" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" type="button" :disabled="!dirty || saving" @click="onSave">{{ saving ? t('siteConfig.saving') : t('siteConfig.save') }}</button>
    </template>
  </ModalShell>

  <Teleport to="body">
    <div v-if="errText" class="modal-root open modal-err" @click.self="errText = ''">
      <div class="modal" role="alertdialog" aria-modal="true">
        <div class="modal-head">
          <h3>{{ t('siteConfig.error.title') }}</h3>
          <p>{{ t('siteConfig.error.desc') }}</p>
        </div>
        <div class="modal-body">
          <pre class="modal-err-text">{{ errText }}</pre>
        </div>
        <div class="modal-foot">
          <button class="btn btn-primary" type="button" data-action="vhost-error-dismiss" @click="errText = ''">{{ t('siteConfig.error.back') }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
