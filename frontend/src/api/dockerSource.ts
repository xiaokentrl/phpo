// Docker 镜像源读写 + 测速薄封装（设置页）。
// 硬红线 4：清单的权威值是 config.yaml，界面回显只认后端读回来的那一份，不在本地记账；
// 测速结果是**这一次请求的响应**，落库的只有地址清单（决策只在下一次拉取时现测现取，不存过期排名）。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'
import type { MirrorSource } from '@/types'

// getSources 读回已保存的镜像源清单（每行一个地址）；无宿主返回 null，卡片据此显示「未接入后端」而不是空清单。
export async function getSources(): Promise<string[] | null> {
  if (!hasBackend()) return null
  return (await app.DockerSourcesGet()) ?? []
}

// setSources 保存清单：空行由后端忽略，写了非法地址后端会点名报错（错误原文由调用方 toast 给用户）。
export async function setSources(hosts: string[]): Promise<void> {
  if (!hasBackend()) return
  await app.DockerSourcesSet(hosts)
}

// probeSources 并发测速界面上这几行地址（只读，不落库）。返回顺序与入参逐行对应，
// 所以界面可以直接把它和文本框的行对上；无宿主返回 null，调用方显示「未接入后端」。
export async function probeSources(hosts: string[]): Promise<MirrorSource[] | null> {
  if (!hasBackend()) return null
  return (await app.DockerSourcesProbe(hosts)) ?? []
}
