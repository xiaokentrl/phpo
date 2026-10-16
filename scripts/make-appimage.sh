#!/usr/bin/env bash
# make-appimage.sh —— 把 build/bin/phpo 打成一颗 Linux 单文件 AppImage
#
# 这是干什么的：产出 build/bin/phpo-x86_64.AppImage，用户下载后 chmod +x 双击即可跑，不用 apt/dnf 装包、
#   不用 root。deb/rpm 链路完全不经过本脚本。
#
# 图形库为什么留给宿主（这条是实测结论，不是偷懒）：phpo 的界面由 WebKitGTK 渲染，WebKit 把渲染、网络、
#   GPU 拆成三个子进程，子进程的可执行文件路径是发行版打包时**编译写死在 libwebkitgtk 里的绝对路径**
#   （本机实测：包内 lib 已生效，子进程仍从 /usr/lib/x86_64-linux-gnu/webkitgtk-6.0/ 起，
#   且 WEBKIT_EXEC_PATH 在 6.0 里根本不被读）。所以：
#   - 把宿主那份子进程 + 包内那份库混着跑 = 版本错配，界面必然白屏或直接崩；
#   - 唯一自洽的做法是图形栈（GTK4 + WebKitGTK + 它的整棵依赖树）整体交给宿主，两边同一版本。
#   代价：跑这颗包要求本机装有 libgtk-4-1、libwebkitgtk-6.0-4、libsoup-3.0-0（与 deb 声明的依赖同一批）。
#
# 错的时候用户看得见什么：
#   - 工具下不下来或哈希不对 → 直接报「sha256 不匹配」并退出，不会出一颗来路不明的包；
#   - 宿主缺图形栈 → 脚本一开始就点名缺哪颗库并停止出包；
#   - 依赖闭包里出现了「既没捆、又没理由留给宿主」的那条 → 核对步逐字点名 soname 与宿主路径，构建失败。
#
# 怎么回来：删掉 build/bin/phpo-x86_64.AppImage 重跑即可；build/.tools 是钉死哈希的工具缓存，删了会重下。
#
# 用法：
#   bash scripts/make-appimage.sh                       # 读 build/bin/phpo，写 build/bin/phpo-x86_64.AppImage
#   BIN_DIR=build/bin OUT=/tmp/t.AppImage bash scripts/make-appimage.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

BINARY="${BINARY:-phpo}"
BIN_DIR="${BIN_DIR:-build/bin}"
SRC_BIN="$BIN_DIR/$BINARY"
OUT="${OUT:-$BIN_DIR/${BINARY}-x86_64.AppImage}"
TOOLS_DIR="${TOOLS_DIR:-build/.tools}"
mkdir -p "$TOOLS_DIR"
# 工具目录一律转成绝对路径：解包 appimagetool 时要 cd 进去执行它，相对路径在那个子 shell 里会失效
TOOLS_DIR="$(cd "$TOOLS_DIR" && pwd)"

# 打包工具钉死版本与 sha256：只从上游 GitHub Releases 取，哈希不符即拒绝使用。
# 哈希取自 https://api.github.com/repos/AppImage/appimagetool/releases 的 tag 1.9.1（assets 的 digest 字段）。
APPIMGATOOL_VERSION="${APPIMGATOOL_VERSION:-1.9.1}"
APPIMGATOOL_ARCH="${APPIMGATOOL_ARCH:-x86_64}"
APPIMGATOOL_SHA256="${APPIMGATOOL_SHA256:-ed4ce84f0d9caff66f50bcca6ff6f35aae54ce8135408b3fa33abfc3cb384eb0}"
APPIMGATOOL_URL="https://github.com/AppImage/appimagetool/releases/download/${APPIMGATOOL_VERSION}/appimagetool-${APPIMGATOOL_ARCH}.AppImage"

# type2 运行时（挂在 AppImage 最前面、负责 mount 与注入 APPDIR 的那小段 ELF）：
# appimagetool 自带不含它，默认现从 GitHub 拉——那等于让构建结果取决于当天的 continuous 版本。
# 因此这里同样钉死版本与哈希，并用 --runtime-file 显式喂进去，让 out 出来的包只认这两份钉死的输入。
# 哈希取自 https://api.github.com/repos/AppImage/type2-runtime/releases 的 tag 20251108。
RUNTIME_VERSION="${RUNTIME_VERSION:-20251108}"
RUNTIME_SHA256="${RUNTIME_SHA256:-2fca8b443c92510f1483a883f60061ad09b46b978b2631c807cd873a47ec260d}"
RUNTIME_URL="https://github.com/AppImage/type2-runtime/releases/download/${RUNTIME_VERSION}/runtime-${APPIMGATOOL_ARCH}"
RUNTIME_FILE="$TOOLS_DIR/runtime-${APPIMGATOOL_ARCH}-${RUNTIME_VERSION}"

