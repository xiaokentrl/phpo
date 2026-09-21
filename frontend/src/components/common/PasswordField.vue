<script setup lang="ts">
// PasswordField：服务版本密码行内编辑（T504）。明文 · 空密码合法 · 任意长度/字符（§1.5 零校验零加密）。
// 👁 显示/隐藏 · 📋 复制 · ↺ 重置回 123456；真实链路写后端 env 并靠 state:changed 回显，无宿主时回落本地 mock。
import { computed, nextTick, ref } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { toast } from '@/composables/useToast'
import { runTask } from '@/composables/useTask'
import { hasBackend } from '@/api/site'
import { syncState } from '@/composables/useStateSync'
import { setPassword, passwordFromEnv } from '@/api/env'
import { copyText, DEFAULT_PASSWORD } from '@/utils/str'

const props = defineProps<{ kind: string; version: string }>()
const { t } = useI18n()

const realValue = computed(() => passwordFromEnv(props.kind, props.version))
const revealed = ref(false)
const editing = ref(false)
const draft = ref('')
const inputEl = ref<HTMLInputElement | null>(null)

async function startEdit(): Promise<void> {
  draft.value = realValue.value
  editing.value = true
  revealed.value = true
  await nextTick()
  inputEl.value?.focus()
  inputEl.value?.select()
}

function cancel(): void {
  editing.value = false
  revealed.value = false
}

// persist：落库（含空串）。真实走后端绑定；mock 直写 env + 回放 update-config 日志。
async function persist(next: string, prev: string): Promise<void> {
  editing.value = false
  revealed.value = false
  try {
    await setPassword(props.kind, props.version, next)
    if (hasBackend()) await syncState() // 写后拉权威快照：回显与 config.yaml 即刻一致（硬红线 4）
  } catch (e) {
    toast(String(e), 'err', 4600)
    return
  }
  if (!hasBackend()) {
    const shown = next.length > 12 ? `${next.slice(0, 8)}…` : next || '∅'
    runTask([props.kind, 'password', 'set', props.version, next], `${props.kind} ${props.version} · password → ${shown}`, {
      type: 'update-config', kind: props.kind, version: props.version, field: 'password', oldValue: prev, newValue: next,
    })
  }
}

function commit(): void {
  const v = draft.value
  if (v === realValue.value) { cancel(); return }
  void persist(v, realValue.value)
}

async function reset(): Promise<void> {
  if (realValue.value === DEFAULT_PASSWORD) { toast(t('svc.passwordIsDefault'), 'info', 1600); return }
  await persist(DEFAULT_PASSWORD, realValue.value)
  toast(t('svc.passwordReset'), 'ok', 1800)
}

async function copy(): Promise<void> {
  if (await copyText(realValue.value)) toast(t('common.copied'), 'ok', 1600)
  else toast(t('common.copyFailed'), 'err', 2200)
}
</script>

<template>
  <span class="inline-edit pw-field" :class="{ editing }" :data-inline="'password'" :data-kind="kind" :data-version="version" :title="t('svc.editPassword')" tabindex="0" @click="!editing && startEdit()" @keydown.enter="!editing && startEdit()">
    <template v-if="editing">
      <input ref="inputEl" v-model="draft" type="text" class="password-input" spellcheck="false" autocomplete="off" @keydown.enter.prevent="commit" @keydown.esc.prevent="cancel" @blur="cancel">
    </template>
    <template v-else>
      <span v-if="revealed" class="pw-reveal">{{ realValue || '∅' }}</span>
      <span v-else class="pw">••••••••</span>
      <span class="pw-actions" @click.stop>
        <button class="pw-btn" type="button" :title="revealed ? t('svc.hidePassword') : t('svc.showPassword')" @click="revealed = !revealed">
          <svg v-if="!revealed" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" /><circle cx="12" cy="12" r="3" /></svg>
          <svg v-else width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19M14.12 14.12a3 3 0 1 1-4.24-4.24" /><path d="M1 1l22 22" /></svg>
        </button>
        <button class="pw-btn" type="button" :title="t('svc.copyPassword')" @click="copy">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2" /><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" /></svg>
        </button>
        <button class="pw-btn" type="button" :title="t('svc.resetPassword')" @click="reset">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-2.64-6.36M21 3v6h-6" /></svg>
        </button>
      </span>
    </template>
  </span>
</template>

<style scoped>
.pw-field{gap:8px}
.pw-reveal{word-break:break-all;user-select:text}
.pw-actions{display:inline-flex;align-items:center;gap:2px}
.pw-btn{display:inline-flex;align-items:center;justify-content:center;padding:2px;border:none;background:transparent;color:inherit;cursor:pointer;border-radius:4px;opacity:0.7;transition:opacity var(--motion-fast),background var(--motion-fast)}
.pw-btn:hover{opacity:1;background:var(--accent-bg)}
</style>
