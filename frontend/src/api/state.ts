// 外壳通道：① 启动权威快照拉取（T607 / 硬红线 4）——真实宿主下前端挂载后主动向后端取一次全量快照并落地，
// 使 dirReady / env / installed 等以 DB 为唯一权威（新用户首启 dirReady 缺 PHPO_HOME → NEEDS_HOME 全拦截 → 弹向导）；
// 无宿主（纯 Vite demo）返回 null，保留占位数据与 mock 事件驱动，不覆盖。
// ② UI 偏好向原生外壳的投影（托盘显隐 / 关闭窗口语义），权威仍在 localStorage（§3.1 原则 5）。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'
import type { StateSnapshot } from '@/types'

// getState 拉取后端当前权威快照；无宿主返回 null
export async function getState(): Promise<StateSnapshot | null> {
  if (!hasBackend()) return null
  const s = await app.GetState()
  return (s ?? null) as unknown as StateSnapshot | null
}

// setTrayPrefs 把两项托盘偏好交给原生外壳（撤下/挂上托盘图标 + 关闭按钮驻留或退出）。
// 不做反向读取：偏好以 localStorage 为准；无宿主时直接跳过，demo 下仅驱动窗口内 AppTrayMenu.vue。
export async function setTrayPrefs(showTray: boolean, minimizeOnClose: boolean): Promise<void> {
  if (!hasBackend()) return
  await app.SetTrayPrefs(showTray, minimizeOnClose)
}

// quitApp 请原生外壳退出进程：托盘菜单「退出 phpo」的唯一出口（忙锁判定在前端，见 AppTrayMenu.vue）。
// 无宿主（纯 Vite demo）时跳过——浏览器里没有可退出的进程。
export async function quitApp(): Promise<void> {
  if (!hasBackend()) return
  await app.Quit()
}
