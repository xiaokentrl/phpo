// cleanSelection 的用例锁（node --test 原生跑，零依赖）：勾选纯逻辑三轮迭代后的行为契约。
// 行为变更必须先改这里——这套契约就是「字面全选 + 整行在用不给勾」的需求拍板记录。
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { applyScopeKeys, keptTotal, rowSpared, selectable, type ScopeMode } from './cleanSelection.ts'
import type { CleanRow } from '@/types'

function row(partial: Partial<CleanRow>): CleanRow {
  return {
    key: partial.key ?? 'row.x', group: 'x', tier: 1, deletable: true, dangerous: false,
    isPhpo: false, risk: 'low', status: 'ok', count: 3, bytes: 0, hasBytes: false,
    scannedAt: '', ...partial,
  }
}

const rows: CleanRow[] = [
  row({ key: 'a.normal', deletable: true }),                                          // 普通外来行：可勾
  row({ key: 'b.danger', dangerous: true, deletable: true }),                          // 危险项：默认照勾（需求拍板）
  row({ key: 'c.info', deletable: false }),                                            // 三档说明行：无复选框
  row({ key: 'd.empty', deletable: false, status: 'unavailable', count: 0 }),          // 没数到的行
  row({ key: 'e.partial', isPhpo: true, phpoKept: 2, phpoCleanable: 1 }),              // 部分在用：可勾
  row({ key: 'f.allused', isPhpo: true, phpoKept: 3 }),                                // 整行在用：保留模式不给勾
]

test('字面全选：remove 档勾上一切可勾行（含危险项与整行在用行）', () => {
  const picked = applyScopeKeys(rows, 'remove' as ScopeMode)
  assert.deepEqual([...picked].sort(), ['a.normal', 'b.danger', 'e.partial', 'f.allused'])
})

test('字面全选：keep 档同套勾选，仅「整行在用」的行出局', () => {
  const picked = applyScopeKeys(rows, 'keep' as ScopeMode)
  assert.deepEqual([...picked].sort(), ['a.normal', 'b.danger', 'e.partial'])
  assert.equal(picked.has('f.allused'), false)
})

test('rowSpared 只在保留模式下成立（remove 档照常可勾）', () => {
  assert.equal(rowSpared('keep', rows[5]), true)
  assert.equal(rowSpared('remove', rows[5]), false)
})

test('整行在用的判定：phpoKept>0 且无可清数；有可清数即不算', () => {
  assert.equal(rowSpared('keep', row({ phpoKept: 1, phpoCleanable: 0 })), true)
  assert.equal(rowSpared('keep', row({ phpoKept: 1, phpoCleanable: undefined })), true)
  assert.equal(rowSpared('keep', row({ phpoKept: 0, phpoCleanable: undefined })), false)
  assert.equal(rowSpared('keep', row({ phpoKept: 1, phpoCleanable: 2 })), false)
})

test('selectable 的三条出局路径', () => {
  assert.equal(selectable('remove', row({ deletable: false })), false, '无删除路径')
  assert.equal(selectable('keep', rows[5]), false, '整行在用（keep）')
  assert.equal(selectable('remove', rows[5]), true, '整行在用（remove 档可勾）')
})

test('keptTotal 只求和 phpoKept，缺席按 0', () => {
  assert.equal(keptTotal(rows), 5)
})
