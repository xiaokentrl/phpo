// useDockerPreflight：首启 + 定时轮询探测 Docker 可用性（硬红线 7 判定源）。
// 探测走只读绑定 DockerStatus（不发事件、不改 Snapshot）；结果经 app.setDocker 落地，
// 供 DockerGate 决定两段式门禁（不可关闭引导 / 稍后再说 → 常驻横幅）。无宿主不拦截。
import { useAppState } from '@/stores/appState'
import { getDockerStatus } from '@/api/docker'
import { hasBackend } from '@/api/site'
import type { DockerStatus } from '@/types'

const POLL_MS = 12_000
let timer: ReturnType<typeof setInterval> | null = null

// brief：绑定调用的底层失败可能是整段 HTML 或多行堆栈（异常端点、代理回吐）。
// 横幅只留单行、限长的诊断片段，人话主句交给 locales 的 docker.* 兜底。
// 按码点截断（与后端 engine.errBrief 的 rune 口径一致）：slice 按 UTF-16 码元切会把代理对劈成半个。
function brief(e: unknown): string {
  const s = String((e as Error)?.message ?? e).replace(/\s+/g, ' ').trim()
  const cps = Array.from(s)
  return cps.length > 120 ? `${cps.slice(0, 120).join('')}…` : s
}

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
    // message 留空 → 门禁/横幅回落 docker.not_running 人话主句；原始错误收进 hint 且限长
    app.setDocker({ status: 'not_running', canStart: false, warning: false, hint: brief(e) })
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
