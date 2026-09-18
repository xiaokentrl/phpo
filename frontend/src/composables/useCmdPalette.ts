// useCmdPalette：命令面板开合状态（模块级 reactive，对齐原型 classList.toggle('open')）
// 全局快捷键在 CmdPalette.vue 内注册；此处仅暴露 open（只读 ref）与 show/hide/toggle。
import { computed, reactive, type ComputedRef } from 'vue'

const state = reactive<{ open: boolean }>({ open: false })

export function useCmdPalette() {
  const open: ComputedRef<boolean> = computed(() => state.open)
  function show(): void {
    state.open = true
  }
  function hide(): void {
    state.open = false
  }
  function toggle(): void {
    state.open = !state.open
  }
  return { open, show, hide, toggle }
}