[ -f "$SRC_BIN" ] || { echo "找不到 $SRC_BIN（先跑 wails3 task build）" >&2; exit 1; }
[ -d build/linux/icons/256x256 ] || { echo "缺 build/linux/icons/256x256/phpo.png（AppImage 的图标来源）" >&2; exit 1; }
case "$(uname -m)" in
  x86_64) : ;;
  *) echo "本脚本只出 x86_64 的 AppImage，当前架构 $(uname -m)；要出别的架构得另钉一份 appimagetool 的 sha256" >&2; exit 1 ;;
esac
command -v unsquashfs >/dev/null 2>&1 || {
  echo "缺 squashfs-tools（apt install squashfs-tools）：AppImage 就是把 AppDir 压成 squashfs 再挂起来" >&2; exit 1; }

# ---------- 1. 取钉死哈希的上游文件（已缓存则复验，未缓存则下载后验） ----------
fetch_pinned() { # $1=URL $2=期望 sha256 $3=落点 $4=给人看的名字
  local url="$1" want="$2" dest="$3" name="$4" part got
  part="$dest.part"
  if [ -f "$dest" ]; then
    echo "→ 复用缓存的 $name"
    got="$(sha256sum "$dest" | awk '{print $1}')"
  else
    echo "→ 下载 $name"
    curl -fL --retry 5 --retry-delay 5 --connect-timeout 60 --max-time 1800 -o "$part" "$url" || {
      rm -f "$part"
      echo "$name 下载失败：$url —— 离线环境请先把该文件放到 $dest 再重跑" >&2
      exit 1
    }
    got="$(sha256sum "$part" | awk '{print $1}')"
  fi
  if [ "$got" != "$want" ]; then
    rm -f "$part"
    echo "$name sha256 不匹配：期望 $want，实得 $got —— 拒绝使用" >&2
    exit 1
  fi
  # 显式 if 而不是 "[ -f x ] && mv"：后者在 set -e 下会让函数带着非零状态退出脚本
  if [ -f "$part" ]; then mv -f "$part" "$dest"; fi
}

AT_APP="$TOOLS_DIR/appimagetool-${APPIMGATOOL_VERSION}-${APPIMGATOOL_ARCH}.AppImage"
AT_EXTRACT="$TOOLS_DIR/appimagetool-${APPIMGATOOL_VERSION}-${APPIMGATOOL_ARCH}-extracted"
fetch_pinned "$APPIMGATOOL_URL" "$APPIMGATOOL_SHA256" "$AT_APP" "appimagetool $APPIMGATOOL_VERSION"
chmod +x "$AT_APP"
fetch_pinned "$RUNTIME_URL" "$RUNTIME_SHA256" "$RUNTIME_FILE" "type2 运行时 $RUNTIME_VERSION"
if [ ! -x "$AT_EXTRACT/AppRun" ]; then
  rm -rf "$AT_EXTRACT"
  # 用 --appimage-extract 摊平目录，不挂 FUSE（容器/无 /dev/fuse 的机器同样能构建）
  ( cd "$TOOLS_DIR" && "$AT_APP" --appimage-extract >/dev/null )
  mv -f "$TOOLS_DIR/squashfs-root" "$AT_EXTRACT"
fi
APPIMAGETOOL="$AT_EXTRACT/AppRun"

# ---------- 2. 铺 AppDir ----------
WORK="$(mktemp -d "$REPO_ROOT/build/.appimage-work.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT INT TERM
APPDIR="$WORK/AppDir"
LIBDIR="$APPDIR/usr/lib"
mkdir -p "$APPDIR/usr/bin" "$LIBDIR"

cp -f "$SRC_BIN" "$APPDIR/usr/bin/$BINARY"
chmod 0755 "$APPDIR/usr/bin/$BINARY"

