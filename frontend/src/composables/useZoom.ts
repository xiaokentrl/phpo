// 缩放 composable：封装 layoutStore 的档位缩放，供命令面板 / 设置页 / HUD 复用
import { computed } from 'vue'
import { UI_SCALE } from '@/constants/layout'
import { useLayoutStore } from '@/stores/layoutStore'

export function useZoom() {
  const layout = useLayoutStore()
  const percent = computed(() => Math.round(layout.scale * 100))

  return {
    layout,
    percent,
    snap: UI_SCALE.snap,
    min: UI_SCALE.min,
    max: UI_SCALE.max,
    zoomIn: () => layout.zoomIn(),
    zoomOut: () => layout.zoomOut(),
    zoomReset: () => layout.zoomReset(),
    setScale: (v: number) => layout.setScale(v),
  }
}
