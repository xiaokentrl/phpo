# phpo

[中文](./README.md) · [English](./README_EN.md)

> 面向 PHP 开发者的本地容器化开发环境管理器：多版本 PHP / MySQL / PostgreSQL / Redis / Nginx 并存，全部跑在 Docker 或 Podman 里。
> 三平台桌面应用，只有 GUI，没有命令行。
>
> 下载地址：<https://github.com/xiaokentrl/phpo/releases/latest>

---

## 一句话能干什么

装一次，本机就有若干套互相不打扰的 PHP 环境：想给 A 站用 8.3、给 B 站用 7.4，就在站点列表里下拉切换，不用装第二台机器、不用改宿主 PHP。

## 核心约定

先说这软件按什么规矩跑，再说破例会怎样：

- **多版本并存**：每个服务一个容器，命名 `phpo-{service}-{version}`。站点切 PHP 精确指向 `php-{version}-fpm:9000`，不做模糊匹配。
- **配置可以手改**：vhost 正文、伪静态都摊给你编辑。模板生成的配置落盘前必过 `nginx -t`，不过就回滚；Nginx 当前没在跑时先照常落盘，等 Nginx 起来后自动补齐校验。你手动改过的那一份不会被自动覆盖。
- **扩展 73 项**：装 PHP 时就把扩展选好，常用 11 项默认勾选。改扩展集 = 重编译 + 固化镜像 + 重建容器，编译过程在日志抽屉逐行直播，失败点名到具体是哪一个扩展。
- **状态以谁为准**：以后端快照为唯一权威，界面不做「先点亮再说」。操作跑完一定回流真实状态，不假装成功。
- **密码明文**：默认 `123456`，可以留空，明文存在 `config.yaml` 里。这是设计取舍（见下方「安全须知」），不是漏洞。
- **改密码 / 改端口要重建**：数据库服务改完这两项要点「重建生效」才落到容器上；数据卷保留，不会因为重建丢数据。
- **备份链路**：逻辑导出 → 暂停服务 → 文件系统快照 → 打包 → 重启。归档里读不动的单个文件跳过并按目录聚合告警，不会因一颗文件判死整包。
- **联网范围**：只在拉镜像 / 拉扩展包 / 检查更新时联网，无遥测。离线缓存命中即零网络；镜像源可以在设置页自己加，只影响 phpo 自己那一次 `docker pull`，不改 Docker daemon 配置。
- **目录**：工作目录默认 `~/phpo`，站点根默认 `~/www`。缓存根 / 备份根 / 每个数据服务的版本数据目录都可以自定义；一旦自定义就完全取代默认，不做二级回退。
- **默认端口按引擎**：Docker 引擎新建站点默认 80，Podman（常见 rootless）默认 8080。你已经保存过的端口不会因为换引擎被改写。
- **容器引擎必须可用**：Docker 或 Podman 二选一，phpo 靠真实拨号判断，不看 CLI 在不在、不看 socket 文件在不在。Linux 用 Docker 需把当前用户加进 `docker` 组并重新登录。
- **改代码先读 [`AGENTS.md`](./AGENTS.md)**：它是项目最高技术总纲，冻结文档，与任何其他文档冲突时以它为准。

## 界面一览

### ① 总览：几套环境在跑，一眼看完
![phpo 总览](./docs/screenshots/01-overview.png)

### ② PHP 服务页：三个版本并排，各管各的
![PHP 服务页](./docs/screenshots/02-service-php.png)

### ③ 站点列表：端口、PHP 版本、健康态，行内就能改
![站点列表](./docs/screenshots/03-sites.png)

### ④ vhost 正文：nginx 配置直接给你改，落盘前先 `nginx -t`
![vhost 编辑器](./docs/screenshots/04-vhost-editor.png)

### ⑤ 伪静态：常见框架预设，Laravel / ThinkPHP / Yii2 / WordPress 那些都在
![伪静态预设](./docs/screenshots/05-rewrite-presets.png)

