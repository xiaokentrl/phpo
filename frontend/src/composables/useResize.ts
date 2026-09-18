// 拖拽 resize 原语：补偿 documentElement.style.zoom，确保各缩放下尺寸跟随指针无错位
// 供模态外壳（T107）复用；指针位移按当前缩放比换算为布局像素增量
import { useLayoutStore } from '@/stores/layoutStore'

export interface ResizeOptions {
  minWidth?: number
  minHeight?: number
  getSize: () => { w: number; h: number }
  setSize: (w: number, h: number) => void
}

export function useResize(opts: ResizeOptions) {
  const layout = useLayoutStore()
  const minW = opts.minWidth ?? 200
  const minH = opts.minHeight ?? 120

  function startResize(e: PointerEvent): void {
    e.preventDefault()
    const zoom = layout.scale || 1
    const sx = e.clientX
    const sy = e.clientY
    const start = opts.getSize()

    const onMove = (ev: PointerEvent) => {
      const dw = (ev.clientX - sx) / zoom
      const dh = (ev.clientY - sy) / zoom
      opts.setSize(Math.max(minW, Math.round(start.w + dw)), Math.max(minH, Math.round(start.h + dh)))
    }
    const onUp = () => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
    }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
  }

  return { startResize }
}
