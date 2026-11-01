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

// syncAll 让后端跑一次**全量**校准（§5.19）：容器 + 基座镜像 + PHP 扩展固化镜像三类缺失一次核清，
// 结果经 docker:state-drift（逐条点名）+ state:changed（快照带 gaps）回流界面。
// 门面方法名仍是生成的 Calibrate——bindings 已按该名生成，语义为「手动同步 = 全量口径」。
// 无宿主（demo）返回 false，调用方只拉本地占位快照。
export async function syncAll(): Promise<boolean> {
  if (!hasBackend()) return false
  await app.Calibrate()
  return true
}

// refreshServices 让后端跑一次**轻量**刷新（§5.19.2 轻量档）：现数一遍「这台机器的 Docker 此刻有哪些
// 服务容器」（含已停止的、含不是 phpo 装的），数出来的与界面已有那份一致时后端一个字都不发。
// 它与 syncAll 的差别只在一件事：这里不逐个版本去核镜像还在不在本机——那是手动「同步状态」的代价。
// 无宿主（demo）返回 false：浏览器里问不到 Docker，界面保持现有那份（「没问到」不等于「没有」）。
export async function refreshServices(): Promise<boolean> {
  if (!hasBackend()) return false
  await app.RefreshServices()
  return true
}

// setTrayPrefs 把托盘偏好投影到原生外壳；能力状态由 TrayStatus 查询，localStorage 仍是偏好权威。
export async function setTrayPrefs(showTray: boolean, minimizeOnClose: boolean): Promise<void> {
  if (!hasBackend()) return
  await app.SetTrayPrefs(showTray, minimizeOnClose)
}

export type TrayStatus = Awaited<ReturnType<typeof app.TrayStatus>>

export async function getTrayStatus(): Promise<TrayStatus | null> {
  if (!hasBackend()) return null
  return await app.TrayStatus()
}

// quitApp 请原生外壳退出进程：托盘菜单「退出 phpo」的唯一出口（忙锁判定在前端，见 AppTrayMenu.vue）。
// 无宿主（纯 Vite demo）时跳过——浏览器里没有可退出的进程。
export async function quitApp(): Promise<void> {
  if (!hasBackend()) return
  await app.Quit()
}