### ⑥ 装 PHP 时就把扩展选好：73 项全量目录，常用 11 项已勾
![安装 PHP 时选扩展](./docs/screenshots/06-install-extensions.png)

### ⑦ 管理扩展：提交的永远是完整目标集，编译过程逐行直播
![管理扩展](./docs/screenshots/07-php-extensions.png)

### ⑧ 日志抽屉：每一步在干什么、卡在哪，逐行写给你看
![任务日志抽屉](./docs/screenshots/08-task-drawer.png)

### ⑨ 数据库服务卡：端口、密码、数据目录都摊在脸上
![MySQL 服务卡](./docs/screenshots/09-service-mysql.png)

### ⑩ 备份：一键打包，读不动的文件不判死整包
![备份恢复](./docs/screenshots/10-backup.png)

### ⑪ 警告代替阻止：停一个 PHP 之前，说清谁在用它
![preflight 警告弹框](./docs/screenshots/11-preflight-warning.png)

### ⑫ 设置：主题、界面语言、缩放、托盘、开机自启动、镜像源、工作根
![设置](./docs/screenshots/12-settings.png)

## 其他功能

- **界面语言 8 种**：简体中文、繁體中文（台湾用语）、English、한국어、Français、Deutsch、Español、Türkçe。设置页点一下就换，不用重启；偏好只存本机。
- **开机自启动**：设置页一颗开关，切换即写入系统自启动注册（Linux `.desktop` / Windows 注册表 / macOS 登录项），关掉即移出。
- **Docker 资源清理面板**：总览页底部 60 行清单，11 组，分档列出容器、镜像、卷、构建缓存与宿主上的落盘目录。数不到的行显示「—」并说明为什么数不到，绝不给个 0 冒充干净。删除必过「预览 → 一次性凭据 → 执行」，四个危险项就算全选也带不走，有数据的项目先挪进回收站留 7 天。
- **备份与恢复**：整包归档带逻辑导出，恢复时明示「dump 未自动重放」，要你自己决定导回去。
- **托盘与单实例**：关闭窗口可以收进托盘；同一台机器只跑一个 phpo，再点快捷方式是把已有窗口前置，不会开出第二个。
- **主题与缩放**：9 套主题，界面缩放可调。

## 系统要求

| 项 | 要求 |
|---|---|
| 容器引擎 | Docker 或 Podman，且当前已在运行 |
| 操作系统 | Windows / macOS / Linux 桌面（三平台都出包；Linux 侧真机验证充分，Win / mac 尚未在真机上走过安装-运行-卸载） |
| Linux 图形栈 | 应用实际链接 `libgtk-4`、`libwebkitgtk-6.0`、`libsoup-3.0` 三族。deb/rpm 的依赖行目前只写了 `libwebkit2gtk-4.1-0` / `webkit2gtk4.1`（与上面那族不是同一个包，已登记待修），所以全新机器装完包仍可能缺运行时库——真缺了按「出问题时先看这几条」那行补装。AppImage 同样要求这三族已在本机 |
| 空间 | 每个 PHP 版本一份基座镜像 + 一份扩展固化镜像，再算离线缓存、备份与数据目录——镜像与缓存都可以在设置页指到别的盘 |
| 权限 | Linux 用 Docker 需在 `docker` 组；写 `/etc/hosts` 需要系统提权（不代装系统包，不代改引擎特权设置） |

## 下载什么包

Release 页每轮发这几颗，`checksums.txt` 是 SHA256 清单，每个包同名 `.sig` 是 Ed25519 签名，`manifest.json` 供应用内升级挑包：

| 平台 | 资产 | 怎么用 |
|---|---|---|
| Linux（Debian / Ubuntu） | `phpo_<版本>_amd64.deb` | `sudo apt install ./phpo_<版本>_amd64.deb`，升级直接装新包覆盖 |
| Linux（Fedora / RHEL / openSUSE） | `phpo-<版本>.x86_64.rpm` | `sudo rpm -Uvh phpo-<版本>.x86_64.rpm` |
| Linux（不想装包） | `phpo-x86_64.AppImage` | `chmod +x` 后直接跑，免 root，不碰包管理器 |
| Windows | `phpo-setup-x64.exe` | NSIS 安装器，双击安装 |
| macOS | `phpo.dmg` | 拖进「应用程序」，装完要去掉 quarantine（见下） |

