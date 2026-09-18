// doctor 前端 API（T603）：环境诊断 15 项 + 一键修复的薄封装。
// 硬红线 4：诊断为纯读，结果以后端为准；仅 Fix 产生写（状态校准/清临时目录），且幂等可重复。
// 无宿主（纯 Vite demo）时 runDoctor 返回 null，让视图保留本地 mock 不覆盖。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'
import type { DoctorReport, DoctorStatus } from '@/types'

// runDoctor 拉取权威诊断报告；无宿主返回 null
export async function runDoctor(): Promise<DoctorReport | null> {
  if (!hasBackend()) return null
  const r = await app.DoctorRun()
  const checks = (r.checks ?? []).map((c) => ({
    id: c.id,
    title: c.title,
    status: c.status as DoctorStatus,
    detail: c.detail,
    hint: c.hint,
    fix: c.fix,
  }))
  return { checks, ok: r.ok, warnings: r.warnings, errors: r.errors }
}

// fixDoctor 执行一项一键修复（calibrate / clear_temp）；无宿主直接返回
export async function fixDoctor(id: string): Promise<void> {
  if (!hasBackend()) return
  await app.DoctorFix(id)
}
