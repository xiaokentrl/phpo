<script setup lang="ts">
// 扩展选择器：安装弹窗与「管理扩展」弹窗共用，按分组铺出当前 PHP 版本可见的全量扩展目录。
// 目录只读不裁决——选中的扩展名照样要过后端 ValidateExt/preflight，这里不做任何限制。
import { computed, ref } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { catalogFor, commonExts, EXT_GROUPS } from '@/constants/ext'

const props = defineProps<{ version: string; modelValue: string[] }>()
const emit = defineEmits<{ 'update:modelValue': [string[]] }>()
const { t } = useI18n()

const query = ref('')
const selected = computed(() => new Set(props.modelValue))

type Group = { key: string; label: string; items: string[] }

// 目录外的自定义名字（用户手填）也要成组显示，否则已选值在界面上凭空消失
const groups = computed<Group[]>(() => {
  const cat = catalogFor(props.version)
  const known = new Set(cat.map((e) => e.name))
  const q = query.value.trim().toLowerCase()
  const out: Group[] = []
  for (const g of EXT_GROUPS) {
    const items = cat.filter((e) => e.group === g && (!q || e.name.includes(q))).map((e) => e.name)
    if (items.length) out.push({ key: g, label: t(`ext.group.${g}`), items })
  }
  const custom = props.modelValue.filter((n) => !known.has(n) && (!q || n.toLowerCase().includes(q)))
  if (custom.length) out.push({ key: 'custom', label: t('ext.group.custom'), items: custom })
  return out
})

function onCount(g: Group): number {
  return g.items.filter((n) => selected.value.has(n)).length
}

function toggle(name: string): void {
  const cur = props.modelValue
  emit('update:modelValue', cur.includes(name) ? cur.filter((x) => x !== name) : [...cur, name])
}

// pickCommon 只增不删：常用项是「补上默认集」，不得把用户已经手工勾选的目录外扩展抹掉
function pickCommon(): void {
  const add = commonExts(props.version).filter((n) => !selected.value.has(n))
  if (add.length) emit('update:modelValue', [...props.modelValue, ...add])
}

function clearAll(): void {
  emit('update:modelValue', [])
}
</script>

<template>
  <div class="ext-picker">
    <div class="ext-picker-tools">
      <input v-model="query" type="text" autocomplete="off" spellcheck="false" :placeholder="t('ext.filter.placeholder')">
      <button class="btn btn-sm" type="button" @click="pickCommon">{{ t('ext.pickCommon') }}</button>
      <button class="btn btn-sm" type="button" :disabled="!modelValue.length" @click="clearAll">{{ t('ext.clearAll') }}</button>
    </div>
    <div v-for="g in groups" :key="g.key" class="ext-group">
      <div class="ext-group-head">
        <span>{{ g.label }}</span>
        <span class="mono">{{ t('ext.selected', { count: onCount(g), total: g.items.length }) }}</span>
      </div>
      <div class="ext-toggle-grid">
        <button
          v-for="n in g.items"
          :key="n"
          type="button"
          class="ext-toggle"
          :class="selected.has(n) ? 'on' : 'off'"
          @click="toggle(n)"
        >{{ n }}</button>
      </div>
    </div>
    <div v-if="!groups.length" class="hint">{{ t('ext.empty.catalog') }}</div>
  </div>
</template>

<style scoped>
.ext-picker-tools{display:flex;gap:6px;align-items:center;margin-bottom:8px}
.ext-picker-tools input{flex:1;min-width:0}
.ext-group-head{display:flex;justify-content:space-between;align-items:center;gap:8px;margin:10px 0 5px;font-size:11.5px;color:var(--text-mute)}
.ext-group:first-child .ext-group-head{margin-top:0}
</style>