> 安装包内的自报版本 ≡ Release 标签 ≡ 文件名里的版本，三者同源。想知道当前版本，看设置页底部或 [`docs/CHANGELOG.md`](./docs/CHANGELOG.md)。

## 安装

前置只有一条：**Docker 或 Podman 得先在跑**。引擎没起来时 phpo 会明确告诉你它连不上哪一个、怎么起来，不会画一张「已安装」的假脸。

### Linux：Docker 路线

```bash
# 1. 装 Docker（官方脚本）
curl -fsSL https://get.docker.com | sh

# 2. 把当前用户加进 docker 组
sudo usermod -aG docker $USER

# 3. 注销并重新登录（必须，否则组不生效）
#    验证：docker ps 不报错即可

# 4. 从 Releases 下对应包名的 deb / rpm / AppImage
#    https://github.com/xiaokentrl/phpo/releases/latest
```

`apt install docker.io` 装的 Docker 会建 `docker` 组但**不会**把你加进去。这种情形 phpo 现在会说「没有权限访问引擎」而不是骗你说「Docker 没运行」。

### Linux：Podman 路线

```bash
# 1. 装 Podman
sudo apt install podman        # Debian / Ubuntu
# 或 sudo dnf install podman   # Fedora

# 2. 启用用户级 socket（Podman 看的是这个，不是「socket 文件在不在」）
systemctl --user enable --now podman.socket

# 3. 指给 phpo（也可以写进桌面会话的环境变量）
export DOCKER_HOST=unix:///run/user/$(id -u)/podman/podman.sock

# 4. 从 Releases 下载包并按上面的方式装
```

rootless Podman 绑不上 1024 以下端口，所以默认站点端口是 8080。真要 80，给引擎放开特权口：`sudo sysctl -w net.ipv4.ip_unprivileged_port_start=80`（写进 `/etc/sysctl.d/` 下次开机仍有效）。

### Windows

1. 先装好并启动 Docker Desktop（或 Podman）。
2. 下载 `phpo-setup-x64.exe`，双击安装。
3. 装完从开始菜单启动。

### macOS

```bash
# 1. 先确保 Docker Desktop（或 colima / Podman machine）已在运行

# 2. 从 Releases 下载 phpo.dmg，拖入「应用程序」

# 3. 去掉 quarantine（必须：当前是 ad-hoc 签名，未做 Developer ID 与公证）
sudo xattr -dr com.apple.quarantine /Applications/phpo.app

# 4. 双击启动
```

当前 CI 在 `macos-14`（Apple Silicon）runner 上出这颗 dmg，Intel Mac 上尚未验证。

### Flatpak（FlatPark 渠道，由项目朋友维护）

`io.github.xiaokentrl.phpo` 这一颗由项目朋友在 FlatPark 侧收录分发，phpo 仓库自身不出 Flatpak 包；**下面的命令本轮未在本机验真**，用完有问题请回仓库开 issue 说明。想在沙箱里跑就走这条路。

沙箱里两条硬限制先说清：`$XDG_RUNTIME_DIR` 是私有的，不放行就看不见宿主的引擎 socket；沙箱内没有 `pkexec`，`/etc/hosts` 要手动编辑。

```bash
# 仓库
flatpak --user remote-add --if-not-exists flatpark https://dl.flatpark.org/flatpark.flatpakrepo
flatpak --user remote-add --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo

# 安装
flatpak install flatpark io.github.xiaokentrl.phpo

# 放行引擎与数据目录
flatpak override --user --filesystem=/run/docker.sock io.github.xiaokentrl.phpo
flatpak override --user --filesystem=~/phpo io.github.xiaokentrl.phpo
flatpak override --user --filesystem=~/www io.github.xiaokentrl.phpo

# 启动
flatpak run io.github.xiaokentrl.phpo
```