# 桌面入口：deb 那份 Exec=phpo 靠宿主 PATH 找可执行文件，AppImage 里没有这个 PATH，
# 所以只把 Exec 换成 AppRun，其余字段（Name/Comment/Icon/Categories）逐字沿用。
sed -e 's|^Exec=phpo|Exec=AppRun|' build/linux/phpo.desktop >"$APPDIR/$BINARY.desktop"
cp -f build/linux/icons/256x256/phpo.png "$APPDIR/$BINARY.png"
ln -sf "$BINARY.png" "$APPDIR/.DirIcon"

cat >"$APPDIR/AppRun" <<'APPRUN'
#!/bin/sh
# AppImage 入口：挂载点每次都不一样（/tmp/.mount_xxxx），所以路径只能在跑的时候算，不能构建期写死。
# 包内 usr/lib 只放 phpo 自己的非图形依赖；GTK4 / WebKitGTK 一律走宿主（原因见 scripts/make-appimage.sh 开头）。
set -e
cd "$(dirname "$0")"
export APPDIR="${APPDIR:-$PWD}"
if [ -d "$APPDIR/usr/lib" ] && [ -n "$(ls -A "$APPDIR/usr/lib" 2>/dev/null)" ]; then
  export LD_LIBRARY_PATH="$APPDIR/usr/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
fi
exec "$APPDIR/usr/bin/phpo" "$@"
APPRUN
chmod 0755 "$APPDIR/AppRun"

# ---------- 3. 捆依赖闭包 ----------
# 刻意留给宿主的分两类：
#   ① 系统层的：glibc/ld（与内核绑定）、GL/EGL/GBM/DRM（跟显卡驱动走）、X11/xcb/wayland（跟显示服务与
#      桌面会话走）、nss/selinux/systemd/udev（跟宿主账号与设备管理走）——捆了会因版本错配崩；
#   ② 图形栈整棵树的：GTK4 + WebKitGTK + JavaScriptCore + libsoup 及其全部下游——因为 WebKit 的子进程
#      只能从宿主写死的路径起（见文件头），子进程与库必须同一版本。
HOST_KEEP='^(ld-linux|linux-vdso|libc\.so|libm\.so|libmvec\.so|libpthread\.so|libdl\.so|librt\.so|libresolv\.so|libnsl\.so|libutil\.so|libanl\.so|libgcc_s\.so|libstdc\+\+\.so|libGL\.so|libGLX\.so|libEGL\.so|libGLdispatch\.so|libglapi\.so|libgallium|libgbm\.so|libdrm\.so|libX|libxcb|libwayland|libnss_|libselinux|libsystemd|libudev|libapparmor)'
GUI_ROOTS_RE='^(libgtk-4|libwebkitgtk-|libjavascriptcoregtk-|libsoup-3)'

# 一条 soname→绝对路径：从已落盘的 ldd 输出里取，不再为同一颗二进制反复解析整条依赖链。
# 改前主程序被递归 ldd 了四遍（认图形栈的根、拷包、核对理由、查 not found），
# 每条 soname 又各起一对 `printf | grep` 子进程——本机实测 137 条 × 三遍，整脚本 user 时间 18.8s
# 而 wall 只有 2.8s，差的全是 fork 出来的碎进程。
ld_snapshot() { # $1=落点文件：递归 ldd 的整份输出（含 stderr，not found 也在里面）
  LD_LIBRARY_PATH="$LIBDIR" ldd "$SRC_BIN" >"$1" 2>&1
}

ld_pairs() { # $1=快照文件
  sed -n 's|^[[:space:]]*\([^[:space:]]\+\)[[:space:]]*=>[[:space:]]*\(/[^[:space:]]\+\).*|\1 \2|p' "$1"
}

# 先拿到主程序的完整闭包（一趟，落盘复用），再从这里认出图形栈的根，逐根展开它们自己的闭包
# 合成「留给宿主」的黑名单。
# 按闭包减而不是按名字前缀减：图形栈下游有 cairo/pango/harfbuzz/gstreamer 一百多颗，点名表维护不住，
# 而且一旦漏一颗就会把包内库和宿主库混成两套版本。
LD_PRE="$WORK/ldd-pre-copy.txt"
ld_snapshot "$LD_PRE"
GUI_SONAMES=()
declare -A GUI_PATH=()
while read -r soname path; do
  [[ $soname =~ $GUI_ROOTS_RE ]] || continue
  GUI_SONAMES+=("$soname")
  GUI_PATH["$soname"]="$path"
