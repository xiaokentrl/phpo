<script setup lang="ts">
// 扩展选择器：安装弹窗与「管理扩展」弹窗共用，按分组铺出当前 PHP 版本可见的全量扩展目录。
// 目录只读不裁决——选中的扩展名照样要过后端 ValidateExt/preflight，这里不做任何限制。
// 「管理扩展」弹窗会传 locked（内建停不掉的那几颗）：它们显示为已开启但点不动，也不进提交集；
// 安装弹窗不传，全部开关照常可勾选。
import { computed, ref } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { catalogFor, commonExts, EXT_GROUPS } from '@/constants/ext'

const props = defineProps<{ version: string; modelValue: string[]; locked?: string[] }>()
const emit = defineEmits<{ 'update:modelValue': [string[]] }>()
const { t } = useI18n()

const query = ref('')
const selected = computed(() => new Set(props.modelValue))
const lockedSet = computed(() => new Set(props.locked ?? []))

// isOn 三档显示的前两档都在这里判：用户勾选的、以及「已经开着但停不掉」的内建项，都画成 on
function isOn(name: string): boolean {
  return selected.value.has(name) || lockedSet.value.has(name)
}

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
  return g.items.filter(isOn).length
}

function toggle(name: string): void {
  if (lockedSet.value.has(name)) return
  const cur = props.modelValue
  emit('update:modelValue', cur.includes(name) ? cur.filter((x) => x !== name) : [...cur, name])
}

// pickCommon 只增不删：常用项是「补上默认集」，不得把用户已经手工勾选的目录外扩展抹掉；
// 内建停不掉的那些本来就在容器里，勾进目标集等于让下次退回基座重建时去重编译它，跳过。
function pickCommon(): void {
  const add = commonExts(props.version).filter((n) => !isOn(n))
  if (add.length) emit('update:modelValue', [...props.modelValue, ...add])
}

function clearAll(): void {
  emit('update:modelValue', props.modelValue.filter((n) => lockedSet.value.has(n)))
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
          :class="[isOn(n) ? 'on' : 'off', lockedSet.has(n) ? 'builtin' : '']"
          :disabled="lockedSet.has(n)"
          :title="lockedSet.has(n) ? t('ext.builtinTip') : undefined"
          @click="toggle(n)"
        >{{ n }}<span v-if="lockedSet.has(n)" class="ext-toggle-tag">{{ t('ext.builtin') }}</span></button>
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
/* 内建档：仍画成 on（它确实开着），但用虚线边 + 压暗表明「这颗点不动」——不改 base.css，生产样式收在本组件内 */
.ext-toggle.builtin{border-style:dashed;opacity:.62;cursor:not-allowed}
.ext-toggle.builtin:hover{opacity:.62}
.ext-toggle-tag{margin-left:5px;font-size:10px;opacity:.8}
</style>

