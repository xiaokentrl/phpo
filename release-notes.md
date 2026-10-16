## English

### phpo 0.1.119 What's Changed from 0.1.118 to 0.1.119

**Version type: Stable**

**Changed**

- Stop the three platform packaging jobs from cloning the whole repository history just to read the version number
- Skip regenerating the frontend TypeScript bindings when no Go source has changed
- Scan the main binary's library dependencies twice instead of four times when building the AppImage
- Move the version type line into each language section of the release body, so English readers no longer see a Chinese-only line

## 中文

### 0.1.119 相比 0.1.118 更新内容

**版本类型：稳定版**

**修改**

- 云端三个平台的出包作业不再为读一个版本号克隆整仓历史
- 没改 Go 源码时不再重新生成前端 TypeScript 绑定
- AppImage 打包时对主程序依赖库的扫描从四遍减到两遍
- 发布正文里的版本类型那一行拆进中英两段各自的位置，英文读者不再只看到一句中文