Podman + Flatpak 还要多两步：放行宿主 socket 与一个转调宿主 `podman` 的 wrapper。

```bash
# /run/user/1000 里的 1000 换成你自己的 id -u
flatpak override --user --filesystem=/run/user/1000/podman/podman.sock io.github.xiaokentrl.phpo
flatpak override --user --env=DOCKER_HOST=unix:///run/user/1000/podman/podman.sock io.github.xiaokentrl.phpo

mkdir -p ~/.var/app/io.github.xiaokentrl.phpo/data/bin
cat > ~/.var/app/io.github.xiaokentrl.phpo/data/bin/podman << 'EOF'
#!/bin/bash
exec /usr/bin/flatpak-spawn --host podman "$@"
EOF
chmod +x ~/.var/app/io.github.xiaokentrl.phpo/data/bin/podman

flatpak override --user --filesystem=/tmp io.github.xiaokentrl.phpo
flatpak override --user --env="PATH=/var/home/$USER/.var/app/io.github.xiaokentrl.phpo/data/bin:/app/bin:/usr/bin" io.github.xiaokentrl.phpo
```

在设置页改过工作根 / 缓存根 / 备份根之后，要同步给沙箱放行新路径，否则 phpo 看得见路径但写不进去。

## 首次运行

1. 向导先定两个根：工作目录根（默认 `~/phpo`）与站点根（默认 `~/www`）。**点「确认并创建」之前不会在盘上建任何东西**——验证阶段只读。
2. 引擎没就绪时这里会拦住，按提示把 Docker / Podman 起起来再回来。
3. 十分钟上手：装 PHP 8.3 → 装 Nginx → 建站 `demo.test` → 开浏览器看到页面 → 需要 7.4 就再装一个、站点列表下拉切换。
4. 数据库服务默认密码 `123456`，可以留空；改密码或端口之后点「重建生效」。

## 出问题时先看这几条

| 现象 | 真实原因与办法 |
|---|---|
| 说引擎未就绪，可我明明装了 | phpo 用真实拨号判断。先确认 `docker ps` / `podman ps` 在你终端里能跑通；Docker 要重登使组生效；Podman 要 `systemctl --user enable --now podman.socket`。沙箱 / 特殊部署（colima、lima、podman machine、Windows named pipe、`docker context`）也都支持，必要时导出 `DOCKER_HOST` 指给 phpo |
| 建站/改端口说端口不行 | 界面会点名真因：被别人占着就说占着的进程或服务；引擎绑不上就说引擎绑不上，不会拿「被占」糊弄你；超出 1–65535 就告诉你范围。改已有站点端口时会自动顺延到首个可用端口，并说清顺延到哪、想换回去怎么办 |
| Linux 启动报 `error while loading shared libraries: libwebkitgtk-6.0.so.4` | 本机缺 GTK4 / WebKitGTK 6.0 / libsoup3 一族，不是包坏了。deb 侧补齐：`sudo apt install libgtk-4-1 libwebkitgtk-6.0-4 libsoup-3.0-0`；rpm 侧装对应的 gtk4 / webkitgtk 6.0 / libsoup3 运行库（包名随发行版略有不同） |
| 双击 app 提示无法验证开发者 | 当前 macOS 包是 ad-hoc 签名、未公证。按上面第 3 步去掉 quarantine |
| 应用内升级点了没装上（Linux） | Linux 的应用内升级目前走不通，请直接用包管理器装新包覆盖；升级失败会自动回滚到旧版，不会把你留在半坏状态 |
| 卸载后重装，数据还在 | 有意如此：卸载默认保留数据卷与 `~/phpo`。要真删干净，去清理面板逐项确认删卷，删卷是二次确认的 |

## 安全须知

