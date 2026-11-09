# Wails v3.0.0 Issue 反馈草稿

## 建议类型

Feature Request / Cross-platform consistency

## 建议标题

`[Feature Request] Implement SystemTray.Show/Hide on Linux or expose unsupported capability`

## 可直接提交的 Issue 正文

### Summary

In Wails v3.0.0, `SystemTray.Show()` and `SystemTray.Hide()` control tray icon visibility on Windows and macOS, but both methods are explicit no-ops on Linux. Since the public methods return no status or error, the same calls silently succeed while leaving the Linux tray item visible.

Could Linux visibility control be implemented using the supported StatusNotifier mechanism, or could the API expose that visibility toggling is unsupported so applications can handle it explicitly?

### Platform behavior in v3.0.0

| Platform | `SystemTray.Show()` | `SystemTray.Hide()` | Result |
|---|---|---|---|
| Windows | Calls `Shell_NotifyIcon(NIM_MODIFY)` and clears `NIS_HIDDEN` | Calls `Shell_NotifyIcon(NIM_MODIFY)` and sets `NIS_HIDDEN` | The application can show or hide its tray icon |
| macOS | Calls the native system-tray show implementation | Calls the native system-tray hide implementation | The application can show or hide its tray icon |
| Linux | Explicit no-op | Explicit no-op | Calling either method does not change item visibility |

This table describes the Wails v3.0.0 implementation, not a claim that every desktop environment displays tray items identically. On Linux, tray-item availability and presentation depend on the desktop's StatusNotifier host.

### Steps to reproduce

1. Run a Wails v3.0.0 application on Linux in a desktop session where a StatusNotifier host is available.
2. Create and show a `SystemTray` item.
3. Call `tray.Hide()`.
4. Observe that the tray item remains visible.
5. Call `tray.Show()` and observe that it also does not change visibility.

The same calls are implemented as visibility changes on Windows and macOS.

### Expected behavior

The same public API should have a predictable cross-platform contract:

- `Show()` makes the tray item visible where the platform supports tray items.
- `Hide()` removes the item from the visible tray without terminating the application.
- If Linux cannot support this operation in some session or desktop environment, the API should expose that limitation or return an observable error instead of silently behaving as if the call succeeded.
- The API should document any platform-specific limitations and the Linux desktop requirements.

The implementation does not need to make tray presentation identical across desktop environments; it should make the operation and its capability explicit and reliable.

### Why this matters

Applications commonly use `Hide()` to implement a user preference for showing a tray icon, and use that preference to decide whether closing the window can leave the process running. On Linux, a call to `Hide()` currently leaves the icon present, while code that assumes the call worked can put the UI and actual tray state out of sync. Because the method returns no result, the application cannot distinguish a successful visibility change from the Linux no-op through the public API.

### Scope clarification

This request is about the **system tray icon**, not the application window and not the desktop Dock/taskbar launcher. Hiding or minimizing a window is a separate operation and does not resolve the tray icon visibility mismatch.

### Source references

- Linux no-op implementations: [`pkg/application/systemtray_linux.go`](https://github.com/wailsapp/wails/blob/v3.0.0/pkg/application/systemtray_linux.go#L861-L867)
- Windows visibility implementation: [`pkg/application/systemtray_windows.go`](https://github.com/wailsapp/wails/blob/v3.0.0/pkg/application/systemtray_windows.go#L624-L645)
- macOS visibility implementation: [`pkg/application/systemtray_darwin.go`](https://github.com/wailsapp/wails/blob/v3.0.0/pkg/application/systemtray_darwin.go#L58-L70)
- Public `SystemTray.Show/Hide` methods: [`pkg/application/systemtray.go`](https://github.com/wailsapp/wails/blob/v3.0.0/pkg/application/systemtray.go#L355-L369)

### Environment

- Wails: `v3.0.0`
- Platform for reproduction: Linux desktop session with a StatusNotifier host
- Distribution / desktop environment / session type: add the exact values from the reproduction system

The platform comparison above is based on the v3.0.0 source implementations. It does not claim that this project has run GUI reproductions on Windows and macOS.

## 反馈依据与项目侧临时处理

这是一个有源码依据的跨平台功能差异，不是将 Dock 与系统托盘混为一谈：Linux 实现对 `Show()` / `Hide()` 明确标为 no-op；Windows 与 macOS 分别调用平台原生显隐逻辑。Issue 因此应作为功能请求提交，并让 Wails 维护者确认 Linux 上合适的实现方式或公开能力声明。

phpo 目前将 Linux 托盘图标开关设为不可操作并保持开启；仅在检测到 StatusNotifier 宿主时允许关闭窗口后驻留。这样避免把无效的 `Hide()` 当成成功，也避免没有托盘入口时留下无法从托盘唤回的进程。该临时策略不代表所有 Linux 桌面环境都支持显示托盘图标。

目前核实的是 Wails `v3.0.0` 源码行为；提 Issue 前应在目标 Linux 桌面会话补全发行版、桌面环境、Wayland/X11 信息及实际复现结果。不要将未实际运行的 Windows/macOS GUI 验收描述为测试结论。
