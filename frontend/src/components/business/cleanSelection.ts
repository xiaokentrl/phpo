// 清理面板的勾选纯逻辑（自 DockerCleanPanel.vue 抽出，2026-11）：范围卡档位下的行状态判定。
// 只吃普通对象、只回普通值——不 import Vue，保证能用 node --test 直接跑（零测试基建依赖）。
// 这里是面板、预览闸门（internal/cache/image_cache.go filterKeepInUse）与执行三层共判定的前端一环；
// 「保留模式」的服务端硬排除在 internal/service/docker_clean_preview.go，别在这里造第二套口径。

import type { CleanRow } from '@/types'

export type ScopeMode = 'keep' | 'remove'

// rowSpared：保留模式下这一行的对象全部被 phpo 已装服务占用——整行保留，可清的没有。
// 判据来自扫描行上的标记：phpoKept>0 而 phpoCleanable 缺席，即「对象有、可清的没有」。
export function rowSpared(scopeMode: ScopeMode, r: CleanRow): boolean {
  return scopeMode === 'keep' && !!r.phpoKept && !r.phpoCleanable
}

// selectable：一张复选框能不能勾。三档说明行（无删除路径）天然出局（deletable=false）；
// 「整行在用」的行在保留模式下没东西可清，也不给勾——勾了预览也只会空手而回。
export function selectable(scopeMode: ScopeMode, r: CleanRow): boolean {
  return r.deletable && !rowSpared(scopeMode, r)
}

// applyScopeKeys：照当前范围卡算默认勾选集。字面全选——凡可勾的行一律勾上，
// 危险项也不例外（安全网在后端预览的知情确认，不在这里）。返回 key 集合，调用方写入 picked。
export function applyScopeKeys(rows: CleanRow[], scopeMode: ScopeMode): Set<string> {
  const next = new Set<string>()
  for (const r of rows) {
    if (selectable(scopeMode, r)) next.add(r.key)
  }
  return next
}

// keptTotal：保留模式将排除的在用对象总量（行上标记求和，仅展示用）
export function keptTotal(rows: CleanRow[]): number {
  return rows.reduce((n, r) => n + (r.phpoKept ?? 0), 0)
}