done < <(ld_pairs "$LD_PRE")
[ "${#GUI_SONAMES[@]}" -gt 0 ] || {
  echo "$SRC_BIN 的闭包里没有 libgtk-4 / libwebkitgtk-6.0：宿主缺图形栈（apt install libgtk-4-1 libwebkitgtk-6.0-4）" >&2
  exit 1
}
declare -A GUI_EXCLUDE=()
for soname in "${GUI_SONAMES[@]}"; do
  GUI_EXCLUDE["$soname"]=1
  while read -r dsoname dpath; do
    [ -n "${dsoname:-}" ] || continue
    GUI_EXCLUDE["$dsoname"]=1
  done < <(LD_LIBRARY_PATH= ldd "${GUI_PATH[$soname]}" 2>/dev/null | sed -n 's|^[[:space:]]*\([^[:space:]]\+\)[[:space:]]*=>[[:space:]]*\(/[^[:space:]]\+\).*|\1 \2|p')
done

copy_one() { # $1=soname $2=宿主绝对路径
  local soname="$1" path="$2"
  case "$soname" in
    *.so | *.so.*) : ;;
    *) return 0 ;;
  esac
  [ -n "${GUI_EXCLUDE[$soname]:-}" ] && return 0
  [[ $soname =~ $HOST_KEEP ]] && return 0
  [ -e "$LIBDIR/$soname" ] && return 0
  # -L：把符号链接摊成实体文件，包里不留指向宿主的断链
  cp -L "$path" "$LIBDIR/$soname"
  chmod 0644 "$LIBDIR/$soname"
}

echo "→ 捆依赖闭包（图形栈 ${#GUI_EXCLUDE[@]} 颗留给宿主）"
while read -r soname path; do
  [ -n "${soname:-}" ] || continue
  [ -n "${path:-}" ] || continue
  copy_one "$soname" "$path"
done < <(ld_pairs "$LD_PRE")

# 包内绝不允许出现图形栈的任何一颗——出现了就是上面那套减法失效，宿主子进程会与包内库版本错配。
for soname in "${!GUI_EXCLUDE[@]}"; do
  [ -e "$LIBDIR/$soname" ] || continue
  echo "  ✗ 包内出现了图形栈的 $soname：WebKit 子进程只能起宿主那一份，混版本必然白屏" >&2
  exit 1
done

# ---------- 4. 闭包核对：包外的每一条都要说得出理由 ----------
echo "→ 核对依赖闭包"
# 包已经灌好，这里必须重新解析一遍（上一份快照记的是拷包前的宿主路径，用它核对等于自己查自己）。
# 但只解析一遍：整份输出落盘，「说得出理由」和「有没有 not found」两条断言都从同一份快照里读。
LD_POST="$WORK/ldd-post-check.txt"
ld_snapshot "$LD_POST"
bad=0
while read -r soname path; do
  [ -n "${soname:-}" ] || continue
  case "$path" in "$LIBDIR"/*) continue ;; esac                       # 已在包内
  [[ $soname =~ $HOST_KEEP ]] && continue                             # ① 系统层，刻意留给宿主
  [ -n "${GUI_EXCLUDE[$soname]:-}" ] && continue                      # ② 图形栈整棵树，随宿主子进程同版本
  echo "  ✗ $soname 既没捆进包内，也说不出留给宿主的理由（宿主解析到 $path）" >&2
  bad=1
done < <(ld_pairs "$LD_POST")
missing="$(grep 'not found' "$LD_POST" || true)"
[ -z "$missing" ] || { echo "  ✗ $SRC_BIN 有解不开的依赖：$missing" >&2; bad=1; }
[ "$bad" = 0 ] || { echo "闭包核对未过，不出包。" >&2; exit 1; }

# ---------- 5. 压成 AppImage ----------
echo "→ 生成 $OUT"
rm -f "$OUT"
# -n：跳过 AppStream 元数据转换（那需要 appstreamcli，与能否运行无关）
# --runtime-file：不给它就会现从 GitHub 拉当天的 continuous 运行时，同一份源码两次出包会拿到不同的头
ARCH="$APPIMGATOOL_ARCH" "$APPIMAGETOOL" -n --runtime-file "$RUNTIME_FILE" \
  "$APPDIR" "$WORK/$BINARY-$APPIMGATOOL_ARCH.AppImage"
mv -f "$WORK/$BINARY-$APPIMGATOOL_ARCH.AppImage" "$OUT"
chmod 0755 "$OUT"

echo "✓ $OUT（$(du -h "$OUT" | cut -f1)，捆了 $(find "$LIBDIR" -maxdepth 1 -name '*.so*' | wc -l) 个库）"
