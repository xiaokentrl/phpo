// Docker 可用性只读探测（首启/轮询门禁，硬红线 7 判定源）。
// 无宿主（纯 Vite demo）返回 null：不拦截，保持占位数据可直接打开各模态验证。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'
import type { DockerStatus } from '@/types'

// getDockerStatus 拉取后端 Docker 探测结论；无宿主返回 null
export async function getDockerStatus(): Promise<DockerStatus | null> {
  if (!hasBackend()) return null
  const s = await app.DockerStatus()
  return (s ?? null) as unknown as DockerStatus | null
}
