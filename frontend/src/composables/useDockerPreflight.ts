// useDockerPreflight：首启 + 定时轮询探测 Docker 可用性（硬红线 7 判定源）。
// 探测走只读绑定 DockerStatus（不发事件、不改 Snapshot）；结果经 app.setDocker 落地，
// 供 DockerGate 决定两段式门禁（不可关闭引导 / 稍后再说 → 常驻横幅）。无宿主不拦截。
import { useAppState } from '@/stores/appState'
import { getDockerStatus } from '@/api/docker'
import { hasBackend } from '@/api/site'
import type { DockerStatus } from '@/types'

const POLL_MS = 12_000
let timer: ReturnType<typeof setInterval> | null = null

// probe 拉取一次并落地；无宿主时置 ok（不拦截，保持 demo 可用）
export async function refreshDocker(): Promise<void> {
  const app = useAppState()
  if (!hasBackend()) {
    app.setDocker({ status: 'ok', canStart: true, warning: false } as DockerStatus)
    return
  }
  try {
    const s = await getDockerStatus()
    if (s) app.setDocker(s)
    else app.setDocker({ status: 'unknown', canStart: false, warning: false })
  } catch (e) {
    app.setDocker({ status: 'not_running', canStart: false, warning: false, message: String((e as Error)?.message ?? e) })
  }
}

export function startDockerPreflight(): void {
  void refreshDocker()
  if (timer === null) timer = setInterval(() => void refreshDocker(), POLL_MS)
}

export function stopDockerPreflight(): void {
  if (timer !== null) {
    clearInterval(timer)
    timer = null
  }
}
