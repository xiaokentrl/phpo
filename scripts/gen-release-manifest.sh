#!/usr/bin/env bash
# gen-release-manifest.sh —— 发布清单 manifest.json（§5.9 升级检测的远端契约）
#
# 作用：扫描发布目录下的三平台产物，逐份算 SHA256 与体积、读同名 `.sig`，按 (os, format) 分列成
#       `assets`，生成应用内「检查更新」要拉的 JSON 清单。客户端（internal/updater/source.go）
#       全部并发探测（每源独立 15s 超时）、在成功的源里取版本序最新的一份，命中后按本机平台选一颗 asset 下载，
#       再做 SHA256 + Ed25519 双校验（硬红线 6）。
#
# 与验签口径逐字一致：signature = base64(Ed25519(小写 sha256Hex 字符串))，见 sign-release.sh 与 verifier.go。
# **未签名的产物不进清单**——没有 `.sig` 的包客户端必然验签失败，宁可不发布这版更新。
#
# 用法：
#   VERSION=0.1.38 scripts/gen-release-manifest.sh
#   VERSION=0.1.38 BIN_DIR=out ASSET_BASE=https://gitee.com/x/phpo/releases/download/v0.1.38 scripts/gen-release-manifest.sh
#
# 环境变量：
#   VERSION      必填，发布版本号（不含 v 前缀）
#   BIN_DIR      产物目录，默认 out
#   OUT          清单落点，默认 $BIN_DIR/manifest.json
#   ASSET_BASE   下载地址前缀（不含文件名）；缺省时按 GITHUB_REPOSITORY + GITHUB_REF_NAME 拼 GitHub Release 直链
#   CHANGELOG    更新说明（可选；单行文本，缺省留空）
#   DOWNLOAD_PAGE 发布页地址（可选；「打开下载页」按钮的目标，缺省留空即客户端不出这颗按钮）
set -euo pipefail

VERSION="${VERSION:?需要 VERSION（发布版本号，不含 v 前缀）}"
BIN_DIR="${BIN_DIR:-out}"
OUT="${OUT:-$BIN_DIR/manifest.json}"
CHANGELOG="${CHANGELOG:-}"
DOWNLOAD_PAGE="${DOWNLOAD_PAGE:-}"

[ -d "$BIN_DIR" ] || { echo "目录不存在: $BIN_DIR" >&2; exit 1; }

if [ -z "${ASSET_BASE:-}" ]; then
  repo="${GITHUB_REPOSITORY:?未设 ASSET_BASE 且不在 GitHub Actions 里（需 GITHUB_REPOSITORY 才能拼直链）}"
  tag="${GITHUB_REF_NAME:?未设 ASSET_BASE 且无 GITHUB_REF_NAME（发布标签，如 v0.1.38）}"
  ASSET_BASE="https://github.com/${repo}/releases/download/${tag}"
fi
ASSET_BASE="${ASSET_BASE%/}"

# JSON 字符串转义：反斜杠与引号先转，再把 CR/LF/TAB 压成空格（清单里不放多行文本，sed 逐行处理不了换行）
json_str() {
  printf '%s' "$1" | tr '\n\r\t' '   ' | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

size_of() {
  wc -c <"$1" | tr -d '[:space:]'
}

assets=()
seen=()
signed=0
# 遍历顺序固定为 LC_ALL=C：同平台重复时「先出现的一份」不能取决于 runner 的 locale
# （本机实测：默认 zh_CN.UTF-8 下 `phpo.exe` 排在 `phpo-setup-x64.exe` 之前，C 序相反）。
while IFS= read -r f; do
  base="$(basename "$f")"
  case "$base" in
    manifest.json|checksums.txt|*.sig) continue ;;
    *.exe)      os=windows; format=exe ;;
    *.dmg)      os=darwin;  format=dmg ;;
    *.AppImage) os=linux;   format=appimage ;;
    *.deb)      os=linux;   format=deb ;;
    *.rpm)      os=linux;   format=rpm ;;
    *) continue ;;
  esac

  # 下载地址直接拼进 JSON 字符串，文件名必须无需转义（只允许 URL 安全字符）
  printf '%s' "$base" | grep -Eq '^[A-Za-z0-9._-]+$' || {
    echo "产物文件名含 URL 不安全字符，跳过: $base" >&2; continue
  }

  key="${os}/${format}"

  sig_file="$f.sig"
  if [ ! -f "$sig_file" ]; then
    echo "! 无 $base.sig，该包不进清单（客户端必然验签失败）" >&2
    continue
  fi
  # .sig 由 `base64 -w0` 写出，末尾带换行；DecodingString 不接受换行，必须压成单行
  sig="$(tr -d '\r\n' <"$sig_file")"
  [ -n "$sig" ] || { echo "! $base.sig 为空，该包不进清单" >&2; continue; }

  # 同一 (os, format) 只留一份：客户端 resolvePlatform 按 assets 顺序取**第一颗**命中本机平台的
  # 条目，两份同平台资产等于让它赌顺序。判重放在签名校验**之后**——未签名的那一份本来就不进清单，
  # 若先登记 seen 会把同平台唯一那份已签名的包也挡掉。
  for s in "${seen[@]+"${seen[@]}"}"; do
    if [ "$s" = "$key" ]; then
      echo "! 同一平台已有一份签名包，本条跳过（重复: $key → $base）" >&2
      continue 2
    fi
  done
  seen+=("$key")

  url="${ASSET_BASE}/${base}"
  assets+=("{\"os\":\"$(json_str "$os")\",\"format\":\"$(json_str "$format")\",\"url\":\"$(json_str "$url")\",\"size\":$(size_of "$f"),\"sha256\":\"$(sha256_of "$f")\",\"signature\":\"$(json_str "$sig")\"}")
  signed=$((signed + 1))
done < <(find "$BIN_DIR" -maxdepth 1 -type f | LC_ALL=C sort)

if [ "$signed" -eq 0 ]; then
  echo "清单内无一个已签名产物：发布前先跑 scripts/sign-release.sh sign（硬红线 6），否则不生成 $OUT" >&2
  exit 1
fi

mkdir -p "$(dirname "$OUT")"
{
  printf '{\n  "version": "%s",\n' "$(json_str "$VERSION")"
  printf '  "changelog": "%s",\n' "$(json_str "$CHANGELOG")"
  printf '  "download_page": "%s",\n' "$(json_str "$DOWNLOAD_PAGE")"
  printf '  "assets": [\n'
  for i in "${!assets[@]}"; do
    if [ "$i" -lt $(( ${#assets[@]} - 1 )) ]; then printf '    %s,\n' "${assets[$i]}"; else printf '    %s\n' "${assets[$i]}"; fi
  done
  printf '  ]\n}\n'
} >"$OUT"

echo "✓ 已生成 $OUT（version=$VERSION，签名包 $signed 份）"