- **`config.yaml` 明文存密码且文件权限归一为 0777**：这是设计代价，同机其他用户读得到那串密码。别在多人共用的机器上放真密码，数据库也只听本机。
- **无遥测**：除了拉包与查更新，不主动往外发任何东西。
- **签名可验**：每个包带 `.sig`。校验链是「包 → SHA256 → 对小写十六进制摘要字符串做 Ed25519 验签」，公钥嵌在应用里，所以升级装不上时会明确拒绝而不是装进去一颗来路不明的包。

## 从源码构建

工具链：Go 1.27、Wails v3.0.0-beta.23、Node 22 + pnpm 12；Linux 上还需要 GTK4 一族开发头（应用是 CGO + WebKit）：

```bash
sudo apt install libgtk-4-dev libwebkitgtk-6.0-dev libsoup-3.0-dev \
  libglib2.0-dev libx11-dev build-essential pkg-config
```

任务全在 `Taskfile.yml`，统一走 `wails3 task`：

```bash
wails3 task dev             # 开发模式热重载
wails3 task build           # 编译
wails3 task check           # 五项门禁 + windows / darwin 交叉编译自证
wails3 task test            # 全量单测
wails3 task package:linux   # 出 deb / rpm / AppImage（本机 Linux）
```

五项门禁脚本（CI 必过，红即阻断合并）：`scripts/check-i18n-keys.go`（八语键集 / 占位符 / 重复键 / 未解析条目行 / 转义形状）、`check-templates.go`（模板 golden 空 diff）、`check-docker-naming.go`、`check-cache-manifest.go`、`check-ext-catalog.go`（扩展目录与后端逐项相等）。

集成测试要真实引擎且默认 skip，只在显式授权时跑：

```bash
PHPO_LIVE=1 go test ./test/integration/...
```

前端语言包 8 侧各 1389 键，键名以 `zh-CN` 为冻结基准；`zh-TW` 由 `scripts/gen-zh-tw.mjs`（OpenCC `s2twp`）派生，不要手改。新增一门语言只加一个 locale 文件。

## 文档

- [`AGENTS.md`](./AGENTS.md)：最高技术总纲，冻结文档，改代码前必读。
- [`docs/`](./docs/)：32 篇中文规格与设计文档——架构、目录规范、领域模型、接口契约、事件流协议、界面规格、跨平台差异、五项策略（密码 / 版本 / 域名 / 路径 / 端口）、离线缓存、备份脱敏、打包发布、用户手册等。
- [`docs/CHANGELOG.md`](./docs/CHANGELOG.md)：逐轮改动与验真记录。
- [`README_EN.md`](./README_EN.md) 是本文件的英文对照；除它以外文档只有中文。
- 冲突以 `AGENTS.md` 为准。

## 已知限制（如实清单）

- deb/rpm 声明的 WebKit 依赖（`libwebkit2gtk-4.1-0` / `webkit2gtk4.1`）与二进制实际链接的 6.0 一族不是同一个包：包能装完，但全新机器可能仍缺运行时库。
- Windows / macOS 包尚未在对应真机上做过安装-运行-卸载走查；Linux 侧真机验证充分。
- macOS 只有 ad-hoc 签名，没有 Developer ID 与公证，首次打开要手动去 quarantine。
- Linux 的应用内升级走不通，需要手动装新包；检查更新与下载本身正常。
- Flatpak 包由项目朋友在 FlatPark 侧维护，phpo 仓库不构建这颗包，安装命令本轮未在本机验真。
- 真宿主 GUI 走查仍有欠账，部分界面结论来自构建产物与门禁背书。

## 许可

MIT，全文见 [`LICENSE`](./LICENSE)。

## 反馈

- 问题与新需求：[Issues](https://github.com/xiaokentrl/phpo/issues)（仓库内有 issue / PR 模板）。
- 报 Bug 请带上：应用版本、操作系统与发行版、容器引擎（Docker / Podman 及版本）、日志抽屉里失败那一单的最后几行。
