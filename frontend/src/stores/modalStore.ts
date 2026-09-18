// 模态状态仓：全局单例宿主，open(name, props) / close()
// 对应原型 openModalShell/closeModal（1525–1532）；M1 仅结构迁移，真实动作后续里程碑接入。
import { defineStore } from 'pinia'
import { shallowRef, markRaw, type Component } from 'vue'

export const useModalStore = defineStore('modal', () => {
  const current = shallowRef<Component | null>(null)
  const props = shallowRef<Record<string, unknown>>({})

  function open(comp: Component, p: Record<string, unknown> = {}): void {
    props.value = p
    current.value = markRaw(comp)
  }
  function close(): void {
    current.value = null
    props.value = {}
  }

  return { current, props, open, close }
})
