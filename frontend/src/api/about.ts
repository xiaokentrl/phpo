// 「关于我们」前端 API：读侧薄封装，数据全部来自后端 AppInfo（硬红线 4）。
// 源码版本 / 构建时间拿不到时后端给空串，视图层显示「未提供」；无宿主（纯 Vite demo）整体返回 null。
import * as app from '../../bindings/phpo/app.js'
import { hasBackend } from '@/api/site'

export interface AppAbout {
  name: string
  version: string
  commit: string
  buildTime: string
  dataDir: string
}

// appInfo 拉取应用信息；无宿主返回 null
export async function appInfo(): Promise<AppAbout | null> {
  if (!hasBackend()) return null
  const m = await app.AppInfo()
  return {
    name: m['name'] ?? '',
    version: m['version'] ?? '',
    commit: m['commit'] ?? '',
    buildTime: m['buildTime'] ?? '',
    dataDir: m['dataDir'] ?? '',
  }
}
